package control

import (
	"context"
	"fmt"
	"strings"
	"time"

	"github.com/fakeyanss/jt808-server-go/internal/clock"
	"github.com/fakeyanss/jt808-server-go/internal/contracts"
	"github.com/fakeyanss/jt808-server-go/internal/repository"
)

// AttachmentProgressStore 是 control 侧的附件进度落库入口与恢复点查询实现。
//
// 附件节点不连库，只通过出站 HTTP 调用这里；control 独占 attachment_files。
// 落库是幂等 upsert：同一业务键的多份快照以最后一份为准，重复上报不会产生多行。
type AttachmentProgressStore struct {
	files repository.AttachmentFileRepository
	clock clock.Clock
}

// NewAttachmentProgressStore 构造进度存储；files 为空时返回 nil。
func NewAttachmentProgressStore(files repository.AttachmentFileRepository, clk clock.Clock) *AttachmentProgressStore {
	if files == nil {
		return nil
	}
	if clk == nil {
		clk = clock.System()
	}
	return &AttachmentProgressStore{files: files, clock: clk}
}

// Save 幂等保存一份进度快照。
func (s *AttachmentProgressStore) Save(ctx context.Context, progress contracts.AttachmentProgress) error {
	if s == nil || s.files == nil {
		return fmt.Errorf("attachment progress store is not configured")
	}
	record, err := attachmentRecordFromProgress(progress, s.clock.Now())
	if err != nil {
		return err
	}
	_, err = s.files.Save(ctx, record)
	return err
}

// Load 返回恢复点；没有记录时返回 ErrAttachmentProgressNotFound。
func (s *AttachmentProgressStore) Load(ctx context.Context, key contracts.AttachmentProgressKey) (*contracts.AttachmentProgress, error) {
	if s == nil || s.files == nil {
		return nil, fmt.Errorf("attachment progress store is not configured")
	}
	record, err := s.files.Get(ctx, repository.AttachmentFileKey{
		Source: normalizeAttachmentSource(key.Source), Phone: key.Phone,
		AlarmNo: key.AlarmNo, FileName: key.FileName,
	})
	if err != nil {
		if err == repository.ErrAttachmentFileNotFound {
			return nil, contracts.ErrAttachmentProgressNotFound
		}
		return nil, err
	}
	progress := attachmentProgressFromRecord(record)
	return &progress, nil
}

func normalizeAttachmentSource(source string) string {
	source = strings.ToLower(strings.TrimSpace(source))
	if source == "" {
		return repository.AttachmentSourceSubia
	}
	return source
}

// normalizeAttachmentStatus 只接受已知状态；未知值按 receiving 处理。
func normalizeAttachmentStatus(status string) string {
	switch strings.ToLower(strings.TrimSpace(status)) {
	case repository.AttachmentStatusCompleted:
		return repository.AttachmentStatusCompleted
	case repository.AttachmentStatusFailed:
		return repository.AttachmentStatusFailed
	case repository.AttachmentStatusExpired:
		return repository.AttachmentStatusExpired
	default:
		return repository.AttachmentStatusReceiving
	}
}

func attachmentRecordFromProgress(progress contracts.AttachmentProgress, now time.Time) (*repository.AttachmentFile, error) {
	phone := strings.TrimSpace(progress.Phone)
	fileName := strings.TrimSpace(progress.FileName)
	if phone == "" || fileName == "" {
		return nil, fmt.Errorf("attachment progress requires phone and file name")
	}
	if progress.TotalSize < 0 || progress.Watermark < 0 {
		return nil, fmt.Errorf("attachment progress sizes must be non-negative")
	}
	segments := make([]repository.AttachmentSegment, 0, len(progress.Segments))
	for _, segment := range progress.Segments {
		if strings.TrimSpace(segment.Key) == "" {
			return nil, fmt.Errorf("attachment progress segment key is required")
		}
		segments = append(segments, repository.AttachmentSegment{
			Index: segment.Index, Key: segment.Key, Offset: segment.Offset,
			Size: segment.Size, MD5: segment.MD5, State: segment.State,
		})
	}
	record := &repository.AttachmentFile{
		Source:        normalizeAttachmentSource(progress.Source),
		Phone:         phone,
		TerminalID:    progress.TerminalID,
		AlarmSign:     progress.AlarmSign,
		AlarmNo:       progress.AlarmNo,
		AlarmSerial:   progress.AlarmSerial,
		FileName:      fileName,
		FileType:      progress.FileType,
		TotalSize:     progress.TotalSize,
		ReceivedBytes: progress.ReceivedBytes,
		Watermark:     progress.Watermark,
		StagingPath:   progress.StagingPath,
		StagingBase:   progress.StagingBase,
		Ranges:        progress.Ranges,
		Segments:      segments,
		Status:        normalizeAttachmentStatus(progress.Status),
		CreatedAt:     progress.CreatedAt,
		CompletedAt:   progress.CompletedAt,
	}
	if record.CreatedAt.IsZero() {
		record.CreatedAt = now
	}
	if record.Status == repository.AttachmentStatusCompleted && record.CompletedAt.IsZero() {
		record.CompletedAt = now
	}
	if record.Status != repository.AttachmentStatusReceiving {
		// 暂存路径只对上报节点有效，终态后不再保留引用。
		record.StagingPath = ""
		record.StagingBase = ""
	}
	return record, nil
}

func attachmentProgressFromRecord(record *repository.AttachmentFile) contracts.AttachmentProgress {
	segments := make([]contracts.AttachmentProgressSegment, 0, len(record.Segments))
	for _, segment := range record.Segments {
		segments = append(segments, contracts.AttachmentProgressSegment{
			Index: segment.Index, Key: segment.Key, Offset: segment.Offset,
			Size: segment.Size, MD5: segment.MD5, State: segment.State,
		})
	}
	return contracts.AttachmentProgress{
		Source:        record.Source,
		Phone:         record.Phone,
		TerminalID:    record.TerminalID,
		AlarmSign:     record.AlarmSign,
		AlarmNo:       record.AlarmNo,
		AlarmSerial:   record.AlarmSerial,
		FileName:      record.FileName,
		FileType:      record.FileType,
		TotalSize:     record.TotalSize,
		ReceivedBytes: record.ReceivedBytes,
		Watermark:     record.Watermark,
		StagingPath:   record.StagingPath,
		StagingBase:   record.StagingBase,
		Ranges:        record.Ranges,
		Segments:      segments,
		Status:        record.Status,
		CreatedAt:     record.CreatedAt,
		UpdatedAt:     record.UpdatedAt,
		CompletedAt:   record.CompletedAt,
	}
}

var _ contracts.AttachmentProgressSink = (*AttachmentProgressStore)(nil)
