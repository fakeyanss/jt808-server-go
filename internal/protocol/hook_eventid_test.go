package protocol

import (
	"context"
	"testing"

	"github.com/stretchr/testify/require"

	actionpkg "github.com/fakeyanss/jt808-server-go/internal/action"
	"github.com/fakeyanss/jt808-server-go/internal/contracts"
	"github.com/fakeyanss/jt808-server-go/internal/protocol/model"
)

type hookRecorder struct {
	actionpkg.Noop
	registers   []actionpkg.RegisterEvent
	auths       []actionpkg.AuthEvent
	locations   []actionpkg.LocationEvent
	alarms      []actionpkg.AlarmEvent
	disconnects []actionpkg.DisconnectEvent
	waybills    []actionpkg.WaybillEvent
	drivers     []actionpkg.DriverIdentityEvent
	canBus      []actionpkg.CANBusEvent
	tachographs []actionpkg.TachographEvent
	reports     []actionpkg.EventReportEvent
	controls    []actionpkg.VehicleControlAckEvent
	upgrades    []actionpkg.TerminalUpgradeResultEvent
	mediaEvents []actionpkg.MediaEvent
	mediaUpload []actionpkg.MediaUploadEvent
	fileUploads []actionpkg.FileUploadCompleteEvent
	attachments []actionpkg.AttachmentInfoEvent
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

func (h *hookRecorder) OnWaybill(_ context.Context, event actionpkg.WaybillEvent) {
	h.waybills = append(h.waybills, event)
}

func (h *hookRecorder) OnDriverIdentity(_ context.Context, event actionpkg.DriverIdentityEvent) {
	h.drivers = append(h.drivers, event)
}

func (h *hookRecorder) OnCANBus(_ context.Context, event actionpkg.CANBusEvent) {
	h.canBus = append(h.canBus, event)
}

func (h *hookRecorder) OnTachograph(_ context.Context, event actionpkg.TachographEvent) {
	h.tachographs = append(h.tachographs, event)
}

func (h *hookRecorder) OnEventReport(_ context.Context, event actionpkg.EventReportEvent) {
	h.reports = append(h.reports, event)
}

func (h *hookRecorder) OnVehicleControlAck(_ context.Context, event actionpkg.VehicleControlAckEvent) {
	h.controls = append(h.controls, event)
}

func (h *hookRecorder) OnTerminalUpgradeResult(_ context.Context, event actionpkg.TerminalUpgradeResultEvent) {
	h.upgrades = append(h.upgrades, event)
}

func (h *hookRecorder) OnMediaEvent(_ context.Context, event actionpkg.MediaEvent) {
	h.mediaEvents = append(h.mediaEvents, event)
}

func (h *hookRecorder) OnMediaUpload(_ context.Context, event actionpkg.MediaUploadEvent) {
	h.mediaUpload = append(h.mediaUpload, event)
}

func (h *hookRecorder) OnFileUploadComplete(_ context.Context, event actionpkg.FileUploadCompleteEvent) {
	h.fileUploads = append(h.fileUploads, event)
}

func (h *hookRecorder) OnAttachmentInfo(_ context.Context, event actionpkg.AttachmentInfoEvent) {
	h.attachments = append(h.attachments, event)
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

// cluster 网关只投递上行事件，不在本进程派发 action 钩子；standalone 保持本地派发。
func TestGatewaySkipsActionHooksWhenUplinkSinkEnabled(t *testing.T) {
	header := &model.MsgHeader{
		MsgID:        0x0100,
		PhoneNumber:  "188765499025",
		SerialNumber: 13,
		Attr:         &model.MsgBodyAttr{VersionDesc: model.Version2019},
	}
	body := []byte{0x01, 0x02}
	ctx := context.WithValue(context.Background(), model.SessionCtxKey{}, &model.Session{ID: "sess-hook-test"})
	ctx = context.WithValue(ctx, model.PacketDecodeCtxKey{}, &model.PacketData{
		Header: header, Body: body, SegCompleted: true,
	})
	data := &model.ProcessData{
		Incoming: &model.Msg0100{Header: header, DeviceID: "device-2", PlateNumber: "京A12346"},
		Outgoing: &model.Msg8100{Result: model.ResultCodeType(model.ResultSuccess)},
	}

	clusterHooks := &hookRecorder{}
	clusterSink := &fakeUplinkSink{}
	cluster := NewJT808MsgProcessor(clusterHooks, WithProcessorDeps(ProcessorDeps{
		Repos:           newTestRepositories(),
		UplinkSink:      clusterSink,
		SkipActionHooks: true,
	}))
	require.NoError(t, cluster.ApplyBusiness(ctx, 0x0100, data))
	require.Equal(t, 1, clusterSink.count())
	require.Empty(t, clusterHooks.registers)

	standaloneHooks := &hookRecorder{}
	standalone := NewJT808MsgProcessor(standaloneHooks, WithProcessorDeps(ProcessorDeps{
		Repos: newTestRepositories(),
	}))
	require.NoError(t, standalone.ApplyBusiness(ctx, 0x0100,
		&model.ProcessData{
			Incoming: &model.Msg0100{Header: header, DeviceID: "device-3", PlateNumber: "京A12347"},
			Outgoing: &model.Msg8100{Result: model.ResultCodeType(model.ResultSuccess)},
		}))
	require.Len(t, standaloneHooks.registers, 1)
}

// control 侧注册/鉴权钩子复用网关 eventId 的用例已迁移到 internal/business。
