package storage

import (
	"testing"

	"github.com/fakeyanss/jt808-server-go/internal/protocol/model"
	"github.com/stretchr/testify/require"
)

func TestCacheSegmentHandlesOutOfOrderAndDuplicateFragments(t *testing.T) {
	cache := getSegmentCache()
	cache.mutex.Lock()
	cache.cacheByKey = make(map[string]*segmentState)
	cache.cachedBytes = 0
	cache.mutex.Unlock()

	third := &model.Segment{Phone: "18800000001", MsgID: 0x0200, SegTotal: 3, SegNo: 3, Data: []byte("C")}
	require.False(t, CacheSegment(third, 11))
	first := &model.Segment{Phone: "18800000001", MsgID: 0x0200, SegTotal: 3, SegNo: 1, Data: []byte("A")}
	require.False(t, CacheSegment(first, 9))
	require.Equal(t, []uint16{2}, MissingSegmentIndexes(first, 9))
	duplicate := &model.Segment{Phone: "18800000001", MsgID: 0x0200, SegTotal: 3, SegNo: 1, Data: []byte("wrong")}
	require.False(t, CacheSegment(duplicate, 9))
	require.Equal(t, []uint16{2}, MissingSegmentIndexes(third, 11))
	second := &model.Segment{Phone: "18800000001", MsgID: 0x0200, SegTotal: 3, SegNo: 2, Data: []byte("B")}
	require.True(t, CacheSegment(second, 10))
	require.Equal(t, []byte("ABC"), second.Data)
	require.Empty(t, MissingSegmentIndexes(second, 10))
	require.False(t, CacheSegment(&model.Segment{Phone: "18800000001", MsgID: 0x0200, SegTotal: 2, SegNo: 3}, 10))
}

func TestFirstFragmentSerialHandlesSerialWraparound(t *testing.T) {
	require.Equal(t, uint16(65534), FirstFragmentSerial(1, 3))
	require.Equal(t, uint16(65535), FirstFragmentSerial(1, 2))
	require.Equal(t, uint16(17), FirstFragmentSerial(19, 3))
}
