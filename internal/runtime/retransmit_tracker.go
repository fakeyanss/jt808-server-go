package runtime

import (
	"context"
	"fmt"
	"sync"
	"time"
)

type sentSegmentState struct {
	total     uint16
	frames    [][]byte
	bytes     int
	updatedAt time.Time
}

// memoryRetransmitTracker 是 RetransmitTracker 的内存实现。
type memoryRetransmitTracker struct {
	mu      sync.Mutex
	entries map[string]*sentSegmentState
	media   map[string]mediaSegmentLink
	bytes   int
}

type mediaSegmentLink struct {
	serial    uint16
	updatedAt time.Time
}

// NewRetransmitTracker 创建独立的下行重传跟踪器。
func NewRetransmitTracker() RetransmitTracker {
	return newMemoryRetransmitTracker()
}

func newMemoryRetransmitTracker() *memoryRetransmitTracker {
	return &memoryRetransmitTracker{entries: make(map[string]*sentSegmentState), media: make(map[string]mediaSegmentLink)}
}

func sentSegmentKey(phone string, originalSerial uint16) string {
	return fmt.Sprintf("%s/%04x", phone, originalSerial)
}

// LinkMedia 把多媒体 ID 关联到保留其编码分片的 JT808 消息流水号，供 0x8800 重传。
func (cache *memoryRetransmitTracker) LinkMedia(_ context.Context, phone string, mediaID uint32, serial uint16) {
	if phone == "" || mediaID == 0 {
		return
	}
	cache.mu.Lock()
	defer cache.mu.Unlock()
	cache.media[fmt.Sprintf("%s/%08x", phone, mediaID)] = mediaSegmentLink{serial: serial, updatedAt: time.Now()}
}

// MediaSerial 返回多媒体 ID 关联的保留流水号。
func (cache *memoryRetransmitTracker) MediaSerial(_ context.Context, phone string, mediaID uint32) (uint16, bool) {
	cache.mu.Lock()
	defer cache.mu.Unlock()
	key := fmt.Sprintf("%s/%08x", phone, mediaID)
	link, ok := cache.media[key]
	if !ok || time.Since(link.updatedAt) > segmentCacheTTL {
		delete(cache.media, key)
		return 0, false
	}
	return link.serial, true
}

// CacheFrame 保留已发送的编码帧，使 0x8003 请求能原样重传。
func (cache *memoryRetransmitTracker) CacheFrame(_ context.Context, phone string, originalSerial, index, total uint16, frame []byte) {
	if phone == "" || total == 0 || total > maxSegmentCount || index == 0 || index > total || len(frame) == 0 || len(frame) > maxSegmentCacheBytes {
		return
	}
	key := sentSegmentKey(phone, originalSerial)
	cache.mu.Lock()
	defer cache.mu.Unlock()
	now := time.Now()
	for k, state := range cache.entries {
		if now.Sub(state.updatedAt) > segmentCacheTTL {
			cache.bytes -= state.bytes
			delete(cache.entries, k)
		}
	}
	state := cache.entries[key]
	if state == nil || state.total != total {
		if state != nil {
			cache.bytes -= state.bytes
		}
		if len(cache.entries) >= maxSegmentCacheEntries {
			cache.evictOldest(key)
		}
		state = &sentSegmentState{total: total, frames: make([][]byte, total)}
		cache.entries[key] = state
	}
	idx := int(index - 1)
	var oldLen int
	if state.frames[idx] != nil {
		oldLen = len(state.frames[idx])
	}
	// 容量判定按替换后的净字节数计算；容量不足时整体拒绝，保持旧帧与
	// 字节计数不变（此前先扣旧帧字节再 return，导致帧还在、计数已扣）。
	if cache.bytes-oldLen+len(frame) > maxSegmentCacheBytes {
		return
	}
	if state.frames[idx] != nil {
		state.bytes -= oldLen
		cache.bytes -= oldLen
	}
	state.frames[idx] = append([]byte(nil), frame...)
	state.bytes += len(frame)
	cache.bytes += len(frame)
	state.updatedAt = now
}

func (cache *memoryRetransmitTracker) evictOldest(excludeKey string) {
	var oldestKey string
	var oldest time.Time
	for key, state := range cache.entries {
		if key == excludeKey {
			continue
		}
		if oldestKey == "" || state.updatedAt.Before(oldest) {
			oldestKey = key
			oldest = state.updatedAt
		}
	}
	if oldestKey == "" {
		return
	}
	cache.bytes -= cache.entries[oldestKey].bytes
	delete(cache.entries, oldestKey)
}

// Frames 按请求的分片序号顺序返回副本；缺失或已过期的条目返回 nil。
func (cache *memoryRetransmitTracker) Frames(_ context.Context, phone string, originalSerial uint16, packetIDs []uint16) [][]byte {
	key := sentSegmentKey(phone, originalSerial)
	cache.mu.Lock()
	defer cache.mu.Unlock()
	state := cache.entries[key]
	if state == nil || time.Since(state.updatedAt) > segmentCacheTTL {
		return nil
	}
	frames := make([][]byte, 0, len(packetIDs))
	for _, id := range packetIDs {
		if id == 0 || id > state.total || state.frames[id-1] == nil {
			frames = append(frames, nil)
			continue
		}
		frames = append(frames, append([]byte(nil), state.frames[id-1]...))
	}
	return frames
}

// 编译期确认内存实现满足接口。
var _ RetransmitTracker = (*memoryRetransmitTracker)(nil)
