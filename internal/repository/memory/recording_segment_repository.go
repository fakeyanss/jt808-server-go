package memory

import (
	"context"
	"fmt"
	"sort"
	"sync"
	"time"

	"github.com/fakeyanss/jt808-server-go/internal/repository"
)

// memoryRecordingSegmentRepository 是录像切片索引的内存实现。
type memoryRecordingSegmentRepository struct {
	mu    sync.RWMutex
	items map[string]*repository.RecordingSegment
}

// NewRecordingSegmentRepository 创建独立的内存录像切片仓储。
func NewRecordingSegmentRepository() *memoryRecordingSegmentRepository {
	return &memoryRecordingSegmentRepository{items: map[string]*repository.RecordingSegment{}}
}

func (r *memoryRecordingSegmentRepository) Save(_ context.Context, segment *repository.RecordingSegment) error {
	if segment == nil || segment.ID == "" || segment.Phone == "" {
		return fmt.Errorf("save recording segment: nil segment, empty id or phone")
	}
	copy := *segment
	copy.Keyframes = append([]byte(nil), segment.Keyframes...)
	r.mu.Lock()
	defer r.mu.Unlock()
	r.items[segment.ID] = &copy
	return nil
}

func (r *memoryRecordingSegmentRepository) Get(_ context.Context, phone, id string) (*repository.RecordingSegment, error) {
	r.mu.RLock()
	defer r.mu.RUnlock()
	item, ok := r.items[id]
	if !ok || item.Phone != phone {
		return nil, repository.ErrRecordingSegmentNotFound
	}
	return cloneRecordingSegment(item), nil
}

func (r *memoryRecordingSegmentRepository) ListByRange(_ context.Context, phone string, channel uint8, start, end time.Time) ([]*repository.RecordingSegment, error) {
	r.mu.RLock()
	defer r.mu.RUnlock()
	out := make([]*repository.RecordingSegment, 0)
	for _, item := range r.items {
		if item.Phone != phone || item.Channel != channel {
			continue
		}
		if !item.EndAt.After(start) || !item.StartAt.Before(end) {
			continue
		}
		out = append(out, cloneRecordingSegment(item))
	}
	sort.Slice(out, func(i, j int) bool {
		if out[i].StartAt.Equal(out[j].StartAt) {
			return out[i].ID < out[j].ID
		}
		return out[i].StartAt.Before(out[j].StartAt)
	})
	return out, nil
}

func (r *memoryRecordingSegmentRepository) ListExpired(_ context.Context, cutoff time.Time, limit int) ([]*repository.RecordingSegment, error) {
	r.mu.RLock()
	defer r.mu.RUnlock()
	out := make([]*repository.RecordingSegment, 0)
	for _, item := range r.items {
		if !item.EndAt.Before(cutoff) {
			continue
		}
		out = append(out, cloneRecordingSegment(item))
	}
	sort.Slice(out, func(i, j int) bool {
		if out[i].EndAt.Equal(out[j].EndAt) {
			return out[i].ID < out[j].ID
		}
		return out[i].EndAt.Before(out[j].EndAt)
	})
	if limit > 0 && len(out) > limit {
		out = out[:limit]
	}
	return out, nil
}

func (r *memoryRecordingSegmentRepository) Delete(_ context.Context, ids []string) (int64, error) {
	r.mu.Lock()
	defer r.mu.Unlock()
	var deleted int64
	for _, id := range ids {
		if _, ok := r.items[id]; ok {
			delete(r.items, id)
			deleted++
		}
	}
	return deleted, nil
}

func (r *memoryRecordingSegmentRepository) ReferencedSegment(_ context.Context, key string) (bool, error) {
	if key == "" {
		return false, nil
	}
	r.mu.RLock()
	defer r.mu.RUnlock()
	for _, item := range r.items {
		if item.ObjectKey == key {
			return true, nil
		}
	}
	return false, nil
}

func (r *memoryRecordingSegmentRepository) Stats(_ context.Context) (int64, int64, error) {
	r.mu.RLock()
	defer r.mu.RUnlock()
	var count, bytes int64
	for _, item := range r.items {
		count++
		bytes += item.Size
	}
	return count, bytes, nil
}

func cloneRecordingSegment(item *repository.RecordingSegment) *repository.RecordingSegment {
	copy := *item
	copy.Keyframes = append([]byte(nil), item.Keyframes...)
	return &copy
}
