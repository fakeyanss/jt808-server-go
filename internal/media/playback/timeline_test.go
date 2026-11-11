package playback

import "testing"

func TestTimelineNormalizesWrapMonotonically(t *testing.T) {
	timeline := &Timeline{}
	// 模拟 32 位毫秒时间戳回绕：0xFFFFFFF0 -> 0x00000010。
	inputs := []uint64{0xFFFFFFF0, 0xFFFFFFF8, 0x00000010, 0x00000020, 0x00000008}
	expected := []uint64{0, 8, 32, 48, 48}
	var last uint64
	for i, raw := range inputs {
		got := timeline.Normalize(raw)
		if got != expected[i] {
			t.Fatalf("normalize(%#x) = %d, want %d", raw, got, expected[i])
		}
		if got < last {
			t.Fatalf("timestamp went backwards: %d -> %d", last, got)
		}
		last = got
	}
}

func TestTimelineIgnoresBackwardsJump(t *testing.T) {
	timeline := &Timeline{}
	if got := timeline.Normalize(1000); got != 0 {
		t.Fatalf("first normalize = %d, want 0", got)
	}
	if got := timeline.Normalize(900); got != 0 {
		t.Fatalf("backwards normalize = %d, want 0", got)
	}
	if got := timeline.Normalize(1100); got != 100 {
		t.Fatalf("after backward jump normalize = %d, want 100", got)
	}
}
