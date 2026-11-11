package media

import (
	"bytes"
	"errors"
	"testing"
	"time"
)

func TestAcquireConflictThenReleaseAndRecreate(t *testing.T) {
	server := NewServer(Config{Dir: t.TempDir(), RecordMode: RecordModeOff})
	t.Cleanup(server.Stop)

	first, err := server.Acquire(SessionRequest{
		Phone: "13800138000", Channel: 1, Kind: StreamKindLive, TTL: time.Minute,
	})
	if err != nil {
		t.Fatalf("first acquire: %v", err)
	}

	// 同一 (phone, channel) 同时只允许一个会话：不顶替、不排队。
	_, err = server.Acquire(SessionRequest{
		Phone: "13800138000", Channel: 1, Kind: StreamKindTerminalPlayback, TTL: time.Minute,
	})
	var conflict *SessionConflictError
	if !errors.As(err, &conflict) {
		t.Fatalf("second acquire error = %v, want SessionConflictError", err)
	}
	if conflict.Code() != "session_conflict" {
		t.Fatalf("conflict code = %q", conflict.Code())
	}
	if conflict.ExistingSessionID != first.ID || conflict.ExistingKind != StreamKindLive {
		t.Fatalf("conflict existing = %s/%s, want %s/%s",
			conflict.ExistingSessionID, conflict.ExistingKind, first.ID, StreamKindLive)
	}
	if conflict.ExistingExpiresAt.IsZero() {
		t.Fatal("conflict must expose the existing session expiry")
	}

	if err := server.Release(first.ID); err != nil {
		t.Fatalf("release: %v", err)
	}
	second, err := server.Acquire(SessionRequest{
		Phone: "13800138000", Channel: 1, Kind: StreamKindTerminalPlayback, TTL: time.Minute,
	})
	if err != nil {
		t.Fatalf("recreate after release: %v", err)
	}
	if second.ID == first.ID {
		t.Fatalf("recreated session reused id %s", second.ID)
	}
	if second.Kind != StreamKindTerminalPlayback {
		t.Fatalf("recreated kind = %s", second.Kind)
	}
}

func TestGOPRingsAreIsolated(t *testing.T) {
	live := newGOPRing(DefaultRingBytes, DefaultRingDuration)
	playback := newGOPRing(DefaultRingBytes, DefaultRingDuration)

	liveKey := []byte{0x00, 0x00, 0x00, 0x01, 0x65, 0x11}
	liveP := []byte{0x00, 0x00, 0x00, 0x01, 0x41, 0x12}
	playbackKey := []byte{0x00, 0x00, 0x00, 0x01, 0x65, 0x21}
	playbackP := []byte{0x00, 0x00, 0x00, 0x01, 0x41, 0x22}

	live.push(0, true, liveKey)
	live.push(40, false, liveP)
	playback.push(1000, true, playbackKey)
	playback.push(1040, false, playbackP)

	liveFrames, _, dropped := live.framesSince(0)
	if dropped {
		t.Fatal("live ring dropped unexpectedly")
	}
	playbackFrames, _, dropped := playback.framesSince(0)
	if dropped {
		t.Fatal("playback ring dropped unexpectedly")
	}
	if len(liveFrames) != 2 || len(playbackFrames) != 2 {
		t.Fatalf("frame counts = %d/%d, want 2/2", len(liveFrames), len(playbackFrames))
	}
	if !bytes.Equal(liveFrames[0].Data, liveKey) || !bytes.Equal(liveFrames[1].Data, liveP) {
		t.Fatal("live ring contents were polluted")
	}
	if !bytes.Equal(playbackFrames[0].Data, playbackKey) || !bytes.Equal(playbackFrames[1].Data, playbackP) {
		t.Fatal("playback ring contents were polluted")
	}
	if liveFrames[0].Data[5] == playbackFrames[0].Data[5] {
		t.Fatal("two rings returned the same payload")
	}
}
