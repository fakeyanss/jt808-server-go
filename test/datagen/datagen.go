package datagen

import (
	"math/rand"
	"strconv"
	"time"

	regen "github.com/AnatolyRugalev/goregen"
	"github.com/rs/zerolog/log"

	"github.com/fakeyanss/jt808-server-go/internal/codec/hex"
	"github.com/fakeyanss/jt808-server-go/internal/config"
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

func GenDevice(deviceConf *config.DeviceConf) *model.Device {
	device := &model.Device{
		ID:              matchTokenAndGen(deviceConf.IDReg),
		Plate:           matchTokenAndGen(deviceConf.PlateReg),
		Phone:           matchTokenAndGen(deviceConf.PhoneReg),
		Keepalive:       time.Duration(deviceConf.Keepalive) * 1000 * 1000 * 1000, // s -> ns
		IMEI:            matchTokenAndGen(deviceConf.IMEIReg),
		SoftwareVersion: "fakeyanss.github.io",
	}
	switch deviceConf.ProtocolVersion {
	case "2019":
		device.VersionDesc = model.Version2019
	case "2013":
		device.VersionDesc = model.Version2013
	case "2011":
		device.VersionDesc = model.Version2011
	}
	log.Debug().Str("device", device.Phone).Msgf("Generate random device=%+v", device)
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
		PhoneNumber:     device.Phone,
		SerialNumber:    1,
		Frag:            nil,
	}
	if device.VersionDesc == model.Version2019 {
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

func GenMsg0100(deviceConf *config.DeviceConf, device *model.Device) *model.Msg0100 {
	return &model.Msg0100{
		Header:         genMsgHeader(model.MsgID0100, device),
		ProvinceID:     uint16(atoi(matchTokenAndGen(deviceConf.ProvinceIDReg))),
		CityID:         uint16(atoi(matchTokenAndGen(deviceConf.CityIDReg))),
		ManufacturerID: "fakeyanss",
		DeviceMode:     "fakeyanss",
		DeviceID:       device.ID,
		PlateColor:     byte(atoi(matchTokenAndGen(deviceConf.PlateColorReg))),
		PlateNumber:    device.Plate,
	}
}

func genGeoMeta(conf *config.DeviceGeoConf) *model.GeoMeta {
	return &model.GeoMeta{
		ACCStatus:              byte(atoi(matchTokenAndGen(conf.Geo.ACCStatusReg))),
		LocationStatus:         byte(atoi(matchTokenAndGen(conf.Geo.LocationStatusReg))),
		LatitudeType:           byte(atoi(matchTokenAndGen(conf.Geo.LatitudeTypeReg))),
		LongitudeType:          byte(atoi(matchTokenAndGen(conf.Geo.LongitudeTypeReg))),
		OperatingStatus:        byte(atoi(matchTokenAndGen(conf.Geo.OperatingStatusReg))),
		GeoEncryptionStatus:    byte(atoi(matchTokenAndGen(conf.Geo.GeoEncryptionStatusReg))),
		LoadStatus:             byte(atoi(matchTokenAndGen(conf.Geo.LoadStatusReg))),
		FuelSystemStatus:       byte(atoi(matchTokenAndGen(conf.Geo.FuelSystemStatusReg))),
		AlternatorSystemStatus: byte(atoi(matchTokenAndGen(conf.Geo.AlternatorSystemStatusReg))),
		DoorLockedStatus:       byte(atoi(matchTokenAndGen(conf.Geo.DoorLockedStatusReg))),
		FrontDoorStatus:        byte(atoi(matchTokenAndGen(conf.Geo.FrontDoorStatusReg))),
		MidDoorStatus:          byte(atoi(matchTokenAndGen(conf.Geo.MidDoorStatusReg))),
		BackDoorStatus:         byte(atoi(matchTokenAndGen(conf.Geo.BackDoorStatusReg))),
		DriverDoorStatus:       byte(atoi(matchTokenAndGen(conf.Geo.DriverDoorStatusReg))),
		CustomDoorStatus:       byte(atoi(matchTokenAndGen(conf.Geo.CustomDoorStatusReg))),
		GPSLocationStatus:      byte(atoi(matchTokenAndGen(conf.Geo.GPSLocationStatusReg))),
		BeidouLocationStatus:   byte(atoi(matchTokenAndGen(conf.Geo.BeidouLocationStatusReg))),
		GLONASSLocationStatus:  byte(atoi(matchTokenAndGen(conf.Geo.GLONASSLocationStatusReg))),
		GalileoLocationStatus:  byte(atoi(matchTokenAndGen(conf.Geo.GalileoLocationStatusReg))),
		DrivingStatus:          byte(atoi(matchTokenAndGen(conf.Geo.DrivingStatusReg))),
	}
}

func GenDeviceGeo(conf *config.DeviceGeoConf, device *model.Device) *model.DeviceGeo {
	deviceGeo := &model.DeviceGeo{
		Phone: device.Phone,
		Geo:   genGeoMeta(conf),
		Location: &model.Location{
			Latitude:  float64(atoi(matchTokenAndGen(conf.Location.LatitudeReg))),
			Longitude: float64(atoi(matchTokenAndGen(conf.Location.LongitudeReg))),
			Altitude:  uint16(atoi(matchTokenAndGen(conf.Location.AltitudeReg))),
		},
		Drive: &model.Drive{
			Speed:     float64(atoi(matchTokenAndGen(conf.Drive.SpeedReg))),
			Direction: uint16(atoi(matchTokenAndGen(conf.Drive.DirectionReg))),
		},
	}

	return deviceGeo
}

func init() {
	rand.Seed(time.Now().UnixNano())
}

func GenMsg0200(conf *config.DeviceGeoConf, device *model.Device, deviceGeo *model.DeviceGeo) *model.Msg0200 {
	latitudeOffset := rand.Float64()*2 - 1  // [-1,1]
	longitudeOffset := rand.Float64()*2 - 1 // [-1,1]
	altitudeOffset := rand.Intn(11) - 5     // [-5,5]
	speedOffset := rand.Float64()*20 - 10   // [-10, 10]
	nextDirection := rand.Intn(360)
	m := &model.Msg0200{
		Header:     genMsgHeader(model.MsgID0200, device),
		AlarmSign:  1024,                      // 暂不支持
		StatusSign: genGeoMeta(conf).Encode(), // 每次生成新的status
		Latitude:   uint32((deviceGeo.Location.Latitude + latitudeOffset) * model.LocationAccuracy),
		Longitude:  uint32((deviceGeo.Location.Longitude + longitudeOffset) * model.LocationAccuracy),
		Altitude:   deviceGeo.Location.Altitude + uint16(altitudeOffset),
		Speed:      uint16((deviceGeo.Drive.Speed + speedOffset) * model.SpeedAccuracy),
		Direction:  uint16(nextDirection),
	}
	m.Time = hex.FormatTime(time.Now())

	// uint降至0后，再-1变为uint最大值。这里直接重设一个速度。
	if m.Latitude > 90*model.LocationAccuracy {
		m.Latitude = 90 * model.LocationAccuracy
	}
	if m.Longitude > 180*model.LocationAccuracy {
		m.Longitude = 180 * model.LocationAccuracy
	}
	if m.Altitude > 5000 {
		m.Altitude = 1000
	}
	if m.Speed > 300*model.SpeedAccuracy {
		m.Speed = 100 * model.SpeedAccuracy
	}
	return m
}
