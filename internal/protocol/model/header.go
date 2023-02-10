package model

import (
	"encoding/binary"

	"github.com/fakeYanss/jt808-server-go/internal/codec/bcd"
	"github.com/pkg/errors"
)

var (
	ErrDecodeHeader = errors.New("Fail to decode header")
)

// 定义消息头
type MsgHeader struct {
	MsgID           uint16            `json:"msgID"`           // 消息ID
	Attr            *MsgBodyAttr      `json:"attr"`            // 消息体属性
	ProtocolVersion uint8             `json:"protocolVersion"` // 协议版本号，默认0表示2011/2013版本，其他为2019后续版本，每次修订递增，初始为1
	PhoneNumber     string            `json:"phoneNumber"`     // 终端手机号
	SerialNumber    uint16            `json:"serialNumber"`    // 消息流水号
	Frag            *MsgFragmentation `json:"frag"`            // 消息包封装项

	Idx int32 `json:"-"` // 读取的packet header下标ID
}

// 将[]byte解码成消息头结构体
func (h *MsgHeader) Decode(pkt []byte) error {
	var idx int32

	h.MsgID = binary.BigEndian.Uint16(pkt[:idx+2]) // 消息id [0,2)位
	idx += 2

	h.Attr = &MsgBodyAttr{}
	err := h.Attr.Decode(pkt[idx : idx+2]) // 消息体属性 [2,4)位
	if err != nil {
		return ErrDecodeHeader
	}
	idx += 2

	if h.Attr.VersionDesc == Version2019 {
		h.ProtocolVersion = pkt[idx] // 2019版本，协议版本号 第4位
		idx++
	}

	// 2013版本，phoneNumber [5,11)位 长度6位；2019版本，phoneNumber [5,15)位 长度10位。
	if h.Attr.VersionDesc == Version2019 {
		h.PhoneNumber = bcd.BCD2NumberStr(pkt[idx : idx+10])
		idx += 10
	} else if h.Attr.VersionDesc == Version2013 {
		h.PhoneNumber = bcd.BCD2NumberStr(pkt[idx : idx+6])
		idx += 6
	} else {
		return ErrDecodeHeader
	}

	h.SerialNumber = binary.BigEndian.Uint16(pkt[idx : idx+2])
	idx += 2

	if h.Attr.PacketFragmentedDesc {
		h.Frag = &MsgFragmentation{}
		err = h.Frag.Decode(pkt[idx : idx+2]) // 消息包封装项，两位
		if err != nil {
			return ErrDecodeHeader
		}
		idx += 2
	}

	h.Idx = idx

	return nil
}

// 将消息头结构体编码成[]byte
func (h *MsgHeader) Encode() ([]byte, error) {
	pkt := make([]byte, 0)

	id := make([]byte, 2)
	binary.BigEndian.PutUint16(id, h.MsgID) // 消息id
	pkt = append(pkt, id...)

	bodyAttrPkt, err := h.Attr.Encode() // 消息体属性
	if err != nil {
		return nil, err
	}
	pkt = append(pkt, bodyAttrPkt...)

	pkt = append(pkt, h.ProtocolVersion) // 协议版本号

	pkt = append(pkt, bcd.NumberStr2BCD(h.PhoneNumber)...) // 手机号

	sn := make([]byte, 2)
	binary.BigEndian.PutUint16(sn, h.SerialNumber) // 消息流水号
	pkt = append(pkt, sn...)

	if h.Frag != nil {
		fragPkt, err := h.Frag.Encode() // 消息包封装项
		if err != nil {
			return nil, err
		}
		pkt = append(pkt, fragPkt...)
	}

	return pkt, nil
}

// 消息体属性字段的bit位
const (
	bodyLengthBit    uint16 = 0b0000001111111111
	encryptionBit    uint16 = 0b0001110000000000
	fragmentationBit uint16 = 0b0010000000000000
	versionSignBit   uint16 = 0b0100000000000000
	extraBit         uint16 = 0b1000000000000000
)

// 加密类型
type EncryptionType int8

const (
	EncryptionUnknown EncryptionType = -1
	EncryptionNone    EncryptionType = 0b000
	EncryptionRSA     EncryptionType = 0b001
)

type PacketFragmentedType bool

const (
	PacketFragmentedFalse PacketFragmentedType = false
	PacketFragmentedTrue  PacketFragmentedType = true
)

type VersionType int8

const (
	Version2013 = 0
	Version2019 = 1
)

// 定义消息体属性
type MsgBodyAttr struct {
	BodyLength       uint16 `json:"bodyLength"`       // 消息体长度
	Encryption       uint8  `json:"encryption"`       // 加密类型
	PacketFragmented uint8  `json:"packetFragmented"` // 分包标识，1：长消息，有分包；2：无分包
	VersionSign      uint8  `json:"versionSign"`      // 版本标识，1：2019版本；2：2013版本
	Extra            uint8  `json:"extra"`            // 预留一个bit位的保留字段

	EncryptionDesc       EncryptionType       `json:"encryptionDesc"`       // 加密类型描述
	PacketFragmentedDesc PacketFragmentedType `json:"packetFragmentedDesc"` // 是否分包描述
	VersionDesc          VersionType          `json:"versionDesc"`          // 版本类型描述
}

func (attr *MsgBodyAttr) Decode(sub []byte) error {
	// 2-3位字节转为二进制数字
	bitNum := binary.BigEndian.Uint16(sub)

	attr.BodyLength = bitNum & bodyLengthBit // 消息体长度 低10位

	// 加密方式 10-12位
	attr.Encryption = uint8((bitNum & encryptionBit) >> 10)
	switch attr.Encryption {
	case uint8(EncryptionNone):
		attr.EncryptionDesc = EncryptionNone
	case uint8(EncryptionRSA):
		attr.EncryptionDesc = EncryptionRSA
	default:
		attr.EncryptionDesc = EncryptionUnknown
	}

	attr.PacketFragmented = uint8(bitNum & fragmentationBit >> 13) // 分包 13位
	if attr.PacketFragmented == 1 {
		attr.PacketFragmentedDesc = PacketFragmentedTrue
	} else {
		attr.PacketFragmentedDesc = PacketFragmentedFalse
	}

	attr.VersionSign = uint8(bitNum & versionSignBit >> 14) // 版本标识 14位
	if attr.VersionSign == 1 {
		attr.VersionDesc = Version2019
	} else {
		attr.VersionDesc = Version2013
	}

	attr.Extra = uint8(bitNum & extraBit >> 15) // 保留 15位
	return nil
}

func (attr *MsgBodyAttr) Encode() ([]byte, error) {
	var bitNum uint16

	bitNum += attr.BodyLength                     // 消息体长度
	bitNum += uint16(attr.Encryption) << 10       // 加密方式
	bitNum += uint16(attr.PacketFragmented) << 13 // 分包
	bitNum += uint16(attr.VersionSign) << 14      // 版本标识
	bitNum += uint16(attr.Extra) << 15            // 保留位

	pkt := make([]byte, 2)
	binary.BigEndian.PutUint16(pkt, bitNum)
	return pkt, nil
}

// 定义分包的封装项
type MsgFragmentation struct {
	Total uint16 `json:"total"` // 分包后的包总数
	Index uint16 `json:"index"` // 包序号，从1开始
}

func (frag *MsgFragmentation) Decode(sub []byte) error {
	frag.Total = binary.BigEndian.Uint16(sub[:2])
	frag.Index = binary.BigEndian.Uint16(sub[2:])
	return nil
}

func (frag *MsgFragmentation) Encode() ([]byte, error) {
	pkt := make([]byte, 0)

	tot := make([]byte, 2)
	binary.BigEndian.PutUint16(tot, frag.Total)
	pkt = append(pkt, tot...)

	idx := make([]byte, 2)
	binary.BigEndian.PutUint16(idx, frag.Index)
	pkt = append(pkt, idx...)

	return pkt, nil
}
