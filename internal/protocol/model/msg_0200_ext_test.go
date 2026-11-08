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
