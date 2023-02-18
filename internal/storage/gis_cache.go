package storage

import (
	"sync"

	"github.com/pkg/errors"

	"github.com/fakeyanss/jt808-server-go/internal/protocol/model"
	"github.com/fakeyanss/jt808-server-go/pkg/container"
)

const RingCapacity int32 = 100

var ErrGisNotFound = errors.New("gis not found")

type GisCache struct {
	cacheByPhone map[string]*container.RingBuffer
	mutex        *sync.Mutex
}

var gisCacheSingleton *GisCache
var gisCacheInitOnce sync.Once

func GetGisCache() *GisCache {
	gisCacheInitOnce.Do(func() {
		gisCacheSingleton = &GisCache{
			cacheByPhone: make(map[string]*container.RingBuffer),
			mutex:        &sync.Mutex{},
		}
	})
	return gisCacheSingleton
}

func (cache *GisCache) GetGisRingByPhone(phone string) *container.RingBuffer {
	cache.mutex.Lock()
	defer cache.mutex.Unlock()
	if rb, ok := cache.cacheByPhone[phone]; ok {
		return rb
	}
	rb := container.NewRingBuffer(RingCapacity)
	cache.cacheByPhone[phone] = rb
	return cache.cacheByPhone[phone]
}

func (cache *GisCache) GetGisLatestByPhone(id string) (*model.GISMeta, error) {
	rb := cache.GetGisRingByPhone(id)
	if latest, ok := rb.Latest().(*model.GISMeta); ok {
		return latest, nil
	}
	return nil, ErrGisNotFound
}

func (cache *GisCache) DelGisByPhone(id string) {
	cache.mutex.Lock()
	defer cache.mutex.Unlock()
	delete(cache.cacheByPhone, id)
}
