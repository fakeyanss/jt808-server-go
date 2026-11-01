package protocol

import (
	"context"
	"encoding/binary"
	"fmt"
	"testing"
	"time"

	actionpkg "github.com/fakeyanss/jt808-server-go/internal/action"
	"github.com/fakeyanss/jt808-server-go/internal/config"
	"github.com/fakeyanss/jt808-server-go/internal/protocol/model"
	"github.com/fakeyanss/jt808-server-go/internal/storage"
	"github.com/stretchr/testify/require"
)

func TestTerminalControlProducesGenericAcknowledgement(t *testing.T) {
	header := &model.MsgHeader{MsgID: 0x8105, SerialNumber: 17, PhoneNumber: "188765432109", Attr: &model.MsgBodyAttr{}}
	result, err := NewJT808MsgProcessor(config.ServerModeStandalone, nil).Process(context.Background(), &model.PacketData{Header: header, Body: []byte{2, 'h', 'o', 's', 't'}})
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
			result, err := NewJT808MsgProcessor(config.ServerModeStandalone, nil).Process(context.Background(), &model.PacketData{Header: header, Body: []byte{0}})
			require.NoError(t, err)
			require.IsType(t, &model.Msg0001{}, result.Outgoing)
			require.Equal(t, messageID, result.Outgoing.(*model.Msg0001).AnswerMessageID)
		})
	}
}

func TestSimulatorParameterTemplateDecodes(t *testing.T) {
	header := &model.MsgHeader{MsgID: 0x8104, SerialNumber: 20, PhoneNumber: "188765432100", Attr: &model.MsgBodyAttr{}}
	result, err := NewJT808MsgProcessor(config.ServerModeStandalone, nil).Process(context.Background(), &model.PacketData{Header: header})
	require.NoError(t, err)
	reply, ok := result.Outgoing.(*model.Msg0104)
	require.True(t, ok)
	require.Equal(t, uint8(39), reply.Parameters.ParamCnt)
	require.Len(t, reply.Parameters.Params, 39)
	last := reply.Parameters.Params[38]
	require.Equal(t, uint32(0x0076), last.ParamID)
	require.Equal(t, uint8(15), last.ParamLen)
}

type recordingHooks struct {
	actionpkg.Noop
	locations []actionpkg.LocationEvent
	alarms    []actionpkg.AlarmEvent
}

func (h *recordingHooks) OnLocation(_ context.Context, event actionpkg.LocationEvent) {
	h.locations = append(h.locations, event)
}

func (h *recordingHooks) OnAlarm(_ context.Context, event actionpkg.AlarmEvent) {
	h.alarms = append(h.alarms, event)
}

func locationReportBody(alarmSign uint32) []byte {
	body := make([]byte, 28)
	binary.BigEndian.PutUint32(body[0:4], alarmSign)
	binary.BigEndian.PutUint32(body[8:12], 39543265)   // 纬度
	binary.BigEndian.PutUint32(body[12:16], 116235432) // 经度
	copy(body[22:28], []byte{0x26, 0x11, 0x01, 0x12, 0x00, 0x00})
	return body
}

// 负向测试：gateway 模式下位置上报只触发 Action 回调，不写设备、定位、报警缓存。
func TestGatewayModeLocationDoesNotWriteStandaloneCaches(t *testing.T) {
	const phone = "188765499001"
	hooks := &recordingHooks{}
	processor := NewJT808MsgProcessor(config.ServerModeGateway, hooks)

	header := &model.MsgHeader{MsgID: 0x0200, SerialNumber: 31, PhoneNumber: phone, Attr: &model.MsgBodyAttr{}}
	result, err := processor.Process(context.Background(), &model.PacketData{Header: header, Body: locationReportBody(1)})
	require.NoError(t, err)
	require.IsType(t, &model.Msg8001{}, result.Outgoing)

	require.Len(t, hooks.locations, 1)
	require.Equal(t, phone, hooks.locations[0].Phone)
	require.Len(t, hooks.alarms, 1)

	_, err = storage.GetDeviceCache().GetDeviceByPhone(phone)
	require.ErrorIs(t, err, storage.ErrDeviceNotFound)

	_, err = storage.GetGeoCache().GetGeoLatestByPhone(phone)
	require.ErrorIs(t, err, storage.ErrGisNotFound)
	require.Empty(t, storage.GetAlarmCache().List(phone))
	storage.GetGeoCache().DelGeoByPhone(phone)
}

// 对照测试：standalone 模式保持原有缓存行为。
func TestStandaloneModeLocationWritesGeoCache(t *testing.T) {
	const phone = "188765499002"
	storage.GetDeviceCache().CacheDevice(&model.Device{Phone: phone, Keepalive: time.Minute})
	t.Cleanup(func() {
		storage.GetDeviceCache().DelDeviceByPhone(phone)
		storage.GetGeoCache().DelGeoByPhone(phone)
	})

	processor := NewJT808MsgProcessor(config.ServerModeStandalone, nil)
	header := &model.MsgHeader{MsgID: 0x0200, SerialNumber: 32, PhoneNumber: phone, Attr: &model.MsgBodyAttr{}}
	result, err := processor.Process(context.Background(), &model.PacketData{Header: header, Body: locationReportBody(0)})
	require.NoError(t, err)
	require.IsType(t, &model.Msg8001{}, result.Outgoing)

	geo, err := storage.GetGeoCache().GetGeoLatestByPhone(phone)
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
			result, err := NewJT808MsgProcessor(config.ServerModeStandalone, nil).Process(context.Background(), &model.PacketData{Header: header, Body: tt.body})
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
