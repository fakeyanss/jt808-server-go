package repository

import (
	"errors"
	"github.com/fakeyanss/jt808-server-go/internal/protocol/model"
	"sync"
)

var ErrRegionResponseAlreadyPending = errors.New("region/route response is already pending")

type regionResponseKey struct {
	phone     string
	queryType uint8
}
type RegionResponseBroker struct {
	mu      sync.Mutex
	waiters map[regionResponseKey]chan *model.Msg0608
}

var regionResponseBroker = &RegionResponseBroker{waiters: map[regionResponseKey]chan *model.Msg0608{}}

func GetRegionResponseBroker() *RegionResponseBroker { return regionResponseBroker }
func (b *RegionResponseBroker) Register(phone string, queryType uint8) (<-chan *model.Msg0608, func(), error) {
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
func (b *RegionResponseBroker) Deliver(msg *model.Msg0608) bool {
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
