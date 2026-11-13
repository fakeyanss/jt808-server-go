package memory

import (
	"context"
	"sort"
	"sync"

	"github.com/fakeyanss/jt808-server-go/internal/repository"
)

// 检索结果查询的默认与最大条数，与媒体文件查询保持一致。
const (
	defaultMediaSearchLimit = 100
	maxMediaSearchLimit     = 500
)

// memoryMediaSearchRepository 是 MediaSearchRepository 的内存实现。
type memoryMediaSearchRepository struct {
	mu      sync.RWMutex
	results map[string]map[uint16][]repository.MediaSearchRecord
}

// NewMediaSearchRepository 创建独立的内存检索结果仓储。
func NewMediaSearchRepository() repository.MediaSearchRepository {
	return &memoryMediaSearchRepository{results: map[string]map[uint16][]repository.MediaSearchRecord{}}
}

func (r *memoryMediaSearchRepository) Replace(_ context.Context, phone string, answerSerial uint16, records []repository.MediaSearchRecord) error {
	if err := repository.ValidateMediaSearch(phone, records); err != nil {
		return err
	}
	normalized := repository.NormalizeMediaSearchRecords(phone, answerSerial, records)
	r.mu.Lock()
	defer r.mu.Unlock()
	if r.results[phone] == nil {
		r.results[phone] = map[uint16][]repository.MediaSearchRecord{}
	}
	r.results[phone][answerSerial] = normalized
	return nil
}

func (r *memoryMediaSearchRepository) List(_ context.Context, phone string, limit int) ([]repository.MediaSearchRecord, error) {
	r.mu.RLock()
	bySerial := r.results[phone]
	all := make([]repository.MediaSearchRecord, 0)
	for _, records := range bySerial {
		all = append(all, records...)
	}
	r.mu.RUnlock()

	sort.Slice(all, func(i, j int) bool {
		if !all[i].ReceivedAt.Equal(all[j].ReceivedAt) {
			return all[i].ReceivedAt.After(all[j].ReceivedAt)
		}
		if all[i].AnswerSerial != all[j].AnswerSerial {
			return all[i].AnswerSerial > all[j].AnswerSerial
		}
		return all[i].Index < all[j].Index
	})
	if limit = normalizeMediaSearchLimit(limit); len(all) > limit {
		all = all[:limit]
	}
	return all, nil
}

// normalizeMediaSearchLimit 约束查询条数，避免单次请求拖垮进程。
func normalizeMediaSearchLimit(limit int) int {
	if limit <= 0 {
		return defaultMediaSearchLimit
	}
	if limit > maxMediaSearchLimit {
		return maxMediaSearchLimit
	}
	return limit
}

var _ repository.MediaSearchRepository = (*memoryMediaSearchRepository)(nil)
