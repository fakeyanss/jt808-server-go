package runtime

import (
	"context"
	"testing"

	"github.com/stretchr/testify/require"
)

func TestRetransmitTrackerReturnsRequestedFramesInOrderAsCopies(t *testing.T) {
	tracker := newMemoryRetransmitTracker()
	ctx := context.Background()
	phone := "18800000001"
	tracker.CacheFrame(ctx, phone, 77, 1, 3, []byte("frame-1"))
	tracker.CacheFrame(ctx, phone, 77, 2, 3, []byte("frame-2"))
	tracker.CacheFrame(ctx, phone, 77, 3, 3, []byte("frame-3"))

	frames := tracker.Frames(ctx, phone, 77, []uint16{3, 1})
	require.Equal(t, [][]byte{[]byte("frame-3"), []byte("frame-1")}, frames)
	frames[0][0] = 'X'
	require.Equal(t, []byte("frame-3"), tracker.Frames(ctx, phone, 77, []uint16{3})[0])
	require.Nil(t, tracker.Frames(ctx, phone, 88, []uint16{1}))
}

// 重写超大分片被容量拒绝时，旧帧与字节计数必须保持不变，后续淘汰精确扣减。
func TestCacheFrameRejectOversizeRewriteKeepsAccounting(t *testing.T) {
	tracker := newMemoryRetransmitTracker()
	ctx := context.Background()

	// 先写 1023 个占位 entry，再写目标 entry，刚好填满 maxSegmentCacheEntries。
	for i := 0; i < maxSegmentCacheEntries-1; i++ {
		tracker.CacheFrame(ctx, "18800000002", uint16(i+1), 1, 1, []byte{byte(i)})
	}
	tracker.CacheFrame(ctx, "18800000001", 7, 1, 1, []byte{0xAA})
	require.Len(t, tracker.entries, maxSegmentCacheEntries)
	require.Equal(t, maxSegmentCacheEntries, tracker.bytes)

	// 用超大帧重写同一分片：净增超出容量上限，应整体拒绝且状态不变。
	tracker.CacheFrame(ctx, "18800000001", 7, 1, 1, make([]byte, maxSegmentCacheBytes))
	require.Equal(t, []byte{0xAA}, tracker.Frames(ctx, "18800000001", 7, []uint16{1})[0], "old frames must be kept after a rejected write")
	require.Equal(t, maxSegmentCacheEntries, tracker.bytes, "global byte count must not shrink after a rejected write")
	require.Equal(t, 1, tracker.entries[sentSegmentKey("18800000001", 7)].bytes, "entry byte count must not shrink after a rejected write")

	// 正常重写（净增不超容量）仍应成功并正确更新计数。
	tracker.CacheFrame(ctx, "18800000001", 7, 1, 1, []byte{0xBB, 0xBB})
	require.Equal(t, []byte{0xBB, 0xBB}, tracker.Frames(ctx, "18800000001", 7, []uint16{1})[0])
	require.Equal(t, maxSegmentCacheEntries+1, tracker.bytes)

	// 写入新 key 触发淘汰最老 entry（最早写入的 18800000002/0001），字节精确扣减。
	tracker.CacheFrame(ctx, "18800000003", 1, 1, 1, []byte{0xCC})
	require.Len(t, tracker.entries, maxSegmentCacheEntries)
	total := 0
	for _, state := range tracker.entries {
		total += state.bytes
	}
	require.Equal(t, total, tracker.bytes, "global byte count must equal the sum of entry byte counts after eviction")
	require.Equal(t, maxSegmentCacheEntries+1, tracker.bytes)
	require.Nil(t, tracker.Frames(ctx, "18800000002", 1, []uint16{1}), "the oldest entry should have been evicted")
}
