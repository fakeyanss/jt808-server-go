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
	"time"

	"github.com/rs/zerolog/log"

	"github.com/fakeyanss/jt808-server-go/internal/contracts"
	"github.com/fakeyanss/jt808-server-go/internal/repository"
	"github.com/fakeyanss/jt808-server-go/internal/storage"
)

const blobOpTimeout = 2 * time.Minute

// registerBlobFile 建立对象存储模式的文件状态。
//
// 检查顺序：共享库已有完成记录 → 直接复用；已有接收记录 → 恢复水位与段列表；
// 新文件 → 检查暂存区总量后落一条 receiving 记录。
func (a *Assembler) registerBlobFile(info AttachmentInfo, date, dir string, entry AttachmentEntry, fileType uint8) (*fileState, error) {
	store := a.cfg.Storage
	if store == nil || store.Blob == nil || store.Files == nil {
		return nil, ErrObjectStoreUnavailable
	}
	st := newFileState(a, info, date, dir, entry, fileType)
	st.store = store
	st.partPath = blobStagingPath(store.StagingDir, info, entry.FileName)
	st.finalPath = ""
	st.createdAt = a.nowTime()

	ctx, cancel := context.WithTimeout(context.Background(), blobOpTimeout)
	defer cancel()

	key := repository.AttachmentFileKey{
		Source: store.Source, Phone: info.Phone, AlarmNo: info.AlarmNo, FileName: entry.FileName,
	}
	existing, err := store.Files.Get(ctx, key)
	switch {
	case err == nil:
		if existing.Status == repository.AttachmentStatusCompleted {
			st.completed = true
			st.meta = metadataFromRecord(existing)
			st.completedAt = existing.CompletedAt
			return st, nil
		}
		st.createdAt = existing.CreatedAt
		st.segments = append([]repository.AttachmentSegment(nil), existing.Segments...)
		st.flushCursor = existing.Watermark
		st.ranges = normalizeRanges(decodeRanges(existing.Ranges), st.size)
		st.covered = coveredBytes(st.ranges)
		if len(st.segments) == 0 && st.flushCursor > 0 {
			// 段列表缺失时以水位为唯一可信来源，避免重复上传同一前缀。
			st.flushCursor = 0
		}
		if len(st.ranges) == 0 && len(st.segments) > 0 {
			// ranges 列缺失时用已提交段还原覆盖区间，断线重连不必重传整段。
			ranges := make([]byteRange, 0, len(st.segments))
			for _, segment := range st.segments {
				ranges = append(ranges, byteRange{Offset: segment.Offset, Length: segment.Size})
			}
			st.ranges = normalizeRanges(ranges, st.size)
			st.covered = coveredBytes(st.ranges)
		}
		st.markActive()
		reserve := st.size - st.flushCursor
		if reserve < 0 {
			reserve = 0
		}
		if !a.reserveStaging(reserve) {
			return nil, ErrStagingCapacity
		}
		return st, nil
	case !isNotFound(err):
		return nil, err
	}

	if max := a.cfg.MaxConcurrent; max > 0 && a.ActiveReceiving() >= max {
		// 新文件在登记阶段拒绝，状态接口显示的 available 才与实际准入一致；
		// 已有接收记录的重连不受此限制。
		return nil, ErrTooManySessions
	}
	if !a.reserveStaging(st.size) {
		return nil, ErrStagingCapacity
	}
	st.markActive()
	if err := os.MkdirAll(filepath.Dir(st.partPath), 0o750); err != nil {
		a.releaseStaging(st.size)
		return nil, fmt.Errorf("create attachment staging dir: %w", err)
	}
	record := &repository.AttachmentFile{
		Source:      store.Source,
		Phone:       info.Phone,
		TerminalID:  info.TerminalID,
		AlarmSign:   hex.EncodeToString(info.AlarmSign),
		AlarmNo:     info.AlarmNo,
		FileName:    entry.FileName,
		FileType:    fileType,
		TotalSize:   st.size,
		StagingPath: st.partPath,
		StagingBase: store.StagingDir,
		Status:      repository.AttachmentStatusReceiving,
		CreatedAt:   st.createdAt,
	}
	if _, err := store.Files.Save(ctx, record); err != nil {
		a.releaseStaging(st.size)
		return nil, err
	}
	st.mu.Lock()
	_ = st.persistBlobMarkerLocked()
	st.mu.Unlock()
	log.Info().Str("phone", info.Phone).Str("file", entry.FileName).Int64("size", st.size).
		Str("staging", st.partPath).Msg("Attachment file registered for object storage")
	return st, nil
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
		key := storage.AttachmentSegment(st.store.Source, st.phone, st.alarmNo, st.fileName, len(st.segments))
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
		st.segments = append(st.segments, repository.AttachmentSegment{
			Index: len(st.segments), Key: key, Offset: start, Size: end - start,
			MD5: hex.EncodeToString(sum[:]), State: "pending",
		})
		st.flushCursor = end
		st.owner.releaseStaging(end - start)
		if err := st.saveBlobRecordLocked(ctx, repository.AttachmentStatusReceiving); err != nil {
			return err
		}
		log.Debug().Str("phone", st.phone).Str("file", st.fileName).Str("key", key).
			Int64("offset", start).Int64("size", end-start).Msg("Attachment segment staged to object store")
	}
	if st.flushCursor >= st.size {
		return st.finalizeBlobLocked(ctx)
	}
	return nil
}

// finalizeBlobLocked 在所有切片提交后把文件标记为完成，并清理本地暂存。
func (st *fileState) finalizeBlobLocked(ctx context.Context) error {
	for i := range st.segments {
		if err := st.store.Blob.MarkCommitted(ctx, st.segments[i].Key); err != nil {
			return fmt.Errorf("mark attachment segment committed: %w", err)
		}
		st.segments[i].State = "committed"
	}
	if err := st.saveBlobRecordLocked(ctx, repository.AttachmentStatusCompleted); err != nil {
		return err
	}
	st.closeFileLocked()
	_ = os.Remove(st.partPath)
	_ = os.Remove(statePath(st.partPath))
	st.completed = true
	st.completedAt = st.owner.nowTime()
	st.markInactive()
	st.meta = Metadata{
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
	st.owner.completed.Add(1)
	log.Info().Str("phone", st.phone).Str("file", st.fileName).Int64("size", st.size).
		Int("segments", len(st.segments)).Msg("Attachment file completed in object store")
	return nil
}

func (st *fileState) saveBlobRecordLocked(ctx context.Context, status string) error {
	if st.store == nil || st.store.Files == nil {
		return ErrObjectStoreUnavailable
	}
	ranges, _ := json.Marshal(st.ranges)
	record := &repository.AttachmentFile{
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
		Segments:      append([]repository.AttachmentSegment(nil), st.segments...),
		Status:        status,
		CreatedAt:     st.createdAt,
	}
	if status == repository.AttachmentStatusCompleted {
		record.CompletedAt = st.owner.nowTime()
		record.StagingPath = ""
	}
	_, err := st.store.Files.Save(ctx, record)
	return err
}

// failBlobLocked 标记失败并清理本地暂存；已提交的切片保留，供排查与断点续传。
func (st *fileState) failBlobLocked() {
	if st.store == nil || st.store.Files == nil {
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
	ctx, cancel := context.WithTimeout(context.Background(), blobOpTimeout)
	defer cancel()
	if err := st.saveBlobRecordLocked(ctx, repository.AttachmentStatusExpired); err != nil {
		log.Warn().Err(err).Str("phone", st.phone).Str("file", st.fileName).
			Msg("Fail to mark attachment file expired")
	}
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

func blobStagingPath(root string, info AttachmentInfo, fileName string) string {
	// 暂存文件名保留可读部分并附加原始文件名的短哈希，避免不同文件名
	// 经过安全字符替换后落到同一个暂存路径。
	sum := sha1.Sum([]byte(fileName))
	return filepath.Join(root, sanitizeLabel(info.Phone), sanitizeLabel(alarmDate(info.AlarmSign, info.Date)),
		sanitizeLabel(info.AlarmNo), sanitizeLabel(fileName)+"-"+hex.EncodeToString(sum[:4])+partSuffix)
}

func isNotFound(err error) bool {
	return err == repository.ErrAttachmentFileNotFound
}

func metadataFromRecord(record *repository.AttachmentFile) Metadata {
	return Metadata{
		Phone:              record.Phone,
		TerminalID:         record.TerminalID,
		AlarmNo:            record.AlarmNo,
		AlarmSignHex:       record.AlarmSign,
		FileName:           record.FileName,
		FileType:           record.FileType,
		Size:               record.TotalSize,
		CompletedAt:        record.CompletedAt,
		FileNameConformant: validateAttachmentFileNameFormat(record.FileName) == nil,
		AlarmSignValid:     record.AlarmSign != "",
	}
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

// selectBlob 从共享库读取已完成附件。
func (a *Assembler) selectBlob(phone string) []Metadata {
	store := a.cfg.Storage
	if store == nil || store.Files == nil {
		return nil
	}
	ctx, cancel := context.WithTimeout(context.Background(), blobOpTimeout)
	defer cancel()
	var records []*repository.AttachmentFile
	var err error
	if phone == "" {
		records, err = store.Files.ListCompletedSource(ctx, store.Source, 1000)
	} else {
		records, err = store.Files.ListByPhone(ctx, store.Source, phone, 1000)
	}
	if err != nil {
		log.Warn().Err(err).Msg("Fail to list attachment metadata from shared store")
		return nil
	}
	result := make([]Metadata, 0, len(records))
	for _, record := range records {
		if record.Status != repository.AttachmentStatusCompleted {
			continue
		}
		result = append(result, metadataFromRecord(record))
	}
	return result
}

// findBlobMetadata 从共享库读取指定文件的完成元数据。
func (a *Assembler) findBlobMetadata(phone, fileName string) (Metadata, error) {
	store := a.cfg.Storage
	if store == nil || store.Files == nil {
		return Metadata{}, ErrObjectStoreUnavailable
	}
	ctx, cancel := context.WithTimeout(context.Background(), blobOpTimeout)
	defer cancel()
	record, err := store.Files.FindByFileName(ctx, store.Source, phone, fileName)
	if err != nil {
		return Metadata{}, ErrFileNotRegistered
	}
	if record.Status != repository.AttachmentStatusCompleted {
		return Metadata{}, ErrFileNotRegistered
	}
	return metadataFromRecord(record), nil
}

// expireReceivingBlob 把长时间无进展的 receiving 记录转为 expired。
//
// 终端半路消失时不会再有进度更新；这些记录此前会一直停在 receiving，
// pending 回收因为“有业务行”而跳过，对象段与本地暂存长期占用。
// 转为 expired 后由 pruneBlobRetention 的终态路径统一回收。
// 只处理共享库中确实超时的行；本进程活跃文件的 updated_at 会随进度刷新。
func (a *Assembler) expireReceivingBlob(now time.Time, timeout time.Duration) int {
	store := a.cfg.Storage
	if store == nil || store.Files == nil {
		return 0
	}
	if timeout <= 0 {
		timeout = DefaultSessionTimeout
	}
	ctx, cancel := context.WithTimeout(context.Background(), blobOpTimeout)
	defer cancel()
	rows, err := store.Files.ListReceivingUpdatedBefore(ctx, now.Add(-timeout), 500)
	if err != nil {
		log.Warn().Err(err).Msg("Fail to list stale attachment receiving records")
		return 0
	}
	removed := 0
	for _, row := range rows {
		if row.StagingPath != "" {
			_ = os.Remove(row.StagingPath)
			_ = os.Remove(statePath(row.StagingPath))
		}
		row.Status = repository.AttachmentStatusExpired
		row.StagingPath = ""
		if _, saveErr := store.Files.Save(ctx, row); saveErr != nil {
			log.Warn().Err(saveErr).Str("phone", row.Phone).Str("file", row.FileName).
				Msg("Fail to mark stale attachment record expired")
			continue
		}
		removed++
	}
	return removed
}

// reconcileBlob 清理崩溃残留。
//
// 规则：
//   - 共享库里超过宽限期仍未完成的记录 → 标记 failed 并删除暂存文件；
//   - 暂存目录里没有对应接收记录的 .part/.state → 超过宽限期直接删除。
//
// 宽限期内的记录保留，终端重连后按同一业务键恢复水位继续接收。
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
	cutoff := a.nowTime().Add(-grace)
	ctx, cancel := context.WithTimeout(context.Background(), blobOpTimeout)
	defer cancel()

	rows, err := store.Files.ListReceivingUpdatedBefore(ctx, cutoff, 1000)
	if err != nil {
		return err
	}
	for _, row := range rows {
		if row.StagingPath != "" {
			_ = os.Remove(row.StagingPath)
			_ = os.Remove(statePath(row.StagingPath))
		}
		row.Status = repository.AttachmentStatusFailed
		row.StagingPath = ""
		if _, saveErr := store.Files.Save(ctx, row); saveErr != nil {
			log.Warn().Err(saveErr).Str("phone", row.Phone).Str("file", row.FileName).
				Msg("Fail to mark stale attachment record failed")
		}
	}
	return a.removeOrphanStaging(cutoff)
}

func (a *Assembler) removeOrphanStaging(cutoff time.Time) error {
	root := a.cfg.Storage.StagingDir
	referenced := map[string]bool{}
	// 宽限期内的接收记录是有效暂存，不删除。
	ctx, cancel := context.WithTimeout(context.Background(), blobOpTimeout)
	defer cancel()
	rows, err := a.cfg.Storage.Files.ListReceivingUpdatedBefore(ctx, a.nowTime(), 10000)
	if err == nil {
		for _, row := range rows {
			if row.StagingPath != "" {
				referenced[row.StagingPath] = true
			}
		}
	}
	return filepath.WalkDir(root, func(path string, entry os.DirEntry, walkErr error) error {
		if walkErr != nil {
			return nil
		}
		if entry.IsDir() || !(len(entry.Name()) > len(partSuffix) && entry.Name()[len(entry.Name())-len(partSuffix):] == partSuffix) {
			return nil
		}
		if referenced[path] {
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

// pruneBlobPending 只回收附件来源前缀下、超过宽限期且不再被元数据引用的 pending 对象。
//
// 与媒体侧共用 `_pending/` 根前缀，因此必须按 attachment/ 前缀扫描，
// 并回查 attachment_files 的段列表；否则媒体侧的全量扫描会误删附件。
func (a *Assembler) pruneBlobPending() {
	store := a.cfg.Storage
	if store == nil || store.Files == nil || store.Blob == nil {
		return
	}
	sweeper, ok := store.Blob.(interface {
		SweepPendingPrefix(context.Context, string, time.Duration, func(string) bool)
	})
	if !ok {
		return
	}
	ctx, cancel := context.WithTimeout(context.Background(), blobOpTimeout)
	defer cancel()
	cutoff := time.Hour
	if a.cfg.SessionTimeout > cutoff {
		cutoff = a.cfg.SessionTimeout
	}
	sweeper.SweepPendingPrefix(ctx, storage.AttachmentPrefix(store.Source), cutoff, func(key string) bool {
		referenced, err := store.Files.ReferencedSegment(ctx, store.Source, key)
		if err != nil {
			// 查询失败时保守保留对象，宁可多留一轮。
			return true
		}
		return referenced
	})
}

// pruneBlobRetention 只清理附件来源前缀与 attachment_files 表。
func (a *Assembler) pruneBlobRetention(now time.Time) int {
	store := a.cfg.Storage
	if store == nil || store.Files == nil || store.Blob == nil || a.cfg.Retention <= 0 {
		return 0
	}
	ctx, cancel := context.WithTimeout(context.Background(), blobOpTimeout)
	defer cancel()
	removed := 0
	// 半成品（failed/expired）也按保留期回收其已提交切片，避免残留 pending 对象。
	for {
		rows, err := store.Files.ListTerminalBefore(ctx, store.Source, now.Add(-a.cfg.Retention), 100)
		if err != nil {
			log.Warn().Err(err).Msg("Fail to list stale attachment records")
			break
		}
		if len(rows) == 0 {
			break
		}
		for _, row := range rows {
			a.deleteAttachmentRecord(ctx, row)
			removed++
		}
		if len(rows) < 100 {
			break
		}
	}
	for {
		rows, err := store.Files.ListCompletedBefore(ctx, store.Source, now.Add(-a.cfg.Retention), 100)
		if err != nil {
			log.Warn().Err(err).Msg("Fail to list expired attachment records")
			return removed
		}
		if len(rows) == 0 {
			return removed
		}
		for _, row := range rows {
			a.deleteAttachmentRecord(ctx, row)
			removed++
		}
		if len(rows) < 100 {
			return removed
		}
	}
}

// deleteAttachmentRecord 删除元数据行与它引用的全部切片对象。
func (a *Assembler) deleteAttachmentRecord(ctx context.Context, row *repository.AttachmentFile) {
	store := a.cfg.Storage
	for _, segment := range row.Segments {
		if err := store.Blob.Delete(ctx, segment.Key); err != nil {
			log.Warn().Err(err).Str("key", segment.Key).Msg("Fail to delete attachment object")
		}
	}
	if err := store.Files.Delete(ctx, row.Key()); err != nil {
		log.Warn().Err(err).Str("phone", row.Phone).Str("file", row.FileName).
			Msg("Fail to delete attachment record")
	}
}
