package memory

import (
	"context"
	"fmt"
	"sync"

	"github.com/fakeyanss/jt808-server-go/internal/repository"
)

// memoryUpgradeResultRepository 是 UpgradeResultRepository 的内存实现。
type memoryUpgradeResultRepository struct {
	mu      sync.RWMutex
	results map[string]repository.UpgradeResult
}

// NewUpgradeResultRepository 创建独立的内存升级结果仓储。
func NewUpgradeResultRepository() repository.UpgradeResultRepository {
	return &memoryUpgradeResultRepository{results: map[string]repository.UpgradeResult{}}
}

func (r *memoryUpgradeResultRepository) Save(_ context.Context, result *repository.UpgradeResult) error {
	if result == nil || result.Phone == "" {
		return fmt.Errorf("save upgrade result: nil result or empty phone")
	}
	r.mu.Lock()
	defer r.mu.Unlock()
	r.results[result.Phone] = *result
	return nil
}

func (r *memoryUpgradeResultRepository) Latest(_ context.Context, phone string) (*repository.UpgradeResult, error) {
	r.mu.RLock()
	defer r.mu.RUnlock()
	result, ok := r.results[phone]
	if !ok {
		return nil, repository.ErrUpgradeResultNotFound
	}
	copy := result
	return &copy, nil
}

var _ repository.UpgradeResultRepository = (*memoryUpgradeResultRepository)(nil)
