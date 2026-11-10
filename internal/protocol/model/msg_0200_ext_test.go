package model

import (
	"testing"

	"github.com/fakeyanss/jt808-server-go/internal/codec/hex"
	"github.com/stretchr/testify/require"
)

const (
	testSubiaoDeviceID   = "ABC1234"
	testBase2019DeviceID = "123456789012345678901234567890"
)

func buildTestAlarmIDNumber(deviceID string, domain ExtDomain) []byte {
	data := hex.WriteString(nil, deviceID)
	data = hex.WriteBCD(data, "210101120000")
	data = hex.WriteByte(data, 5)
	data = hex.WriteByte(data, 2)
	for i := 0; i < reservedLength(domain); i++ {
		data = hex.WriteByte(data, uint8(0xA0+i))
	}
	return data
}

func buildTestADAS(domain ExtDomain) []byte {
	data := hex.WriteDoubleWord(nil, 0x01020304)
	data = hex.WriteByte(data, 0x01)
	data = hex.WriteByte(data, 0x01)
	data = hex.WriteByte(data, 0x01)
	data = hex.WriteByte(data, 60)
	data = hex.WriteByte(data, 20)
	data = hex.WriteByte(data, 0x01)
	data = hex.WriteByte(data, 0x01)
	data = hex.WriteByte(data, 80)
	data = hex.WriteByte(data, 70)
	data = hex.WriteWord(data, 100)
	data = hex.WriteDoubleWord(data, 0x07D5A8C0)
	data = hex.WriteDoubleWord(data, 0x0E5A9FA0)
	data = hex.WriteBCD(data, "201231235959")
	data = hex.WriteWord(data, 0x0003)
	return append(data, buildTestAlarmIDNumber(testDeviceID(domain), domain)...)
}

func buildTestDSM(domain ExtDomain) []byte {
	data := hex.WriteDoubleWord(nil, 0x11223344)
	data = hex.WriteByte(data, 0x02)
	data = hex.WriteByte(data, 0x02)
	data = hex.WriteByte(data, 0x02)
	data = hex.WriteByte(data, 5)
	data = hex.WriteBytes(data, []byte{0x11, 0x22, 0x33, 0x44})
	data = hex.WriteByte(data, 66)
	data = hex.WriteWord(data, 200)
	data = hex.WriteDoubleWord(data, 0x07D5A8C1)
	data = hex.WriteDoubleWord(data, 0x0E5A9FA1)
	data = hex.WriteBCD(data, "210101120000")
	data = hex.WriteWord(data, 0x0011)
	return append(data, buildTestAlarmIDNumber(testDeviceID(domain), domain)...)
}

func buildTestTPMS(domain ExtDomain) []byte {
	data := hex.WriteDoubleWord(nil, 0x55667788)
	data = hex.WriteByte(data, 0x01)
	data = hex.WriteByte(data, 55)
	data = hex.WriteWord(data, 150)
	data = hex.WriteDoubleWord(data, 0x07D5A8C2)
	data = hex.WriteDoubleWord(data, 0x0E5A9FA2)
	data = hex.WriteBCD(data, "220202030405")
	data = hex.WriteWord(data, 0x0022)
	data = append(data, buildTestAlarmIDNumber(testDeviceID(domain), domain)...)
	data = hex.WriteByte(data, 2)
	for i := 0; i < 2; i++ {
		data = hex.WriteWord(data, uint16(i))
		data = hex.WriteWord(data, 1)
		data = hex.WriteWord(data, uint16(230+i))
		data = hex.WriteWord(data, uint16(35+i))
		data = hex.WriteWord(data, uint16(80+i))
	}
	return data
}

func buildTestBSD(domain ExtDomain) []byte {
	data := hex.WriteDoubleWord(nil, 0x99AABBCC)
	data = hex.WriteByte(data, 0x02)
	data = hex.WriteByte(data, 0x02)
	data = hex.WriteByte(data, 45)
	data = hex.WriteWord(data, 300)
	data = hex.WriteDoubleWord(data, 0x07D5A8C3)
	data = hex.WriteDoubleWord(data, 0x0E5A9FA3)
	data = hex.WriteBCD(data, "230303040506")
	data = hex.WriteWord(data, 0x0033)
	return append(data, buildTestAlarmIDNumber(testDeviceID(domain), domain)...)
}

func testDeviceID(domain ExtDomain) string {
	if domain == ExtDomainBase2019 {
		return testBase2019DeviceID
	}
	return testSubiaoDeviceID
}

func buildTestExtItems(domain ExtDomain) []LocationExtItem {
	return []LocationExtItem{
		{ID: 0x64, Length: uint8(len(buildTestADAS(domain))), Data: buildTestADAS(domain)},
		{ID: 0x65, Length: uint8(len(buildTestDSM(domain))), Data: buildTestDSM(domain)},
		{ID: 0x66, Length: uint8(len(buildTestTPMS(domain))), Data: buildTestTPMS(domain)},
		{ID: 0x67, Length: uint8(len(buildTestBSD(domain))), Data: buildTestBSD(domain)},
		{ID: 0xE0, Length: 3, Data: []byte{0xDE, 0xAD, 0xBE}},
	}
}

func msg0200WithDomain(domain ExtDomain, items []LocationExtItem) *Msg0200 {
	version := Version2013
	if domain == ExtDomainBase2019 {
		version = Version2019
	}
	return &Msg0200{
		Header:   &MsgHeader{Attr: &MsgBodyAttr{VersionDesc: version}},
		ExtItems: items,
	}
}

func TestParseExtItemsSubiaoFields(t *testing.T) {
	msg := msg0200WithDomain(ExtDomainSubiao, buildTestExtItems(ExtDomainSubiao))
	parsed := msg.ParseExtItems()
	require.Len(t, parsed, 5)

	adas, ok := parsed[0].(ExtAlarmADAS)
	require.True(t, ok)
	require.Equal(t, uint8(0x64), adas.ExtID)
	require.Equal(t, ExtTypeADAS, adas.ExtType)
	require.Equal(t, ExtDomainSubiao, adas.ExtDomain)
	require.Equal(t, uint32(0x01020304), adas.AlarmID)
	require.Equal(t, uint8(0x01), adas.FlagState)
	require.Equal(t, uint8(0x01), adas.AlarmOrEventType)
	require.Equal(t, uint8(0x01), adas.AlarmLevel)
	require.Equal(t, uint8(60), adas.FrontVehicleSpeed)
	require.Equal(t, uint8(20), adas.FrontVehicleDistance)
	require.Equal(t, uint8(0x01), adas.DeviationType)
	require.Equal(t, uint8(0x01), adas.RoadSignType)
	require.Equal(t, uint8(80), adas.RoadSignData)
	require.Equal(t, uint8(70), adas.Speed)
	require.Equal(t, uint16(100), adas.Altitude)
	require.Equal(t, uint32(0x07D5A8C0), adas.Latitude)
	require.Equal(t, uint32(0x0E5A9FA0), adas.Longitude)
	require.Equal(t, "201231235959", adas.DateTime)
	require.Equal(t, uint16(0x0003), adas.VehicleState)
	require.Equal(t, testSubiaoDeviceID, adas.AlarmIDNumber.DeviceID)
	require.Equal(t, "210101120000", adas.AlarmIDNumber.Time)
	require.Equal(t, uint8(5), adas.AlarmIDNumber.Sequence)
	require.Equal(t, uint8(2), adas.AlarmIDNumber.AttachmentCount)
	require.Equal(t, []byte{0xA0}, adas.AlarmIDNumber.Reserved)

	dsm, ok := parsed[1].(ExtAlarmDSM)
	require.True(t, ok)
	require.Equal(t, ExtTypeDSM, dsm.ExtType)
	require.Equal(t, uint32(0x11223344), dsm.AlarmID)
	require.Equal(t, uint8(0x02), dsm.AlarmOrEventType)
	require.Equal(t, uint8(5), dsm.FatigueDegree)
	require.Equal(t, []byte{0x11, 0x22, 0x33, 0x44}, dsm.Reserved)
	require.Equal(t, uint8(66), dsm.Speed)
	require.Equal(t, uint16(200), dsm.Altitude)
	require.Equal(t, "210101120000", dsm.DateTime)

	tpms, ok := parsed[2].(ExtAlarmTPMS)
	require.True(t, ok)
	require.Equal(t, ExtTypeTPMS, tpms.ExtType)
	require.Equal(t, uint32(0x55667788), tpms.AlarmID)
	require.Equal(t, uint16(0x0022), tpms.VehicleState)
	require.Len(t, tpms.Items, 2)
	require.Equal(t, ExtTPMSItem{
		Position: 0, AlarmOrEventType: 1, Pressure: 230, Temperature: 35, BatteryLevel: 80,
	}, tpms.Items[0])
	require.Equal(t, ExtTPMSItem{
		Position: 1, AlarmOrEventType: 1, Pressure: 231, Temperature: 36, BatteryLevel: 81,
	}, tpms.Items[1])

	bsd, ok := parsed[3].(ExtAlarmBSD)
	require.True(t, ok)
	require.Equal(t, ExtTypeBSD, bsd.ExtType)
	require.Equal(t, uint32(0x99AABBCC), bsd.AlarmID)
	require.Equal(t, uint8(45), bsd.Speed)
	require.Equal(t, uint16(300), bsd.Altitude)
	require.Equal(t, "230303040506", bsd.DateTime)

	raw, ok := parsed[4].(ExtRawItem)
	require.True(t, ok)
	require.Equal(t, uint8(0xE0), raw.ExtID)
	require.Equal(t, ExtTypeRaw, raw.ExtType)
	require.Equal(t, ExtDomainSubiao, raw.ExtDomain)
	require.Equal(t, uint8(3), raw.Length)
	require.Equal(t, []byte{0xDE, 0xAD, 0xBE}, raw.Raw)
	require.Equal(t, []byte{0xDE, 0xAD, 0xBE}, ExtRawBytes(parsed))
}

func TestParseExtItemsBase2019Fields(t *testing.T) {
	msg := msg0200WithDomain(ExtDomainBase2019, buildTestExtItems(ExtDomainBase2019))
	parsed := msg.ParseExtItems()
	require.Len(t, parsed, 5)

	adas, ok := parsed[0].(ExtAlarmADAS)
	require.True(t, ok)
	require.Equal(t, ExtDomainBase2019, adas.ExtDomain)
	require.Equal(t, testBase2019DeviceID, adas.AlarmIDNumber.DeviceID)
	require.Equal(t, []byte{0xA0, 0xA1}, adas.AlarmIDNumber.Reserved)

	tpms, ok := parsed[2].(ExtAlarmTPMS)
	require.True(t, ok)
	require.Equal(t, testBase2019DeviceID, tpms.AlarmIDNumber.DeviceID)
	require.Len(t, tpms.Items, 2)
}

func TestParseExtItemsDoesNotCrossDomains(t *testing.T) {
	subiaoData := buildTestADAS(ExtDomainSubiao)
	base2019Data := buildTestADAS(ExtDomainBase2019)

	underSubiao := msg0200WithDomain(ExtDomainSubiao, []LocationExtItem{
		{ID: 0x64, Length: uint8(len(subiaoData)), Data: subiaoData},
	}).ParseExtItems()
	require.IsType(t, ExtAlarmADAS{}, underSubiao[0])

	// 苏标数据用 base2019 域解析时长度不符，必须整体退化为 raw，而不是截断字段。
	crossParsed := msg0200WithDomain(ExtDomainBase2019, []LocationExtItem{
		{ID: 0x64, Length: uint8(len(subiaoData)), Data: subiaoData},
	}).ParseExtItems()
	require.IsType(t, ExtRawItem{}, crossParsed[0])

	// 反向同理：base2019 数据不能按苏标 16 字节报警标识号截断解析。
	reverseParsed := msg0200WithDomain(ExtDomainSubiao, []LocationExtItem{
		{ID: 0x64, Length: uint8(len(base2019Data)), Data: base2019Data},
	}).ParseExtItems()
	require.IsType(t, ExtRawItem{}, reverseParsed[0])

	base2019Parsed := msg0200WithDomain(ExtDomainBase2019, []LocationExtItem{
		{ID: 0x64, Length: uint8(len(base2019Data)), Data: base2019Data},
	}).ParseExtItems()
	base2019ADAS, ok := base2019Parsed[0].(ExtAlarmADAS)
	require.True(t, ok)
	require.Equal(t, testBase2019DeviceID, base2019ADAS.AlarmIDNumber.DeviceID)
	require.NotEqual(t, underSubiao[0].(ExtAlarmADAS).AlarmIDNumber.DeviceID, base2019ADAS.AlarmIDNumber.DeviceID)
}

func TestParseExtItemsUnknownIDNeverFails(t *testing.T) {
	parsed := ParseExtItems([]LocationExtItem{
		{ID: 0xE0, Length: 1, Data: []byte{0x01}},
	}, ExtDomainSubiao)
	require.Len(t, parsed, 1)
	raw, ok := parsed[0].(ExtRawItem)
	require.True(t, ok)
	require.Equal(t, []byte{0x01}, raw.Raw)
}

func TestParseExtItemsNilHeaderDefaultsToSubiao(t *testing.T) {
	data := buildTestADAS(ExtDomainSubiao)
	msg := &Msg0200{ExtItems: []LocationExtItem{{ID: 0x64, Length: uint8(len(data)), Data: data}}}
	require.IsType(t, ExtAlarmADAS{}, msg.ParseExtItems()[0])
}

func buildRealistic0200Body(alarmSign uint32, items []LocationExtItem) []byte {
	data := hex.WriteDoubleWord(nil, alarmSign)
	data = hex.WriteDoubleWord(data, 0x00000003)
	data = hex.WriteDoubleWord(data, 0x07D5A8C0)
	data = hex.WriteDoubleWord(data, 0x0E5A9FA0)
	data = hex.WriteWord(data, 100)
	data = hex.WriteWord(data, 70)
	data = hex.WriteWord(data, 90)
	data = hex.WriteBCD(data, "201231235959")
	for _, item := range items {
		data = hex.WriteByte(data, item.ID)
		data = hex.WriteByte(data, uint8(len(item.Data)))
		data = hex.WriteBytes(data, item.Data)
	}
	return data
}

// 真实风格报文：附加信息 TLV 长度按标准使用 1 字节，包含 0x64/0x65
// 苏标扩展与一个未知 ID，验证整条链路解码、字段解析与 raw 兜底。
func TestMsg0200DecodeRealStyleTLV(t *testing.T) {
	adas := buildTestADAS(ExtDomainSubiao)
	dsm := buildTestDSM(ExtDomainSubiao)
	unknown := []byte{0xDE, 0xAD, 0xBE, 0xEF}
	items := []LocationExtItem{
		{ID: 0x64, Length: uint8(len(adas)), Data: adas},
		{ID: 0x65, Length: uint8(len(dsm)), Data: dsm},
		{ID: 0xE0, Length: uint8(len(unknown)), Data: unknown},
	}
	body := buildRealistic0200Body(0x00000001, items)
	header := &MsgHeader{
		MsgID:        MsgID0200,
		PhoneNumber:  "188765432120",
		SerialNumber: 7,
		Attr:         &MsgBodyAttr{VersionSign: 0, VersionDesc: Version2013},
	}

	msg := &Msg0200{}
	require.NoError(t, msg.Decode(&PacketData{Header: header, Body: body}))
	require.Len(t, msg.ExtItems, 3)
	require.Equal(t, items[0], msg.ExtItems[0])
	require.Equal(t, items[1], msg.ExtItems[1])
	require.Equal(t, items[2], msg.ExtItems[2])

	parsed := msg.ParseExtItems()
	require.Len(t, parsed, 3)
	adasParsed, ok := parsed[0].(ExtAlarmADAS)
	require.True(t, ok)
	require.Equal(t, uint8(0x01), adasParsed.AlarmOrEventType)
	require.Equal(t, uint32(0x07D5A8C0), adasParsed.Latitude)
	require.Equal(t, testSubiaoDeviceID, adasParsed.AlarmIDNumber.DeviceID)

	dsmParsed, ok := parsed[1].(ExtAlarmDSM)
	require.True(t, ok)
	require.Equal(t, uint8(0x02), dsmParsed.AlarmOrEventType)
	require.Equal(t, uint8(5), dsmParsed.FatigueDegree)

	raw, ok := parsed[2].(ExtRawItem)
	require.True(t, ok)
	require.Equal(t, uint8(0xE0), raw.ExtID)
	require.Equal(t, uint8(len(unknown)), raw.Length)
	require.Equal(t, unknown, raw.Raw)
	require.Equal(t, unknown, ExtRawBytes(parsed))

	// 编码侧必须同样写 1 字节长度，且与解码输入逐字节一致。
	encoded, err := msg.Encode()
	require.NoError(t, err)
	encodedBody := encoded[len(encoded)-int(msg.Header.Attr.BodyLength):]
	require.Equal(t, body, encodedBody)
}

// AlarmIDNumberBytes 必须按各扩展项的固定布局截取，而不是简单取尾部：
// 0x66 的报警标识号后面还跟着轮胎列表，尾部截取会读错字节。
func TestAlarmIDNumberBytesFromEachExtItem(t *testing.T) {
	expected := buildTestAlarmIDNumber(testSubiaoDeviceID, ExtDomainSubiao)
	cases := []struct {
		id   uint8
		data []byte
	}{
		{id: 0x64, data: buildTestADAS(ExtDomainSubiao)},
		{id: 0x65, data: buildTestDSM(ExtDomainSubiao)},
		{id: 0x66, data: buildTestTPMS(ExtDomainSubiao)},
		{id: 0x67, data: buildTestBSD(ExtDomainSubiao)},
	}
	for _, item := range cases {
		msg := msg0200WithDomain(ExtDomainSubiao, []LocationExtItem{
			{ID: item.id, Length: uint8(len(item.data)), Data: item.data},
		})
		require.Equal(t, expected, msg.AlarmIDNumberBytes(), "ext id 0x%02x", item.id)
	}
}

func TestAlarmIDNumberBytesUnavailable(t *testing.T) {
	// 没有 0x64~0x67 扩展项。
	require.Nil(t, msg0200WithDomain(ExtDomainSubiao, nil).AlarmIDNumberBytes())

	// 长度与定义不符时不得截取。
	broken := buildTestADAS(ExtDomainSubiao)
	msg := msg0200WithDomain(ExtDomainSubiao, []LocationExtItem{
		{ID: 0x64, Length: uint8(len(broken) - 1), Data: broken[:len(broken)-1]},
	})
	require.Nil(t, msg.AlarmIDNumberBytes())

	// base2019 的报警标识号为 40 字节，不能截断成苏标 16 字节。
	baseItems := buildTestExtItems(ExtDomainBase2019)
	require.Nil(t, msg0200WithDomain(ExtDomainBase2019, baseItems).AlarmIDNumberBytes())
}

func TestAlarmIDNumberBytesReturnsCopy(t *testing.T) {
	data := buildTestADAS(ExtDomainSubiao)
	msg := msg0200WithDomain(ExtDomainSubiao, []LocationExtItem{{ID: 0x64, Length: uint8(len(data)), Data: data}})
	got := msg.AlarmIDNumberBytes()
	require.Len(t, got, 16)
	got[0] ^= 0xFF
	require.NotEqual(t, got, msg.AlarmIDNumberBytes())
}

func buildBaseExtItem(id uint8, data []byte) LocationExtItem {
	return LocationExtItem{ID: id, Length: uint8(len(data)), Data: data}
}

func buildTirePressureData() []byte {
	data := hex.WriteWord(nil, 230)
	data = hex.WriteWord(data, 231)
	data = hex.WriteWord(data, 232)
	for i := 3; i < 15; i++ {
		data = hex.WriteWord(data, 0xFFFF)
	}
	return data
}

func buildBaseExtItems(domain ExtDomain) []LocationExtItem {
	items := []LocationExtItem{
		buildBaseExtItem(0x01, hex.WriteDoubleWord(nil, 12345)),
		buildBaseExtItem(0x02, hex.WriteWord(nil, 678)),
		buildBaseExtItem(0x03, hex.WriteWord(nil, 789)),
		buildBaseExtItem(0x04, hex.WriteWord(nil, 5)),
	}
	if domain == ExtDomainBase2019 {
		items = append(items,
			buildBaseExtItem(0x05, buildTirePressureData()),
			buildBaseExtItem(0x06, hex.WriteWord(nil, uint16(0xFF9C))),
		)
	}
	return append(items,
		buildBaseExtItem(0x11, hex.WriteDoubleWord(hex.WriteByte(nil, 2), 0x01020304)),
		buildBaseExtItem(0x11, hex.WriteByte(nil, 0)),
		buildBaseExtItem(0x12, hex.WriteByte(hex.WriteDoubleWord(hex.WriteByte(nil, 3), 0x05060708), 1)),
		buildBaseExtItem(0x13, hex.WriteByte(hex.WriteWord(hex.WriteDoubleWord(nil, 0x0A0B0C0D), 600), 1)),
		buildBaseExtItem(0x25, hex.WriteDoubleWord(nil, 0x00007FFF)),
		buildBaseExtItem(0x2A, hex.WriteWord(nil, 0x0003)),
		buildBaseExtItem(0x2B, hex.WriteDoubleWord(nil, 0x00050006)),
		buildBaseExtItem(0x30, []byte{23}),
		buildBaseExtItem(0x31, []byte{12}),
	)
}

func TestParseExtItemsBaseFields2019(t *testing.T) {
	parsed := msg0200WithDomain(ExtDomainBase2019, buildBaseExtItems(ExtDomainBase2019)).ParseExtItems()
	require.Len(t, parsed, 15)

	mileage, ok := parsed[0].(ExtMileage)
	require.True(t, ok)
	require.Equal(t, ExtTypeMileage, mileage.ExtType)
	require.Equal(t, ExtDomainBase2019, mileage.ExtDomain)
	require.Equal(t, uint32(12345), mileage.Mileage)

	oil, ok := parsed[1].(ExtOil)
	require.True(t, ok)
	require.Equal(t, uint16(678), oil.Oil)

	recordedSpeed, ok := parsed[2].(ExtRecordedSpeed)
	require.True(t, ok)
	require.Equal(t, uint16(789), recordedSpeed.Speed)

	manualAlarm, ok := parsed[3].(ExtManualAlarmID)
	require.True(t, ok)
	require.Equal(t, uint16(5), manualAlarm.AlarmID)

	tirePressure, ok := parsed[4].(ExtTirePressure)
	require.True(t, ok)
	require.Equal(t, ExtTypeTirePressure, tirePressure.ExtType)
	require.Len(t, tirePressure.Pressures, 15)
	require.Equal(t, []uint16{230, 231, 232}, tirePressure.Pressures[:3])
	require.Equal(t, uint16(0xFFFF), tirePressure.Pressures[14])

	carTemperature, ok := parsed[5].(ExtCarTemperature)
	require.True(t, ok)
	require.Equal(t, int16(-100), carTemperature.Temperature)

	overSpeed, ok := parsed[6].(ExtOverSpeedAlarm)
	require.True(t, ok)
	require.Equal(t, uint8(2), overSpeed.LocationType)
	require.Equal(t, uint32(0x01020304), overSpeed.AreaID)
	require.True(t, overSpeed.HasAreaID)

	overSpeedNoArea, ok := parsed[7].(ExtOverSpeedAlarm)
	require.True(t, ok)
	require.Equal(t, uint8(0), overSpeedNoArea.LocationType)
	require.False(t, overSpeedNoArea.HasAreaID)
	require.Zero(t, overSpeedNoArea.AreaID)

	areaAlarm, ok := parsed[8].(ExtAreaAlarm)
	require.True(t, ok)
	require.Equal(t, uint8(3), areaAlarm.LocationType)
	require.Equal(t, uint32(0x05060708), areaAlarm.AreaID)
	require.Equal(t, uint8(1), areaAlarm.Direction)

	routeTime, ok := parsed[9].(ExtRouteTimeAlarm)
	require.True(t, ok)
	require.Equal(t, uint32(0x0A0B0C0D), routeTime.RouteID)
	require.Equal(t, uint16(600), routeTime.DrivingTime)
	require.Equal(t, uint8(1), routeTime.Result)

	vehicleSignal, ok := parsed[10].(ExtVehicleSignal)
	require.True(t, ok)
	require.Equal(t, uint32(0x00007FFF), vehicleSignal.Value)
	require.True(t, vehicleSignal.LowBeam)
	require.True(t, vehicleSignal.Clutch)
	require.False(t, vehicleSignal.Value&(1<<15) != 0)

	ioStatus, ok := parsed[11].(ExtIOStatus)
	require.True(t, ok)
	require.Equal(t, uint16(3), ioStatus.Value)
	require.True(t, ioStatus.DeepSleep)
	require.True(t, ioStatus.Sleep)

	analog, ok := parsed[12].(ExtAnalog)
	require.True(t, ok)
	require.Equal(t, uint16(0x0006), analog.AD0)
	require.Equal(t, uint16(0x0005), analog.AD1)

	wifi, ok := parsed[13].(ExtWIFISignal)
	require.True(t, ok)
	require.Equal(t, uint8(23), wifi.Strength)

	gnss, ok := parsed[14].(ExtGNSSSatellites)
	require.True(t, ok)
	require.Equal(t, uint8(12), gnss.Count)
}

// 0x05/0x06 仅 2019 定义，旧版本域收到同 ID 必须保留原始字节而不是猜测解析。
func TestParseExtItemsLegacyDomainKeeps2019OnlyItemsRaw(t *testing.T) {
	items := []LocationExtItem{
		buildBaseExtItem(0x01, hex.WriteDoubleWord(nil, 1)),
		buildBaseExtItem(0x05, buildTirePressureData()),
		buildBaseExtItem(0x06, hex.WriteWord(nil, 25)),
	}
	parsed := msg0200WithDomain(ExtDomainSubiao, items).ParseExtItems()
	require.Len(t, parsed, 3)
	require.IsType(t, ExtMileage{}, parsed[0])

	tireRaw, ok := parsed[1].(ExtRawItem)
	require.True(t, ok)
	require.Equal(t, uint8(0x05), tireRaw.ExtID)
	require.Equal(t, ExtDomainSubiao, tireRaw.ExtDomain)
	require.Equal(t, items[1].Data, tireRaw.Raw)

	tempRaw, ok := parsed[2].(ExtRawItem)
	require.True(t, ok)
	require.Equal(t, uint8(0x06), tempRaw.ExtID)
	require.Equal(t, items[2].Data, tempRaw.Raw)

	// 2019 域下相同字节必须解析为结构化项。
	baseParsed := msg0200WithDomain(ExtDomainBase2019, items).ParseExtItems()
	require.IsType(t, ExtTirePressure{}, baseParsed[1])
	require.IsType(t, ExtCarTemperature{}, baseParsed[2])
}

func TestParseExtItemsBaseLengthMismatchFallsBackToRaw(t *testing.T) {
	cases := []struct {
		id     uint8
		domain ExtDomain
		data   []byte
	}{
		{id: 0x01, domain: ExtDomainSubiao, data: []byte{0x01, 0x02, 0x03}},
		{id: 0x02, domain: ExtDomainSubiao, data: []byte{0x01}},
		{id: 0x03, domain: ExtDomainSubiao, data: []byte{0x01}},
		{id: 0x04, domain: ExtDomainSubiao, data: []byte{0x01}},
		{id: 0x05, domain: ExtDomainBase2019, data: buildTirePressureData()[:29]},
		{id: 0x06, domain: ExtDomainBase2019, data: []byte{0x01, 0x02, 0x03}},
		{id: 0x11, domain: ExtDomainSubiao, data: []byte{0x01, 0x02}},
		{id: 0x12, domain: ExtDomainSubiao, data: []byte{0x01, 0x02, 0x03, 0x04, 0x05}},
		{id: 0x13, domain: ExtDomainSubiao, data: []byte{0x01, 0x02, 0x03, 0x04, 0x05, 0x06}},
		{id: 0x25, domain: ExtDomainSubiao, data: []byte{0x01, 0x02, 0x03}},
		{id: 0x2A, domain: ExtDomainSubiao, data: []byte{0x01}},
		{id: 0x2B, domain: ExtDomainSubiao, data: []byte{0x01, 0x02, 0x03}},
		{id: 0x30, domain: ExtDomainSubiao, data: []byte{0x01, 0x02}},
		{id: 0x31, domain: ExtDomainSubiao, data: nil},
	}
	for _, tc := range cases {
		item := buildBaseExtItem(tc.id, tc.data)
		parsed := ParseExtItems([]LocationExtItem{item}, tc.domain)
		require.Len(t, parsed, 1, "ext id 0x%02X", tc.id)
		raw, ok := parsed[0].(ExtRawItem)
		require.True(t, ok, "ext id 0x%02X should fall back to raw", tc.id)
		require.Equal(t, tc.id, raw.ExtID)
		require.Equal(t, tc.domain, raw.ExtDomain)
		require.Equal(t, tc.data, raw.Raw)
	}
}

func TestParseExtItemsDuplicateIDsPreserved(t *testing.T) {
	items := []LocationExtItem{
		buildBaseExtItem(0x01, hex.WriteDoubleWord(nil, 100)),
		buildBaseExtItem(0x01, hex.WriteDoubleWord(nil, 200)),
		buildBaseExtItem(0x31, []byte{8}),
		buildBaseExtItem(0x31, []byte{9}),
	}
	parsed := ParseExtItems(items, ExtDomainSubiao)
	require.Len(t, parsed, 4)
	require.Equal(t, uint32(100), parsed[0].(ExtMileage).Mileage)
	require.Equal(t, uint32(200), parsed[1].(ExtMileage).Mileage)
	require.Equal(t, uint8(8), parsed[2].(ExtGNSSSatellites).Count)
	require.Equal(t, uint8(9), parsed[3].(ExtGNSSSatellites).Count)
}

// 结构化解析必须是无损派生视图：Decode 后再 Encode 与原报文逐字节一致，
// 且 ParseExtItems 不得改写原始 TLV 列表。
func TestMsg0200ExtRoundTripLossless(t *testing.T) {
	items := buildBaseExtItems(ExtDomainBase2019)
	items = append(items,
		buildBaseExtItem(0x31, []byte{6}),          // 重复 ID
		buildBaseExtItem(0x07, []byte{0, 0, 0, 0}), // 本轮不做结构化，必须保留
		buildBaseExtItem(0xE0, []byte{0xDE, 0xAD, 0xBE}),
	)
	body := buildRealistic0200Body(0, items)
	header := &MsgHeader{
		MsgID:        MsgID0200,
		PhoneNumber:  "188765432120",
		SerialNumber: 8,
		Attr:         &MsgBodyAttr{VersionSign: 0, VersionDesc: Version2019},
	}

	msg := &Msg0200{}
	require.NoError(t, msg.Decode(&PacketData{Header: header, Body: body}))
	before := append([]LocationExtItem(nil), msg.ExtItems...)
	parsed := msg.ParseExtItems()
	require.Len(t, parsed, len(items))
	require.Equal(t, before, msg.ExtItems)

	encoded, err := msg.Encode()
	require.NoError(t, err)
	encodedBody := encoded[len(encoded)-int(msg.Header.Attr.BodyLength):]
	require.Equal(t, body, encodedBody)

	require.IsType(t, ExtRawItem{}, parsed[len(items)-2])
	require.IsType(t, ExtRawItem{}, parsed[len(items)-1])
}
