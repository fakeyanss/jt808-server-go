package runtime

import (
	"errors"
	"sync"

	"github.com/fakeyanss/jt808-server-go/internal/protocol/model"
)

// ErrLocationResponseAlreadyPending 表示同一设备与平台流水号已有等待中的定位查询。
var ErrLocationResponseAlreadyPending = errors.New("location response is already pending")

// LocationResponseBroker 按设备号与平台消息流水号关联定位信息应答（A 类）。
type LocationResponseBroker interface {
	Register(phone string, serial uint16) (<-chan *model.Msg0201, func(), error)
	Deliver(msg *model.Msg0201) bool
}

type memoryLocationResponseBroker struct {
	mu      sync.Mutex
	waiters map[locationResponseKey]chan *model.Msg0201
}

type locationResponseKey struct {
	phone  string
	serial uint16
}

// NewLocationResponseBroker 创建独立的定位信息应答配对器。
func NewLocationResponseBroker() LocationResponseBroker {
	return newMemoryLocationResponseBroker()
}

func newMemoryLocationResponseBroker() *memoryLocationResponseBroker {
	return &memoryLocationResponseBroker{waiters: make(map[locationResponseKey]chan *model.Msg0201)}
}

func (b *memoryLocationResponseBroker) Register(phone string, serial uint16) (<-chan *model.Msg0201, func(), error) {
	key := locationResponseKey{phone: phone, serial: serial}
	b.mu.Lock()
	defer b.mu.Unlock()
	if _, exists := b.waiters[key]; exists {
		return nil, nil, ErrLocationResponseAlreadyPending
	}
	ch := make(chan *model.Msg0201, 1)
	b.waiters[key] = ch
	return ch, func() {
		b.mu.Lock()
		if b.waiters[key] == ch {
			delete(b.waiters, key)
		}
		b.mu.Unlock()
	}, nil
}

func (b *memoryLocationResponseBroker) Deliver(msg *model.Msg0201) bool {
	if msg == nil || msg.Header == nil {
		return false
	}
	key := locationResponseKey{phone: msg.Header.PhoneNumber, serial: msg.AnswerSerialNumber}
	b.mu.Lock()
	ch, ok := b.waiters[key]
	if ok {
		delete(b.waiters, key)
	}
	b.mu.Unlock()
	if ok {
		ch <- msg
	}
	return ok
}

// 编译期确认内存实现满足接口。
var _ LocationResponseBroker = (*memoryLocationResponseBroker)(nil)
