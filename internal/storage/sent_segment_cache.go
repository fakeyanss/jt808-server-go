package storage

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

// sentSegmentCache 是 RetransmitTracker 的内存实现。
type sentSegmentCache struct {
	mu      sync.Mutex
	entries map[string]*sentSegmentState
	media   map[string]mediaSegmentLink
	bytes   int
}

type mediaSegmentLink struct {
	serial    uint16
	updatedAt time.Time
}

var sentSegments = newSentSegmentCache()

func newSentSegmentCache() *sentSegmentCache {
	return &sentSegmentCache{entries: make(map[string]*sentSegmentState), media: make(map[string]mediaSegmentLink)}
}

func sentSegmentKey(phone string, originalSerial uint16) string {
	return fmt.Sprintf("%s/%04x", phone, originalSerial)
}

// LinkMedia links a multimedia ID to the JT808 message serial whose encoded
// fragments the terminal simulator retained for 0x8800 retransmits.
func (cache *sentSegmentCache) LinkMedia(_ context.Context, phone string, mediaID uint32, serial uint16) {
	if phone == "" || mediaID == 0 {
		return
	}
	cache.mu.Lock()
	defer cache.mu.Unlock()
	cache.media[fmt.Sprintf("%s/%08x", phone, mediaID)] = mediaSegmentLink{serial: serial, updatedAt: time.Now()}
}

// MediaSerial returns the retained serial for a multimedia ID.
func (cache *sentSegmentCache) MediaSerial(_ context.Context, phone string, mediaID uint32) (uint16, bool) {
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

// CacheFrame retains simulator frames so a 0x8003 request can retransmit the
// original encoded fragment verbatim.
func (cache *sentSegmentCache) CacheFrame(_ context.Context, phone string, originalSerial, index, total uint16, frame []byte) {
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
	if state.frames[idx] != nil {
		state.bytes -= len(state.frames[idx])
		cache.bytes -= len(state.frames[idx])
	}
	if cache.bytes+len(frame) > maxSegmentCacheBytes {
		return
	}
	state.frames[idx] = append([]byte(nil), frame...)
	state.bytes += len(frame)
	cache.bytes += len(frame)
	state.updatedAt = now
}

func (cache *sentSegmentCache) evictOldest(excludeKey string) {
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

// Frames returns copies in the requested packet ID order.
// Missing or expired cache entries return nil.
func (cache *sentSegmentCache) Frames(_ context.Context, phone string, originalSerial uint16, packetIDs []uint16) [][]byte {
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

// 以下为兼容既有调用点的包级薄封装。

// CacheMediaSegment 关联多媒体 ID 与分片消息流水号。
func CacheMediaSegment(phone string, mediaID uint32, serial uint16) {
	sentSegments.LinkMedia(context.Background(), phone, mediaID, serial)
}

// GetMediaSegmentSerial 查询多媒体 ID 关联的消息流水号。
func GetMediaSegmentSerial(phone string, mediaID uint32) (uint16, bool) {
	return sentSegments.MediaSerial(context.Background(), phone, mediaID)
}

// CacheSentSegmentFrame 保留已发送的分片帧，供 0x8003 原样重传。
func CacheSentSegmentFrame(phone string, originalSerial, index, total uint16, frame []byte) {
	sentSegments.CacheFrame(context.Background(), phone, originalSerial, index, total, frame)
}

// GetSentSegmentFrames 按请求序号返回分片帧副本。
func GetSentSegmentFrames(phone string, originalSerial uint16, packetIDs []uint16) [][]byte {
	return sentSegments.Frames(context.Background(), phone, originalSerial, packetIDs)
}
