package datagen

import (
	"encoding/json"
	"math"
	"os"
	"strconv"
	"time"

	regen "github.com/AnatolyRugalev/goregen"
	"github.com/rs/zerolog/log"

	"github.com/fakeyanss/jt808-server-go/internal/protocol/model"
)

func matchTokenAndGen(input string) string {
	output, _ := regen.Generate(input)
	return output
}

func atoi(input string) int64 {
	output, err := strconv.Atoi(input)
	if err != nil {
		return 0
	}
	return int64(output)
}

// 可以定义设备模板为json，包含变量字段为随机值，支持num/str, 标记为{{rand:num:len}}, {{rand:str:len}}
type deviceTpl struct {
	ID              string        `json:"id"`
	PlateNumber     string        `json:"plateNumber"`
	PhoneNumber     string        `json:"phoneNumber"`
	IMEI            string        `json:"imei"`
	TransProto      string        `json:"transProto"`      // 传输协议，TCP/UDP
	Keepalive       time.Duration `json:"keepalive"`       // 保活时间，单位：s
	ProtocalVersion string        `json:"protocalVersion"` // 协议版本，2019/2013/2011
	SoftwareVersion string        `json:"softwareVersion"` // 软件版本
}

func GenDevice() *model.Device {
	tpl, _ := os.ReadFile("./configs/device.tpl.json")
	deviceTpl := deviceTpl{}
	_ = json.Unmarshal(tpl, &deviceTpl)
	device := &model.Device{
		ID:              matchTokenAndGen(deviceTpl.ID),
		PlateNumber:     matchTokenAndGen(deviceTpl.PlateNumber),
		PhoneNumber:     matchTokenAndGen(deviceTpl.PhoneNumber),
		Keepalive:       deviceTpl.Keepalive * 1000 * 1000 * 1000, // s -> ns
		ProtocalVersion: model.Version2019,
		IMEI:            matchTokenAndGen(deviceTpl.IMEI),
		SoftwareVersion: matchTokenAndGen(deviceTpl.SoftwareVersion),
	}
	switch deviceTpl.ProtocalVersion {
	case "2019":
		device.ProtocalVersion = model.Version2019
	case "2013":
		device.ProtocalVersion = model.Version2013
	case "2011":
		device.ProtocalVersion = model.Version2011
	}
	log.Debug().Str("device", device.PhoneNumber).Msgf("Generate random device=%+v", device)
	return device
}

func genMsgHeader(msgID uint16, device *model.Device) *model.MsgHeader {
	msgHeader := &model.MsgHeader{
		MsgID: msgID,
		Attr: &model.MsgBodyAttr{
			Encryption:           uint8(model.EncryptionNone),
			PacketFragmented:     0,
			VersionSign:          1,
			Extra:                0,
			EncryptionDesc:       model.EncryptionNone,
			PacketFragmentedDesc: model.PacketFragmentedFalse,
			VersionDesc:          model.Version2019,
		},
		ProtocolVersion: 1,
		PhoneNumber:     device.PhoneNumber,
		SerialNumber:    1,
		Frag:            nil,
	}
	if device.ProtocalVersion == model.Version2019 {
		msgHeader.Attr.VersionSign = 1
		msgHeader.Attr.VersionDesc = model.Version2019
		msgHeader.ProtocolVersion = 1
	} else {
		msgHeader.Attr.VersionSign = 0
		msgHeader.Attr.VersionDesc = model.Version2013
		msgHeader.ProtocolVersion = 0
	}
	return msgHeader
}

func GenMsg0002(device *model.Device) *model.Msg0002 {
	return &model.Msg0002{
		Header: genMsgHeader(model.MsgID0002, device),
	}
}

type msg0100Tpl struct {
	ProvinceID     string `json:"provinceId"`
	CityID         string `json:"cityId"`
	ManufacturerID string `json:"manufacturerId"`
	DeviceMode     string `json:"deviceMode"`
	PlateColor     string `json:"plateColor"`
}

func GenMsg0100(device *model.Device) *model.Msg0100 {
	tpl, _ := os.ReadFile("./configs/msg0100.tpl.json")
	msg0100Tpl := msg0100Tpl{}
	_ = json.Unmarshal(tpl, &msg0100Tpl)
	return &model.Msg0100{
		Header:         genMsgHeader(model.MsgID0100, device),
		ProvinceID:     uint16(atoi(matchTokenAndGen(msg0100Tpl.ProvinceID))),
		CityID:         uint16(atoi(matchTokenAndGen(msg0100Tpl.CityID))),
		ManufacturerID: msg0100Tpl.ManufacturerID,
		DeviceMode:     msg0100Tpl.DeviceMode,
		DeviceID:       device.ID,
		PlateColor:     byte(atoi(matchTokenAndGen(msg0100Tpl.PlateColor))),
		PlateNumber:    device.PlateNumber,
	}
}

type msg0200Tpl struct {
	AlarmSign  string `json:"alarmSign"`  // 不大于uint32
	StatusSign string `json:"statusSign"` // 不大于uint32
	Latitude   string `json:"latitude"`
	Longitude  string `json:"longitude"`
	Altitude   string `json:"altitude"`
	Speed      string `json:"speed"`
	Direction  string `json:"direction"`
}

func GenMsg0200(device *model.Device) *model.Msg0200 {
	tpl, _ := os.ReadFile("./configs/msg0200.tpl.json")
	msg0200Tpl := msg0200Tpl{}
	_ = json.Unmarshal(tpl, &msg0200Tpl)
	msg := &model.Msg0200{
		Header:    genMsgHeader(model.MsgID0200, device),
		Latitude:  116307629,
		Longitude: 40058359,
		Altitude:  312,
		Speed:     3,
		Direction: 99,
		Time:      "200707192359",
	}
	alarmSign := atoi(matchTokenAndGen(msg0200Tpl.AlarmSign))
	if alarmSign > math.MaxUint32 {
		alarmSign = math.MaxUint32
	}
	msg.AlarmSign = uint32(alarmSign)
	statusSign := atoi(matchTokenAndGen(msg0200Tpl.StatusSign))
	if statusSign > math.MaxUint32 {
		statusSign = math.MaxUint32
	}
	msg.StatusSign = uint32(statusSign)
	return msg
}
