package storage

import (
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

var sentSegments = &sentSegmentCache{entries: make(map[string]*sentSegmentState), media: make(map[string]mediaSegmentLink)}

func sentSegmentKey(phone string, originalSerial uint16) string {
	return fmt.Sprintf("%s/%04x", phone, originalSerial)
}

// CacheMediaSegment links a multimedia ID to the JT808 message serial whose
// encoded fragments the terminal simulator retained for 0x8800 retransmits.
func CacheMediaSegment(phone string, mediaID uint32, serial uint16) {
	if phone == "" || mediaID == 0 {
		return
	}
	sentSegments.mu.Lock()
	defer sentSegments.mu.Unlock()
	sentSegments.media[fmt.Sprintf("%s/%08x", phone, mediaID)] = mediaSegmentLink{serial: serial, updatedAt: time.Now()}
}

func GetMediaSegmentSerial(phone string, mediaID uint32) (uint16, bool) {
	sentSegments.mu.Lock()
	defer sentSegments.mu.Unlock()
	key := fmt.Sprintf("%s/%08x", phone, mediaID)
	link, ok := sentSegments.media[key]
	if !ok || time.Since(link.updatedAt) > segmentCacheTTL {
		delete(sentSegments.media, key)
		return 0, false
	}
	return link.serial, true
}

// CacheSentSegmentFrame retains simulator frames so a 0x8003 request can
// retransmit the original encoded fragment verbatim.
func CacheSentSegmentFrame(phone string, originalSerial, index, total uint16, frame []byte) {
	if phone == "" || total == 0 || total > maxSegmentCount || index == 0 || index > total || len(frame) == 0 || len(frame) > maxSegmentCacheBytes {
		return
	}
	key := sentSegmentKey(phone, originalSerial)
	sentSegments.mu.Lock()
	defer sentSegments.mu.Unlock()
	now := time.Now()
	for k, state := range sentSegments.entries {
		if now.Sub(state.updatedAt) > segmentCacheTTL {
			sentSegments.bytes -= state.bytes
			delete(sentSegments.entries, k)
		}
	}
	state := sentSegments.entries[key]
	if state == nil || state.total != total {
		if state != nil {
			sentSegments.bytes -= state.bytes
		}
		if len(sentSegments.entries) >= maxSegmentCacheEntries {
			sentSegments.evictOldest(key)
		}
		state = &sentSegmentState{total: total, frames: make([][]byte, total)}
		sentSegments.entries[key] = state
	}
	idx := int(index - 1)
	if state.frames[idx] != nil {
		state.bytes -= len(state.frames[idx])
		sentSegments.bytes -= len(state.frames[idx])
	}
	if sentSegments.bytes+len(frame) > maxSegmentCacheBytes {
		return
	}
	state.frames[idx] = append([]byte(nil), frame...)
	state.bytes += len(frame)
	sentSegments.bytes += len(frame)
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

// GetSentSegmentFrames returns copies in the requested packet ID order.
// Missing or expired cache entries return nil.
func GetSentSegmentFrames(phone string, originalSerial uint16, packetIDs []uint16) [][]byte {
	key := sentSegmentKey(phone, originalSerial)
	sentSegments.mu.Lock()
	defer sentSegments.mu.Unlock()
	state := sentSegments.entries[key]
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
