package model

import (
	"bytes"
	"testing"

	"github.com/stretchr/testify/require"
)

func encodedBody(t *testing.T, msg interface {
	Encode() ([]byte, error)
	GetHeader() *MsgHeader
}) []byte {
	t.Helper()
	pkt, err := msg.Encode()
	require.NoError(t, err)
	return pkt[len(pkt)-int(msg.GetHeader().Attr.BodyLength):]
}

func TestMsg8900RoundTripWithoutBody(t *testing.T) {
	msg := &Msg8900{
		Header: genMsgHeader(0x8900),
		Type:   TransparentTypePeripheralInfo,
		IDs:    []uint8{PeripheralIDADAS, PeripheralIDDSM, 0xFF},
	}
	body := encodedBody(t, msg)
	// 类型 + 总数 + ID 列表，不写任何消息体。
	require.Equal(t, []byte{0xF8, 0x03, 0x64, 0x65, 0xFF}, body)

	decoded := &Msg8900{}
	require.NoError(t, decoded.Decode(&PacketData{Header: genMsgHeader(0x8900), Body: body}))
	require.Equal(t, msg.Type, decoded.Type)
	require.Equal(t, msg.IDs, decoded.IDs)
}

func TestMsg8900RejectsCountMismatch(t *testing.T) {
	body := []byte{0xF7, 0x02, 0x64}
	require.ErrorIs(t, (&Msg8900{}).Decode(&PacketData{Header: genMsgHeader(0x8900), Body: body}), ErrDecodeMsg)
}

func TestMsg8900RejectsTooManyIDs(t *testing.T) {
	msg := &Msg8900{Header: genMsgHeader(0x8900), Type: TransparentTypePeripheralStatus, IDs: make([]uint8, 256)}
	_, err := msg.Encode()
	require.ErrorIs(t, err, ErrEncodeMsg)
}

func TestMsg0900StatusRoundTrip(t *testing.T) {
	msg := &Msg0900{
		Header: genMsgHeader(0x0900),
		Type:   TransparentTypePeripheralStatus,
		Items: []PeripheralItem{
			{ID: PeripheralIDADAS, Raw: []byte{0x01, 0x00, 0x00, 0x00, 0x02}},
			// 长度不是 5：保留 Raw，不解析 Status。
			{ID: PeripheralIDDSM, Raw: []byte{0xAA, 0xBB, 0xCC}},
			// 未知外设 ID 仍按 Type 解析内容结构。
			{ID: 0x99, Raw: []byte{0x00, 0xFF, 0xFF, 0xFF, 0xFF}},
		},
	}
	body := encodedBody(t, msg)
	require.Equal(t, byte(0xF7), body[0])
	require.Equal(t, byte(3), body[1])

	decoded := &Msg0900{}
	require.NoError(t, decoded.Decode(&PacketData{Header: genMsgHeader(0x0900), Body: body}))
	require.Len(t, decoded.Items, 3)
	require.Equal(t, msg.Items[0].Raw, decoded.Items[0].Raw)
	require.NotNil(t, decoded.Items[0].Status)
	require.Equal(t, uint8(1), decoded.Items[0].Status.WorkStatus)
	require.Equal(t, uint32(2), decoded.Items[0].Status.AlarmStatus)
	require.Nil(t, decoded.Items[1].Status)
	require.Equal(t, msg.Items[1].Raw, decoded.Items[1].Raw)
	require.NotNil(t, decoded.Items[2].Status)
	require.Equal(t, uint32(0xFFFFFFFF), decoded.Items[2].Status.AlarmStatus)
}

func TestMsg0900StatusEncodeUsesStructuredContent(t *testing.T) {
	item := PeripheralItem{ID: PeripheralIDADAS, Status: &PeripheralStatus{WorkStatus: 2, AlarmStatus: 0x01020304}}
	content, err := item.encodeContent(TransparentTypePeripheralStatus)
	require.NoError(t, err)
	require.Equal(t, []byte{0x02, 0x01, 0x02, 0x03, 0x04}, content)
}

func TestMsg0900SystemRoundTrip(t *testing.T) {
	// 软件版本用非 UTF-8 字节，验证保留原始内容而不强制转 string。
	system := &PeripheralSystem{
		CompanyName:     []byte("Acme"),
		ProductModel:    []byte("ADAS-1"),
		HardwareVersion: []byte("HW1"),
		SoftwareVersion: []byte{0x81, 0x40, 0x31},
		DeviceID:        []byte("DEV0001"),
		CustomerCode:    []byte("C001"),
	}
	msg := &Msg0900{
		Header: genMsgHeader(0x0900),
		Type:   TransparentTypePeripheralInfo,
		Items:  []PeripheralItem{{ID: PeripheralIDTPMS, System: system}},
	}
	body := encodedBody(t, msg)

	decoded := &Msg0900{}
	require.NoError(t, decoded.Decode(&PacketData{Header: genMsgHeader(0x0900), Body: body}))
	require.Len(t, decoded.Items, 1)
	require.Equal(t, system, decoded.Items[0].System)

	// 六段长度字段回填正确：每段 1 字节长度 + 内容，最后一段之后不再有字节。
	raw := decoded.Items[0].Raw
	idx := 0
	for _, want := range [][]byte{system.CompanyName, system.ProductModel, system.HardwareVersion, system.SoftwareVersion, system.DeviceID, system.CustomerCode} {
		require.Equal(t, byte(len(want)), raw[idx])
		idx++
		require.Equal(t, want, raw[idx:idx+len(want)])
		idx += len(want)
	}
	require.Equal(t, len(raw), idx)
}

func TestMsg0900MalformedSystemDegradesToRaw(t *testing.T) {
	// 第二条的 F8 内容长度越界，只降级该条目，其他条目继续解析。
	good := []byte{
		0x04, 'A', 'c', 'm', 'e',
		0x01, 'P',
		0x01, 'H',
		0x01, 'S',
		0x01, 'D',
		0x01, 'C',
	}
	bad := []byte{0x10, 'x'} // 声明 16 字节但只有 1 字节
	body := []byte{0xF8, 0x02}
	body = append(body, PeripheralIDADAS, byte(len(good)))
	body = append(body, good...)
	body = append(body, PeripheralIDBSD, byte(len(bad)))
	body = append(body, bad...)

	decoded := &Msg0900{}
	require.NoError(t, decoded.Decode(&PacketData{Header: genMsgHeader(0x0900), Body: body}))
	require.Len(t, decoded.Items, 2)
	require.NotNil(t, decoded.Items[0].System)
	require.Equal(t, []byte("Acme"), decoded.Items[0].System.CompanyName)
	require.Nil(t, decoded.Items[1].System)
	require.Equal(t, bad, decoded.Items[1].Raw)
}

func TestMsg0900UnknownTypeKeepsRaw(t *testing.T) {
	raw := []byte{0x01, 0x64, 0x02, 0x00, 0x05}
	msg := &Msg0900{
		Header: genMsgHeader(0x0900),
		Type:   0x0B,
		Items:  []PeripheralItem{{ID: 0x64, Raw: raw}},
	}
	body := encodedBody(t, msg)

	decoded := &Msg0900{}
	require.NoError(t, decoded.Decode(&PacketData{Header: genMsgHeader(0x0900), Body: body}))
	require.Equal(t, uint8(0x0B), decoded.Type)
	require.Equal(t, raw, decoded.Items[0].Raw)
	require.Nil(t, decoded.Items[0].Status)
	require.Nil(t, decoded.Items[0].System)
}

func TestMsg0900RejectsTruncatedFrame(t *testing.T) {
	body := []byte{0xF7, 0x02, 0x64, 0x05, 0x01}
	require.ErrorIs(t, (&Msg0900{}).Decode(&PacketData{Header: genMsgHeader(0x0900), Body: body}), ErrDecodeMsg)

	body = []byte{0xF7, 0x01, 0x64, 0x01, 0x01, 0x99}
	require.ErrorIs(t, (&Msg0900{}).Decode(&PacketData{Header: genMsgHeader(0x0900), Body: body}), ErrDecodeMsg)
}

func TestMsg0900RejectsTooManyItems(t *testing.T) {
	msg := &Msg0900{Header: genMsgHeader(0x0900), Items: make([]PeripheralItem, 256)}
	_, err := msg.Encode()
	require.ErrorIs(t, err, ErrEncodeMsg)
}

func TestMsg0900RejectsOversizedSystem(t *testing.T) {
	item := PeripheralItem{ID: PeripheralIDADAS, System: &PeripheralSystem{CompanyName: bytes.Repeat([]byte{'A'}, 256)}}
	_, err := item.encodeContent(TransparentTypePeripheralInfo)
	require.Error(t, err)

	// 各段合法但总长超过 255 时也必须报错，而不是截断。
	item = PeripheralItem{ID: PeripheralIDADAS, System: &PeripheralSystem{
		CompanyName:     bytes.Repeat([]byte{'A'}, 100),
		ProductModel:    bytes.Repeat([]byte{'B'}, 100),
		HardwareVersion: bytes.Repeat([]byte{'C'}, 60),
	}}
	_, err = item.encodeContent(TransparentTypePeripheralInfo)
	require.Error(t, err)
}
