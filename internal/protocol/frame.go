package protocol

import (
	"bytes"
	"io"
)

// 标识位[2] + 消息头[21] + 消息体[1023 * 2(转义预留)]  + 校验码[1] + 标识位[2]
const MAX_FRAME_LEN = 2 + 21 + 1023*2 + 1 + 2

type FramePayload []byte

type FrameCodec interface {
	Write(io.Writer, FramePayload) error  // data -> frame，并写入io.Writer
	Read(io.Reader) (FramePayload, error) // 从io.Reader中提取frame payload，并返回给上层
}

type JT808FrameCodec struct {
}

func NewJT808FrameCodec() *JT808FrameCodec {
	return &JT808FrameCodec{}
}

func (pc *JT808FrameCodec) Write(w io.Writer, payload FramePayload) error {
	var p = payload
	for {
		n, err := w.Write([]byte(p))
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

func (pc *JT808FrameCodec) Read(r io.Reader) (FramePayload, error) {
	buf := make([]byte, MAX_FRAME_LEN)
	_, err := r.Read(buf)
	if err != nil {
		return nil, err
	}
	// 移除末尾多余的0
	buf = bytes.TrimRight(buf, "\x00")

	return FramePayload(buf), nil
}
