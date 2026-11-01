package repository

import (
	"context"
	"sync"

	"github.com/pkg/errors"

	"github.com/fakeyanss/jt808-server-go/internal/protocol/model"
)

var ErrDeviceParamsNotFound = errors.New("device params not found")

// memoryDeviceParamsRepository 是 DeviceParamsRepository 的内存实现。
type memoryDeviceParamsRepository struct {
	cacheByPhone map[string]*model.DeviceParams
	mutex        *sync.Mutex
}

// newMemoryDeviceParamsRepository 创建独立的内存参数仓储。
func newMemoryDeviceParamsRepository() *memoryDeviceParamsRepository {
	return &memoryDeviceParamsRepository{
		cacheByPhone: make(map[string]*model.DeviceParams),
		mutex:        &sync.Mutex{},
	}
}

// Get 按手机号查询终端参数。
func (cache *memoryDeviceParamsRepository) Get(_ context.Context, phone string) (*model.DeviceParams, error) {
	cache.mutex.Lock()
	defer cache.mutex.Unlock()
	if d, ok := cache.cacheByPhone[phone]; ok {
		return d, nil
	}
	return nil, ErrDeviceParamsNotFound
}

// Replace 覆盖写入终端参数。
func (cache *memoryDeviceParamsRepository) Replace(_ context.Context, d *model.DeviceParams) error {
	cache.mutex.Lock()
	defer cache.mutex.Unlock()
	cache.cacheByPhone[d.DevicePhone] = d
	return nil
}

// Delete 删除终端参数。
func (cache *memoryDeviceParamsRepository) Delete(_ context.Context, phone string) error {
	cache.mutex.Lock()
	defer cache.mutex.Unlock()
	_, ok := cache.cacheByPhone[phone]
	if !ok {
		return nil
	}
	delete(cache.cacheByPhone, phone)
	return nil
}
