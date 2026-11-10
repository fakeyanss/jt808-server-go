package model

import (
	"bytes"
	"fmt"

	"github.com/fakeyanss/jt808-server-go/internal/codec/hex"
)

// 0x8105 终端控制的命令字（JT/T 808-2019 表 19）。
const (
	CommandWordUpgrade       uint8 = 0x01 // 升级
	CommandWordConnectServer uint8 = 0x02 // 连接到指定服务器
	CommandWordReset         uint8 = 0x04 // 终端复位
	CommandWordFactoryReset  uint8 = 0x05 // 恢复出厂设置
)

// 0x8105 命令参数的槽位编号，按标准表 19 的字段顺序从 0 开始。
// 0x0D 及以上未在标准中定义，作为厂商自定义扩展按原样保留并回编码。
const (
	CommandParamConnectControl uint8 = 0
	CommandParamDialPointName  uint8 = 1
	CommandParamDialUserName   uint8 = 2
	CommandParamDialPassword   uint8 = 3
	CommandParamServerAddress  uint8 = 4
	CommandParamTCPPort        uint8 = 5
	CommandParamUDPPort        uint8 = 6
	CommandParamManufacturerID uint8 = 7
	CommandParamAuthCode       uint8 = 8
	CommandParamHardwareVer    uint8 = 9
	CommandParamFirmwareVer    uint8 = 10
	CommandParamURL            uint8 = 11
	CommandParamConnectTimeout uint8 = 12
)

// standardCommandParameterCount 是标准定义的参数槽位数。
const standardCommandParameterCount = int(CommandParamConnectTimeout) + 1

// commandParameterNames 与标准槽位一一对应，用于日志与文档展示。
var commandParameterNames = [standardCommandParameterCount]string{
	"连接控制",
	"拨号点名称",
	"拨号用户名",
	"拨号密码",
	"服务器地址",
	"TCP端口",
	"UDP端口",
	"制造商ID",
	"监管平台鉴权码",
	"硬件版本",
	"固件版本",
	"URL的完整地址",
	"连接到指定服务器时限",
}

// CommandParameterName 返回标准槽位的参数名称；未定义的扩展槽位返回空串。
func CommandParameterName(id uint8) string {
	if int(id) < len(commandParameterNames) {
		return commandParameterNames[id]
	}
	return ""
}

// CommandParameter 是类型化后的 0x8105 命令参数项。
//
// ID 为参数槽位编号（0~12 为标准字段，>=13 为厂商自定义扩展），
// Length 必须与 Value 的实际字节数一致；编解码两侧都会做校验。
// Value 保存线上原始字节：字符串字段为 GBK 编码，端口与时限字段为大端 WORD。
type CommandParameter struct {
	ID     uint8  `json:"id"`
	Length uint8  `json:"length"`
	Value  []byte `json:"value"`
}

// Msg8105 is a platform terminal-control command.
//
// CommandParam 保留原有“整段原始命令参数”路径：非空且 Parameters 为空时
// 按原逻辑直接写入 GBK 字符串，确保既有调用方行为不变。
// Parameters 为新增的类型化能力：非空时按标准槽位排序编码，忽略 CommandParam。
type Msg8105 struct {
	Header       *MsgHeader         `json:"header"`
	CommandWord  uint8              `json:"commandWord"`
	CommandParam string             `json:"commandParam,omitempty"`
	Parameters   []CommandParameter `json:"parameters,omitempty"`
}

func (m *Msg8105) Decode(packet *PacketData) error {
	if packet == nil || packet.Header == nil || len(packet.Body) < 1 {
		return ErrDecodeMsg
	}
	m.Header = packet.Header
	m.CommandWord = packet.Body[0]
	m.Parameters = nil
	if len(packet.Body) > 1 {
		raw := packet.Body[1:]
		idx := 1
		m.CommandParam = hex.ReadGBK(packet.Body, &idx, len(raw))
		if supportsCommandParameters(m.CommandWord) {
			params, err := decodeCommandParameters(raw)
			if err != nil {
				return err
			}
			m.Parameters = params
		}
	}
	return nil
}

func (m *Msg8105) Encode() ([]byte, error) {
	if m.Header == nil {
		return nil, ErrEncodeMsg
	}
	if len(m.Parameters) == 0 {
		payload := []byte{m.CommandWord}
		payload = hex.WriteGBK(payload, m.CommandParam)
		return writeHeader(m, payload)
	}
	if !supportsCommandParameters(m.CommandWord) {
		return nil, fmt.Errorf("command word %d does not support typed parameters", m.CommandWord)
	}

	values := make(map[uint8][]byte, len(m.Parameters))
	maxID := standardCommandParameterCount - 1
	for _, param := range m.Parameters {
		if int(param.Length) != len(param.Value) {
			return nil, fmt.Errorf("command parameter %d length %d does not match value length %d",
				param.ID, param.Length, len(param.Value))
		}
		if len(param.Value) > 255 {
			return nil, fmt.Errorf("command parameter %d exceeds 255 bytes", param.ID)
		}
		if _, exists := values[param.ID]; exists {
			return nil, fmt.Errorf("duplicate command parameter id %d", param.ID)
		}
		values[param.ID] = param.Value
		if int(param.ID) > maxID {
			maxID = int(param.ID)
		}
	}

	payload := []byte{m.CommandWord}
	for id := 0; id <= maxID; id++ {
		payload = append(payload, values[uint8(id)]...)
		payload = append(payload, ';')
	}
	return writeHeader(m, payload)
}

func (m *Msg8105) GetHeader() *MsgHeader { return m.Header }

func (m *Msg8105) GenOutgoing(_ JT808Msg) error { return ErrGenOutgoingMsg }

// supportsCommandParameters 判断命令字的命令参数是否按标准槽位组织。
// 标准只对命令字 1（升级）与 2（连接指定服务器）定义了参数。
func supportsCommandParameters(commandWord uint8) bool {
	return commandWord == CommandWordUpgrade || commandWord == CommandWordConnectServer
}

// decodeCommandParameters 按半角分号切分命令参数，槽位序号即参数 ID。
// 末尾分号产生的空段不代表参数槽位；空槽位在模型中保持缺省。
func decodeCommandParameters(raw []byte) ([]CommandParameter, error) {
	if len(raw) == 0 {
		return nil, nil
	}
	segments := bytes.Split(raw, []byte{';'})
	if len(segments) > 0 && len(segments[len(segments)-1]) == 0 {
		segments = segments[:len(segments)-1]
	}
	params := make([]CommandParameter, 0, len(segments))
	for index, segment := range segments {
		if len(segment) == 0 {
			continue
		}
		if index > 255 || len(segment) > 255 {
			return nil, ErrDecodeMsg
		}
		params = append(params, CommandParameter{
			ID:     uint8(index),
			Length: uint8(len(segment)),
			Value:  append([]byte(nil), segment...),
		})
	}
	return params, nil
}
