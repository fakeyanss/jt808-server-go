package runtime

import (
	"context"
	"sync"
	"time"
)

// HeartbeatRegistry 记录进程内最近一次心跳时间（A 类，仅进程内存）。
//
// 心跳落库被节流时，保活判定仍需要实时心跳：否则数据库里的 last_com_time 最长会
// 落后一个节流周期，可能把在线设备误判为离线。重启后该记录清空，保活改以库中
// 的 last_com_time 作为"上次在线时间"。
type HeartbeatRegistry interface {
	Touch(ctx context.Context, phone string, at time.Time)
	Last(ctx context.Context, phone string) (time.Time, bool)
	Remove(ctx context.Context, phone string)
}

type memoryHeartbeatRegistry struct {
	mutex sync.RWMutex
	last  map[string]time.Time
}

// NewHeartbeatRegistry 创建独立的心跳记录表。
func NewHeartbeatRegistry() HeartbeatRegistry {
	return &memoryHeartbeatRegistry{last: make(map[string]time.Time)}
}

func (r *memoryHeartbeatRegistry) Touch(_ context.Context, phone string, at time.Time) {
	r.mutex.Lock()
	defer r.mutex.Unlock()
	if prev, ok := r.last[phone]; ok && prev.After(at) {
		return
	}
	r.last[phone] = at
}

func (r *memoryHeartbeatRegistry) Last(_ context.Context, phone string) (time.Time, bool) {
	r.mutex.RLock()
	defer r.mutex.RUnlock()
	at, ok := r.last[phone]
	return at, ok
}

func (r *memoryHeartbeatRegistry) Remove(_ context.Context, phone string) {
	r.mutex.Lock()
	defer r.mutex.Unlock()
	delete(r.last, phone)
}
