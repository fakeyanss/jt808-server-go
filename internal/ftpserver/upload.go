package ftpserver

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"sort"
	"strconv"
	"sync"
	"sync/atomic"
	"time"

	"github.com/rs/zerolog/log"

	"github.com/fakeyanss/jt808-server-go/internal/contracts"
	"github.com/fakeyanss/jt808-server-go/internal/storage"
)

var (
	// ErrUploadCancelled 表示上传已被 0x9207 取消。
	ErrUploadCancelled = errors.New("ftp upload cancelled")
	// ErrFileTooLarge 表示超过任务允许的单文件上限。
	ErrFileTooLarge = errors.New("ftp upload exceeds max file size")
	// ErrStagingCapacity 表示本地暂存区已满。
	ErrStagingCapacity = errors.New("ftp staging capacity exceeded")
)

// upload 表示一次 STOR/APPE 的暂存与切片状态。
//
// 它只做顺序写：FTP 是流式协议，没有随机偏移语义；REST/APPE 只是选择
// 从哪个连续前缀继续，偏移对不上时从头覆盖。切片对象是事实来源，
// 因此重新列出同前缀对象就能确定已提交到哪。
type upload struct {
	srv         *Server
	task        LoginResult
	fileName    string
	staging     string
	file        *os.File
	segmentSize int64

	total    int64
	flushed  int64
	segments []contracts.AttachmentProgressSegment

	staged atomic.Int64

	mu          sync.Mutex
	paused      atomic.Bool
	cancelled   atomic.Bool
	pauseSignal chan struct{}
	cancelCh    chan struct{}
	closed      bool
}

func (s *Server) newUpload(task LoginResult, fileName string) *upload {
	dir := filepath.Join(s.cfg.Dir, "ftp1078", storage.SafeSegment(task.Phone), strconv.Itoa(int(task.Serial)))
	return &upload{
		srv:         s,
		task:        task,
		fileName:    fileName,
		staging:     filepath.Join(dir, storage.SafeSegment(fileName)+partSuffix),
		segmentSize: s.segmentSize(),
		pauseSignal: make(chan struct{}),
		cancelCh:    make(chan struct{}),
	}
}

// open 打开暂存并按模式重建已提交前缀。
//
// overwrite=true 是 STOR 默认语义：同名重传整文件覆盖；
// 否则按 REST/APPE 的期望偏移续传，偏移与现有暂存不一致时退回从头覆盖。
func (u *upload) open(ctx context.Context, wantOffset int64, overwrite bool) error {
	if err := os.MkdirAll(filepath.Dir(u.staging), 0o750); err != nil {
		return fmt.Errorf("create ftp staging dir: %w", err)
	}
	if overwrite && wantOffset <= 0 {
		if err := u.resetObjects(ctx); err != nil {
			return err
		}
	}
	file, err := os.OpenFile(u.staging, os.O_CREATE|os.O_RDWR, 0o644)
	if err != nil {
		return fmt.Errorf("open ftp staging: %w", err)
	}
	u.file = file
	info, err := file.Stat()
	if err != nil {
		_ = file.Close()
		return err
	}
	u.total = info.Size()

	if err := u.rebuild(ctx); err != nil {
		_ = file.Close()
		return err
	}
	if overwrite && wantOffset <= 0 {
		u.total = 0
		u.flushed = 0
		u.segments = nil
		if err := file.Truncate(0); err != nil {
			_ = file.Close()
			return err
		}
		return nil
	}
	// REST/APPE：偏移必须与已落暂存一致，否则从头覆盖。
	if wantOffset > 0 && wantOffset != u.total {
		log.Warn().Str("phone", u.task.Phone).Uint16("serial", u.task.Serial).
			Int64("want", wantOffset).Int64("staging", u.total).
			Msg("FTP resume offset does not match staging; overwrite from zero")
		if err := u.removeAllObjects(ctx); err != nil {
			_ = file.Close()
			return err
		}
		u.total = 0
		u.flushed = 0
		u.segments = nil
		if err := file.Truncate(0); err != nil {
			_ = file.Close()
			return err
		}
	}
	if u.total < u.flushed {
		// 暂存丢失但对象仍在：无法补出缺口，整文件重传。
		if err := u.removeAllObjects(ctx); err != nil {
			_ = file.Close()
			return err
		}
		u.total = 0
		u.flushed = 0
		u.segments = nil
		if err := file.Truncate(0); err != nil {
			_ = file.Close()
			return err
		}
	}
	u.adjustStaged(u.total)
	return nil
}

// adjustStaged 把暂存占用同步到实际文件长度；同一进程内重连只补差额。
func (u *upload) adjustStaged(total int64) {
	previous := u.staged.Load()
	switch {
	case total > previous:
		if !u.srv.reserveStaging(total - previous) {
			log.Warn().Str("phone", u.task.Phone).Uint16("serial", u.task.Serial).
				Int64("want", total).Msg("FTP staging capacity exceeded while resuming")
		}
	case total < previous:
		u.srv.releaseStaging(previous - total)
	}
	u.staged.Store(total)
}

// rebuild 列举同文件前缀，保留“从 0 开始的完整切片窗口”，删除其它残片。
func (u *upload) rebuild(ctx context.Context) error {
	lister, ok := u.srv.cfg.Blob.(contracts.BlobLister)
	if !ok {
		return nil
	}
	prefix := u.prefix()
	objects, err := lister.ListObjects(ctx, prefix)
	if err != nil {
		return fmt.Errorf("list ftp objects: %w", err)
	}
	byOffset := make(map[int64]bool, len(objects))
	var extras []string
	for _, object := range objects {
		info, parseErr := storage.ParseAttachmentSegmentKey(object.Key)
		if parseErr != nil {
			extras = append(extras, object.Key)
			continue
		}
		if info.Source != storage.SourceFTP1078 || info.Phone != storage.SafeSegment(u.task.Phone) ||
			info.AlarmNo != strconv.Itoa(int(u.task.Serial)) ||
			info.FileName != storage.SafeSegment(u.fileName) {
			continue
		}
		if info.Size != u.segmentSize || info.Offset%u.segmentSize != 0 {
			extras = append(extras, object.Key)
			continue
		}
		byOffset[info.Offset] = true
	}
	var segments []contracts.AttachmentProgressSegment
	for offset := int64(0); ; offset += u.segmentSize {
		if !byOffset[offset] {
			break
		}
		segments = append(segments, contracts.AttachmentProgressSegment{
			Index: int(offset / u.segmentSize), Key: u.segmentKey(offset, u.segmentSize),
			Offset: offset, Size: u.segmentSize, State: string(contracts.BlobStateCommitted),
		})
		delete(byOffset, offset)
	}
	for offset := range byOffset {
		extras = append(extras, u.segmentKey(offset, u.segmentSize))
	}
	for _, key := range extras {
		if err := u.srv.cfg.Blob.Delete(ctx, key); err != nil {
			log.Warn().Err(err).Str("key", key).Msg("Fail to delete non-contiguous ftp object")
		}
	}
	u.segments = segments
	u.flushed = int64(len(segments)) * u.segmentSize
	// 续传会改写尾部切片，旧清单不再代表当前事实，先删掉。
	if err := u.srv.cfg.Blob.Delete(ctx, u.doneKey()); err != nil {
		log.Warn().Err(err).Str("key", u.doneKey()).Msg("Fail to delete stale ftp manifest")
	}
	return nil
}

func (u *upload) removeAllObjects(ctx context.Context) error {
	lister, ok := u.srv.cfg.Blob.(contracts.BlobLister)
	if !ok {
		return nil
	}
	objects, err := lister.ListObjects(ctx, u.prefix())
	if err != nil {
		return fmt.Errorf("list ftp objects: %w", err)
	}
	for _, object := range objects {
		if err := u.srv.cfg.Blob.Delete(ctx, object.Key); err != nil {
			return fmt.Errorf("delete ftp object %s: %w", object.Key, err)
		}
	}
	// 清单在文件目录之外，前缀列举覆盖不到，必须显式删除。
	if err := u.srv.cfg.Blob.Delete(ctx, u.doneKey()); err != nil {
		return fmt.Errorf("delete ftp manifest: %w", err)
	}
	return nil
}

func (u *upload) resetObjects(ctx context.Context) error {
	u.segments = nil
	u.flushed = 0
	return u.removeAllObjects(ctx)
}

func (u *upload) prefix() string {
	return storage.FTP1078Attachments + storage.SafeSegment(u.task.Phone) + "/" +
		strconv.Itoa(int(u.task.Serial)) + "/" + storage.SafeSegment(u.fileName) + "/"
}

func (u *upload) doneKey() string {
	return storage.AttachmentDoneObjectKey(storage.SourceFTP1078, u.task.Phone,
		strconv.Itoa(int(u.task.Serial)), u.fileName)
}

func (u *upload) segmentKey(offset, size int64) string {
	return storage.AttachmentFTPSegmentObjectKey(
		u.task.Phone, strconv.Itoa(int(u.task.Serial)), u.fileName, offset, size, int(offset/u.segmentSize))
}

// Write 顺序写入暂存，并在完整窗口到齐时立即提交对象。
func (u *upload) Write(ctx context.Context, data []byte) error {
	if len(data) == 0 {
		return nil
	}
	if u.cancelled.Load() {
		return ErrUploadCancelled
	}
	if err := u.waitIfPaused(); err != nil {
		return err
	}
	if max := u.task.MaxBytes; max > 0 && u.total+int64(len(data)) > max {
		return ErrFileTooLarge
	}
	if !u.srv.reserveStaging(int64(len(data))) {
		return ErrStagingCapacity
	}
	u.staged.Add(int64(len(data)))
	if _, err := u.file.WriteAt(data, u.total); err != nil {
		u.srv.releaseStaging(int64(len(data)))
		u.staged.Add(-int64(len(data)))
		return err
	}
	u.total += int64(len(data))
	for u.total-u.flushed >= u.segmentSize {
		if err := u.flushSegment(ctx, u.flushed, u.segmentSize); err != nil {
			return err
		}
	}
	return nil
}

func (u *upload) flushSegment(ctx context.Context, offset, size int64) error {
	payload := make([]byte, size)
	if _, err := u.file.ReadAt(payload, offset); err != nil && err != io.EOF {
		return fmt.Errorf("read ftp staging: %w", err)
	}
	key := u.segmentKey(offset, size)
	info, err := u.srv.cfg.Blob.Put(ctx, key, bytes.NewReader(payload), contracts.BlobOptions{
		ContentType: "application/octet-stream",
		Metadata: map[string]string{
			"phone": u.task.Phone, "serial": strconv.Itoa(int(u.task.Serial)),
			"fileName": u.fileName,
		},
	})
	if err != nil {
		return fmt.Errorf("put ftp segment %s: %w", key, err)
	}
	if _, err := u.srv.cfg.Blob.Promote(ctx, info); err != nil {
		return fmt.Errorf("promote ftp segment %s: %w", key, err)
	}
	u.segments = append(u.segments, contracts.AttachmentProgressSegment{
		Index: int(offset / u.segmentSize), Key: key, Offset: offset, Size: size,
		State: string(contracts.BlobStatePending),
	})
	u.flushed = offset + size
	u.report(contracts.AttachmentStatusReceiving, 0)
	log.Debug().Str("phone", u.task.Phone).Uint16("serial", u.task.Serial).
		Str("key", key).Int64("offset", offset).Msg("FTP segment staged to object store")
	return nil
}

// finish 提交尾部切片、写完成清单并发完成进度。
func (u *upload) finish(ctx context.Context) error {
	if u.total > u.flushed {
		if err := u.flushSegment(ctx, u.flushed, u.total-u.flushed); err != nil {
			return err
		}
	}
	for i := range u.segments {
		if err := u.srv.cfg.Blob.MarkCommitted(ctx, u.segments[i].Key); err != nil {
			return fmt.Errorf("mark ftp segment committed: %w", err)
		}
		u.segments[i].State = string(contracts.BlobStateCommitted)
	}
	if err := u.writeManifest(ctx); err != nil {
		return err
	}
	u.report(contracts.AttachmentStatusCompleted, u.total)
	u.cleanupLocal()
	log.Info().Str("phone", u.task.Phone).Uint16("serial", u.task.Serial).
		Str("file", u.fileName).Int64("size", u.total).Int("segments", len(u.segments)).
		Msg("FTP upload completed")
	return nil
}

// writeManifest 只在所有切片 Promote 完成后写 .done。
//
// 清单是“完成”的唯一显式标记；扫描时它存在但切片不连续按异常处理。
func (u *upload) writeManifest(ctx context.Context) error {
	manifest := storage.AttachmentDoneManifest{
		FileName:    u.fileName,
		TotalSize:   u.total,
		SegmentSize: u.segmentSize,
		Segments:    len(u.segments),
		CompletedAt: u.srv.now().UnixMilli(),
	}
	payload, err := json.Marshal(manifest)
	if err != nil {
		return err
	}
	key := u.doneKey()
	info, err := u.srv.cfg.Blob.Put(ctx, key, bytes.NewReader(payload),
		contracts.BlobOptions{ContentType: "application/json"})
	if err != nil {
		return fmt.Errorf("put ftp manifest %s: %w", key, err)
	}
	if _, err := u.srv.cfg.Blob.Promote(ctx, info); err != nil {
		return fmt.Errorf("promote ftp manifest %s: %w", key, err)
	}
	if err := u.srv.cfg.Blob.MarkCommitted(ctx, key); err != nil {
		return fmt.Errorf("mark ftp manifest committed: %w", err)
	}
	return nil
}

// interrupt 在连接中断/暂停时保留暂存与已提交切片，上报当前水位。
func (u *upload) interrupt() {
	u.report(contracts.AttachmentStatusReceiving, u.total)
	u.closeLocal()
}

// abort 在取消时丢弃暂存与对象，上报 failed。
func (u *upload) abort(ctx context.Context) {
	u.mu.Lock()
	if u.closed {
		u.mu.Unlock()
		return
	}
	u.closed = true
	u.mu.Unlock()
	if u.file != nil {
		_ = u.file.Close()
	}
	_ = os.Remove(u.staging)
	if err := u.removeAllObjects(ctx); err != nil {
		log.Warn().Err(err).Msg("Fail to delete cancelled ftp objects")
	}
	u.releaseStaged()
	u.report(contracts.AttachmentStatusFailed, u.total)
}

func (u *upload) cleanupLocal() {
	u.closeLocal()
	_ = os.Remove(u.staging)
	u.releaseStaged()
}

func (u *upload) closeLocal() {
	u.mu.Lock()
	defer u.mu.Unlock()
	if u.closed {
		return
	}
	u.closed = true
	if u.file != nil {
		if err := u.file.Close(); err != nil {
			log.Warn().Err(err).Str("staging", u.staging).Msg("Fail to close ftp staging")
		}
		u.file = nil
	}
}

func (u *upload) releaseStaged() {
	if size := u.staged.Swap(0); size > 0 {
		u.srv.releaseStaging(size)
	}
}

func (u *upload) report(status string, totalSize int64) {
	progress := contracts.AttachmentProgress{
		Source:        storage.SourceFTP1078,
		Phone:         u.task.Phone,
		AlarmNo:       strconv.Itoa(int(u.task.Serial)),
		AlarmSerial:   strconv.Itoa(int(u.task.Serial)),
		FileName:      u.fileName,
		TotalSize:     totalSize,
		ReceivedBytes: u.total,
		Watermark:     u.flushed,
		StagingPath:   u.staging,
		StagingBase:   u.srv.cfg.Dir,
		Segments:      append([]contracts.AttachmentProgressSegment(nil), u.segments...),
		Status:        status,
		UpdatedAt:     u.srv.now(),
	}
	if status != contracts.AttachmentStatusReceiving {
		progress.StagingPath = ""
		progress.StagingBase = ""
	}
	u.srv.enqueueProgress(progress)
}

func (u *upload) pause() {
	if u.paused.Swap(true) {
		return
	}
	u.mu.Lock()
	u.pauseSignal = make(chan struct{})
	u.mu.Unlock()
}

func (u *upload) resume() {
	if !u.paused.Swap(false) {
		return
	}
	u.mu.Lock()
	close(u.pauseSignal)
	u.mu.Unlock()
}

func (u *upload) cancel() {
	if u.cancelled.Swap(true) {
		return
	}
	close(u.cancelCh)
}

func (u *upload) waitIfPaused() error {
	for {
		if u.cancelled.Load() {
			return ErrUploadCancelled
		}
		if !u.paused.Load() {
			return nil
		}
		u.mu.Lock()
		signal := u.pauseSignal
		u.mu.Unlock()
		select {
		case <-signal:
		case <-u.cancelCh:
			return ErrUploadCancelled
		case <-time.After(200 * time.Millisecond):
		}
	}
}

// segmentOffsets 返回已提交切片的偏移集合，供测试与排查。
func (u *upload) segmentOffsets() []int64 {
	offsets := make([]int64, 0, len(u.segments))
	for _, segment := range u.segments {
		offsets = append(offsets, segment.Offset)
	}
	sort.Slice(offsets, func(i, j int) bool { return offsets[i] < offsets[j] })
	return offsets
}
