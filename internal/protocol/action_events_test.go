package protocol

import (
	"context"
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/fakeyanss/jt808-server-go/internal/protocol/model"
)

func actionHeader(msgID uint16, phone string, serial uint16) *model.MsgHeader {
	return &model.MsgHeader{
		MsgID:        msgID,
		PhoneNumber:  phone,
		SerialNumber: serial,
		Attr:         &model.MsgBodyAttr{VersionDesc: model.Version2019},
	}
}

func dispatchActionEvent(t *testing.T, in model.JT808Msg) *hookRecorder {
	t.Helper()
	header := in.GetHeader()
	ctx := context.WithValue(context.Background(), model.PacketDecodeCtxKey{}, &model.PacketData{
		Header: header, Body: []byte{0x01}, SegCompleted: true,
	})
	hooks := &hookRecorder{}
	NewJT808MsgProcessor(hooks).dispatchHooks(ctx, &model.ProcessData{Incoming: in})
	return hooks
}

func TestWaybillEvent(t *testing.T) {
	header := actionHeader(0x0701, "13800000001", 1)
	hooks := dispatchActionEvent(t, &model.Msg0701{Header: header, Content: "运单A"})

	require.Len(t, hooks.waybills, 1)
	event := hooks.waybills[0]
	require.NotEmpty(t, event.EventID)
	require.Equal(t, "13800000001", event.Phone)
	require.Equal(t, uint16(1), event.Serial)
	require.NotEmpty(t, event.Time)
	require.Equal(t, "运单A", event.Content)
}

func TestDriverIdentityEvent(t *testing.T) {
	header := actionHeader(0x0702, "13800000002", 2)
	hooks := dispatchActionEvent(t, &model.Msg0702{
		Header: header, State: 1, Time: "261112103000", ReadResult: 0,
		Name: "张三", QualificationCode: "Q-1", IssuingAuthority: "交通局",
		ValidUntil: "20300101", IdentityNumber: "ID-1",
	})

	require.Len(t, hooks.drivers, 1)
	event := hooks.drivers[0]
	require.NotEmpty(t, event.EventID)
	require.Equal(t, "13800000002", event.Phone)
	require.Equal(t, uint16(2), event.Serial)
	require.Equal(t, "261112103000", event.Time)
	require.Equal(t, uint8(1), event.State)
	require.Equal(t, "张三", event.Name)
	require.Equal(t, "Q-1", event.QualificationCode)
	require.Equal(t, "交通局", event.IssuingAuthority)
	require.Equal(t, "20300101", event.ValidUntil)
	require.Equal(t, "ID-1", event.IdentityNumber)
}

func TestCANBusEvent(t *testing.T) {
	header := actionHeader(0x0705, "13800000003", 3)
	var data [8]byte
	for i := range data {
		data[i] = byte(i + 1)
	}
	hooks := dispatchActionEvent(t, &model.Msg0705{
		Header: header, Count: 1, FirstTime: "261112103000",
		Items: []model.CANDataItem{{CANID: 0x18FEF100, Data: data}},
	})

	require.Len(t, hooks.canBus, 1)
	event := hooks.canBus[0]
	require.NotEmpty(t, event.EventID)
	require.Equal(t, uint16(3), event.Serial)
	require.Equal(t, "261112103000", event.Time)
	require.Equal(t, uint16(1), event.Count)
	require.Len(t, event.Items, 1)
	require.Equal(t, uint32(0x18FEF100), event.Items[0].CANID)
	require.Equal(t, data[:], event.Items[0].Data)
}

func TestTachographEvent(t *testing.T) {
	header := actionHeader(0x0700, "13800000004", 4)
	hooks := dispatchActionEvent(t, &model.Msg0700{
		Header: header, AnswerSerial: 9, Command: 2, Data: []byte{0xAA, 0xBB},
	})

	require.Len(t, hooks.tachographs, 1)
	event := hooks.tachographs[0]
	require.NotEmpty(t, event.EventID)
	require.Equal(t, uint16(9), event.AnswerSerial)
	require.Equal(t, uint8(2), event.Command)
	require.Equal(t, []byte{0xAA, 0xBB}, event.Data)
}

func TestEventReportEvent(t *testing.T) {
	header := actionHeader(0x0301, "13800000005", 5)
	hooks := dispatchActionEvent(t, &model.Msg0301{Header: header, EventID: 7})

	require.Len(t, hooks.reports, 1)
	event := hooks.reports[0]
	require.NotEmpty(t, event.EventID)
	require.Equal(t, uint8(7), event.ReportID)
	require.Equal(t, "13800000005", event.Phone)
}

func TestVehicleControlAckEvent(t *testing.T) {
	header := actionHeader(0x0500, "13800000006", 6)
	hooks := dispatchActionEvent(t, &model.Msg0500{Header: header, ControlFlags: 0x1234})

	require.Len(t, hooks.controls, 1)
	event := hooks.controls[0]
	require.NotEmpty(t, event.EventID)
	require.Equal(t, uint16(0x1234), event.ControlFlags)
	require.Equal(t, uint16(6), event.Serial)
}

func TestTerminalUpgradeResultEvent(t *testing.T) {
	header := actionHeader(0x0108, "13800000007", 7)
	hooks := dispatchActionEvent(t, &model.Msg0108{Header: header, UpgradeType: 1, Result: 2})

	require.Len(t, hooks.upgrades, 1)
	event := hooks.upgrades[0]
	require.NotEmpty(t, event.EventID)
	require.Equal(t, uint8(1), event.UpgradeType)
	require.Equal(t, uint8(2), event.Result)
}

func TestMediaEvent(t *testing.T) {
	header := actionHeader(0x0800, "13800000008", 8)
	hooks := dispatchActionEvent(t, &model.Msg0800{
		Header: header, MultiMediaID: 100, MultiMediaType: 0,
		MultiMediaContainer: 0, EventID: 1, LogicChannelID: 2,
	})

	require.Len(t, hooks.mediaEvents, 1)
	event := hooks.mediaEvents[0]
	require.NotEmpty(t, event.EventID)
	require.Equal(t, uint32(100), event.MultiMediaID)
	require.Equal(t, uint8(0), event.MultiMediaType)
	require.Equal(t, uint8(0), event.MultiMediaContainer)
	require.Equal(t, uint8(1), event.MediaEventID)
	require.Equal(t, uint8(2), event.LogicChannelID)
}

func TestMediaUploadEvent(t *testing.T) {
	header := actionHeader(0x0801, "13800000009", 9)
	hooks := dispatchActionEvent(t, &model.Msg0801{
		Header: header, MultiMediaID: 101, MultiMediaType: 1,
		MultiMediaContainer: 2, EventID: 3, LogicChannelID: 4,
		FragmentData: []byte{1, 2, 3},
	})

	require.Len(t, hooks.mediaUpload, 1)
	event := hooks.mediaUpload[0]
	require.NotEmpty(t, event.EventID)
	require.Equal(t, uint32(101), event.MultiMediaID)
	require.Equal(t, uint8(1), event.MultiMediaType)
	require.Equal(t, uint8(2), event.MultiMediaContainer)
	require.Equal(t, uint8(3), event.MediaEventID)
	require.Equal(t, uint8(4), event.LogicChannelID)
	require.Equal(t, 3, event.FragmentBytes)
}

func TestFileUploadCompleteEvent(t *testing.T) {
	header := actionHeader(0x1206, "13800000010", 10)
	hooks := dispatchActionEvent(t, &model.Msg1206{
		Header: header, AnswerSerialNumber: 11, Result: 1,
	})

	require.Len(t, hooks.fileUploads, 1)
	event := hooks.fileUploads[0]
	require.NotEmpty(t, event.EventID)
	require.Equal(t, uint16(11), event.AnswerSerialNumber)
	require.Equal(t, uint8(1), event.Result)
}

func TestAttachmentInfoEvent(t *testing.T) {
	header := actionHeader(0x1210, "13800000011", 11)
	alarmSign := make([]byte, 16)
	alarmSign[0] = 0xAA
	hooks := dispatchActionEvent(t, &model.Msg1210{
		Header: header, TerminalID: "T1", AlarmSign: alarmSign,
		AlarmNo: "A-1", InfoType: 1, AttachmentCount: 1,
		Attachments: []model.AlarmAttachmentEntry{{FileName: "a.jpg", FileSize: 1024}},
	})

	require.Len(t, hooks.attachments, 1)
	event := hooks.attachments[0]
	require.NotEmpty(t, event.EventID)
	require.Equal(t, "T1", event.TerminalID)
	require.Equal(t, alarmSign, event.AlarmSign)
	require.Equal(t, "A-1", event.AlarmNo)
	require.Equal(t, uint8(1), event.InfoType)
	require.Equal(t, uint8(1), event.AttachmentCount)
	require.Len(t, event.Attachments, 1)
	require.Equal(t, "a.jpg", event.Attachments[0].FileName)
	require.Equal(t, uint32(1024), event.Attachments[0].FileSize)
}
