package storage

import (
	"sync"

	"github.com/pkg/errors"

	"github.com/fakeyanss/jt808-server-go/internal/protocol/model"
)

var ErrDeviceParamsNotFound = errors.New("device params not found")

type DeviceParamsCache struct {
	cacheByPhone map[string]*model.DeviceParams
	mutex        *sync.Mutex
}

var paramsCacheSingleton *DeviceParamsCache
var paramsCacheInitOnce sync.Once

func GetDeviceParamsCache() *DeviceParamsCache {
	paramsCacheInitOnce.Do(func() {
		paramsCacheSingleton = &DeviceParamsCache{
			cacheByPhone: make(map[string]*model.DeviceParams),
			mutex:        &sync.Mutex{},
		}
	})
	return paramsCacheSingleton
}

func (cache *DeviceParamsCache) GetDeviceParamsByPhone(phone string) (*model.DeviceParams, error) {
	cache.mutex.Lock()
	defer cache.mutex.Unlock()
	if d, ok := cache.cacheByPhone[phone]; ok {
		return d, nil
	}
	return nil, ErrDeviceParamsNotFound
}

func (cache *DeviceParamsCache) CacheDeviceParams(d *model.DeviceParams) {
	cache.mutex.Lock()
	defer cache.mutex.Unlock()
	cache.cacheByPhone[d.DevicePhone] = d
}

func (cache *DeviceParamsCache) DelDeviceParamsByPhone(phone string) {
	cache.mutex.Lock()
	defer cache.mutex.Unlock()
	_, ok := cache.cacheByPhone[phone]
	if !ok {
		return // find none device params, skip
	}
	delete(cache.cacheByPhone, phone)
}
