package storage

import (
	"fmt"
	"sync"
	"time"

	"github.com/fakeyanss/jt808-server-go/internal/protocol/model"
)

const segmentCacheTTL = 2 * time.Minute
const maxSegmentCount = 4096
const maxSegmentCacheEntries = 1024
const maxSegmentCacheBytes = 16 << 20

type segmentState struct {
	total     uint16
	parts     [][]byte
	received  int
	bytes     int
	updatedAt time.Time
}

type SegmentCache struct {
	cacheByKey  map[string]*segmentState
	mutex       *sync.Mutex
	cachedBytes int
}

var segmentCacheSingleton *SegmentCache
var segmentCacheInitOnce sync.Once

func getSegmentCache() *SegmentCache {
	segmentCacheInitOnce.Do(func() {
		segmentCacheSingleton = &SegmentCache{
			cacheByKey: make(map[string]*segmentState),
			mutex:      &sync.Mutex{},
		}
	})
	return segmentCacheSingleton
}

// CacheSegment stores fragments by device, message ID and original message serial.
// It returns true only after all distinct fragment indexes have arrived, regardless
// of their arrival order. Duplicate fragments do not duplicate their payload.
func CacheSegment(seg *model.Segment, serialNumber uint16) bool {
	if seg == nil || seg.SegTotal == 0 || seg.SegNo == 0 || seg.SegNo > seg.SegTotal || len(seg.Data) == 0 {
		return false
	}
	cache := getSegmentCache()
	// JT808 assigns consecutive serial numbers to fragments. Normalize each
	// fragment to the first fragment's serial so out-of-order arrivals join.
	firstSerial := serialNumber - (seg.SegNo - 1)
	key := fmt.Sprintf("%s/%04x/%04x", seg.Phone, seg.MsgID, firstSerial)
	cache.mutex.Lock()
	defer cache.mutex.Unlock()

	now := time.Now()
	for k, state := range cache.cacheByKey {
		if now.Sub(state.updatedAt) > segmentCacheTTL {
			cache.cachedBytes -= state.bytes
			delete(cache.cacheByKey, k)
		}
	}
	if seg.SegTotal > maxSegmentCount || len(seg.Data) > maxSegmentCacheBytes {
		return false
	}

	state := cache.cacheByKey[key]
	if state == nil || state.total != seg.SegTotal {
		if state != nil {
			cache.cachedBytes -= state.bytes
		}
		if len(cache.cacheByKey) >= maxSegmentCacheEntries {
			cache.evictOldest(key)
		}
		state = &segmentState{total: seg.SegTotal, parts: make([][]byte, seg.SegTotal)}
		cache.cacheByKey[key] = state
	}
	state.updatedAt = now
	idx := int(seg.SegNo - 1)
	if state.parts[idx] == nil {
		for cache.cachedBytes+len(seg.Data) > maxSegmentCacheBytes {
			if !cache.evictOldest(key) {
				return false
			}
		}
		state.parts[idx] = append([]byte(nil), seg.Data...)
		state.received++
		state.bytes += len(seg.Data)
		cache.cachedBytes += len(seg.Data)
	}
	if state.received != int(state.total) {
		return false
	}

	assembled := make([]byte, 0)
	for _, part := range state.parts {
		assembled = append(assembled, part...)
	}
	seg.Data = assembled
	cache.cachedBytes -= state.bytes
	delete(cache.cacheByKey, key)
	return true
}

func (cache *SegmentCache) evictOldest(excludeKey string) bool {
	var oldestKey string
	var oldest time.Time
	for key, state := range cache.cacheByKey {
		if key == excludeKey {
			continue
		}
		if oldestKey == "" || state.updatedAt.Before(oldest) {
			oldestKey = key
			oldest = state.updatedAt
		}
	}
	if oldestKey == "" {
		return false
	}
	cache.cachedBytes -= cache.cacheByKey[oldestKey].bytes
	delete(cache.cacheByKey, oldestKey)
	return true
}
