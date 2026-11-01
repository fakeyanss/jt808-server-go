package runtime

import (
	"context"
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

// memoryFragmentAssembler 是 FragmentAssembler 的内存实现。
type memoryFragmentAssembler struct {
	cacheByKey  map[string]*segmentState
	mutex       *sync.Mutex
	cachedBytes int
}

// NewFragmentAssembler 创建独立的内存分包重组器。
func NewFragmentAssembler() FragmentAssembler {
	return newMemoryFragmentAssembler()
}

func newMemoryFragmentAssembler() *memoryFragmentAssembler {
	return &memoryFragmentAssembler{
		cacheByKey: make(map[string]*segmentState),
		mutex:      &sync.Mutex{},
	}
}

// CacheSegment 按设备、消息 ID 与原消息流水号缓存分片。
// 只有全部分片序号都到达时才返回 true，与分片到达顺序无关；重复分片不会重复占用负载。
func (cache *memoryFragmentAssembler) CacheSegment(_ context.Context, seg *model.Segment, serialNumber uint16) bool {
	if seg == nil || seg.SegTotal == 0 || seg.SegNo == 0 || seg.SegNo > seg.SegTotal || len(seg.Data) == 0 {
		return false
	}
	// JT808 为连续分片分配连续流水号。把每个分片归一化到首个分片的流水号，
	// 使乱序到达的分片也能归入同一个重组槽位。
	firstSerial := FirstFragmentSerial(serialNumber, seg.SegNo)
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

// MissingIndexes 返回该分段消息仍缺失的 1 基分片序号，用于 0x8003 请求。
func (cache *memoryFragmentAssembler) MissingIndexes(_ context.Context, seg *model.Segment, serialNumber uint16) []uint16 {
	if seg == nil || seg.SegTotal == 0 || seg.SegNo == 0 || seg.SegNo > seg.SegTotal {
		return nil
	}
	firstSerial := FirstFragmentSerial(serialNumber, seg.SegNo)
	key := fmt.Sprintf("%s/%04x/%04x", seg.Phone, seg.MsgID, firstSerial)
	cache.mutex.Lock()
	defer cache.mutex.Unlock()
	state := cache.cacheByKey[key]
	if state == nil || state.total != seg.SegTotal || time.Since(state.updatedAt) > segmentCacheTTL {
		return nil
	}
	missing := make([]uint16, 0, int(state.total)-state.received)
	for idx, part := range state.parts {
		if part == nil {
			missing = append(missing, uint16(idx+1))
		}
	}
	return missing
}

func (cache *memoryFragmentAssembler) evictOldest(excludeKey string) bool {
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

// FirstFragmentSerial 把分片流水号换算回原消息流水号，处理 JT808 1..65535 的回绕。
func FirstFragmentSerial(serialNumber, fragmentIndex uint16) uint16 {
	for i := uint16(1); i < fragmentIndex; i++ {
		if serialNumber <= 1 {
			serialNumber = 65535
		} else {
			serialNumber--
		}
	}
	return serialNumber
}

// 编译期确认内存实现满足接口。
var _ FragmentAssembler = (*memoryFragmentAssembler)(nil)
