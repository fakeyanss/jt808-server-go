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

// 同号多条命令时，只完成应答流水号对应的那一条，未命中的不得误清。
func TestSessionAcknowledgeSerialMatchesOnlyAnsweredSerial(t *testing.T) {
	left, _ := net.Pipe()
	defer left.Close()
	const phone = "18800000000"
	session := &Session{Conn: left, AckTimeout: time.Hour, MaxRetries: 1}
	defer session.ClosePending()

	session.TrackReliable(0x8103, 10, phone, []byte("first"))
	session.TrackReliable(0x8103, 11, phone, []byte("second"))
	require.Equal(t, 2, session.PendingCount())

	// 先回第二条应答，第一条仍在等待。
	require.True(t, session.AcknowledgeSerial(11, phone))
	require.Equal(t, 1, session.PendingCount())
	require.True(t, session.Acknowledge(0x8103, 10))

	// 重复应答不得清除任何等待项。
	require.False(t, session.AcknowledgeSerial(11, phone))
	require.Equal(t, 0, session.PendingCount())
}

// 错手机号、错会话、找不到等待项的应答都返回 false 且不清除任何等待项。
func TestSessionAcknowledgeSerialIgnoresMismatchedResponse(t *testing.T) {
	left, _ := net.Pipe()
	defer left.Close()
	const phone = "18800000000"
	session := &Session{Conn: left, AckTimeout: time.Hour, MaxRetries: 1}
	defer session.ClosePending()

	session.TrackReliable(0x8103, 12, phone, []byte("frame"))

	require.False(t, session.AcknowledgeSerial(12, "18800000001"), "wrong phone must not clear pending")
	require.Equal(t, 1, session.PendingCount())

	other := &Session{AckTimeout: time.Hour, MaxRetries: 1}
	defer other.ClosePending()
	require.False(t, other.AcknowledgeSerial(12, phone), "wrong session must not clear pending")
	require.Equal(t, 1, session.PendingCount())

	require.False(t, session.Acknowledge(0x8104, 12), "unmatched message id must not clear pending")
	require.False(t, session.AcknowledgeSerial(99, phone), "unknown serial must not clear pending")
	require.Equal(t, 1, session.PendingCount())
}

// 协议体不含流水号的应答（0x0608/0x0702/0x8800）仅在唯一候选时清算。
func TestSessionAcknowledgeTypedResponseRequiresUniquePending(t *testing.T) {
	left, _ := net.Pipe()
	defer left.Close()
	const phone = "18800000000"
	session := &Session{Conn: left, AckTimeout: time.Hour, MaxRetries: 1}
	defer session.ClosePending()

	session.TrackReliable(0x8608, 21, phone, []byte("q1"))
	session.TrackReliable(0x8608, 22, phone, []byte("q2"))
	require.False(t, session.AcknowledgeTypedResponse(0x0608, phone), "ambiguous type match must not clear any pending")
	require.Equal(t, 2, session.PendingCount())

	require.True(t, session.Acknowledge(0x8608, 21))
	require.True(t, session.AcknowledgeTypedResponse(0x0608, phone))
	require.Equal(t, 0, session.PendingCount())
}

// 超时后按既有重传次数清理等待项。
func TestSessionPendingTimeoutCleansEntry(t *testing.T) {
	left, right := net.Pipe()
	defer left.Close()
	defer right.Close()
	const phone = "18800000000"
	session := &Session{Conn: left, AckTimeout: 20 * time.Millisecond, MaxRetries: 1}
	defer session.ClosePending()

	go func() {
		buf := make([]byte, 64)
		for {
			if _, err := right.Read(buf); err != nil {
				return
			}
		}
	}()

	session.TrackReliable(0x8103, 13, phone, []byte("frame"))
	require.Eventually(t, func() bool { return session.PendingCount() == 0 }, 2*time.Second, 5*time.Millisecond)
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
