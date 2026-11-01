package repository

import (
	"context"
	"errors"
	"sync"

	"golang.org/x/exp/maps"

	"github.com/fakeyanss/jt808-server-go/internal/protocol/model"
)

var ErrDeviceNotFound = errors.New("device not found")

// memoryDeviceRepository 是 DeviceRepository 的内存实现。
type memoryDeviceRepository struct {
	cacheByPlate map[string]*model.Device
	cacheByPhone map[string]*model.Device
	mutex        *sync.Mutex
}

// newMemoryDeviceRepository 创建独立的内存设备仓储。
func newMemoryDeviceRepository() *memoryDeviceRepository {
	return &memoryDeviceRepository{
		cacheByPlate: make(map[string]*model.Device),
		cacheByPhone: make(map[string]*model.Device),
		mutex:        &sync.Mutex{},
	}
}

// List 返回全量设备快照。
func (cache *memoryDeviceRepository) List(_ context.Context) ([]*model.Device, error) {
	cache.mutex.Lock()
	defer cache.mutex.Unlock()
	devices := maps.Values(cache.cacheByPhone)
	for i, device := range devices {
		devices[i] = cloneDevice(device)
	}
	return devices, nil
}

// GetByPlate 按车牌查询设备快照。
func (cache *memoryDeviceRepository) GetByPlate(_ context.Context, carPlate string) (*model.Device, error) {
	cache.mutex.Lock()
	defer cache.mutex.Unlock()
	if d, ok := cache.cacheByPlate[carPlate]; ok {
		return cloneDevice(d), nil
	}
	return nil, ErrDeviceNotFound
}

// GetByPhone 按手机号查询设备快照。
func (cache *memoryDeviceRepository) GetByPhone(_ context.Context, phone string) (*model.Device, error) {
	cache.mutex.Lock()
	defer cache.mutex.Unlock()
	if d, ok := cache.cacheByPhone[phone]; ok {
		return cloneDevice(d), nil
	}
	return nil, ErrDeviceNotFound
}

// Upsert 写入设备，同时维护车牌与手机号索引。
func (cache *memoryDeviceRepository) Upsert(_ context.Context, d *model.Device) error {
	cache.mutex.Lock()
	defer cache.mutex.Unlock()
	copy := cloneDevice(d)
	cache.cacheByPlate[copy.Plate] = copy
	cache.cacheByPhone[copy.Phone] = copy
	return nil
}

// DeleteByPhone 按手机号删除设备及其车牌索引。
func (cache *memoryDeviceRepository) DeleteByPhone(_ context.Context, phone string) error {
	cache.mutex.Lock()
	defer cache.mutex.Unlock()
	d, ok := cache.cacheByPhone[phone]
	if !ok {
		return nil
	}
	delete(cache.cacheByPlate, d.Plate)
	delete(cache.cacheByPhone, d.Phone)
	return nil
}

// DeleteByPlate 按车牌删除设备及其手机号索引。
func (cache *memoryDeviceRepository) DeleteByPlate(_ context.Context, carPlate string) error {
	cache.mutex.Lock()
	defer cache.mutex.Unlock()
	d, ok := cache.cacheByPlate[carPlate]
	if !ok {
		return nil
	}
	delete(cache.cacheByPlate, d.Plate)
	delete(cache.cacheByPhone, d.Phone)
	return nil
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
