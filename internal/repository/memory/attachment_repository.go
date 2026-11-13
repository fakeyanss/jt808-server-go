package memory

import (
	"context"
	"fmt"
	"sort"
	"sync"
	"time"

	"github.com/fakeyanss/jt808-server-go/internal/repository"
)

type memoryAttachmentRepository struct {
	mu    sync.RWMutex
	files map[repository.AttachmentFileKey]*repository.AttachmentFile
}

// NewAttachmentFileRepository 创建内存附件元数据仓储。
func NewAttachmentFileRepository() repository.AttachmentFileRepository {
	return &memoryAttachmentRepository{files: map[repository.AttachmentFileKey]*repository.AttachmentFile{}}
}

func cloneAttachment(file *repository.AttachmentFile) *repository.AttachmentFile {
	copy := *file
	copy.Segments = append([]repository.AttachmentSegment(nil), file.Segments...)
	copy.Ranges = append([]byte(nil), file.Ranges...)
	return &copy
}

func normalizeAttachment(file *repository.AttachmentFile) error {
	if file == nil {
		return fmt.Errorf("attachment file is nil")
	}
	if file.Phone == "" || file.FileName == "" {
		return fmt.Errorf("attachment file requires phone and file name")
	}
	if file.Source == "" {
		file.Source = repository.AttachmentSourceSubia
	}
	if file.Status == "" {
		file.Status = repository.AttachmentStatusReceiving
	}
	if file.CreatedAt.IsZero() {
		file.CreatedAt = time.Now()
	}
	file.UpdatedAt = time.Now()
	sort.Slice(file.Segments, func(i, j int) bool { return file.Segments[i].Index < file.Segments[j].Index })
	return nil
}

func (r *memoryAttachmentRepository) Save(_ context.Context, file *repository.AttachmentFile) (*repository.AttachmentFile, error) {
	if err := normalizeAttachment(file); err != nil {
		return nil, err
	}
	stored := cloneAttachment(file)
	r.mu.Lock()
	defer r.mu.Unlock()
	if existing, ok := r.files[stored.Key()]; ok {
		stored.CreatedAt = existing.CreatedAt
		if stored.CompletedAt.IsZero() && stored.Status == repository.AttachmentStatusCompleted {
			stored.CompletedAt = stored.UpdatedAt
		}
	}
	r.files[stored.Key()] = stored
	return cloneAttachment(stored), nil
}

func (r *memoryAttachmentRepository) Get(_ context.Context, key repository.AttachmentFileKey) (*repository.AttachmentFile, error) {
	r.mu.RLock()
	defer r.mu.RUnlock()
	file, ok := r.files[key]
	if !ok {
		return nil, repository.ErrAttachmentFileNotFound
	}
	return cloneAttachment(file), nil
}

func (r *memoryAttachmentRepository) FindByFileName(_ context.Context, source, phone, fileName string) (*repository.AttachmentFile, error) {
	r.mu.RLock()
	defer r.mu.RUnlock()
	var newest *repository.AttachmentFile
	for _, file := range r.files {
		if file.Source != source || file.Phone != phone || file.FileName != fileName {
			continue
		}
		if newest == nil || file.UpdatedAt.After(newest.UpdatedAt) {
			newest = file
		}
	}
	if newest == nil {
		return nil, repository.ErrAttachmentFileNotFound
	}
	return cloneAttachment(newest), nil
}

func (r *memoryAttachmentRepository) ListByPhone(_ context.Context, source, phone string, limit int) ([]*repository.AttachmentFile, error) {
	r.mu.RLock()
	defer r.mu.RUnlock()
	matched := make([]*repository.AttachmentFile, 0)
	for _, file := range r.files {
		if file.Source == source && file.Phone == phone {
			matched = append(matched, cloneAttachment(file))
		}
	}
	sort.Slice(matched, func(i, j int) bool { return matched[i].UpdatedAt.After(matched[j].UpdatedAt) })
	if limit > 0 && len(matched) > limit {
		matched = matched[:limit]
	}
	return matched, nil
}

func (r *memoryAttachmentRepository) ListByAlarm(_ context.Context, source, phone, alarmNo string) ([]*repository.AttachmentFile, error) {
	r.mu.RLock()
	defer r.mu.RUnlock()
	matched := make([]*repository.AttachmentFile, 0)
	for _, file := range r.files {
		if file.Source == source && file.Phone == phone && file.AlarmNo == alarmNo {
			matched = append(matched, cloneAttachment(file))
		}
	}
	sort.Slice(matched, func(i, j int) bool { return matched[i].FileName < matched[j].FileName })
	return matched, nil
}

func (r *memoryAttachmentRepository) ListCompletedSource(_ context.Context, source string, limit int) ([]*repository.AttachmentFile, error) {
	r.mu.RLock()
	defer r.mu.RUnlock()
	matched := make([]*repository.AttachmentFile, 0)
	for _, file := range r.files {
		if file.Source == source && file.Status == repository.AttachmentStatusCompleted {
			matched = append(matched, cloneAttachment(file))
		}
	}
	sort.Slice(matched, func(i, j int) bool { return matched[i].CompletedAt.After(matched[j].CompletedAt) })
	if limit > 0 && len(matched) > limit {
		matched = matched[:limit]
	}
	return matched, nil
}

func (r *memoryAttachmentRepository) ListReceivingUpdatedBefore(_ context.Context, cutoff time.Time, limit int) ([]*repository.AttachmentFile, error) {
	r.mu.RLock()
	defer r.mu.RUnlock()
	matched := make([]*repository.AttachmentFile, 0)
	for _, file := range r.files {
		if file.Status == repository.AttachmentStatusReceiving && file.UpdatedAt.Before(cutoff) {
			matched = append(matched, cloneAttachment(file))
		}
	}
	sort.Slice(matched, func(i, j int) bool { return matched[i].UpdatedAt.Before(matched[j].UpdatedAt) })
	if limit > 0 && len(matched) > limit {
		matched = matched[:limit]
	}
	return matched, nil
}

func (r *memoryAttachmentRepository) ListCompletedBefore(_ context.Context, source string, cutoff time.Time, limit int) ([]*repository.AttachmentFile, error) {
	r.mu.RLock()
	defer r.mu.RUnlock()
	matched := make([]*repository.AttachmentFile, 0)
	for _, file := range r.files {
		if file.Source != source || file.Status != repository.AttachmentStatusCompleted {
			continue
		}
		if file.CompletedAt.IsZero() || !file.CompletedAt.Before(cutoff) {
			continue
		}
		matched = append(matched, cloneAttachment(file))
	}
	sort.Slice(matched, func(i, j int) bool { return matched[i].CompletedAt.Before(matched[j].CompletedAt) })
	if limit > 0 && len(matched) > limit {
		matched = matched[:limit]
	}
	return matched, nil
}

func (r *memoryAttachmentRepository) ListTerminalBefore(_ context.Context, source string, cutoff time.Time, limit int) ([]*repository.AttachmentFile, error) {
	r.mu.RLock()
	defer r.mu.RUnlock()
	matched := make([]*repository.AttachmentFile, 0)
	for _, file := range r.files {
		if file.Source != source || (file.Status != repository.AttachmentStatusFailed && file.Status != repository.AttachmentStatusExpired) {
			continue
		}
		if !file.UpdatedAt.Before(cutoff) {
			continue
		}
		matched = append(matched, cloneAttachment(file))
	}
	sort.Slice(matched, func(i, j int) bool { return matched[i].UpdatedAt.Before(matched[j].UpdatedAt) })
	if limit > 0 && len(matched) > limit {
		matched = matched[:limit]
	}
	return matched, nil
}

func (r *memoryAttachmentRepository) ReferencedSegment(_ context.Context, source, key string) (bool, error) {
	r.mu.RLock()
	defer r.mu.RUnlock()
	for _, file := range r.files {
		if file.Source != source {
			continue
		}
		for _, segment := range file.Segments {
			if segment.Key == key {
				return true, nil
			}
		}
	}
	return false, nil
}

func (r *memoryAttachmentRepository) Delete(_ context.Context, key repository.AttachmentFileKey) error {
	r.mu.Lock()
	defer r.mu.Unlock()
	delete(r.files, key)
	return nil
}

func (r *memoryAttachmentRepository) Stats(_ context.Context, source string) (int64, int64, int64, error) {
	r.mu.RLock()
	defer r.mu.RUnlock()
	var files, completed, bytes int64
	for _, file := range r.files {
		if source != "" && file.Source != source {
			continue
		}
		files++
		if file.Status == repository.AttachmentStatusCompleted {
			completed++
			bytes += file.TotalSize
		}
	}
	return files, completed, bytes, nil
}

var _ repository.AttachmentFileRepository = (*memoryAttachmentRepository)(nil)
