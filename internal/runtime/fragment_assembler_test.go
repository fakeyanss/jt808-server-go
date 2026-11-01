package runtime

import (
	"context"
	"fmt"
	"sync"
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

func TestFragmentAssemblerConcurrentIndependentMessages(t *testing.T) {
	assembler := NewFragmentAssembler()
	ctx := context.Background()
	const messages = 64

	results := make(chan bool, messages)
	var wg sync.WaitGroup
	for i := 0; i < messages; i++ {
		wg.Add(1)
		go func(i int) {
			defer wg.Done()
			base := uint16(i*4 + 1)
			phone := fmt.Sprintf("188%08d", i)
			completed := false
			for part := uint16(1); part <= 3; part++ {
				completed = assembler.CacheSegment(ctx, &model.Segment{
					Phone:    phone,
					MsgID:    0x0200,
					SegTotal: 3,
					SegNo:    part,
					Data:     []byte{byte('A' + part)},
				}, base+part-1)
			}
			results <- completed
		}(i)
	}
	wg.Wait()
	close(results)

	for completed := range results {
		require.True(t, completed, "all fragments should reassemble for each independent message")
	}
}

func TestFragmentAssemblerInvalidFragments(t *testing.T) {
	assembler := NewFragmentAssembler()
	ctx := context.Background()

	require.False(t, assembler.CacheSegment(ctx, nil, 1))
	require.False(t, assembler.CacheSegment(ctx, &model.Segment{Phone: "p", MsgID: 0x0200, SegTotal: 3, SegNo: 4}, 1))
	require.False(t, assembler.CacheSegment(ctx, &model.Segment{Phone: "p", MsgID: 0x0200, SegTotal: 3, SegNo: 1}, 1),
		"empty payload fragments must be rejected")
	require.Nil(t, assembler.MissingIndexes(ctx, nil, 1))
}
