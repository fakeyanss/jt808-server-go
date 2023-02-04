package model

import "net"

type Device struct {
	ID          string            `json:"id"`
	TransProto  TransportProtocol `json:"transProto"`
	Conn        net.Conn          `json:"conn"`
	Authed      bool              `json:"authed"`      // 是否鉴权通过
	LastComTime int64             `json:"lastComTime"` // 最近一次交互时间
}
