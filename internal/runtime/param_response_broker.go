package runtime

import (
	"errors"
	"sync"

	"github.com/fakeyanss/jt808-server-go/internal/protocol/model"
)

// ErrParamResponseAlreadyPending 表示同一设备与平台流水号已有等待中的参数查询。
var ErrParamResponseAlreadyPending = errors.New("parameter response is already pending")

// ParamResponseBroker 按设备号与平台消息流水号关联终端参数应答（A 类）。
// 产物是本地 chan 配对，无法持久化，因此归入 runtime 独立持有。
type ParamResponseBroker interface {
	Register(phone string, serial uint16) (<-chan *model.Msg0104, func(), error)
	Deliver(msg *model.Msg0104) bool
}

type memoryParamResponseBroker struct {
	mu      sync.Mutex
	waiters map[paramResponseKey]chan *model.Msg0104
}

type paramResponseKey struct {
	phone  string
	serial uint16
}

// NewParamResponseBroker 创建独立的参数应答配对器。
func NewParamResponseBroker() ParamResponseBroker {
	return newMemoryParamResponseBroker()
}

func newMemoryParamResponseBroker() *memoryParamResponseBroker {
	return &memoryParamResponseBroker{waiters: make(map[paramResponseKey]chan *model.Msg0104)}
}

func (b *memoryParamResponseBroker) Register(phone string, serial uint16) (<-chan *model.Msg0104, func(), error) {
	key := paramResponseKey{phone: phone, serial: serial}
	b.mu.Lock()
	defer b.mu.Unlock()
	if _, exists := b.waiters[key]; exists {
		return nil, nil, ErrParamResponseAlreadyPending
	}
	ch := make(chan *model.Msg0104, 1)
	b.waiters[key] = ch
	return ch, func() {
		b.mu.Lock()
		if b.waiters[key] == ch {
			delete(b.waiters, key)
		}
		b.mu.Unlock()
	}, nil
}

func (b *memoryParamResponseBroker) Deliver(msg *model.Msg0104) bool {
	if msg == nil || msg.Header == nil {
		return false
	}
	key := paramResponseKey{phone: msg.Header.PhoneNumber, serial: msg.AnswerSerialNumber}
	b.mu.Lock()
	ch, exists := b.waiters[key]
	if exists {
		delete(b.waiters, key)
	}
	b.mu.Unlock()
	if !exists {
		return false
	}
	ch <- msg
	return true
}

// 编译期确认内存实现满足接口。
var _ ParamResponseBroker = (*memoryParamResponseBroker)(nil)
