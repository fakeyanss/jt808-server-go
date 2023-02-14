package protocol

import (
	"bufio"
	"bytes"
	"context"
	"io"
	"net"

	"github.com/pkg/errors"
	"github.com/rs/zerolog"
	"github.com/rs/zerolog/log"

	"github.com/fakeYanss/jt808-server-go/internal/protocol/model"
)

const (
	// 标识位[2] + 消息头[21] + 消息体[1023 * 2(转义预留)]  + 校验码[1] + 标识位[2]
	MaxFrameLen = 2 + 21 + 1023*2 + 1 + 2
)

var (
	ErrFrameReadEmpty = errors.New("Read empty frame")
)

type FramePayload []byte

type FrameHandler interface {
	Recv(ctx context.Context) (FramePayload, error) // data -> frame，并写入io.Writer
	Send(FramePayload) error                        // 从io.Reader中提取frame payload，并返回给上层
}

type JT808FrameHandler struct {
	rbuf *bufio.Reader

	// wbuf *bufio.Writer // 发送消息应该立即发出，不能使用缓存writer
	writer io.Writer
}

func NewJT808FrameHandler(conn net.Conn) *JT808FrameHandler {
	return &JT808FrameHandler{
		rbuf:   bufio.NewReader(conn),
		writer: conn,
	}
}

func (fh *JT808FrameHandler) Recv(ctx context.Context) (FramePayload, error) {
	buf := make([]byte, MaxFrameLen)
	_, err := fh.rbuf.Read(buf)
	if err != nil {
		return nil, errors.Wrap(err, "Fail to read stream to framePayload")
	}
	// 移除末尾多余的0
	buf = bytes.TrimRight(buf, "\x00")

	if len(buf) == 0 {
		return nil, ErrFrameReadEmpty
	}

	if log.Logger.GetLevel() == zerolog.DebugLevel {
		var sessionID string
		session := ctx.Value(model.SessionCtxKey{}).(*model.Session)
		if session != nil {
			sessionID = session.ID // client端不设置session
		}
		log.Debug().
			Str("id", sessionID).
			Int("frame_len", len(buf)).
			Hex("frame_payload", buf). // for debug
			Msg("Received frame.")
	}

	return FramePayload(buf), nil
}

func (fh *JT808FrameHandler) Send(payload FramePayload) error {
	var p = payload
	if len(p) == 0 {
		log.Debug().Msg("The payload is empty when sending, skip.")
		return nil
	}
	for {
		n, err := fh.writer.Write([]byte(p))
		if err != nil {
			return errors.Wrap(err, "Failed to send payload")
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
