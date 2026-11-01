package repository

import (
	"context"
	"errors"
	"sync"

	"github.com/fakeyanss/jt808-server-go/internal/protocol/model"
)

var ErrTerminalPropertiesNotFound = errors.New("terminal properties not found")

// memoryTerminalPropertiesRepository 是 TerminalPropertiesRepository 的内存实现。
// 读写均返回快照，调用方修改返回值不影响仓储内部状态。
type memoryTerminalPropertiesRepository struct {
	cacheByPhone map[string]*model.TerminalProperties
	mutex        *sync.Mutex
}

// newMemoryTerminalPropertiesRepository 创建独立的内存属性仓储。
func newMemoryTerminalPropertiesRepository() *memoryTerminalPropertiesRepository {
	return &memoryTerminalPropertiesRepository{
		cacheByPhone: make(map[string]*model.TerminalProperties),
		mutex:        &sync.Mutex{},
	}
}

// Get 按手机号查询终端属性档案。
func (cache *memoryTerminalPropertiesRepository) Get(_ context.Context, phone string) (*model.TerminalProperties, error) {
	cache.mutex.Lock()
	defer cache.mutex.Unlock()
	if p, ok := cache.cacheByPhone[phone]; ok {
		return cloneTerminalProperties(p), nil
	}
	return nil, ErrTerminalPropertiesNotFound
}

// Replace 覆盖写入终端属性档案。
func (cache *memoryTerminalPropertiesRepository) Replace(_ context.Context, properties *model.TerminalProperties) error {
	cache.mutex.Lock()
	defer cache.mutex.Unlock()
	cache.cacheByPhone[properties.Phone] = cloneTerminalProperties(properties)
	return nil
}

// Delete 删除终端属性档案。
func (cache *memoryTerminalPropertiesRepository) Delete(_ context.Context, phone string) error {
	cache.mutex.Lock()
	defer cache.mutex.Unlock()
	delete(cache.cacheByPhone, phone)
	return nil
}

func cloneTerminalProperties(p *model.TerminalProperties) *model.TerminalProperties {
	if p == nil {
		return nil
	}
	clone := *p
	if p.Extra != nil {
		clone.Extra = make(map[string]string, len(p.Extra))
		for k, v := range p.Extra {
			clone.Extra[k] = v
		}
	}
	return &clone
}
