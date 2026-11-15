package protocol

import (
	"context"
	"time"

	actionpkg "github.com/fakeyanss/jt808-server-go/internal/action"
	"github.com/fakeyanss/jt808-server-go/internal/protocol/model"
)

// dispatchExtendedHooks 派发一期的扩展业务事件。
func (mp *JT808MsgProcessor) dispatchExtendedHooks(ctx context.Context, data *model.ProcessData) {
	DispatchExtendedHooks(ctx, mp.hooks, data)
}

// DispatchExtendedHooks 派发一期的扩展业务事件。
// 只在消息已成功解码（类型断言成立）后调用，解析失败的报文不会走到这里。
func DispatchExtendedHooks(ctx context.Context, hooks actionpkg.Hooks, data *model.ProcessData) {
	if data == nil || data.Incoming == nil {
		return
	}
	switch in := data.Incoming.(type) {
	case *model.Msg0701:
		hooks.OnWaybill(ctx, newWaybillEvent(hookEventID(ctx, 0x0701, in), in))
	case *model.Msg0702:
		hooks.OnDriverIdentity(ctx, newDriverIdentityEvent(hookEventID(ctx, 0x0702, in), in))
	case *model.Msg0705:
		hooks.OnCANBus(ctx, newCANBusEvent(hookEventID(ctx, 0x0705, in), in))
	case *model.Msg0700:
		hooks.OnTachograph(ctx, newTachographEvent(hookEventID(ctx, 0x0700, in), in))
	case *model.Msg0301:
		hooks.OnEventReport(ctx, newEventReportEvent(hookEventID(ctx, 0x0301, in), in))
	case *model.Msg0500:
		hooks.OnVehicleControlAck(ctx, newVehicleControlAckEvent(hookEventID(ctx, 0x0500, in), in))
	case *model.Msg0108:
		hooks.OnTerminalUpgradeResult(ctx, newTerminalUpgradeResultEvent(hookEventID(ctx, 0x0108, in), in))
	case *model.Msg0800:
		hooks.OnMediaEvent(ctx, newMediaEvent(hookEventID(ctx, 0x0800, in), in))
	case *model.Msg0801:
		hooks.OnMediaUpload(ctx, newMediaUploadEvent(hookEventID(ctx, 0x0801, in), in))
	case *model.Msg1206:
		hooks.OnFileUploadComplete(ctx, newFileUploadCompleteEvent(hookEventID(ctx, 0x1206, in), in))
	case *model.Msg1210:
		hooks.OnAttachmentInfo(ctx, newAttachmentInfoEvent(hookEventID(ctx, 0x1210, in), in))
	}
}

// hookTime 返回事件时间：协议自带时间时用协议值，否则用接收时间（YYMMDDhhmmss）。
func hookTime(protocolTime string) string {
	if protocolTime != "" {
		return protocolTime
	}
	return time.Now().Format("060102150405")
}

func newWaybillEvent(eventID string, in *model.Msg0701) actionpkg.WaybillEvent {
	return actionpkg.WaybillEvent{
		EventID: eventID,
		Phone:   in.Header.PhoneNumber,
		Serial:  in.Header.SerialNumber,
		Time:    hookTime(""),
		Content: in.Content,
	}
}

func newDriverIdentityEvent(eventID string, in *model.Msg0702) actionpkg.DriverIdentityEvent {
	return actionpkg.DriverIdentityEvent{
		EventID:           eventID,
		Phone:             in.Header.PhoneNumber,
		Serial:            in.Header.SerialNumber,
		Time:              hookTime(in.Time),
		State:             in.State,
		ReadResult:        in.ReadResult,
		Name:              in.Name,
		QualificationCode: in.QualificationCode,
		IssuingAuthority:  in.IssuingAuthority,
		ValidUntil:        in.ValidUntil,
		IdentityNumber:    in.IdentityNumber,
	}
}

func newCANBusEvent(eventID string, in *model.Msg0705) actionpkg.CANBusEvent {
	items := make([]actionpkg.CANDataItem, 0, len(in.Items))
	for _, item := range in.Items {
		items = append(items, actionpkg.CANDataItem{
			CANID: item.CANID,
			Data:  append([]byte(nil), item.Data[:]...),
		})
	}
	return actionpkg.CANBusEvent{
		EventID: eventID,
		Phone:   in.Header.PhoneNumber,
		Serial:  in.Header.SerialNumber,
		Time:    hookTime(in.FirstTime),
		Count:   in.Count,
		Items:   items,
	}
}

func newTachographEvent(eventID string, in *model.Msg0700) actionpkg.TachographEvent {
	return actionpkg.TachographEvent{
		EventID:      eventID,
		Phone:        in.Header.PhoneNumber,
		Serial:       in.Header.SerialNumber,
		Time:         hookTime(""),
		AnswerSerial: in.AnswerSerial,
		Command:      in.Command,
		Data:         append([]byte(nil), in.Data...),
	}
}

func newEventReportEvent(eventID string, in *model.Msg0301) actionpkg.EventReportEvent {
	return actionpkg.EventReportEvent{
		EventID:  eventID,
		Phone:    in.Header.PhoneNumber,
		Serial:   in.Header.SerialNumber,
		Time:     hookTime(""),
		ReportID: in.EventID,
	}
}

func newVehicleControlAckEvent(eventID string, in *model.Msg0500) actionpkg.VehicleControlAckEvent {
	return actionpkg.VehicleControlAckEvent{
		EventID:      eventID,
		Phone:        in.Header.PhoneNumber,
		Serial:       in.Header.SerialNumber,
		Time:         hookTime(""),
		ControlFlags: in.ControlFlags,
	}
}

func newTerminalUpgradeResultEvent(eventID string, in *model.Msg0108) actionpkg.TerminalUpgradeResultEvent {
	return actionpkg.TerminalUpgradeResultEvent{
		EventID:     eventID,
		Phone:       in.Header.PhoneNumber,
		Serial:      in.Header.SerialNumber,
		Time:        hookTime(""),
		UpgradeType: in.UpgradeType,
		Result:      in.Result,
	}
}

func newMediaEvent(eventID string, in *model.Msg0800) actionpkg.MediaEvent {
	return actionpkg.MediaEvent{
		EventID:             eventID,
		Phone:               in.Header.PhoneNumber,
		Serial:              in.Header.SerialNumber,
		Time:                hookTime(""),
		MultiMediaID:        in.MultiMediaID,
		MultiMediaType:      in.MultiMediaType,
		MultiMediaContainer: in.MultiMediaContainer,
		MediaEventID:        in.EventID,
		LogicChannelID:      in.LogicChannelID,
	}
}

func newMediaUploadEvent(eventID string, in *model.Msg0801) actionpkg.MediaUploadEvent {
	return actionpkg.MediaUploadEvent{
		EventID:             eventID,
		Phone:               in.Header.PhoneNumber,
		Serial:              in.Header.SerialNumber,
		Time:                hookTime(""),
		MultiMediaID:        in.MultiMediaID,
		MultiMediaType:      in.MultiMediaType,
		MultiMediaContainer: in.MultiMediaContainer,
		MediaEventID:        in.EventID,
		LogicChannelID:      in.LogicChannelID,
		FragmentBytes:       len(in.FragmentData),
	}
}

func newFileUploadCompleteEvent(eventID string, in *model.Msg1206) actionpkg.FileUploadCompleteEvent {
	return actionpkg.FileUploadCompleteEvent{
		EventID:            eventID,
		Phone:              in.Header.PhoneNumber,
		Serial:             in.Header.SerialNumber,
		Time:               hookTime(""),
		AnswerSerialNumber: in.AnswerSerialNumber,
		Result:             in.Result,
	}
}

func newAttachmentInfoEvent(eventID string, in *model.Msg1210) actionpkg.AttachmentInfoEvent {
	attachments := make([]actionpkg.AttachmentEntry, 0, len(in.Attachments))
	for _, entry := range in.Attachments {
		attachments = append(attachments, actionpkg.AttachmentEntry{
			FileName: entry.FileName,
			FileSize: entry.FileSize,
		})
	}
	return actionpkg.AttachmentInfoEvent{
		EventID:         eventID,
		Phone:           in.Header.PhoneNumber,
		Serial:          in.Header.SerialNumber,
		Time:            hookTime(""),
		TerminalID:      in.TerminalID,
		AlarmSign:       append([]byte(nil), in.AlarmSign...),
		AlarmNo:         in.AlarmNo,
		InfoType:        in.InfoType,
		AttachmentCount: in.AttachmentCount,
		Attachments:     attachments,
	}
}
