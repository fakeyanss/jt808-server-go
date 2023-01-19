package protocol

import (
	"bytes"
	"io"

	"github.com/fakeYanss/jt808-server-go/pkg/model"
)

type FramePayload []byte

type FrameHandler interface {
	Write(io.Writer, FramePayload) error  // data -> frame，并写入io.Writer
	Read(io.Reader) (FramePayload, error) // 从io.Reader中提取frame payload，并返回给上层
}

type JT808FrameHandler struct {
}

func NewJT808FrameHandler() *JT808FrameHandler {
	return &JT808FrameHandler{}
}

func (fh *JT808FrameHandler) Write(w io.Writer, payload FramePayload) error {
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

func (fh *JT808FrameHandler) Read(r io.Reader) (FramePayload, error) {
	buf := make([]byte, model.MaxFrameLen)
	_, err := r.Read(buf)
	if err != nil {
		return nil, err
	}
	// 移除末尾多余的0
	buf = bytes.TrimRight(buf, "\x00")

	return FramePayload(buf), nil
}
