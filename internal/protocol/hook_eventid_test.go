package protocol

import (
	"context"
	"testing"

	"github.com/stretchr/testify/require"

	actionpkg "github.com/fakeyanss/jt808-server-go/internal/action"
	"github.com/fakeyanss/jt808-server-go/internal/contracts"
	"github.com/fakeyanss/jt808-server-go/internal/protocol/model"
	"github.com/fakeyanss/jt808-server-go/internal/repository/database"
)

type hookRecorder struct {
	actionpkg.Noop
	registers   []actionpkg.RegisterEvent
	auths       []actionpkg.AuthEvent
	locations   []actionpkg.LocationEvent
	alarms      []actionpkg.AlarmEvent
	disconnects []actionpkg.DisconnectEvent
}

func (h *hookRecorder) OnRegister(_ context.Context, event actionpkg.RegisterEvent) {
	h.registers = append(h.registers, event)
}

func (h *hookRecorder) OnAuth(_ context.Context, event actionpkg.AuthEvent) {
	h.auths = append(h.auths, event)
}

func (h *hookRecorder) OnLocation(_ context.Context, event actionpkg.LocationEvent) {
	h.locations = append(h.locations, event)
}

func (h *hookRecorder) OnAlarm(_ context.Context, event actionpkg.AlarmEvent) {
	h.alarms = append(h.alarms, event)
}

func (h *hookRecorder) OnDisconnect(_ context.Context, event actionpkg.DisconnectEvent) {
	h.disconnects = append(h.disconnects, event)
}

// 网关侧钩子的 eventId 必须与投递给管控的 eventId 完全一致，外部才能去重。
func TestGatewayHookEventIDMatchesUplinkEvent(t *testing.T) {
	header := &model.MsgHeader{
		MsgID:        0x0200,
		PhoneNumber:  "188765499022",
		SerialNumber: 9,
		Attr:         &model.MsgBodyAttr{VersionDesc: model.Version2019},
	}
	body := locationTestBody()
	ctx := context.WithValue(context.Background(), model.PacketDecodeCtxKey{}, &model.PacketData{
		Header:       header,
		Body:         body,
		SegCompleted: true,
	})
	data := &model.ProcessData{Incoming: &model.Msg0200{Header: header, Time: "261112103000"}}

	event := buildUplinkEvent(ctx, 0x0200, data, contracts.ReliabilityBestEffort, "gw-test", "")
	hooks := &hookRecorder{}
	NewJT808MsgProcessor(hooks).dispatchHooks(ctx, data)

	require.Len(t, hooks.locations, 1)
	require.Equal(t, event.EventID, hooks.locations[0].EventID)
}

// 0x0704 是批量报文，事件 ID 必须在消息 ID 后追加序号，避免同批不同位置被外部去重掉。
func TestGatewayBatchLocationHookEventIDsArePerItem(t *testing.T) {
	header := &model.MsgHeader{
		MsgID:        0x0704,
		PhoneNumber:  "188765499022",
		SerialNumber: 10,
		Attr:         &model.MsgBodyAttr{VersionDesc: model.Version2019},
	}
	ctx := context.WithValue(context.Background(), model.PacketDecodeCtxKey{}, &model.PacketData{
		Header: header, Body: []byte{0x01}, SegCompleted: true,
	})
	data := &model.ProcessData{Incoming: &model.Msg0704{
		Header: header,
		Items: []model.BatchLocationItem{
			{Loc: model.Msg0200{Header: header, Time: "261112103000"}},
			{Loc: model.Msg0200{Header: header, Time: "261112103010"}},
		},
	}}

	hooks := &hookRecorder{}
	NewJT808MsgProcessor(hooks).dispatchHooks(ctx, data)

	require.Len(t, hooks.locations, 2)
	base := hookEventID(ctx, 0x0704, data.Incoming)
	require.Equal(t, base+"#0", hooks.locations[0].EventID)
	require.Equal(t, base+"#1", hooks.locations[1].EventID)
}

// control 侧注册钩子复用网关 eventId，并用网关随事件带来的 0x8100 结果填充载荷。
func TestControlRegisterHookUsesEventIDAndGatewayReply(t *testing.T) {
	stores := database.NewMemory()
	header := &model.MsgHeader{
		MsgID:        0x0100,
		PhoneNumber:  "188765499023",
		SerialNumber: 11,
		Attr:         &model.MsgBodyAttr{VersionDesc: model.Version2019},
	}
	data := &model.ProcessData{Incoming: &model.Msg0100{
		Header: header, DeviceID: "device-1", PlateNumber: "京A12345",
	}}
	ctx := context.WithValue(context.Background(), UplinkEventCtxKey{}, UplinkEventContext{
		EventID: "evt-register", ReplyMsgID: 0x8100, ReplyResult: 1,
	})

	hooks := &hookRecorder{}
	mp := NewJT808MsgProcessor(hooks, WithProcessorDeps(ProcessorDeps{Repos: stores}))
	require.NoError(t, mp.ApplyControlBusiness(ctx, 0x0100, data))

	require.Len(t, hooks.registers, 1)
	require.Equal(t, "evt-register", hooks.registers[0].EventID)
	require.Equal(t, uint8(1), hooks.registers[0].Result)
}

// control 侧鉴权钩子同样复用网关 eventId，并反映网关 0x8001 的实际结果。
func TestControlAuthHookUsesEventIDAndGatewayReply(t *testing.T) {
	ctx := context.Background()
	stores := database.NewMemory()
	header := &model.MsgHeader{
		MsgID:        0x0102,
		PhoneNumber:  "188765499024",
		SerialNumber: 12,
		Attr:         &model.MsgBodyAttr{VersionDesc: model.Version2019},
	}
	require.NoError(t, stores.Devices.SaveRegistration(ctx, &model.Device{
		Phone: header.PhoneNumber, AuthCode: "auth-code",
	}))
	data := &model.ProcessData{Incoming: &model.Msg0102{
		Header: header, AuthCode: "auth-code",
	}}
	eventCtx := context.WithValue(ctx, UplinkEventCtxKey{}, UplinkEventContext{
		EventID: "evt-auth", ReplyMsgID: 0x8001, ReplyResult: 0,
	})

	hooks := &hookRecorder{}
	mp := NewJT808MsgProcessor(hooks, WithProcessorDeps(ProcessorDeps{Repos: stores}))
	require.NoError(t, mp.ApplyControlBusiness(eventCtx, 0x0102, data))

	require.Len(t, hooks.auths, 1)
	require.Equal(t, "evt-auth", hooks.auths[0].EventID)
	require.True(t, hooks.auths[0].Success)
}
