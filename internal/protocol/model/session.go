package model

import "net"

type SessionCtxKey struct{} // 定义全局session context key

type Session struct {
	ID   string
	Conn net.Conn
}
