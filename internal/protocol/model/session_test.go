package model

import (
	"io"
	"net"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
)

func TestSessionAcknowledgeStopsReliableRetransmission(t *testing.T) {
	left, right := net.Pipe()
	defer left.Close()
	defer right.Close()
	session := &Session{Conn: left}
	session.TrackReliable(0x0200, 42, "18800000000", []byte("frame"))
	session.Acknowledge(0x0200, 42)

	session.pendingMu.Lock()
	defer session.pendingMu.Unlock()
	require.Empty(t, session.pending)
}

func TestSessionRetransmitsSameFrameUntilAcknowledged(t *testing.T) {
	left, right := net.Pipe()
	defer left.Close()
	defer right.Close()
	session := &Session{Conn: left, AckTimeout: 25 * time.Millisecond, MaxRetries: 1}
	frame := []byte("frame")

	go func() { _ = session.WriteFrame(frame) }()
	first := make([]byte, len(frame))
	_, err := io.ReadFull(right, first)
	require.NoError(t, err)
	require.Equal(t, frame, first)

	session.TrackReliable(0x0200, 42, "18800000000", frame)
	second := make([]byte, len(frame))
	_ = right.SetReadDeadline(time.Now().Add(time.Second))
	_, err = io.ReadFull(right, second)
	require.NoError(t, err)
	require.Equal(t, frame, second)
	session.Acknowledge(0x0200, 42)
}

func TestSessionTracksOnlyMessagesRequiringAcknowledgment(t *testing.T) {
	left, right := net.Pipe()
	defer left.Close()
	defer right.Close()
	session := &Session{Conn: left}
	session.TrackReliable(0x8001, 1, "18800000000", []byte("ack"))
	session.TrackReliable(0x8100, 2, "18800000000", []byte("registration response"))
	require.Nil(t, session.pending)
}
