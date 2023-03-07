package storage

import (
	"sync"

	"github.com/pkg/errors"

	"github.com/fakeyanss/jt808-server-go/internal/protocol/model"
	"github.com/fakeyanss/jt808-server-go/pkg/container"
)

const RingCapacity int32 = 100

var ErrGisNotFound = errors.New("gis not found")

type GeoCache struct {
	cacheByPhone map[string]*container.RingBuffer
	mutex        *sync.Mutex
}

var geoCacheSingleton *GeoCache
var geoCacheInitOnce sync.Once

func GetGeoCache() *GeoCache {
	geoCacheInitOnce.Do(func() {
		geoCacheSingleton = &GeoCache{
			cacheByPhone: make(map[string]*container.RingBuffer),
			mutex:        &sync.Mutex{},
		}
	})
	return geoCacheSingleton
}

func (cache *GeoCache) GetGeoRingByPhone(phone string) *container.RingBuffer {
	cache.mutex.Lock()
	defer cache.mutex.Unlock()
	if rb, ok := cache.cacheByPhone[phone]; ok {
		return rb
	}
	rb := container.NewRingBuffer(RingCapacity)
	cache.cacheByPhone[phone] = rb
	return cache.cacheByPhone[phone]
}

func (cache *GeoCache) GetGeoLatestByPhone(phone string) (*model.DeviceGeo, error) {
	rb := cache.GetGeoRingByPhone(phone)
	if latest, ok := rb.Latest().(*model.DeviceGeo); ok {
		return latest, nil
	}
	return nil, ErrGisNotFound
}

func (cache *GeoCache) DelGeoByPhone(phone string) {
	cache.mutex.Lock()
	defer cache.mutex.Unlock()
	delete(cache.cacheByPhone, phone)
}
