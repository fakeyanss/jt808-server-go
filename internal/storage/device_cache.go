package storage

import (
	"errors"
	"sync"
	"time"

	"golang.org/x/exp/maps"

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

func (cache *DeviceCache) ListDevice() []*model.Device {
	return maps.Values(cache.cacheByID)
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

func (cache *DeviceCache) cacheDevice(d *model.Device) {
	cache.cacheByID[d.ID] = d
	cache.cacheByPlate[d.PlateNumber] = d
	cache.cacheByPhone[d.PhoneNumber] = d
}

func (cache *DeviceCache) CacheDevice(d *model.Device) {
	cache.mutex.Lock()
	defer cache.mutex.Unlock()
	now := time.Now()
	d.LastComTime = &now
	cache.cacheDevice(d)
}

func (cache *DeviceCache) delDevice(id *string, carPlate *string, phone *string) {
	var d *model.Device
	var ok bool
	if id != nil {
		d, ok = cache.cacheByID[*id]
	}
	if carPlate != nil {
		d, ok = cache.cacheByPlate[*carPlate]
	}
	if phone != nil {
		d, ok = cache.cacheByPhone[*phone]
	}
	if !ok {
		return // find none device, skip
	}
	delete(cache.cacheByID, d.ID)
	delete(cache.cacheByPlate, d.PlateNumber)
	delete(cache.cacheByPhone, d.PhoneNumber)
}

func (cache *DeviceCache) DelDeviceByID(id string) {
	cache.mutex.Lock()
	defer cache.mutex.Unlock()
	cache.delDevice(&id, nil, nil)
}

func (cache *DeviceCache) DelDeviceByCarPlate(carPlate string) {
	cache.mutex.Lock()
	defer cache.mutex.Unlock()
	cache.delDevice(nil, &carPlate, nil)
}

func (cache *DeviceCache) DelDeviceByPhone(phone string) {
	cache.mutex.Lock()
	defer cache.mutex.Unlock()
	cache.delDevice(nil, nil, &phone)
}
