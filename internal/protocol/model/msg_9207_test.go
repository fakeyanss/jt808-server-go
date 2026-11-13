package model

import (
	"testing"

	"github.com/stretchr/testify/require"
)

// 0x9207 报文体固定 3 字节：应答流水号 + 控制类型。
func TestMsg9207EncodeDecode(t *testing.T) {
	header := &MsgHeader{MsgID: 0x9207, SerialNumber: 11, PhoneNumber: "13800138000", Attr: &MsgBodyAttr{}}
	msg := &Msg9207{Header: header, AnswerSerialNumber: 7, Control: 2}
	body, err := msg.Encode()
	require.NoError(t, err)
	// Encode 返回的是完整帧体（含报文头），最后 3 字节是业务体。
	require.Equal(t, []byte{0x00, 0x07, 0x02}, body[len(body)-3:])

	decoded := &Msg9207{}
	err = decoded.Decode(&PacketData{Header: header, Body: body[len(body)-3:]})
	require.NoError(t, err)
	require.EqualValues(t, 7, decoded.AnswerSerialNumber)
	require.EqualValues(t, 2, decoded.Control)
}

func TestMsg9207RejectsBrokenBody(t *testing.T) {
	err := (&Msg9207{}).Decode(&PacketData{Header: &MsgHeader{}, Body: []byte{0x00, 0x07}})
	require.Error(t, err)
}
