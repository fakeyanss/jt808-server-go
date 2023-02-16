package client

import "github.com/fakeYanss/jt808-server-go/internal/protocol/model"

type Server interface {
	Dial(addr string) error
	Start()
	Stop()
	Send(msg *model.JT808Msg)
}
