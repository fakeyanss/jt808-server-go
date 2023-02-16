package datagen

import (
	"time"

	"github.com/fakeYanss/jt808-server-go/internal/protocol/model"
)

func GenDevice() *model.Device {
	return &model.Device{
		ID:          "1234ABCD",
		PlateNumber: "京A12345",
		PhoneNumber: "12345678901234567890",
		Keepalive:   60 * time.Second,

		ProtocalVersion: model.Version2019,
		AuthCode:        "test",
		IMEI:            "qwerasdf",
		SoftwareVersion: "v1",
	}
}

func GenMsgHeader(msgID uint16) *model.MsgHeader {
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
		PhoneNumber:     "12345678901234567890",
		SerialNumber:    1,
		Frag:            nil,
	}
	return msgHeader
}

func GenMsg0100() *model.Msg0100 {
	return &model.Msg0100{
		Header:         GenMsgHeader(0x0100),
		ProvinceID:     31,
		CityID:         115,
		ManufacturerID: "fakeyanss",
		DeviceMode:     "fakeyanss.github.io",
		DeviceID:       "1234ABCD",
		PlateColor:     1,
		PlateNumber:    "京A12345",
	}
}
