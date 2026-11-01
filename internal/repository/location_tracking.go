package repository

import (
	"sync"
	"time"
)

type locationTrackingState struct {
	interval  time.Duration
	expiresAt time.Time
}
type LocationTrackingCache struct {
	mu      sync.Mutex
	byPhone map[string]locationTrackingState
	changed map[string]chan struct{}
}

var locationTrackingCache = &LocationTrackingCache{byPhone: make(map[string]locationTrackingState), changed: make(map[string]chan struct{})}

func GetLocationTrackingCache() *LocationTrackingCache { return locationTrackingCache }

func (c *LocationTrackingCache) Set(phone string, intervalSeconds uint16, validitySeconds uint32) {
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

func (c *LocationTrackingCache) Changed(phone string) <-chan struct{} {
	c.mu.Lock()
	defer c.mu.Unlock()
	if c.changed[phone] == nil {
		c.changed[phone] = make(chan struct{})
	}
	return c.changed[phone]
}

func (c *LocationTrackingCache) notifyLocked(phone string) {
	if ch := c.changed[phone]; ch != nil {
		close(ch)
	}
	c.changed[phone] = make(chan struct{})
}

// Interval returns the active temporary interval or the configured fallback.
func (c *LocationTrackingCache) Interval(phone string, fallback time.Duration) time.Duration {
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
