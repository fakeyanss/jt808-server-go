package runtime

import (
	"errors"
	"sync"

	"github.com/fakeyanss/jt808-server-go/internal/protocol/model"
)

// ErrRegionResponseAlreadyPending 表示同一设备与查询类型已有等待中的区域/路线查询。
var ErrRegionResponseAlreadyPending = errors.New("region/route response is already pending")

// RegionResponseBroker 按设备号与查询类型关联区域/路线应答（A 类）。
type RegionResponseBroker interface {
	Register(phone string, queryType uint8) (<-chan *model.Msg0608, func(), error)
	Deliver(msg *model.Msg0608) bool
}

type regionResponseKey struct {
	phone     string
	queryType uint8
}

type memoryRegionResponseBroker struct {
	mu      sync.Mutex
	waiters map[regionResponseKey]chan *model.Msg0608
}

// NewRegionResponseBroker 创建独立的区域/路线应答配对器。
func NewRegionResponseBroker() RegionResponseBroker {
	return newMemoryRegionResponseBroker()
}

func newMemoryRegionResponseBroker() *memoryRegionResponseBroker {
	return &memoryRegionResponseBroker{waiters: map[regionResponseKey]chan *model.Msg0608{}}
}

func (b *memoryRegionResponseBroker) Register(phone string, queryType uint8) (<-chan *model.Msg0608, func(), error) {
	k := regionResponseKey{phone, queryType}
	b.mu.Lock()
	defer b.mu.Unlock()
	if _, ok := b.waiters[k]; ok {
		return nil, nil, ErrRegionResponseAlreadyPending
	}
	ch := make(chan *model.Msg0608, 1)
	b.waiters[k] = ch
	return ch, func() {
		b.mu.Lock()
		if b.waiters[k] == ch {
			delete(b.waiters, k)
		}
		b.mu.Unlock()
	}, nil
}

func (b *memoryRegionResponseBroker) Deliver(msg *model.Msg0608) bool {
	if msg == nil || msg.Header == nil {
		return false
	}
	k := regionResponseKey{msg.Header.PhoneNumber, msg.QueryType}
	b.mu.Lock()
	ch, ok := b.waiters[k]
	if ok {
		delete(b.waiters, k)
	}
	b.mu.Unlock()
	if ok {
		ch <- msg
	}
	return ok
}

// 编译期确认内存实现满足接口。
var _ RegionResponseBroker = (*memoryRegionResponseBroker)(nil)
