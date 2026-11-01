package runtime

import (
	"errors"
	"sync"

	"github.com/fakeyanss/jt808-server-go/internal/protocol/model"
)

// ErrPropertyResponseAlreadyPending 表示同一设备已有等待中的终端属性查询。
var ErrPropertyResponseAlreadyPending = errors.New("terminal property response is already pending")

// PropertyResponseBroker 按设备号关联终端属性应答（A 类，本地 chan 配对）。
type PropertyResponseBroker interface {
	Register(phone string) (<-chan *model.Msg0107, func(), error)
	Deliver(msg *model.Msg0107) bool
}

type memoryPropertyResponseBroker struct {
	mu      sync.Mutex
	waiters map[string]chan *model.Msg0107
}

// NewPropertyResponseBroker 创建独立的终端属性应答配对器。
func NewPropertyResponseBroker() PropertyResponseBroker {
	return newMemoryPropertyResponseBroker()
}

func newMemoryPropertyResponseBroker() *memoryPropertyResponseBroker {
	return &memoryPropertyResponseBroker{waiters: make(map[string]chan *model.Msg0107)}
}

func (b *memoryPropertyResponseBroker) Register(phone string) (<-chan *model.Msg0107, func(), error) {
	b.mu.Lock()
	defer b.mu.Unlock()
	if _, exists := b.waiters[phone]; exists {
		return nil, nil, ErrPropertyResponseAlreadyPending
	}
	ch := make(chan *model.Msg0107, 1)
	b.waiters[phone] = ch
	return ch, func() {
		b.mu.Lock()
		if b.waiters[phone] == ch {
			delete(b.waiters, phone)
		}
		b.mu.Unlock()
	}, nil
}

func (b *memoryPropertyResponseBroker) Deliver(msg *model.Msg0107) bool {
	if msg == nil || msg.Header == nil {
		return false
	}
	phone := msg.Header.PhoneNumber
	b.mu.Lock()
	ch, ok := b.waiters[phone]
	if ok {
		delete(b.waiters, phone)
	}
	b.mu.Unlock()
	if ok {
		ch <- msg
	}
	return ok
}

// 编译期确认内存实现满足接口。
var _ PropertyResponseBroker = (*memoryPropertyResponseBroker)(nil)
