package model

import (
	"encoding/binary"
	"fmt"
	"strconv"

	"github.com/fakeYanss/jt808-server-go/pkg/util"
	"github.com/rs/zerolog/log"
)

// 标识位[2] + 消息头[21] + 消息体[1023 * 2(转义预留)]  + 校验码[1] + 标识位[2]
const (
	MaxFrameLen       = 2 + 21 + 1023*2 + 1 + 2
	EncryptionNone    = "000"
	EncryptionRSA     = "001"
	EncryptionUnknown = ""
)

type JT808Msg interface {
	Decode([]byte) error     // []byte -> struct
	Encode() ([]byte, error) //  struct -> []byte
}

// 定义消息头
type JT808MsgHeader struct {
	MsgId            int16                 `json:"msgId"`            // 消息ID
	MsgBodyAttr      JT808MsgBodyAttr      `json:"msgBodyAttr"`      // 消息体属性
	ProtocolVersion  byte                  `json:"protocolVersion"`  // 协议版本号
	PhoneNumber      string                `json:"phoneNumber"`      // 终端手机号
	SerialNumber     int16                 `json:"serialNumber"`     // 消息流水号
	MsgFragmentation JT808MsgFragmentation `json:"msgFragmentation"` // 消息包封装项
}

func (h *JT808MsgHeader) Decode(pkt []byte) error {
	h.MsgId = int16(binary.BigEndian.Uint16(pkt[:2]))
	h.MsgBodyAttr = JT808MsgBodyAttr{}
	err := h.MsgBodyAttr.Decode(pkt[2:4])
	if err != nil {
		return nil
	}
	h.ProtocolVersion = pkt[4]
	h.PhoneNumber = util.Bcd2NumberStr(pkt[5:11]) // todo: phoneNumber长度是6 or 10 ?
	if h.MsgBodyAttr.PacketFragmented {
		h.MsgFragmentation = JT808MsgFragmentation{}
		h.MsgFragmentation.Decode(pkt[17:21])
	}
	return nil
}

// 定义消息体属性
type JT808MsgBodyAttr struct {
	BodyLength       int32  `json:"bodyLength"`       // 消息体长度
	Encryption       string `json:"encryption"`       // 是否加密
	PacketFragmented bool   `json:"packetFragmented"` // 是否分包
	Version          byte   `json:"version"`          // 版本标识，0表示协议版本是最早一期的版本，1表示已经引入协议版本标识的功能
	Extra            byte   `json:"extra"`            // 预留一个bit位的保留字段
}

func (a *JT808MsgBodyAttr) Decode(sub []byte) error {
	// 2-3位字节转为二进制数字
	bitNum := binary.BigEndian.Uint16(sub)
	// 二进制数字转为字符串
	bitStr := fmt.Sprintf("%0*b", 15, bitNum)

	bodyLength, err := strconv.ParseInt(bitStr[:10], 2, 10)
	if err != nil {
		log.Error().
			Err(err).
			Msg("Failed to decode msg body attr")
		return err
	}
	a.BodyLength = int32(bodyLength)

	switch bitStr[10:12] {
	case EncryptionNone:
		a.Encryption = EncryptionNone
	case EncryptionRSA:
		a.Encryption = EncryptionRSA
	default:
		a.Encryption = EncryptionUnknown
	}

	a.PacketFragmented = (bitStr[12] == 1)

	a.Version = bitStr[13]
	a.Extra = bitStr[14]
	return nil
}

// 定义分包的封装项
type JT808MsgFragmentation struct {
	Total int16 // 分包后的包总数
	Index int16 // 包序号，从1开始
}

func (f *JT808MsgFragmentation) Decode(sub []byte) error {
	f.Total = int16(binary.BigEndian.Uint16(sub[:2]))
	f.Index = int16(binary.BigEndian.Uint16(sub[2:]))
	return nil
}

// 终端注册消息
type Msg0100 struct {
	Header JT808MsgHeader `json:"header"`
}

func (m *Msg0100) Encode() ([]byte, error) {
	return nil, nil
}

func (m *Msg0100) Decode(pkt []byte) error {
	m.Header.Decode(pkt)
	return nil
}

// 终端注册应答消息
type Msg8100 struct {
}

// 终端注销消息
type Msg0003 struct {
}

// 终端鉴权消息
type Msg0102 struct {
}
