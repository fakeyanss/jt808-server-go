package model

import (
	"testing"

	"github.com/stretchr/testify/require"
)

// TestMsg8500VersionLayouts 校验 2019 控制类型列表与 2011/2013 单字节控制标志。
func TestMsg8500VersionLayouts(t *testing.T) {
	t.Run("2019", func(t *testing.T) {
		msg := &Msg8500{
			Header:   genMsgHeaderVersion(0x8500, Version2019),
			Controls: []VehicleControl{{Type: VehicleControlTypeDoor, Value: 0}},
		}
		decoded := &Msg8500{}
		pkt := roundTrip(t, msg, decoded)
		// 控制类型数量(2) + 类型ID(2) + 参数(1)
		require.Len(t, pkt.Body, 2+3)
		require.Equal(t, msg.Controls, decoded.Controls)
		// 2019 车门锁闭(0)对应 2013 控制标志 bit0=1
		require.Equal(t, uint16(1), decoded.EchoControlFlags())

		open := &Msg8500{
			Header:   genMsgHeaderVersion(0x8500, Version2019),
			Controls: []VehicleControl{{Type: VehicleControlTypeDoor, Value: 1}},
		}
		require.Equal(t, uint16(0), open.EchoControlFlags())
	})

	t.Run("2013", func(t *testing.T) {
		msg := &Msg8500{Header: genMsgHeaderVersion(0x8500, Version2013), ControlFlags: 1}
		decoded := &Msg8500{}
		pkt := roundTrip(t, msg, decoded)
		require.Len(t, pkt.Body, 1)
		require.Equal(t, uint16(1), decoded.ControlFlags)
		require.Equal(t, uint16(1), decoded.EchoControlFlags())
	})
}

// TestMsg8500Validation 校验最小长度、未知控制类型与 2013 标志位宽度。
func TestMsg8500Validation(t *testing.T) {
	header2019 := genMsgHeaderVersion(0x8500, Version2019)
	header2013 := genMsgHeaderVersion(0x8500, Version2013)

	require.Error(t, (&Msg8500{}).Decode(&PacketData{Header: header2019, Body: []byte{0}}))
	require.Error(t, (&Msg8500{}).Decode(&PacketData{Header: header2013, Body: nil}))
	require.NoError(t, (&Msg8500{}).Decode(&PacketData{Header: header2013, Body: []byte{1}}))

	// 控制类型数量声明 1 个，但缺少类型 ID 与参数
	require.Error(t, (&Msg8500{}).Decode(&PacketData{Header: header2019, Body: []byte{0, 1}}))
	// 未知控制类型
	require.Error(t, (&Msg8500{}).Decode(&PacketData{Header: header2019, Body: []byte{0, 1, 0, 2, 1}}))
	// 2013 控制标志超出单字节范围
	_, err := (&Msg8500{Header: header2013, ControlFlags: 0x0100}).Encode()
	require.Error(t, err)
	// 2019 未知控制类型禁止编码
	_, err = (&Msg8500{Header: header2019, Controls: []VehicleControl{{Type: 0x0002, Value: 1}}}).Encode()
	require.Error(t, err)
}
