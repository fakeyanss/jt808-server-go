package storage

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
