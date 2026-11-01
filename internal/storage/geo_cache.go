package storage

import (
	"context"
	"sync"

	"github.com/pkg/errors"

	"github.com/fakeyanss/jt808-server-go/internal/protocol/model"
	"github.com/fakeyanss/jt808-server-go/pkg/container"
)

const RingCapacity int32 = 100

var ErrGisNotFound = errors.New("gis not found")

// GeoCache 是 GeoRepository 的内存实现，按设备维护定位环形缓冲。
type GeoCache struct {
	cacheByPhone map[string]*container.RingBuffer
	mutex        *sync.Mutex
}

var geoCacheSingleton *GeoCache
var geoCacheInitOnce sync.Once

// NewGeoCache 创建独立的内存定位热读仓储。
func NewGeoCache() *GeoCache {
	return &GeoCache{
		cacheByPhone: make(map[string]*container.RingBuffer),
		mutex:        &sync.Mutex{},
	}
}

func GetGeoCache() *GeoCache {
	geoCacheInitOnce.Do(func() {
		geoCacheSingleton = NewGeoCache()
	})
	return geoCacheSingleton
}

// Append 将一条定位写入设备环形缓冲。
func (cache *GeoCache) Append(_ context.Context, geo *model.DeviceGeo) error {
	if geo == nil {
		return nil
	}
	cache.Ring(context.Background(), geo.Phone).Write(geo)
	return nil
}

// Latest 返回设备最新一条定位。
func (cache *GeoCache) Latest(_ context.Context, phone string) (*model.DeviceGeo, error) {
	rb := cache.Ring(context.Background(), phone)
	if latest, ok := rb.Latest().(*model.DeviceGeo); ok {
		return latest, nil
	}
	return nil, ErrGisNotFound
}

// Ring 返回设备环形缓冲；不存在时创建。保留给既有写入/读取调用点过渡使用。
func (cache *GeoCache) Ring(_ context.Context, phone string) *container.RingBuffer {
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
func (cache *GeoCache) Delete(_ context.Context, phone string) error {
	cache.mutex.Lock()
	defer cache.mutex.Unlock()
	delete(cache.cacheByPhone, phone)
	return nil
}

// 以下为兼容既有调用点的薄封装。

func (cache *GeoCache) GetGeoRingByPhone(phone string) *container.RingBuffer {
	return cache.Ring(context.Background(), phone)
}

func (cache *GeoCache) GetGeoLatestByPhone(phone string) (*model.DeviceGeo, error) {
	return cache.Latest(context.Background(), phone)
}

func (cache *GeoCache) DelGeoByPhone(phone string) {
	_ = cache.Delete(context.Background(), phone)
}
