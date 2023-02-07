package storage

import (
	"errors"
	"sync"
	"time"

	"github.com/fakeYanss/jt808-server-go/internal/protocol/model"
)

var ErrDeviceNotFound = errors.New("device not found")

type DeviceCache struct {
	cacheByID    map[string]*model.Device
	cacheByPlate map[string]*model.Device
	cacheByPhone map[string]*model.Device
	mutex        *sync.Mutex
}

var deviceCacheSingleton *DeviceCache
var deviceCacheInitOnce sync.Once

func GetDeviceCache() *DeviceCache {
	deviceCacheInitOnce.Do(func() {
		deviceCacheSingleton = &DeviceCache{
			cacheByID:    make(map[string]*model.Device),
			cacheByPlate: make(map[string]*model.Device),
			cacheByPhone: make(map[string]*model.Device),
			mutex:        &sync.Mutex{},
		}
	})
	return deviceCacheSingleton
}

func (cache *DeviceCache) GetDeviceByID(id string) (*model.Device, error) {
	cache.mutex.Lock()
	defer cache.mutex.Unlock()
	if d, ok := cache.cacheByID[id]; ok {
		return d, nil
	}
	return nil, ErrDeviceNotFound
}

func (cache *DeviceCache) HasID(id string) bool {
	d, err := cache.GetDeviceByID(id)
	return d != nil && err == nil
}

func (cache *DeviceCache) GetDeviceByPlate(carPlate string) (*model.Device, error) {
	cache.mutex.Lock()
	defer cache.mutex.Unlock()
	if d, ok := cache.cacheByPlate[carPlate]; ok {
		return d, nil
	}
	return nil, ErrDeviceNotFound
}

func (cache *DeviceCache) HasPlate(carPlate string) bool {
	d, err := cache.GetDeviceByPlate(carPlate)
	return d != nil && err == nil
}

func (cache *DeviceCache) GetDeviceByPhone(phone string) (*model.Device, error) {
	cache.mutex.Lock()
	defer cache.mutex.Unlock()
	if d, ok := cache.cacheByPhone[phone]; ok {
		return d, nil
	}
	return nil, ErrDeviceNotFound
}

func (cache *DeviceCache) CacheDevice(d *model.Device) {
	cache.mutex.Lock()
	defer cache.mutex.Unlock()
	d.LastComTime = time.Now().UnixMilli()
	cache.cacheByID[d.ID] = d
	cache.cacheByPlate[d.PlateNumber] = d
	cache.cacheByPhone[d.PhoneNumber] = d
}

func (cache *DeviceCache) DelDeviceByID(id string) {
	cache.mutex.Lock()
	defer cache.mutex.Unlock()
	if d, ok := cache.cacheByID[id]; ok {
		delete(cache.cacheByPlate, d.PlateNumber)
	}
	delete(cache.cacheByID, id)
}

func (cache *DeviceCache) DelDeviceByCarPlate(carPlate string) {
	cache.mutex.Lock()
	defer cache.mutex.Unlock()
	if d, ok := cache.cacheByPlate[carPlate]; ok {
		delete(cache.cacheByID, d.ID)
	}
	delete(cache.cacheByPlate, carPlate)
}
