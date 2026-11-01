package protocol

import (
	"context"
	"encoding/binary"
	"fmt"
	"testing"
	"time"

	"github.com/fakeyanss/jt808-server-go/internal/protocol/model"
	"github.com/fakeyanss/jt808-server-go/internal/storage"
	"github.com/stretchr/testify/require"
)

func TestTerminalControlProducesGenericAcknowledgement(t *testing.T) {
	header := &model.MsgHeader{MsgID: 0x8105, SerialNumber: 17, PhoneNumber: "188765432109", Attr: &model.MsgBodyAttr{}}
	result, err := NewJT808MsgProcessor(nil).Process(context.Background(), &model.PacketData{Header: header, Body: []byte{2, 'h', 'o', 's', 't'}})
	require.NoError(t, err)
	require.IsType(t, &model.Msg0001{}, result.Outgoing)
	ack := result.Outgoing.(*model.Msg0001)
	require.Equal(t, uint16(17), ack.AnswerSerialNumber)
	require.Equal(t, uint16(0x8105), ack.AnswerMessageID)
	require.Equal(t, uint8(0), ack.Result)
}

func TestRegionRouteDeletionProducesGenericAcknowledgement(t *testing.T) {
	for _, messageID := range []uint16{0x8601, 0x8603, 0x8605, 0x8607} {
		t.Run(fmt.Sprintf("%04x", messageID), func(t *testing.T) {
			header := &model.MsgHeader{MsgID: messageID, SerialNumber: 18, PhoneNumber: "188765432109", Attr: &model.MsgBodyAttr{}}
			result, err := NewJT808MsgProcessor(nil).Process(context.Background(), &model.PacketData{Header: header, Body: []byte{0}})
			require.NoError(t, err)
			require.IsType(t, &model.Msg0001{}, result.Outgoing)
			require.Equal(t, messageID, result.Outgoing.(*model.Msg0001).AnswerMessageID)
		})
	}
}

func TestSimulatorParameterTemplateDecodes(t *testing.T) {
	header := &model.MsgHeader{MsgID: 0x8104, SerialNumber: 20, PhoneNumber: "188765432100", Attr: &model.MsgBodyAttr{}}
	result, err := NewJT808MsgProcessor(nil).Process(context.Background(), &model.PacketData{Header: header})
	require.NoError(t, err)
	reply, ok := result.Outgoing.(*model.Msg0104)
	require.True(t, ok)
	require.Equal(t, uint8(39), reply.Parameters.ParamCnt)
	require.Len(t, reply.Parameters.Params, 39)
	last := reply.Parameters.Params[38]
	require.Equal(t, uint32(0x0076), last.ParamID)
	require.Equal(t, uint8(15), last.ParamLen)
}

func locationReportBody(alarmSign uint32) []byte {
	body := make([]byte, 28)
	binary.BigEndian.PutUint32(body[0:4], alarmSign)
	binary.BigEndian.PutUint32(body[8:12], 39543265)   // 纬度
	binary.BigEndian.PutUint32(body[12:16], 116235432) // 经度
	copy(body[22:28], []byte{0x26, 0x11, 0x01, 0x12, 0x00, 0x00})
	return body
}

// 位置上报写入定位缓存。
func TestLocationReportWritesGeoCache(t *testing.T) {
	const phone = "188765499002"
	storage.Default().Devices.Upsert(context.Background(), &model.Device{Phone: phone, Keepalive: time.Minute})
	t.Cleanup(func() {
		storage.Default().Devices.DeleteByPhone(context.Background(), phone)
		storage.Default().Geo.Delete(context.Background(), phone)
	})

	processor := NewJT808MsgProcessor(nil)
	header := &model.MsgHeader{MsgID: 0x0200, SerialNumber: 32, PhoneNumber: phone, Attr: &model.MsgBodyAttr{}}
	result, err := processor.Process(context.Background(), &model.PacketData{Header: header, Body: locationReportBody(0)})
	require.NoError(t, err)
	require.IsType(t, &model.Msg8001{}, result.Outgoing)

	geo, err := storage.Default().Geo.Latest(context.Background(), phone)
	require.NoError(t, err)
	require.Equal(t, phone, geo.Phone)
}

func TestSimulatorRepliesToRegionDriverRecorderAndMultimediaCommands(t *testing.T) {
	tests := []struct {
		id       uint16
		body     []byte
		response any
	}{
		{0x8608, []byte{1, 0, 0, 0, 0}, &model.Msg0608{}},
		{0x8702, nil, &model.Msg0702{}},
		{0x8700, []byte{3, 0xaa}, &model.Msg0700{}},
		{0x8701, []byte{3, 0xaa}, &model.Msg0001{}},
		{0x8801, []byte{1, 0, 1, 0, 0, 0, 1, 1, 0, 0, 0, 0}, &model.Msg0805{}},
		{0x8802, make([]byte, 15), &model.Msg0802{}},
		{0x8803, []byte{1, 1, 1, 0x26, 0x10, 0x28, 0, 0, 0, 0x26, 0x10, 0x28, 0x23, 0x59, 0x59, 1}, &model.Msg0001{}},
		{0x8804, []byte{1, 0, 30, 1, 2}, &model.Msg0001{}},
		{0x8805, []byte{0, 0, 0, 1, 1}, &model.Msg0001{}},
	}
	for _, tt := range tests {
		t.Run(fmt.Sprintf("%04x", tt.id), func(t *testing.T) {
			header := &model.MsgHeader{MsgID: tt.id, SerialNumber: 19, PhoneNumber: "188765432109", Attr: &model.MsgBodyAttr{}}
			result, err := NewJT808MsgProcessor(nil).Process(context.Background(), &model.PacketData{Header: header, Body: tt.body})
			require.NoError(t, err)
			require.IsType(t, tt.response, result.Outgoing)
			if ack, ok := result.Outgoing.(*model.Msg0001); ok {
				require.Equal(t, tt.id, ack.AnswerMessageID)
			}
			if capture, ok := result.Outgoing.(*model.Msg0805); ok {
				require.Equal(t, uint16(19), capture.AnswerSerial)
				require.Equal(t, uint8(0), capture.Result)
			}
		})
	}
}
