package repository

import (
	"context"
	"fmt"
	"sync"
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/fakeyanss/jt808-server-go/internal/protocol/model"
)

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
