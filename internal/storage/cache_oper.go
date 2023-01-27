package storage

import (
	"errors"
	"net"
	"sync"
)

var ErrSessionNotExist = errors.New("error not exit")

var sessionCache sync.Map

type Session struct {
	ID   string `json:"id"`
	Conn net.Conn
}

func GetSession(id string) (*Session, error) {
	if s, ok := sessionCache.Load(id); ok {
		return s.(*Session), nil
	}
	return nil, ErrSessionNotExist
}

func SetSession(s *Session) {
	sessionCache.Store(s.ID, s)
}
