package memory

import (
	"context"
	"fmt"
	"sort"
	"sync"

	"github.com/fakeyanss/jt808-server-go/internal/repository"
)

// defaultMediaQueryLimit 是仓储层单次查询的默认与最大条数。
const (
	defaultMediaQueryLimit = 100
	maxMediaQueryLimit     = 1000
)

func normalizeMediaLimit(limit int) int {
	if limit <= 0 {
		return defaultMediaQueryLimit
	}
	if limit > maxMediaQueryLimit {
		return maxMediaQueryLimit
	}
	return limit
}

// memoryMediaFileRepository 是 MediaFileRepository 与 MediaCaptureRepository 的内存实现。
type memoryMediaFileRepository struct {
	mu       sync.RWMutex
	files    map[mediaFileKey]*repository.MediaFile
	captures map[string][]*repository.MediaCapture
}

type mediaFileKey struct {
	phone   string
	mediaID uint32
}

// NewMediaFileRepository 创建独立的内存媒体文件仓储。
func NewMediaFileRepository() *memoryMediaFileRepository {
	return &memoryMediaFileRepository{
		files:    map[mediaFileKey]*repository.MediaFile{},
		captures: map[string][]*repository.MediaCapture{},
	}
}

func (r *memoryMediaFileRepository) Save(_ context.Context, file *repository.MediaFile) error {
	if file == nil || file.Phone == "" || file.MediaID == 0 {
		return fmt.Errorf("save media file: nil file, empty phone or zero media id")
	}
	copy := *file
	r.mu.Lock()
	defer r.mu.Unlock()
	r.files[mediaFileKey{phone: file.Phone, mediaID: file.MediaID}] = &copy
	return nil
}

func (r *memoryMediaFileRepository) Get(_ context.Context, phone string, mediaID uint32) (*repository.MediaFile, error) {
	r.mu.RLock()
	defer r.mu.RUnlock()
	file := r.files[mediaFileKey{phone: phone, mediaID: mediaID}]
	if file == nil {
		return nil, repository.ErrMediaFileNotFound
	}
	copy := *file
	return &copy, nil
}

func (r *memoryMediaFileRepository) List(_ context.Context, phone string, filter repository.MediaFileFilter) ([]*repository.MediaFile, error) {
	r.mu.RLock()
	defer r.mu.RUnlock()
	matched := make([]*repository.MediaFile, 0)
	for key, file := range r.files {
		if key.phone != phone || !mediaFileMatches(file, filter) {
			continue
		}
		copy := *file
		matched = append(matched, &copy)
	}
	sort.Slice(matched, func(i, j int) bool {
		if !matched[i].ReceivedAt.Equal(matched[j].ReceivedAt) {
			return matched[i].ReceivedAt.After(matched[j].ReceivedAt)
		}
		return matched[i].MediaID > matched[j].MediaID
	})
	if limit := normalizeMediaLimit(filter.Limit); len(matched) > limit {
		matched = matched[:limit]
	}
	return matched, nil
}

func (r *memoryMediaFileRepository) Stats(_ context.Context) (int64, int64, error) {
	r.mu.RLock()
	defer r.mu.RUnlock()
	var count, bytes int64
	for _, file := range r.files {
		count++
		bytes += file.FileSize
	}
	return count, bytes, nil
}

func (r *memoryMediaFileRepository) SaveCapture(_ context.Context, capture *repository.MediaCapture) error {
	if capture == nil || capture.Phone == "" || capture.RecordID == "" {
		return fmt.Errorf("save media capture: nil capture, empty phone or record id")
	}
	copy := *capture
	copy.Items = append([]repository.MediaCaptureItem(nil), capture.Items...)
	r.mu.Lock()
	defer r.mu.Unlock()
	list := r.captures[capture.Phone]
	for i, existing := range list {
		if existing.RecordID == capture.RecordID {
			list[i] = &copy
			r.captures[capture.Phone] = list
			return nil
		}
	}
	r.captures[capture.Phone] = append(list, &copy)
	return nil
}

func (r *memoryMediaFileRepository) ListCaptures(_ context.Context, phone string, limit int) ([]*repository.MediaCapture, error) {
	r.mu.RLock()
	defer r.mu.RUnlock()
	stored := r.captures[phone]
	result := make([]*repository.MediaCapture, 0, len(stored))
	for i := len(stored) - 1; i >= 0; i-- {
		copy := *stored[i]
		copy.Items = make([]repository.MediaCaptureItem, len(stored[i].Items))
		for j, item := range stored[i].Items {
			copy.Items[j] = item
			if file, ok := r.files[mediaFileKey{phone: phone, mediaID: item.MediaID}]; ok {
				copy.Items[j].Arrived = true
				copy.Items[j].FileSize = file.FileSize
			}
		}
		result = append(result, &copy)
		if len(result) >= normalizeMediaLimit(limit) {
			break
		}
	}
	return result, nil
}

func mediaFileMatches(file *repository.MediaFile, filter repository.MediaFileFilter) bool {
	if filter.ChannelID != nil && file.ChannelID != *filter.ChannelID {
		return false
	}
	if filter.MediaType != nil && file.MediaType != *filter.MediaType {
		return false
	}
	if filter.AlarmOnly && file.AlarmSign == 0 {
		return false
	}
	if filter.Start != nil && file.LocationTime.Before(*filter.Start) {
		return false
	}
	if filter.End != nil && file.LocationTime.After(*filter.End) {
		return false
	}
	return true
}

var (
	_ repository.MediaFileRepository    = (*memoryMediaFileRepository)(nil)
	_ repository.MediaCaptureRepository = (*memoryMediaFileRepository)(nil)
)
