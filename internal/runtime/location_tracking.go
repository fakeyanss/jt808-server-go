package runtime

import (
	"sync"
	"time"
)

type locationTrackingState struct {
	interval  time.Duration
	expiresAt time.Time
}

// LocationTracker 临时定位跟踪参数管理器（A 类）。
// 保存 0x8103 下发的临时定位间隔与有效期，并通过本地 chan 通知订阅者；
// 定时器与 chan 无法持久化，因此归入 runtime。
type LocationTracker interface {
	Set(phone string, intervalSeconds uint16, validitySeconds uint32)
	Changed(phone string) <-chan struct{}
	Interval(phone string, fallback time.Duration) time.Duration
}

type memoryLocationTracker struct {
	mu      sync.Mutex
	byPhone map[string]locationTrackingState
	changed map[string]chan struct{}
}

// NewLocationTracker 创建独立的临时定位跟踪管理器。
func NewLocationTracker() LocationTracker {
	return newMemoryLocationTracker()
}

func newMemoryLocationTracker() *memoryLocationTracker {
	return &memoryLocationTracker{byPhone: make(map[string]locationTrackingState), changed: make(map[string]chan struct{})}
}

func (c *memoryLocationTracker) Set(phone string, intervalSeconds uint16, validitySeconds uint32) {
	c.mu.Lock()
	defer c.mu.Unlock()
	if intervalSeconds == 0 {
		delete(c.byPhone, phone)
		c.notifyLocked(phone)
		return
	}
	c.byPhone[phone] = locationTrackingState{interval: time.Duration(intervalSeconds) * time.Second, expiresAt: time.Now().Add(time.Duration(validitySeconds) * time.Second)}
	c.notifyLocked(phone)
}

func (c *memoryLocationTracker) Changed(phone string) <-chan struct{} {
	c.mu.Lock()
	defer c.mu.Unlock()
	if c.changed[phone] == nil {
		c.changed[phone] = make(chan struct{})
	}
	return c.changed[phone]
}

func (c *memoryLocationTracker) notifyLocked(phone string) {
	if ch := c.changed[phone]; ch != nil {
		close(ch)
	}
	c.changed[phone] = make(chan struct{})
}

// Interval 返回当前生效的临时定位间隔；无有效配置时返回 fallback。
func (c *memoryLocationTracker) Interval(phone string, fallback time.Duration) time.Duration {
	c.mu.Lock()
	defer c.mu.Unlock()
	state, ok := c.byPhone[phone]
	if !ok {
		return fallback
	}
	if !time.Now().Before(state.expiresAt) {
		delete(c.byPhone, phone)
		return fallback
	}
	return state.interval
}

// 编译期确认内存实现满足接口。
var _ LocationTracker = (*memoryLocationTracker)(nil)
