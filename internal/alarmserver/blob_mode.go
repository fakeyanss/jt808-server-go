package alarmserver

import (
	"bytes"
	"context"
	"crypto/md5"
	"crypto/sha1"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"time"

	"github.com/rs/zerolog/log"

	"github.com/fakeyanss/jt808-server-go/internal/contracts"
	"github.com/fakeyanss/jt808-server-go/internal/storage"
)

const blobOpTimeout = 2 * time.Minute

// registerBlobFile 建立对象存储模式的文件状态。
//
// 恢复点以 control 为权威：先向 control 查询该文件的进度；control 不可达时
// 不阻断接收，退回本地暂存标记 + 对象存储 key 扫描重建进度。
func (a *Assembler) registerBlobFile(info AttachmentInfo, date, dir string, entry AttachmentEntry, fileType uint8) (*fileState, error) {
	store := a.cfg.Storage
	if store == nil || store.Blob == nil {
		return nil, ErrObjectStoreUnavailable
	}
	st := newFileState(a, info, date, dir, entry, fileType)
	st.store = store
	st.partPath = blobStagingPath(store.StagingDir, info, entry.FileName)
	st.finalPath = ""
	st.createdAt = a.nowTime()

	// 本地暂存标记是本节点自己的事实：保存已落暂存但尚未成对象的覆盖区间。
	if persisted, err := readPersistedState(statePath(st.partPath)); err == nil && persisted.Size == st.size {
		st.ranges = normalizeRanges(persisted.Ranges, st.size)
		if st.fileType == 0 && persisted.FileType != 0 {
			st.fileType = persisted.FileType
		}
		if persisted.LastActivity > 0 {
			st.lastActivity = time.Unix(0, persisted.LastActivity)
		}
	}

	ctx, cancel := context.WithTimeout(context.Background(), blobOpTimeout)
	defer cancel()

	key := contracts.AttachmentProgressKey{
		Source: store.Source, Phone: info.Phone, AlarmNo: info.AlarmNo, FileName: entry.FileName,
	}
	restored := false
	if progress, err := a.loadProgress(ctx, key); err != nil {
		log.Warn().Err(err).Str("phone", info.Phone).Str("file", entry.FileName).
			Msg("Attachment progress is unavailable from control; falling back to object storage scan")
	} else if progress != nil {
		restored = st.applyProgress(progress)
	}
	if !restored {
		if segments, completed, err := a.scanCommittedSegments(ctx, key, st.size); err != nil {
			log.Warn().Err(err).Str("phone", info.Phone).Str("file", entry.FileName).
				Msg("Fail to rebuild attachment progress from object keys")
		} else if len(segments) > 0 {
			st.applyScannedSegments(segments, completed)
			restored = true
		}
	}
	if restored {
		if st.completed {
			st.meta = st.buildMeta()
			return st, nil
		}
		if err := os.MkdirAll(filepath.Dir(st.partPath), 0o750); err != nil {
			return nil, fmt.Errorf("create attachment staging dir: %w", err)
		}
		st.markActive()
		reserve := st.size - st.flushCursor
		if reserve < 0 {
			reserve = 0
		}
		if !a.reserveStaging(reserve) {
			st.markInactive()
			return nil, ErrStagingCapacity
		}
		// 恢复后的进度同时回填 control：扫描重建的记录不影响下一次恢复。
		st.reportLocked(contracts.AttachmentStatusReceiving)
		return st, nil
	}

	if max := a.cfg.MaxConcurrent; max > 0 && a.ActiveReceiving() >= max {
		// 新文件在登记阶段拒绝，状态接口显示的 available 才与实际准入一致；
		// 已有进度的重连不受此限制。
		return nil, ErrTooManySessions
	}
	if !a.reserveStaging(st.size) {
		return nil, ErrStagingCapacity
	}
	st.markActive()
	if err := os.MkdirAll(filepath.Dir(st.partPath), 0o750); err != nil {
		a.releaseStaging(st.size)
		st.markInactive()
		return nil, fmt.Errorf("create attachment staging dir: %w", err)
	}
	st.mu.Lock()
	_ = st.persistBlobMarkerLocked()
	st.mu.Unlock()
	st.reportLocked(contracts.AttachmentStatusReceiving)
	log.Info().Str("phone", info.Phone).Str("file", entry.FileName).Int64("size", st.size).
		Str("staging", st.partPath).Msg("Attachment file registered for object storage")
	return st, nil
}

// loadProgress 向 control 查询恢复点；未配置上报通道时视为没有记录。
func (a *Assembler) loadProgress(ctx context.Context, key contracts.AttachmentProgressKey) (*contracts.AttachmentProgress, error) {
	store := a.cfg.Storage
	if store == nil || store.Progress == nil {
		return nil, nil
	}
	progress, err := store.Progress.Load(ctx, key)
	if err != nil {
		if err == contracts.ErrAttachmentProgressNotFound {
			return nil, nil
		}
		return nil, err
	}
	return progress, nil
}

// applyProgress 采纳 control 返回的恢复点；总长不一致时不采纳，按新文件重收。
func (st *fileState) applyProgress(progress *contracts.AttachmentProgress) bool {
	if progress == nil {
		return false
	}
	if progress.TotalSize > 0 && progress.TotalSize != st.size {
		log.Warn().Str("phone", st.phone).Str("file", st.fileName).
			Int64("progressSize", progress.TotalSize).Int64("reportSize", st.size).
			Msg("Ignore attachment progress with mismatched total size")
		return false
	}
	if st.fileType == 0 && progress.FileType != 0 {
		st.fileType = progress.FileType
	}
	if !progress.CreatedAt.IsZero() {
		st.createdAt = progress.CreatedAt
	}
	st.segments = toSegments(progress.Segments)
	st.ranges = mergeSegmentRanges(st.ranges, st.segments, st.size)
	st.covered = coveredBytes(st.ranges)
	st.flushCursor = contiguousPrefix(st.segments)
	if progress.Status == contracts.AttachmentStatusCompleted || st.flushCursor >= st.size && st.size > 0 {
		st.completed = true
		st.completedAt = progress.CompletedAt
		if st.completedAt.IsZero() {
			st.completedAt = st.owner.nowTime()
		}
	}
	return true
}

// applyScannedSegments 采纳对象存储扫描结果。
func (st *fileState) applyScannedSegments(segments []contracts.AttachmentProgressSegment, completed bool) {
	st.segments = segments
	st.ranges = mergeSegmentRanges(st.ranges, st.segments, st.size)
	st.covered = coveredBytes(st.ranges)
	st.flushCursor = contiguousPrefix(st.segments)
	if completed {
		st.completed = true
		st.completedAt = st.owner.nowTime()
	}
}

// scanCommittedSegments 按对象 key 重建单个文件的切片列表。
//
// 只依赖 key 内的终端、报警号、文件名、偏移、大小与总长，不依赖任何上报内容。
func (a *Assembler) scanCommittedSegments(ctx context.Context, key contracts.AttachmentProgressKey, size int64) ([]contracts.AttachmentProgressSegment, bool, error) {
	store := a.cfg.Storage
	if store == nil || store.Blob == nil {
		return nil, false, nil
	}
	lister, ok := store.Blob.(contracts.BlobLister)
	if !ok {
		return nil, false, nil
	}
	prefix := fmt.Sprintf("%s%s/%s/%s/",
		storage.AttachmentPrefix(key.Source), storage.SafeSegment(key.Phone),
		storage.SafeSegment(key.AlarmNo), storage.SafeSegment(key.FileName))
	objects, err := lister.ListObjects(ctx, prefix)
	if err != nil {
		return nil, false, fmt.Errorf("list attachment segments: %w", err)
	}
	segments := make([]contracts.AttachmentProgressSegment, 0, len(objects))
	for _, object := range objects {
		info, parseErr := storage.ParseAttachmentSegmentKey(object.Key)
		if parseErr != nil {
			log.Warn().Err(parseErr).Str("key", object.Key).Msg("Skip attachment object with invalid key metadata")
			continue
		}
		if info.Source != key.Source || info.Phone != storage.SafeSegment(key.Phone) ||
			info.AlarmNo != storage.SafeSegment(key.AlarmNo) || info.FileName != storage.SafeSegment(key.FileName) {
			continue
		}
		if info.TotalSize != size {
			continue
		}
		segments = append(segments, contracts.AttachmentProgressSegment{
			Index:  info.Index,
			Key:    info.ObjectKey,
			Offset: info.Offset,
			Size:   info.Size,
			State:  string(contracts.BlobStatePending),
		})
	}
	sort.Slice(segments, func(i, j int) bool { return segments[i].Index < segments[j].Index })
	completed := size > 0 && contiguousPrefix(segments) >= size
	return segments, completed, nil
}

// reserveStaging 预留暂存额度；超过上限时返回 false，由调用方拒绝登记。
func (a *Assembler) reserveStaging(size int64) bool {
	if size <= 0 {
		return true
	}
	limit := a.cfg.Storage.MaxStagingBytes
	for {
		current := a.staging.Load()
		if limit > 0 && current+size > limit {
			return false
		}
		if a.staging.CompareAndSwap(current, current+size) {
			return true
		}
	}
}

func (a *Assembler) releaseStaging(size int64) {
	if size <= 0 {
		return
	}
	for {
		current := a.staging.Load()
		next := current - size
		if next < 0 {
			next = 0
		}
		if a.staging.CompareAndSwap(current, next) {
			return
		}
	}
}

// blobWriteLocked 把数据写入本地暂存，并在切片窗口补齐时立即上传。
func (st *fileState) blobWriteLocked(offset int64, data []byte) error {
	if st.removed {
		return ErrFileNotRegistered
	}
	if st.completed {
		return nil
	}
	if len(data) == 0 || len(data) > FramePayloadLimit {
		return ErrInvalidChunkLength
	}
	if offset < 0 || offset+int64(len(data)) > st.size {
		return ErrOffsetOutOfRange
	}
	if err := st.ensureOpenLocked(); err != nil {
		return err
	}
	if _, err := st.file.WriteAt(data, offset); err != nil {
		return err
	}
	st.markRangeLocked(offset, int64(len(data)))
	st.lastActivity = st.owner.nowTime()
	return st.flushReadyLocked()
}

// flushReadyLocked 按顺序提交已经完全覆盖的切片窗口。
//
// 只有当前窗口（flushCursor 起的一个 SegmentSize）在本地到齐才上传；
// 窗口外的乱序数据留在暂存文件里，等窗口补齐后再一起提交。
func (st *fileState) flushReadyLocked() error {
	if st.store == nil || st.completed || st.size <= 0 {
		return nil
	}
	ctx, cancel := context.WithTimeout(context.Background(), blobOpTimeout)
	defer cancel()
	for st.flushCursor < st.size {
		start := st.flushCursor
		end := start + st.store.SegmentSize
		if end > st.size {
			end = st.size
		}
		if !rangeCovered(st.ranges, start, end) {
			break
		}
		if err := st.ensureOpenLocked(); err != nil {
			return err
		}
		payload := make([]byte, end-start)
		if _, err := st.file.ReadAt(payload, start); err != nil && err != io.EOF {
			return fmt.Errorf("read attachment staging: %w", err)
		}
		sum := md5.Sum(payload)
		index := len(st.segments)
		key := storage.AttachmentSegmentObjectKey(
			st.store.Source, st.phone, st.alarmNo, st.fileName, start, end-start, st.size, index)
		info, err := st.store.Blob.Put(ctx, key, bytes.NewReader(payload), contracts.BlobOptions{
			ContentType: "application/octet-stream",
			MD5:         hex.EncodeToString(sum[:]),
			Metadata: map[string]string{
				"phone": st.phone, "fileName": st.fileName, "alarmNo": st.alarmNo,
			},
		})
		if err != nil {
			return fmt.Errorf("put attachment segment %s: %w", key, err)
		}
		if _, err := st.store.Blob.Promote(ctx, info); err != nil {
			return fmt.Errorf("promote attachment segment %s: %w", key, err)
		}
		st.segments = append(st.segments, contracts.AttachmentProgressSegment{
			Index: index, Key: key, Offset: start, Size: end - start,
			MD5: hex.EncodeToString(sum[:]), State: string(contracts.BlobStatePending),
		})
		st.flushCursor = end
		st.owner.releaseStaging(end - start)
		st.reportLocked(contracts.AttachmentStatusReceiving)
		log.Debug().Str("phone", st.phone).Str("file", st.fileName).Str("key", key).
			Int64("offset", start).Int64("size", end-start).Msg("Attachment segment staged to object store")
	}
	if st.flushCursor >= st.size {
		return st.finalizeBlobLocked(ctx)
	}
	return nil
}

// finalizeBlobLocked 在所有切片提交后把对象标记为已提交，并上报完成。
func (st *fileState) finalizeBlobLocked(ctx context.Context) error {
	for i := range st.segments {
		if err := st.store.Blob.MarkCommitted(ctx, st.segments[i].Key); err != nil {
			return fmt.Errorf("mark attachment segment committed: %w", err)
		}
		st.segments[i].State = string(contracts.BlobStateCommitted)
	}
	st.closeFileLocked()
	_ = os.Remove(st.partPath)
	_ = os.Remove(statePath(st.partPath))
	st.completed = true
	st.completedAt = st.owner.nowTime()
	st.markInactive()
	st.meta = st.buildMeta()
	st.reportLocked(contracts.AttachmentStatusCompleted)
	st.owner.completed.Add(1)
	log.Info().Str("phone", st.phone).Str("file", st.fileName).Int64("size", st.size).
		Int("segments", len(st.segments)).Msg("Attachment file completed in object store")
	return nil
}

// reportLocked 把当前进度压入异步上报队列。队列满时丢弃，control 可扫描补齐。
func (st *fileState) reportLocked(status string) {
	if st.store == nil {
		return
	}
	st.owner.enqueueProgress(st.snapshotLocked(status))
}

// snapshotLocked 生成一份不共享内部切片的进度快照。
func (st *fileState) snapshotLocked(status string) contracts.AttachmentProgress {
	ranges, _ := json.Marshal(st.ranges)
	progress := contracts.AttachmentProgress{
		Source:        st.store.Source,
		Phone:         st.phone,
		TerminalID:    st.terminalID,
		AlarmSign:     hex.EncodeToString(st.alarmSign),
		AlarmNo:       st.alarmNo,
		FileName:      st.fileName,
		FileType:      st.fileType,
		TotalSize:     st.size,
		ReceivedBytes: coveredBytes(st.ranges),
		Watermark:     st.flushCursor,
		StagingPath:   st.partPath,
		StagingBase:   st.store.StagingDir,
		Ranges:        ranges,
		Segments:      append([]contracts.AttachmentProgressSegment(nil), st.segments...),
		Status:        status,
		CreatedAt:     st.createdAt,
		UpdatedAt:     st.owner.nowTime(),
	}
	if status == contracts.AttachmentStatusCompleted {
		progress.CompletedAt = st.completedAt
	}
	if status != contracts.AttachmentStatusReceiving {
		// 暂存路径只对上报节点有效，终态后不再让 control 持有该引用。
		progress.StagingPath = ""
	}
	return progress
}

func (st *fileState) buildMeta() Metadata {
	return Metadata{
		Phone:              st.phone,
		TerminalID:         st.terminalID,
		AlarmNo:            st.alarmNo,
		AlarmSignHex:       hex.EncodeToString(st.alarmSign),
		FileName:           st.fileName,
		FileType:           st.fileType,
		Size:               st.size,
		CompletedAt:        st.completedAt,
		FileNameConformant: validateAttachmentFileNameFormat(st.fileName) == nil,
		AlarmSignValid:     validateAlarmSign(st.alarmSign) == nil,
	}
}

// failBlobLocked 标记失败并清理本地暂存；已提交的切片保留，供排查与断点续传。
func (st *fileState) failBlobLocked() {
	if st.store == nil {
		return
	}
	st.closeFileLocked()
	_ = os.Remove(st.partPath)
	_ = os.Remove(statePath(st.partPath))
	st.markInactive()
	remaining := st.size - st.flushCursor
	if remaining > 0 {
		st.owner.releaseStaging(remaining)
	}
	st.reportLocked(contracts.AttachmentStatusExpired)
}

func (st *fileState) persistBlobMarkerLocked() error {
	return writePersistedState(statePath(st.partPath), persistedState{
		FileName:     st.fileName,
		FileType:     st.fileType,
		Size:         st.size,
		Phone:        st.phone,
		TerminalID:   st.terminalID,
		AlarmNo:      st.alarmNo,
		AlarmSign:    st.alarmSign,
		Date:         st.date,
		LastActivity: st.lastActivity.UnixNano(),
		Ranges:       append([]byteRange(nil), st.ranges...),
	})
}

// rangeCovered 判断 [start,end) 是否完整落在某个已覆盖区间内。
func rangeCovered(ranges []byteRange, start, end int64) bool {
	for _, item := range ranges {
		if item.Offset <= start && item.end() >= end {
			return true
		}
	}
	return false
}

func decodeRanges(raw []byte) []byteRange {
	if len(raw) == 0 {
		return nil
	}
	var ranges []byteRange
	if json.Unmarshal(raw, &ranges) != nil {
		return nil
	}
	return ranges
}

// mergeSegmentRanges 把已提交切片视为已覆盖区间，再与本地暂存区间合并。
//
// 重启后本地暂存可能丢失，但对象切片仍在；把它们并入覆盖集合，
// 0x9212 才不会要求终端重传已经上传过的字节。
func mergeSegmentRanges(ranges []byteRange, segments []contracts.AttachmentProgressSegment, size int64) []byteRange {
	merged := append([]byteRange(nil), ranges...)
	for _, segment := range segments {
		if segment.Size <= 0 {
			continue
		}
		merged = append(merged, byteRange{Offset: segment.Offset, Length: segment.Size})
	}
	return normalizeRanges(merged, size)
}

// contiguousPrefix 返回按序切片覆盖到的连续字节前缀。
func contiguousPrefix(segments []contracts.AttachmentProgressSegment) int64 {
	if len(segments) == 0 {
		return 0
	}
	sorted := append([]contracts.AttachmentProgressSegment(nil), segments...)
	sort.Slice(sorted, func(i, j int) bool { return sorted[i].Offset < sorted[j].Offset })
	var cursor int64
	for _, segment := range sorted {
		if segment.Offset > cursor {
			break
		}
		if end := segment.Offset + segment.Size; end > cursor {
			cursor = end
		}
	}
	return cursor
}

func toSegments(items []contracts.AttachmentProgressSegment) []contracts.AttachmentProgressSegment {
	if len(items) == 0 {
		return nil
	}
	return append([]contracts.AttachmentProgressSegment(nil), items...)
}

func blobStagingPath(root string, info AttachmentInfo, fileName string) string {
	// 暂存文件名保留可读部分并附加原始文件名的短哈希，避免不同文件名
	// 经过安全字符替换后落到同一个暂存路径。
	sum := sha1.Sum([]byte(fileName))
	return filepath.Join(root, sanitizeLabel(info.Phone), sanitizeLabel(alarmDate(info.AlarmSign, info.Date)),
		sanitizeLabel(info.AlarmNo), sanitizeLabel(fileName)+"-"+hex.EncodeToString(sum[:4])+partSuffix)
}

func (st *fileState) markActive() {
	if st.counted {
		return
	}
	st.counted = true
	st.owner.receiving.Add(1)
}

func (st *fileState) markInactive() {
	if !st.counted {
		return
	}
	st.counted = false
	st.owner.receiving.Add(-1)
}

// ActiveReceiving 返回本进程当前接收中的附件数量。
func (a *Assembler) ActiveReceiving() int {
	value := a.receiving.Load()
	if value < 0 {
		return 0
	}
	return int(value)
}

// reconcileBlob 清理本节点暂存区的崩溃残留。
//
// 附件节点不连库，无法按业务行判断归属；只按 .part 标记与修改时间回收，
// 宽限期内的残留保留，终端重连后可继续使用同一暂存文件。
func (a *Assembler) reconcileBlob() error {
	store := a.cfg.Storage
	if store == nil {
		return nil
	}
	if err := os.MkdirAll(store.StagingDir, 0o750); err != nil {
		return fmt.Errorf("create attachment staging root: %w", err)
	}
	grace := a.cfg.SessionTimeout
	if grace <= 0 {
		grace = DefaultSessionTimeout
	}
	return a.removeOrphanStaging(a.nowTime().Add(-grace))
}

func (a *Assembler) removeOrphanStaging(cutoff time.Time) error {
	root := a.cfg.Storage.StagingDir
	active := a.ActivePaths()
	return filepath.WalkDir(root, func(path string, entry os.DirEntry, walkErr error) error {
		if walkErr != nil {
			return nil
		}
		if entry.IsDir() || !strings.HasSuffix(entry.Name(), partSuffix) {
			return nil
		}
		if _, ok := active[path]; ok {
			return nil
		}
		info, statErr := entry.Info()
		if statErr != nil || info.ModTime().After(cutoff) {
			return nil
		}
		log.Warn().Str("file", path).Msg("Remove orphan attachment staging file")
		_ = os.Remove(path)
		_ = os.Remove(statePath(path))
		return nil
	})
}
