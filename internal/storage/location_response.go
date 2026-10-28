package storage

import (
	"errors"
	"sync"

	"github.com/fakeyanss/jt808-server-go/internal/protocol/model"
)

var ErrLocationResponseAlreadyPending = errors.New("location response is already pending")

type LocationResponseBroker struct {
	mu      sync.Mutex
	waiters map[locationResponseKey]chan *model.Msg0201
}
type locationResponseKey struct {
	phone  string
	serial uint16
}

var locationResponseBroker = &LocationResponseBroker{waiters: make(map[locationResponseKey]chan *model.Msg0201)}

func GetLocationResponseBroker() *LocationResponseBroker { return locationResponseBroker }
func (b *LocationResponseBroker) Register(phone string, serial uint16) (<-chan *model.Msg0201, func(), error) {
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
func (b *LocationResponseBroker) Deliver(msg *model.Msg0201) bool {
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
