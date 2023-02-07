package model

import "net"

type DeviceStatus int8

const (
	DeviceOffline  DeviceStatus = 0
	DeviceOnline   DeviceStatus = 1
	DeviceSleeping DeviceStatus = 2
)

type Device struct {
	ID          string            `json:"id"`
	PlateNumber string            `json:"plateNumber"`
	PhoneNumber string            `json:"phoneNumber"`
	SessionID   string            `json:"sessionId"`
	TransProto  TransportProtocol `json:"transProto"`
	Conn        net.Conn          `json:"conn"`
	Authed      bool              `json:"authed"`      // 是否鉴权通过
	LastComTime int64             `json:"lastComTime"` // 最近一次交互时间
	Status      DeviceStatus      `json:"status"`
}

type DeviceGis struct {
	ID  string  `json:"id"`
	GIS GISMeta `json:"gis"`
}

// 地理位置信息状态位字段的bit位
const (
	accBit                    uint32 = 0b00000000000000000000000000000001
	locationStatusBit         uint32 = 0b00000000000000000000000000000010
	LatitudeTypeBit           uint32 = 0b00000000000000000000000000000100
	LongitudeTypeBit          uint32 = 0b00000000000000000000000000001000
	operatingStatusBit        uint32 = 0b00000000000000000000000000010000
	gisEncryptionStatusBit    uint32 = 0b00000000000000000000000000100000
	loadStatusBit             uint32 = 0b00000000000000000000001100000000
	fuelSystemStatusBit       uint32 = 0b00000000000000000000010000000000
	alternatorSystemStatusBit uint32 = 0b00000000000000000000100000000000
	doorLockedStatusBit       uint32 = 0b00000000000000000001000000000000
	frontDoorStatusBit        uint32 = 0b00000000000000000010000000000000
	midDoorStatusBit          uint32 = 0b00000000000000000100000000000000
	backDoorStatusBit         uint32 = 0b00000000000000001000000000000000
	driverDoorStatusBit       uint32 = 0b00000000000000010000000000000000
	customDoorStatusBit       uint32 = 0b00000000000000100000000000000000
	gpsLocationStatusBit      uint32 = 0b00000000000001000000000000000000
	beidouLocatlonStatusBit   uint32 = 0b00000000000010000000000000000000
	glonassLocationStatusBit  uint32 = 0b00000000000100000000000000000000
	galileoLocationStatusBit  uint32 = 0b00000000001000000000000000000000
	drivingStatusBit          uint32 = 0b00000000010000000000000000000000
)

type GISMeta struct {
	ACCStatus           uint8 `json:"accStatus"`           // bit0, 0:ACC 关;1: ACC 开
	LocationStatus      uint8 `json:"locationStatus"`      // bit1, 0:未定位;1:定位
	LatitudeType        uint8 `json:"latitudeType"`        // bit2, 0:北纬;1:南纬
	LongitudeType       uint8 `json:"longitudeType"`       // bit3, 0:东经;1:西经
	OperatingStatus     uint8 `json:"operatingStatus"`     // bit4, 0:运营状态;1:停运状态
	GISEncryptionStatus uint8 `json:"gisEncryptionStatus"` // bit5, 0:经纬度未经保密插件加密;1:经纬度已经保密插件加密

	// bit6-7位保留

	LoadStatus             uint8 `json:"loadStatus"`             // bit8-9, 00:空车;01:半载;10:保留;11:满载 (可用于客车的空、重车及货车的空载、满载状态表示，人工输入或传感器获取)
	FuelSystemStatus       uint8 `json:"FuelSystemStatus"`       // bit10, 0:车辆油路正常;1:车辆油路断开
	AlternatorSystemStatus uint8 `json:"AlternatorSystemStatus"` // bit11, 0:车辆电路正常;1:车辆电路断开
	DoorLockedStatus       uint8 `json:"DoorLockedStatus"`       // bit12, 0:车门解锁;1:车门加锁
	FrontDoorStatus        uint8 `json:"frontDoorStatus"`        // bit13, 0:门 1 关;1:门 1 开(前门)
	MidDoorStatus          uint8 `json:"midDoorStatus"`          // bit14, 0:门 2 关;1:门 2 开(中门)
	BackDoorStatus         uint8 `json:"backDoorStatus"`         // bit15, 0:门 3 关;1:门 3 开(后门)
	DriverDoorStatus       uint8 `json:"driverDoorStatus"`       // bit16, 0:门 4 关;1:门 4 开(驾驶席门)
	CustomDoorStatus       uint8 `json:"customDoorStatus"`       // bit17, 0:门 5 关;1:门 5 开(自定义)
	GPSLocationStatus      uint8 `json:"gpsLocationStatus"`      // bit18, 0:未使用 GPS 卫星进行定位;1:使用 GPS 卫星进行定位
	BeidouLocatlonStatus   uint8 `json:"beidouLocationStatus"`   // bit19, 0:未使用北斗卫星进行定位;1:使用北斗卫星进行定位
	GLONASSLocationStatus  uint8 `json:"glonassLocationStatus"`  // bit20, 0:未使用 GLONASS 卫星进行定位;1:使用 GLONASS 卫星进行定位
	GalileoLocationStatus  uint8 `json:"galileoLocationStatus"`  // bit21, 0:未使用 Galileo 卫星进行定位;1:使用 Galileo 卫星进行定位
	DrivingStatus          uint8 `json:"drivingStatus"`          // bit22, 0:车辆处于停止状态;1:车辆处于行驶状态

	// bit23-31位保留
}

// 输入Msg0200的Status，按照协议解码GISMeta结构体
func (g *GISMeta) Decode(status uint32) {
	g.ACCStatus = uint8(status & accBit)
	g.LocationStatus = uint8((status & loadStatusBit) >> 1)
	g.LatitudeType = uint8((status & LatitudeTypeBit) >> 2)
	g.LongitudeType = uint8((status & LongitudeTypeBit) >> 3)
	g.OperatingStatus = uint8((status & operatingStatusBit) >> 4)
	g.GISEncryptionStatus = uint8((status & gisEncryptionStatusBit) >> 5)
	g.LoadStatus = uint8((status & loadStatusBit) >> 8)
	g.FuelSystemStatus = uint8((status & fuelSystemStatusBit) >> 10)
	g.AlternatorSystemStatus = uint8((status & alternatorSystemStatusBit) >> 11)
	g.DoorLockedStatus = uint8((status & doorLockedStatusBit) >> 12)
	g.FrontDoorStatus = uint8((status & frontDoorStatusBit) >> 13)
	g.MidDoorStatus = uint8((status & midDoorStatusBit) >> 14)
	g.BackDoorStatus = uint8((status & backDoorStatusBit) >> 15)
	g.DriverDoorStatus = uint8((status & driverDoorStatusBit) >> 16)
	g.CustomDoorStatus = uint8((status & customDoorStatusBit) >> 17)
	g.GPSLocationStatus = uint8((status & gpsLocationStatusBit) >> 18)
	g.BeidouLocatlonStatus = uint8((status & beidouLocatlonStatusBit) >> 19)
	g.GLONASSLocationStatus = uint8((status & glonassLocationStatusBit) >> 20)
	g.GalileoLocationStatus = uint8((status & galileoLocationStatusBit) >> 21)
	g.DrivingStatus = uint8((status & drivingStatusBit) >> 22)
}

type AlarmMeta struct {
}
