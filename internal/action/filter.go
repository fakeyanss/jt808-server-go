package action

import (
	"context"
	"fmt"
	"strings"
)

// eventNames 是可订阅的规范事件名，与 HTTP 回调 payload 里的 event 字段一致。
var eventNames = []string{
	"register",
	"auth",
	"location",
	"alarm",
	"disconnect",
	"waybill",
	"driverIdentity",
	"canBus",
	"tachograph",
	"eventReport",
	"vehicleControlAck",
	"terminalUpgradeResult",
	"mediaEvent",
	"mediaUpload",
	"fileUploadComplete",
	"attachmentInfo",
}

// EventNames 返回全部可订阅事件名；调用方不得修改返回切片。
func EventNames() []string {
	return append([]string(nil), eventNames...)
}

// NewFilter 按事件名过滤下游钩子。
// names 为空表示不配置范围，原样返回 next（保持全量派发的默认行为）。
// 未知事件名返回错误，让启动阶段直接拒绝，而不是运行期静默少发。
func NewFilter(next Hooks, names []string) (Hooks, error) {
	if next == nil {
		next = Noop{}
	}
	if len(names) == 0 {
		return next, nil
	}
	enabled := make(map[string]bool, len(names))
	for _, raw := range names {
		name := strings.TrimSpace(raw)
		canonical, ok := canonicalEventName(name)
		if !ok {
			return nil, fmt.Errorf("unknown action event %q; valid events: %s",
				raw, strings.Join(eventNames, ", "))
		}
		enabled[canonical] = true
	}
	return &filter{next: next, enabled: enabled}, nil
}

// canonicalEventName 忽略大小写匹配规范事件名。
func canonicalEventName(name string) (string, bool) {
	for _, candidate := range eventNames {
		if strings.EqualFold(candidate, name) {
			return candidate, true
		}
	}
	return "", false
}

// filter 是 Hooks 的事件范围装饰器：只把命中范围的回调转发给 next。
type filter struct {
	next    Hooks
	enabled map[string]bool
}

func (f *filter) OnRegister(ctx context.Context, event RegisterEvent) {
	if f.enabled["register"] {
		f.next.OnRegister(ctx, event)
	}
}

func (f *filter) OnAuth(ctx context.Context, event AuthEvent) {
	if f.enabled["auth"] {
		f.next.OnAuth(ctx, event)
	}
}

func (f *filter) OnLocation(ctx context.Context, event LocationEvent) {
	if f.enabled["location"] {
		f.next.OnLocation(ctx, event)
	}
}

func (f *filter) OnAlarm(ctx context.Context, event AlarmEvent) {
	if f.enabled["alarm"] {
		f.next.OnAlarm(ctx, event)
	}
}

func (f *filter) OnDisconnect(ctx context.Context, event DisconnectEvent) {
	if f.enabled["disconnect"] {
		f.next.OnDisconnect(ctx, event)
	}
}

func (f *filter) OnWaybill(ctx context.Context, event WaybillEvent) {
	if f.enabled["waybill"] {
		f.next.OnWaybill(ctx, event)
	}
}

func (f *filter) OnDriverIdentity(ctx context.Context, event DriverIdentityEvent) {
	if f.enabled["driverIdentity"] {
		f.next.OnDriverIdentity(ctx, event)
	}
}

func (f *filter) OnCANBus(ctx context.Context, event CANBusEvent) {
	if f.enabled["canBus"] {
		f.next.OnCANBus(ctx, event)
	}
}

func (f *filter) OnTachograph(ctx context.Context, event TachographEvent) {
	if f.enabled["tachograph"] {
		f.next.OnTachograph(ctx, event)
	}
}

func (f *filter) OnEventReport(ctx context.Context, event EventReportEvent) {
	if f.enabled["eventReport"] {
		f.next.OnEventReport(ctx, event)
	}
}

func (f *filter) OnVehicleControlAck(ctx context.Context, event VehicleControlAckEvent) {
	if f.enabled["vehicleControlAck"] {
		f.next.OnVehicleControlAck(ctx, event)
	}
}

func (f *filter) OnTerminalUpgradeResult(ctx context.Context, event TerminalUpgradeResultEvent) {
	if f.enabled["terminalUpgradeResult"] {
		f.next.OnTerminalUpgradeResult(ctx, event)
	}
}

func (f *filter) OnMediaEvent(ctx context.Context, event MediaEvent) {
	if f.enabled["mediaEvent"] {
		f.next.OnMediaEvent(ctx, event)
	}
}

func (f *filter) OnMediaUpload(ctx context.Context, event MediaUploadEvent) {
	if f.enabled["mediaUpload"] {
		f.next.OnMediaUpload(ctx, event)
	}
}

func (f *filter) OnFileUploadComplete(ctx context.Context, event FileUploadCompleteEvent) {
	if f.enabled["fileUploadComplete"] {
		f.next.OnFileUploadComplete(ctx, event)
	}
}

func (f *filter) OnAttachmentInfo(ctx context.Context, event AttachmentInfoEvent) {
	if f.enabled["attachmentInfo"] {
		f.next.OnAttachmentInfo(ctx, event)
	}
}
