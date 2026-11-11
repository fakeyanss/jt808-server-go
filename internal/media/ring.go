package media

import (
	"sync"
	"time"
)

// ringFrame 是帧环中的一个帧。
type ringFrame struct {
	Seq   uint64
	PTS   uint64
	Key   bool
	Audio bool
	Data  []byte
	// Config 是 AAC 的 AudioSpecificConfig，仅音频帧使用。
	Config []byte
	At     time.Time
}

// gopRing 是“以关键帧为锚点”的内存环形缓冲。
//
// 形状与约束：
//   - 只保留视频帧；音频不参与 seek，也不决定可播放起点；
//   - 淘汰以整 GOP 为单位：从最旧的关键帧开始整段丢弃，保证任意起点都能解码；
//   - 至少保留当前 GOP，即使它本身超过容量上限（否则无法播放）；
//   - 读取方按 Seq 追帧，落后于 base 即判定为“已被淘汰”，由调用方断连重连。
type gopRing struct {
	mu          sync.Mutex
	frames      []ringFrame
	base        uint64
	next        uint64
	bytes       int64
	maxBytes    int64
	maxDuration time.Duration
	notify      chan struct{}
	closed      bool
}

// DefaultRingBytes 是单路帧环的默认容量上限。
const DefaultRingBytes = 32 * 1024 * 1024

// DefaultRingDuration 是单路帧环的默认时间跨度上限。
const DefaultRingDuration = 60 * time.Second

func newGOPRing(maxBytes int64, maxDuration time.Duration) *gopRing {
	if maxBytes <= 0 {
		maxBytes = DefaultRingBytes
	}
	if maxDuration <= 0 {
		maxDuration = DefaultRingDuration
	}
	return &gopRing{
		maxBytes:    maxBytes,
		maxDuration: maxDuration,
		notify:      make(chan struct{}),
	}
}

// push 追加一帧并执行整 GOP 淘汰。
func (r *gopRing) push(pts uint64, key bool, data []byte) {
	if len(data) == 0 {
		return
	}
	frame := ringFrame{Seq: r.next, PTS: pts, Key: key, Data: data, At: time.Now()}
	r.next++

	r.mu.Lock()
	r.frames = append(r.frames, frame)
	r.bytes += int64(len(data))
	r.evictLocked()
	close(r.notify)
	r.notify = make(chan struct{})
	r.mu.Unlock()
}

// evictLocked 按整 GOP 淘汰，调用方必须持有锁。
func (r *gopRing) evictLocked() {
	for len(r.frames) > 0 {
		overBytes := r.maxBytes > 0 && r.bytes > r.maxBytes
		overTime := r.maxDuration > 0 && r.spanLocked() > r.maxDuration
		if !overBytes && !overTime {
			return
		}
		cut := r.secondKeyframeLocked()
		if cut <= 0 {
			// 只有一个 GOP：整段保留，否则没有可播放起点。
			return
		}
		for i := 0; i < cut; i++ {
			r.bytes -= int64(len(r.frames[i].Data))
		}
		r.frames = append(r.frames[:0], r.frames[cut:]...)
		r.base = r.frames[0].Seq
	}
}

func (r *gopRing) spanLocked() time.Duration {
	if len(r.frames) < 2 {
		return 0
	}
	first := r.frames[0].PTS
	last := r.frames[len(r.frames)-1].PTS
	if last < first && first <= 0xFFFFFFFF && last <= 0xFFFFFFFF {
		// 32 位毫秒时间戳回绕：0xFFFFFFF0 -> 0x00000010 是 32ms。
		return time.Duration(0xFFFFFFFF-first+last+1) * time.Millisecond
	}
	if last < first {
		return 0
	}
	return time.Duration(last-first) * time.Millisecond
}

// secondKeyframeLocked 返回第二个关键帧的下标；不存在时返回 -1。
func (r *gopRing) secondKeyframeLocked() int {
	seen := 0
	for i, frame := range r.frames {
		if frame.Key {
			seen++
			if seen == 2 {
				return i
			}
		}
	}
	return -1
}

// pushAudio 追加一个 AAC 帧；音频不参与 GOP 锚点。
func (r *gopRing) pushAudio(pts uint64, data, config []byte) {
	if len(data) == 0 {
		return
	}
	r.mu.Lock()
	r.frames = append(r.frames, ringFrame{
		Seq: r.next, PTS: pts, Audio: true, Data: data, Config: config, At: time.Now(),
	})
	r.next++
	r.bytes += int64(len(data))
	r.evictLocked()
	close(r.notify)
	r.notify = make(chan struct{})
	r.mu.Unlock()
}

// startFrame 返回最近一个关键帧；没有任何关键帧时返回 false。
func (r *gopRing) startFrame() (ringFrame, bool) {
	r.mu.Lock()
	defer r.mu.Unlock()
	for i := len(r.frames) - 1; i >= 0; i-- {
		if r.frames[i].Key {
			return r.frames[i], true
		}
	}
	return ringFrame{}, false
}

// startCursor 返回最近一个关键帧的 Seq；没有任何关键帧时返回 false。
func (r *gopRing) startCursor() (uint64, bool) {
	r.mu.Lock()
	defer r.mu.Unlock()
	for i := len(r.frames) - 1; i >= 0; i-- {
		if r.frames[i].Key {
			return r.frames[i].Seq, true
		}
	}
	return 0, false
}

// framesSince 返回 Seq>=cursor 的帧快照。
// dropped=true 表示 cursor 已被淘汰，调用方必须从新的关键帧重新开始。
func (r *gopRing) framesSince(cursor uint64) (frames []ringFrame, next uint64, dropped bool) {
	r.mu.Lock()
	defer r.mu.Unlock()
	if cursor < r.base {
		return nil, r.base, true
	}
	start := 0
	for start < len(r.frames) && r.frames[start].Seq < cursor {
		start++
	}
	if start >= len(r.frames) {
		return nil, r.next, false
	}
	out := make([]ringFrame, len(r.frames)-start)
	copy(out, r.frames[start:])
	return out, r.next, false
}

// notifyCh 返回下一次 push 时会被关闭的通知通道。
func (r *gopRing) notifyCh() <-chan struct{} {
	r.mu.Lock()
	defer r.mu.Unlock()
	return r.notify
}

func (r *gopRing) close() {
	r.mu.Lock()
	if !r.closed {
		r.closed = true
		close(r.notify)
	}
	r.mu.Unlock()
}

func (r *gopRing) isClosed() bool {
	r.mu.Lock()
	defer r.mu.Unlock()
	return r.closed
}

func (r *gopRing) size() (frames int, bytes int64) {
	r.mu.Lock()
	defer r.mu.Unlock()
	return len(r.frames), r.bytes
}
