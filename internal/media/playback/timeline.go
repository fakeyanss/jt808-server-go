package playback

import "time"

// maxUint32 是 1078 终端常见的 32 位毫秒时间戳回绕点。
const maxUint32 = uint64(1)<<32 - 1

// Timeline 把 1078 原始时间戳归一化为“相对会话起点的单调毫秒”。
//
// 1078 的时间戳字段是 8 字节，但大量终端实际只填 32 位毫秒：49.7 天回绕一次。
// 直接写入 FLV/RTMP 会在回绕后出现时间戳倒退，播放器卡死或反复重连。
// 这里按差值累加，并在检测到 32 位回绕时补齐 2^32；不做回退，保证输出单调。
type Timeline struct {
	started bool
	base    uint64
	lastRaw uint64
	last    uint64
}

// Reset 丢弃历史，下一次 Normalize 重新以该值作为起点。
func (t *Timeline) Reset() {
	t.started = false
	t.base = 0
	t.lastRaw = 0
	t.last = 0
}

// Base 返回会话起点的原始时间戳。
func (t *Timeline) Base() uint64 { return t.base }

// Normalize 返回相对起点的毫秒数，保证单调不减。
func (t *Timeline) Normalize(raw uint64) uint64 {
	if !t.started {
		t.started = true
		t.base = raw
		t.lastRaw = raw
		t.last = 0
		return 0
	}
	var delta uint64
	advance := true
	switch {
	case raw >= t.lastRaw:
		delta = raw - t.lastRaw
	case t.lastRaw <= maxUint32 && raw <= maxUint32:
		// 32 位回绕与真实倒退在数值上无法直接区分：取更近的一种解释。
		// 0xFFFFFFF8 -> 0x00000010 的回绕距离是 24ms，倒退距离是 4G，
		// 因此判定为回绕；0x20 -> 0x08 则判定为倒退，保持单调。
		wrapDelta := maxUint32 - t.lastRaw + raw + 1
		backDelta := t.lastRaw - raw
		if wrapDelta < backDelta {
			delta = wrapDelta
		} else {
			advance = false
		}
	default:
		// 真正的倒退（终端重启/切换时钟源）：不输出负时间戳，保持上一值。
		advance = false
	}
	if !advance {
		// 丢弃倒退样本，不推进 lastRaw；后续回到正确时钟后可继续累加。
		return t.last
	}
	t.lastRaw = raw
	t.last += delta
	return t.last
}

// Millisecond 把归一化结果转成 time.Duration，便于 RTMP/FLV 写入。
func (t *Timeline) Millisecond(raw uint64) time.Duration {
	return time.Duration(t.Normalize(raw)) * time.Millisecond
}
