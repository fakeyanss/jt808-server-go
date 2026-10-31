package storage

import (
	"testing"

	"github.com/stretchr/testify/require"
)

func TestSentSegmentCacheReturnsRequestedFramesInOrderAsCopies(t *testing.T) {
	phone := "18800000001"
	CacheSentSegmentFrame(phone, 77, 1, 3, []byte("frame-1"))
	CacheSentSegmentFrame(phone, 77, 2, 3, []byte("frame-2"))
	CacheSentSegmentFrame(phone, 77, 3, 3, []byte("frame-3"))

	frames := GetSentSegmentFrames(phone, 77, []uint16{3, 1})
	require.Equal(t, [][]byte{[]byte("frame-3"), []byte("frame-1")}, frames)
	frames[0][0] = 'X'
	require.Equal(t, []byte("frame-3"), GetSentSegmentFrames(phone, 77, []uint16{3})[0])
	require.Nil(t, GetSentSegmentFrames(phone, 88, []uint16{1}))
}
