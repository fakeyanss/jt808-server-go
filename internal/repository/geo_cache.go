package repository

import (
	"context"
	"sync"

	"github.com/pkg/errors"

	"github.com/fakeyanss/jt808-server-go/internal/protocol/model"
	"github.com/fakeyanss/jt808-server-go/pkg/container"
)

const RingCapacity int32 = 100

var ErrGisNotFound = errors.New("gis not found")

// memoryGeoRepository 是 GeoRepository 的内存实现，按设备维护定位环形缓冲。
type memoryGeoRepository struct {
	cacheByPhone map[string]*container.RingBuffer
	mutex        *sync.Mutex
}

// newMemoryGeoRepository 创建独立的内存定位热读仓储。
func newMemoryGeoRepository() *memoryGeoRepository {
	return &memoryGeoRepository{
		cacheByPhone: make(map[string]*container.RingBuffer),
		mutex:        &sync.Mutex{},
	}
}

// Append 将一条定位写入设备环形缓冲。
func (cache *memoryGeoRepository) Append(_ context.Context, geo *model.DeviceGeo) error {
	if geo == nil {
		return nil
	}
	cache.ring(geo.Phone).Write(geo)
	return nil
}

// Latest 返回设备最新一条定位。
func (cache *memoryGeoRepository) Latest(_ context.Context, phone string) (*model.DeviceGeo, error) {
	rb := cache.ring(phone)
	if latest, ok := rb.Latest().(*model.DeviceGeo); ok {
		return latest, nil
	}
	return nil, ErrGisNotFound
}

// ring 返回设备环形缓冲；不存在时创建。
func (cache *memoryGeoRepository) ring(phone string) *container.RingBuffer {
	cache.mutex.Lock()
	defer cache.mutex.Unlock()
	if rb, ok := cache.cacheByPhone[phone]; ok {
		return rb
	}
	rb := container.NewRingBuffer(RingCapacity)
	cache.cacheByPhone[phone] = rb
	return rb
}

// Delete 删除设备定位缓冲。
func (cache *memoryGeoRepository) Delete(_ context.Context, phone string) error {
	cache.mutex.Lock()
	defer cache.mutex.Unlock()
	delete(cache.cacheByPhone, phone)
	return nil
}
