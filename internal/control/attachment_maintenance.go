package control

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"sort"
	"time"

	"github.com/rs/zerolog/log"

	"github.com/fakeyanss/jt808-server-go/internal/clock"
	"github.com/fakeyanss/jt808-server-go/internal/contracts"
	"github.com/fakeyanss/jt808-server-go/internal/observability"
	"github.com/fakeyanss/jt808-server-go/internal/repository"
	"github.com/fakeyanss/jt808-server-go/internal/storage"
)

const attachmentMaintenanceBatch = 100

// AttachmentMaintenanceOptions 是附件索引维护参数。
type AttachmentMaintenanceOptions struct {
	// SessionTimeout 是 receiving 记录的判死阈值。
	SessionTimeout time.Duration
	// Retention 是已完成/终态附件及其对象的保留期；<=0 表示不清理。
	Retention time.Duration
	// Metrics 记录 .done 清单与切片不一致的异常次数。
	Metrics *observability.Metrics
}

// AttachmentMaintenance 是 control 侧的附件索引维护器。
//
// 附件节点不连库，过期标记、pending 回收、保留期清理与扫描补齐都在这里执行。
// 定位：对象存储是切片的事实来源，attachment_files 只是可重建的目录。
type AttachmentMaintenance struct {
	files   repository.AttachmentFileRepository
	blob    contracts.BlobStore
	clock   clock.Clock
	opts    AttachmentMaintenanceOptions
	metrics *observability.Metrics
}

// NewAttachmentMaintenance 构造维护器；files 或 blob 为空时返回 nil。
func NewAttachmentMaintenance(files repository.AttachmentFileRepository, blob contracts.BlobStore,
	opts AttachmentMaintenanceOptions, clk clock.Clock) *AttachmentMaintenance {
	if files == nil || blob == nil {
		return nil
	}
	if clk == nil {
		clk = clock.System()
	}
	if opts.SessionTimeout <= 0 {
		opts.SessionTimeout = 30 * time.Minute
	}
	return &AttachmentMaintenance{files: files, blob: blob, clock: clk, opts: opts, metrics: opts.Metrics}
}

func attachmentSources() []string {
	return []string{repository.AttachmentSourceSubia, repository.AttachmentSourceFTP1078}
}

// ExpireReceiving 把长时间无进展的 receiving 记录转为 expired。
//
// 只改库：暂存文件在边缘节点本地，control 不得跨网删除。
func (m *AttachmentMaintenance) ExpireReceiving(ctx context.Context) (int, error) {
	if m == nil {
		return 0, nil
	}
	cutoff := m.clock.Now().Add(-m.opts.SessionTimeout)
	rows, err := m.files.ListReceivingUpdatedBefore(ctx, cutoff, 500)
	if err != nil {
		return 0, fmt.Errorf("list stale attachment receiving records: %w", err)
	}
	expired := 0
	for _, row := range rows {
		row.Status = repository.AttachmentStatusExpired
		row.StagingPath = ""
		row.StagingBase = ""
		if _, saveErr := m.files.Save(ctx, row); saveErr != nil {
			log.Warn().Err(saveErr).Str("phone", row.Phone).Str("file", row.FileName).
				Msg("Fail to mark stale attachment record expired")
			continue
		}
		expired++
	}
	return expired, nil
}

// SweepPending 按附件来源前缀回收无业务行引用的 pending 对象。
func (m *AttachmentMaintenance) SweepPending(ctx context.Context) {
	if m == nil {
		return
	}
	sweeper, ok := m.blob.(interface {
		SweepPendingPrefix(context.Context, string, time.Duration, func(string) bool)
	})
	if !ok {
		return
	}
	olderThan := time.Hour
	if m.opts.SessionTimeout > olderThan {
		olderThan = m.opts.SessionTimeout
	}
	for _, source := range attachmentSources() {
		source := source
		sweeper.SweepPendingPrefix(ctx, storage.AttachmentPrefix(source), olderThan, func(key string) bool {
			referenced, err := m.files.ReferencedSegment(ctx, source, key)
			if err != nil {
				// 查询失败时保守保留对象，宁可多留一轮。
				return true
			}
			return referenced
		})
	}
}

// PruneRetention 删除超过保留期的附件记录及其切片对象。
func (m *AttachmentMaintenance) PruneRetention(ctx context.Context) (int, error) {
	if m == nil || m.opts.Retention <= 0 {
		return 0, nil
	}
	cutoff := m.clock.Now().Add(-m.opts.Retention)
	removed := 0
	for _, source := range attachmentSources() {
		for {
			rows, err := m.files.ListTerminalBefore(ctx, source, cutoff, attachmentMaintenanceBatch)
			if err != nil {
				return removed, fmt.Errorf("list stale attachment records: %w", err)
			}
			for _, row := range rows {
				m.deleteRecord(ctx, row)
				removed++
			}
			if len(rows) < attachmentMaintenanceBatch {
				break
			}
		}
		for {
			rows, err := m.files.ListCompletedBefore(ctx, source, cutoff, attachmentMaintenanceBatch)
			if err != nil {
				return removed, fmt.Errorf("list expired attachment records: %w", err)
			}
			for _, row := range rows {
				m.deleteRecord(ctx, row)
				removed++
			}
			if len(rows) < attachmentMaintenanceBatch {
				break
			}
		}
	}
	return removed, nil
}

func (m *AttachmentMaintenance) deleteRecord(ctx context.Context, row *repository.AttachmentFile) {
	for _, segment := range row.Segments {
		if err := m.blob.Delete(ctx, segment.Key); err != nil {
			log.Warn().Err(err).Str("key", segment.Key).Msg("Fail to delete attachment object")
		}
	}
	// 完成清单与切片同属一次上传，清理时必须一并删除；否则残留 .done 会让
	// 下一轮扫描把“清单存在但无切片”判成异常。
	if row.Source == storage.SourceFTP1078 {
		doneKey := storage.AttachmentDoneObjectKey(row.Source, row.Phone, row.AlarmNo, row.FileName)
		if err := m.blob.Delete(ctx, doneKey); err != nil {
			log.Warn().Err(err).Str("key", doneKey).Msg("Fail to delete attachment manifest object")
		}
	}
	if err := m.files.Delete(ctx, row.Key()); err != nil {
		log.Warn().Err(err).Str("phone", row.Phone).Str("file", row.FileName).
			Msg("Fail to delete attachment record")
	}
}

// Backfill 扫描 attachment/ 前缀并按对象 key 重建缺失索引。
//
// 只依赖 key 内的元数据（终端、报警号、文件名、偏移、段大小、总长），
// 因此节点上报全部丢失也能恢复出水位与完成状态。
func (m *AttachmentMaintenance) Backfill(ctx context.Context) (int, error) {
	if m == nil {
		return 0, nil
	}
	lister, ok := m.blob.(contracts.BlobLister)
	if !ok {
		return 0, fmt.Errorf("blob backend %T does not support listing", m.blob)
	}
	type group struct {
		segments      []contracts.AttachmentProgressSegment
		total         int64
		modified      time.Time
		manifestFound bool
		manifest      *storage.AttachmentDoneManifest
	}
	groups := make(map[contracts.AttachmentProgressKey]*group)
	ensure := func(key contracts.AttachmentProgressKey) *group {
		item, ok := groups[key]
		if !ok {
			item = &group{}
			groups[key] = item
		}
		return item
	}
	for _, source := range attachmentSources() {
		objects, err := lister.ListObjects(ctx, storage.AttachmentPrefix(source))
		if err != nil {
			return 0, fmt.Errorf("list %s attachment objects: %w", source, err)
		}
		for _, object := range objects {
			if storage.IsAttachmentDoneKey(object.Key) {
				info, parseErr := storage.ParseAttachmentDoneKey(object.Key)
				if parseErr != nil {
					log.Warn().Err(parseErr).Str("key", object.Key).
						Msg("Skip attachment manifest with invalid key metadata")
					continue
				}
				key := contracts.AttachmentProgressKey{
					Source: info.Source, Phone: info.Phone, AlarmNo: info.AlarmNo, FileName: info.FileName,
				}
				item := ensure(key)
				item.manifestFound = true
				if object.LastModified.After(item.modified) {
					item.modified = object.LastModified
				}
				manifest, readErr := m.readManifest(ctx, object.Key)
				if readErr != nil {
					log.Warn().Err(readErr).Str("key", object.Key).
						Msg("Fail to read attachment completion manifest; treat as anomaly")
					continue
				}
				item.manifest = manifest
				if manifest.TotalSize > item.total {
					item.total = manifest.TotalSize
				}
				continue
			}
			info, parseErr := storage.ParseAttachmentSegmentKey(object.Key)
			if parseErr != nil {
				log.Warn().Err(parseErr).Str("key", object.Key).
					Msg("Skip attachment object with invalid key metadata")
				continue
			}
			key := contracts.AttachmentProgressKey{
				Source: info.Source, Phone: info.Phone, AlarmNo: info.AlarmNo, FileName: info.FileName,
			}
			item := ensure(key)
			item.segments = append(item.segments, contracts.AttachmentProgressSegment{
				Index: info.Index, Key: info.ObjectKey, Offset: info.Offset, Size: info.Size,
				State: string(contracts.BlobStateCommitted),
			})
			if info.TotalSize > item.total {
				item.total = info.TotalSize
			}
			if object.LastModified.After(item.modified) {
				item.modified = object.LastModified
			}
		}
	}
	saved := 0
	for key, item := range groups {
		if len(item.segments) == 0 && !item.manifestFound {
			continue
		}
		if err := m.backfillOne(ctx, key, item.segments, item.total, item.modified, item.manifestFound, item.manifest); err != nil {
			log.Warn().Err(err).Str("phone", key.Phone).Str("file", key.FileName).
				Msg("Fail to backfill attachment index")
			continue
		}
		saved++
	}
	if saved > 0 {
		log.Info().Int("saved", saved).Msg("Attachment index backfilled from object storage")
	}
	return saved, nil
}

func (m *AttachmentMaintenance) backfillOne(ctx context.Context, key contracts.AttachmentProgressKey,
	scanned []contracts.AttachmentProgressSegment, scannedTotal int64, modified time.Time,
	manifestFound bool, manifest *storage.AttachmentDoneManifest) error {
	existing, err := m.files.Get(ctx, repository.AttachmentFileKey{
		Source: key.Source, Phone: key.Phone, AlarmNo: key.AlarmNo, FileName: key.FileName,
	})
	switch {
	case err == nil:
		// 终态记录不复活；completed 也不再扫描覆盖。
		if existing.Status != repository.AttachmentStatusReceiving {
			return nil
		}
	case err == repository.ErrAttachmentFileNotFound:
		existing = nil
	default:
		return err
	}

	byKey := make(map[string]contracts.AttachmentProgressSegment)
	total := scannedTotal
	var createdAt time.Time
	if existing != nil {
		total = existing.TotalSize
		if total < scannedTotal {
			total = scannedTotal
		}
		createdAt = existing.CreatedAt
		for _, segment := range existing.Segments {
			byKey[segment.Key] = contracts.AttachmentProgressSegment{
				Index: segment.Index, Key: segment.Key, Offset: segment.Offset,
				Size: segment.Size, MD5: segment.MD5, State: segment.State,
			}
		}
	}
	for _, segment := range scanned {
		if _, ok := byKey[segment.Key]; !ok {
			byKey[segment.Key] = segment
		}
	}
	segments := make([]contracts.AttachmentProgressSegment, 0, len(byKey))
	var received int64
	for _, segment := range byKey {
		segments = append(segments, segment)
		received += segment.Size
	}
	sort.Slice(segments, func(i, j int) bool { return segments[i].Index < segments[j].Index })

	watermark := contiguousPrefix(segments)
	status := repository.AttachmentStatusReceiving
	completedAt := time.Time{}
	if key.Source == storage.SourceFTP1078 {
		// FTP 上传在连接结束前不知道总长，完成与否只能由 .done 清单确认。
		// 清单存在但切片不连续/缺片时不能判完成，按异常计数并保留 receiving。
		if manifestFound {
			if manifest != nil && manifest.TotalSize > 0 && watermark >= manifest.TotalSize &&
				len(segments) == manifest.Segments {
				status = repository.AttachmentStatusCompleted
				completedAt = modified
				if manifest.CompletedAt > 0 {
					completedAt = time.UnixMilli(manifest.CompletedAt)
				}
			} else {
				if m.metrics != nil {
					m.metrics.IncFTP1078DoneAnomaly()
				}
				log.Warn().Str("phone", key.Phone).Str("file", key.FileName).
					Int64("watermark", watermark).Int64("scanned_total", scannedTotal).
					Msg("FTP completion manifest exists but segments are not contiguous; keep receiving")
			}
		}
	} else if total > 0 && watermark >= total {
		status = repository.AttachmentStatusCompleted
		completedAt = modified
	}
	if createdAt.IsZero() {
		createdAt = modified
	}
	recordSegments := make([]repository.AttachmentSegment, 0, len(segments))
	for _, segment := range segments {
		recordSegments = append(recordSegments, repository.AttachmentSegment{
			Index: segment.Index, Key: segment.Key, Offset: segment.Offset,
			Size: segment.Size, MD5: segment.MD5, State: segment.State,
		})
	}
	record := &repository.AttachmentFile{
		Source: key.Source, Phone: key.Phone, AlarmNo: key.AlarmNo, FileName: key.FileName,
		TotalSize: total, ReceivedBytes: received, Watermark: watermark,
		Segments: recordSegments, Status: status, CreatedAt: createdAt, CompletedAt: completedAt,
	}
	if existing != nil {
		record.TerminalID = existing.TerminalID
		record.AlarmSign = existing.AlarmSign
		record.AlarmSerial = existing.AlarmSerial
		record.FileType = existing.FileType
		record.Ranges = existing.Ranges
	}
	_, err = m.files.Save(ctx, record)
	return err
}

func (m *AttachmentMaintenance) readManifest(ctx context.Context, key string) (*storage.AttachmentDoneManifest, error) {
	reader, err := m.blob.Open(ctx, key)
	if err != nil {
		return nil, err
	}
	defer reader.Close()
	var manifest storage.AttachmentDoneManifest
	if err := json.NewDecoder(io.LimitReader(reader, 1<<20)).Decode(&manifest); err != nil {
		return nil, err
	}
	if manifest.TotalSize <= 0 {
		return nil, fmt.Errorf("manifest %s has invalid total size %d", key, manifest.TotalSize)
	}
	return &manifest, nil
}

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
