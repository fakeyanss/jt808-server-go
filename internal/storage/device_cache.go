package storage

import (
	"errors"
	"sync"

	"golang.org/x/exp/maps"

	"github.com/fakeyanss/jt808-server-go/internal/protocol/model"
)

var ErrDeviceNotFound = errors.New("device not found")

type DeviceCache struct {
	cacheByPlate map[string]*model.Device
	cacheByPhone map[string]*model.Device
	mutex        *sync.Mutex
}

var deviceCacheSingleton *DeviceCache
var deviceCacheInitOnce sync.Once

func GetDeviceCache() *DeviceCache {
	deviceCacheInitOnce.Do(func() {
		deviceCacheSingleton = &DeviceCache{
			cacheByPlate: make(map[string]*model.Device),
			cacheByPhone: make(map[string]*model.Device),
			mutex:        &sync.Mutex{},
		}
	})
	return deviceCacheSingleton
}

func (cache *DeviceCache) ListDevice() []*model.Device {
	cache.mutex.Lock()
	defer cache.mutex.Unlock()
	devices := maps.Values(cache.cacheByPhone)
	for i, device := range devices {
		devices[i] = cloneDevice(device)
	}
	return devices
}

func (cache *DeviceCache) GetDeviceByPlate(carPlate string) (*model.Device, error) {
	cache.mutex.Lock()
	defer cache.mutex.Unlock()
	if d, ok := cache.cacheByPlate[carPlate]; ok {
		return cloneDevice(d), nil
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
		return cloneDevice(d), nil
	}
	return nil, ErrDeviceNotFound
}

func (cache *DeviceCache) HasPhone(phone string) bool {
	d, err := cache.GetDeviceByPhone(phone)
	return d != nil && err == nil
}

func (cache *DeviceCache) cacheDevice(d *model.Device) {
	copy := cloneDevice(d)
	cache.cacheByPlate[copy.Plate] = copy
	cache.cacheByPhone[copy.Phone] = copy
}

// cloneDevice keeps callers from mutating cache-owned state after a lookup.
// Device fields are scalar values except Conn, which intentionally remains a
// shared transport handle.
func cloneDevice(device *model.Device) *model.Device {
	if device == nil {
		return nil
	}
	copy := *device
	return &copy
}

func (cache *DeviceCache) CacheDevice(d *model.Device) {
	cache.mutex.Lock()
	defer cache.mutex.Unlock()
	cache.cacheDevice(d)
}

func (cache *DeviceCache) delDevice(carPlate, phone *string) {
	var d *model.Device
	var ok bool
	if carPlate != nil {
		d, ok = cache.cacheByPlate[*carPlate]
	}
	if phone != nil {
		d, ok = cache.cacheByPhone[*phone]
	}
	if !ok {
		return // find none device, skip
	}
	delete(cache.cacheByPlate, d.Plate)
	delete(cache.cacheByPhone, d.Phone)
}

func (cache *DeviceCache) DelDeviceByCarPlate(carPlate string) {
	cache.mutex.Lock()
	defer cache.mutex.Unlock()
	cache.delDevice(&carPlate, nil)
}

func (cache *DeviceCache) DelDeviceByPhone(phone string) {
	cache.mutex.Lock()
	defer cache.mutex.Unlock()
	cache.delDevice(nil, &phone)
}
