package storage

import (
	"sync"

	"github.com/fakeYanss/jt808-server-go/internal/protocol/model"
	"github.com/fakeYanss/jt808-server-go/internal/util/ring"
)

const RingCapacity int32 = 100

type GisCache struct {
	cacheByID map[string]*ring.RingBuffer
	mutex     *sync.Mutex
}

var gisCacheSingleton *GisCache
var gisCacheInitOnce sync.Once

func GetGisCache() *GisCache {
	gisCacheInitOnce.Do(func() {
		gisCacheSingleton = &GisCache{
			cacheByID: make(map[string]*ring.RingBuffer),
			mutex:     &sync.Mutex{},
		}
	})
	return gisCacheSingleton
}

func (cache *GisCache) GetGisRingByID(id string) *ring.RingBuffer {
	cache.mutex.Lock()
	defer cache.mutex.Unlock()
	if rb, ok := cache.cacheByID[id]; ok {
		return rb
	}
	rb := ring.NewRingBuffer(RingCapacity)
	cache.cacheByID[id] = rb
	return cache.cacheByID[id]
}

func (cache *GisCache) GetGisLatestByID(id string) *model.GISMeta {
	rb := cache.GetGisRingByID(id)
	return rb.Latest().(*model.GISMeta)
}

func (cache *GisCache) DelGisByID(id string) {
	cache.mutex.Lock()
	defer cache.mutex.Unlock()
	delete(cache.cacheByID, id)
}
