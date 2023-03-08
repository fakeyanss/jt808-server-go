package protocol

import (
	"sync"

	"github.com/pkg/errors"
	"github.com/rs/zerolog/log"

	"github.com/fakeyanss/jt808-server-go/internal/protocol/model"
)

const (
	boundaryMark = 0x7e
	escapeMark   = 0x7d
	escapeOne    = 0x01
	escapeTwo    = 0x02
)

var (
	ErrEmptyPacket  = errors.New("Empty packet")
	ErrVerifyFailed = errors.New("Verify failed")
	ErrEncodeType   = errors.New("Error data type")
)

type PacketCodec interface {
	Decode([]byte) (*model.PacketData, error)

	Encode(any) ([]byte, error)
}

type JT808PacketCodec struct {
}

var jt808PacketCodec *JT808PacketCodec
var codecOnce sync.Once

func NewJT808PacketCodec() *JT808PacketCodec {
	codecOnce.Do(func() {
		jt808PacketCodec = &JT808PacketCodec{}
	})
	return jt808PacketCodec
}

// Decode JT808 packet.
//
// 反转义 -> 校验 -> 反序列化
func (pc *JT808PacketCodec) Decode(payload []byte) (*model.PacketData, error) {
	pkt := pc.unescape(payload)

	verifyCode := payload[len(payload)-1]
	pkt, err := pc.verify(pkt)
	if err != nil {
		return nil, err
	}

	pd := &model.PacketData{
		Header:     &model.MsgHeader{},
		VerifyCode: verifyCode,
	}

	err = pd.Header.Decode(pkt)
	if err != nil {
		return nil, errors.Wrap(err, "Fail to decode packet")
	}

	pd.Body = pkt[pd.Header.Idx:]

	pd.Header.Idx = 0 // reset idx

	return pd, nil
}

// Encode JT808 packet.
//
// 序列化 -> 生成校验码 -> 转义
func (pc *JT808PacketCodec) Encode(data any) (pkt []byte, err error) {
	if in, ok := data.(model.JT808Msg); ok {
		pkt, err = in.Encode()
	} else if out, ok := data.(model.JT808Msg); ok {
		pkt, err = out.Encode()
	} else {
		return nil, ErrEncodeType
	}

	if err != nil {
		return nil, errors.Wrap(err, "Fail to encode jtmsg")
	}

	pkt = pc.genVerifier(pkt)

	payload := pc.escape(pkt)

	return payload, nil
}

// Unescape JT808 packet.
//
// 去除前后标识符0x7e, 并将转义的数据包反转义:
//
//	0x7d0x02 -> 0x7e
//	0x7d0x01 -> 0x7d
func (pc *JT808PacketCodec) unescape(src []byte) []byte {
	dst := make([]byte, 0)
	i, n := 1, len(src)
	for i < n-1 {
		if i < n-2 && src[i] == 0x7d && src[i+1] == 0x02 {
			dst = append(dst, boundaryMark)
			i += 2
		} else if i < n-2 && src[i] == 0x7d && src[i+1] == 0x01 {
			dst = append(dst, escapeMark)
			i += 2
		} else {
			dst = append(dst, src[i])
			i++
		}
	}
	return dst
}

// Escape JT808 packet.
//
// 转义数据包：
//
//	0x7e -> 0x7d0x02
//	0x7d -> 0x7d0x01
//
// 并加上前后标识符0x7e
func (pc *JT808PacketCodec) escape(src []byte) []byte {
	dst := make([]byte, 0)
	dst = append(dst, boundaryMark)
	for _, v := range src {
		if v == boundaryMark {
			dst = append(dst, escapeMark, escapeTwo)
		} else if v == escapeMark {
			dst = append(dst, escapeMark, escapeOne)
		} else {
			dst = append(dst, v)
		}
	}
	dst = append(dst, boundaryMark)
	return dst
}

// 消息体异或校验，并去掉校验码
func (pc *JT808PacketCodec) verify(pkt []byte) ([]byte, error) {
	n := len(pkt)
	if n == 0 {
		return nil, ErrEmptyPacket
	}
	expected := pkt[n-1]
	var actual byte
	for _, v := range pkt[:n-1] {
		actual ^= v
	}
	if expected == actual {
		return pkt[:n-1], nil
	}
	log.Debug().Msgf("verify expect=%v, but actual=%v", expected, actual)
	return nil, ErrVerifyFailed
}

// 生成校验码
func (pc *JT808PacketCodec) genVerifier(pkt []byte) []byte {
	var code byte
	for _, v := range pkt {
		code ^= v
	}
	pkt = append(pkt, code)
	return pkt
}
