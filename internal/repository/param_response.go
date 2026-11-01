package repository

import (
	"errors"
	"sync"

	"github.com/fakeyanss/jt808-server-go/internal/protocol/model"
)

var ErrParamResponseAlreadyPending = errors.New("parameter response is already pending")

// ParamResponseBroker correlates terminal parameter responses with platform
// queries by phone number and the platform message serial number.
type ParamResponseBroker struct {
	mu      sync.Mutex
	waiters map[paramResponseKey]chan *model.Msg0104
}

type paramResponseKey struct {
	phone  string
	serial uint16
}

var paramResponseBroker = &ParamResponseBroker{waiters: make(map[paramResponseKey]chan *model.Msg0104)}

func GetParamResponseBroker() *ParamResponseBroker { return paramResponseBroker }

func (b *ParamResponseBroker) Register(phone string, serial uint16) (<-chan *model.Msg0104, func(), error) {
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

func (b *ParamResponseBroker) Deliver(msg *model.Msg0104) bool {
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
