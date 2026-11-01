package storage

import (
	"context"
	"sync"

	"github.com/pkg/errors"

	"github.com/fakeyanss/jt808-server-go/internal/protocol/model"
)

var ErrDeviceParamsNotFound = errors.New("device params not found")

// DeviceParamsCache 是 DeviceParamsRepository 的内存实现。
type DeviceParamsCache struct {
	cacheByPhone map[string]*model.DeviceParams
	mutex        *sync.Mutex
}

var paramsCacheSingleton *DeviceParamsCache
var paramsCacheInitOnce sync.Once

// NewDeviceParamsCache 创建独立的内存参数仓储。
func NewDeviceParamsCache() *DeviceParamsCache {
	return &DeviceParamsCache{
		cacheByPhone: make(map[string]*model.DeviceParams),
		mutex:        &sync.Mutex{},
	}
}

func GetDeviceParamsCache() *DeviceParamsCache {
	paramsCacheInitOnce.Do(func() {
		paramsCacheSingleton = NewDeviceParamsCache()
	})
	return paramsCacheSingleton
}

// Get 按手机号查询终端参数。
func (cache *DeviceParamsCache) Get(_ context.Context, phone string) (*model.DeviceParams, error) {
	cache.mutex.Lock()
	defer cache.mutex.Unlock()
	if d, ok := cache.cacheByPhone[phone]; ok {
		return d, nil
	}
	return nil, ErrDeviceParamsNotFound
}

// Replace 覆盖写入终端参数。
func (cache *DeviceParamsCache) Replace(_ context.Context, d *model.DeviceParams) error {
	cache.mutex.Lock()
	defer cache.mutex.Unlock()
	cache.cacheByPhone[d.DevicePhone] = d
	return nil
}

// Delete 删除终端参数。
func (cache *DeviceParamsCache) Delete(_ context.Context, phone string) error {
	cache.mutex.Lock()
	defer cache.mutex.Unlock()
	_, ok := cache.cacheByPhone[phone]
	if !ok {
		return nil
	}
	delete(cache.cacheByPhone, phone)
	return nil
}

// 以下为兼容既有调用点的薄封装。

func (cache *DeviceParamsCache) GetDeviceParamsByPhone(phone string) (*model.DeviceParams, error) {
	return cache.Get(context.Background(), phone)
}

func (cache *DeviceParamsCache) CacheDeviceParams(d *model.DeviceParams) {
	_ = cache.Replace(context.Background(), d)
}

func (cache *DeviceParamsCache) DelDeviceParamsByPhone(phone string) {
	_ = cache.Delete(context.Background(), phone)
}
