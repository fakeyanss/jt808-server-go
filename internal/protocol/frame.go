package protocol

import (
	"bufio"
	"bytes"
	"io"
	"net"
)

const (
	// 标识位[2] + 消息头[21] + 消息体[1023 * 2(转义预留)]  + 校验码[1] + 标识位[2]
	MaxFrameLen = 2 + 21 + 1023*2 + 1 + 2
)

type FramePayload []byte

type FrameHandler interface {
	Write(FramePayload) error    // data -> frame，并写入io.Writer
	Read() (FramePayload, error) // 从io.Reader中提取frame payload，并返回给上层
}

type JT808FrameHandler struct {
	rbuf *bufio.Reader
	// wbuf *bufio.Writer
	writer io.Writer
}

func NewJT808FrameHandler(conn net.Conn) *JT808FrameHandler {
	return &JT808FrameHandler{
		rbuf:   bufio.NewReader(conn),
		writer: conn,
	}
}

func (fh *JT808FrameHandler) Write(payload FramePayload) error {
	var p = payload
	for {
		n, err := fh.writer.Write([]byte(p))
		if err != nil {
			return err
		}
		if n >= len(p) {
			break
		}
		if n < len(p) {
			p = p[n:] // 没写完所有数据，再写一次
		}
	}
	return nil
}

func (fh *JT808FrameHandler) Read() (FramePayload, error) {
	buf := make([]byte, MaxFrameLen)
	_, err := fh.rbuf.Read(buf)
	if err != nil {
		return nil, err
	}
	// 移除末尾多余的0
	buf = bytes.TrimRight(buf, "\x00")

	return FramePayload(buf), nil
}
