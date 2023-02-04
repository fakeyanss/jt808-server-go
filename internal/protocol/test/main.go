package main

import (
	"fmt"
)

type msg interface {
	getID() int32
}

type msg1 struct {
	id    int32
	phone string
}

func (m *msg1) getID() int32 {
	return m.id
}

type msg2 struct {
	id    int32
	email string
}

func (m *msg2) getID() int32 {
	return m.id
}

type call struct {
	m  msg
	fn func(msg, msg)
}

var calls map[int32]*call

func initCalls() {
	calls = make(map[int32]*call)
	calls[1] = &call{
		m: &msg1{},
		fn: func(msgType msg, m msg) {
			// todo 修改phone += "9999"
			// 用类型推断有点啰嗦，如何利用msg的类型来实现？go不支持将type作为参数传递
			m1 := m.(*msg1)
			m1.phone += "99999"
		},
	}
	// calls[2] = ....
}

func main() {
	initCalls()

	m1 := msg1{id: 1, phone: "12345678901"}
	call1 := calls[m1.id]
	call1.fn(call1.m, &m1)
	fmt.Printf("msg1=%v", m1)
}
