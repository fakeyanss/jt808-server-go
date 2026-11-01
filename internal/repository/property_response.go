package repository

import (
	"errors"
	"sync"

	"github.com/fakeyanss/jt808-server-go/internal/protocol/model"
)

var ErrPropertyResponseAlreadyPending = errors.New("terminal property response is already pending")

type PropertyResponseBroker struct {
	mu      sync.Mutex
	waiters map[string]chan *model.Msg0107
}

var propertyResponseBroker = &PropertyResponseBroker{waiters: make(map[string]chan *model.Msg0107)}

func GetPropertyResponseBroker() *PropertyResponseBroker { return propertyResponseBroker }
func (b *PropertyResponseBroker) Register(phone string) (<-chan *model.Msg0107, func(), error) {
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
func (b *PropertyResponseBroker) Deliver(msg *model.Msg0107) bool {
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
