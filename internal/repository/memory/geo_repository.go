package memory

import (
	"context"
	"sync"

	"github.com/fakeyanss/jt808-server-go/internal/protocol/model"
	"github.com/fakeyanss/jt808-server-go/internal/repository"
	"github.com/fakeyanss/jt808-server-go/pkg/container"
)

const RingCapacity int32 = 100

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
//
// 以终端位置时间为单调基准：晚到的旧定位不覆盖更新值。时间相等视为同一
// 时刻，保留最后到达者；终端时钟回拨时，最新快照会停在回拨前的最大时间，
// 直到终端时间重新超过它，避免旧上报把新状态改回去。
func (cache *memoryGeoRepository) Append(_ context.Context, geo *model.DeviceGeo) error {
	if geo == nil {
		return nil
	}
	cache.mutex.Lock()
	defer cache.mutex.Unlock()

	rb := cache.ringLocked(geo.Phone)
	if latest, ok := rb.Latest().(*model.DeviceGeo); ok && latest != nil && geo.Time.Before(latest.Time) {
		return nil
	}
	rb.Write(geo)
	return nil
}

// Latest 返回设备最新一条定位。
func (cache *memoryGeoRepository) Latest(_ context.Context, phone string) (*model.DeviceGeo, error) {
	rb := cache.ring(phone)
	if latest, ok := rb.Latest().(*model.DeviceGeo); ok {
		return latest, nil
	}
	return nil, repository.ErrGisNotFound
}

// ring 返回设备环形缓冲；不存在时创建。
func (cache *memoryGeoRepository) ring(phone string) *container.RingBuffer {
	cache.mutex.Lock()
	defer cache.mutex.Unlock()
	return cache.ringLocked(phone)
}

// ringLocked 与 ring 相同，但要求调用方已持有 cache.mutex。
func (cache *memoryGeoRepository) ringLocked(phone string) *container.RingBuffer {
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

// NewGeoRepository 创建独立的内存定位热读仓储。
func NewGeoRepository() repository.GeoRepository { return newMemoryGeoRepository() }
