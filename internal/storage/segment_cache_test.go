package storage

import (
	"context"
	"testing"

	"github.com/fakeyanss/jt808-server-go/internal/protocol/model"
	"github.com/stretchr/testify/require"
)

func TestFragmentAssemblerHandlesOutOfOrderAndDuplicateFragments(t *testing.T) {
	assembler := newMemoryFragmentAssembler()
	ctx := context.Background()

	third := &model.Segment{Phone: "18800000001", MsgID: 0x0200, SegTotal: 3, SegNo: 3, Data: []byte("C")}
	require.False(t, assembler.CacheSegment(ctx, third, 11))
	first := &model.Segment{Phone: "18800000001", MsgID: 0x0200, SegTotal: 3, SegNo: 1, Data: []byte("A")}
	require.False(t, assembler.CacheSegment(ctx, first, 9))
	require.Equal(t, []uint16{2}, assembler.MissingIndexes(ctx, first, 9))
	duplicate := &model.Segment{Phone: "18800000001", MsgID: 0x0200, SegTotal: 3, SegNo: 1, Data: []byte("wrong")}
	require.False(t, assembler.CacheSegment(ctx, duplicate, 9))
	require.Equal(t, []uint16{2}, assembler.MissingIndexes(ctx, third, 11))
	second := &model.Segment{Phone: "18800000001", MsgID: 0x0200, SegTotal: 3, SegNo: 2, Data: []byte("B")}
	require.True(t, assembler.CacheSegment(ctx, second, 10))
	require.Equal(t, []byte("ABC"), second.Data)
	require.Empty(t, assembler.MissingIndexes(ctx, second, 10))
	require.False(t, assembler.CacheSegment(ctx, &model.Segment{Phone: "18800000001", MsgID: 0x0200, SegTotal: 2, SegNo: 3}, 10))
}

func TestFirstFragmentSerialHandlesSerialWraparound(t *testing.T) {
	require.Equal(t, uint16(65534), FirstFragmentSerial(1, 3))
	require.Equal(t, uint16(65535), FirstFragmentSerial(1, 2))
	require.Equal(t, uint16(17), FirstFragmentSerial(19, 3))
}
