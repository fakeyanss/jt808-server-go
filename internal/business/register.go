package business

import (
	"context"
	"time"

	"github.com/pkg/errors"

	"github.com/fakeyanss/jt808-server-go/internal/protocol"
	"github.com/fakeyanss/jt808-server-go/internal/protocol/model"
	"github.com/fakeyanss/jt808-server-go/internal/repository"
)

// processTerminalReplyBusiness 是 0x0001 的管控半边占位实现。
//
// 应答不在这里落库：管控的 uplink HTTP 入口在解析出 TerminalReply 后直接调用
// TerminalReplyHandler（outbox），此处只用于归属声明与注册表一致性校验。
func (p *Processor) processTerminalReplyBusiness(context.Context, *model.ProcessData) error {
	return nil
}

// processRegisterBusiness 是 0x0100 的管控半边：把注册信息与鉴权码落库。
//
// 只写注册拥有的静态字段，不做整行覆盖：多副本下 0x0100 可能晚于 0x0102
// 到达，整行写回会冲掉鉴权刚置上的在线状态、会话与最后通信时间。
func (p *Processor) processRegisterBusiness(ctx context.Context, data *model.ProcessData) error {
	in, ok := data.Incoming.(*model.Msg0100)
	if !ok || in == nil {
		return nil
	}
	devices := p.repos.Devices

	existing, err := devices.GetByPhone(ctx, in.Header.PhoneNumber)
	if err != nil && !errors.Is(err, repository.ErrDeviceNotFound) {
		return err
	}
	device := existing
	if device == nil {
		device = &model.Device{
			ID:              in.DeviceID,
			Plate:           in.PlateNumber,
			Phone:           in.Header.PhoneNumber,
			Keepalive:       time.Minute,
			Status:          model.DeviceStatusOffline,
			VersionDesc:     in.Header.Attr.VersionDesc,
			ProtocolVersion: in.Header.ProtocolVersion,
		}
	} else {
		device.ID = in.DeviceID
		device.Plate = in.PlateNumber
		device.VersionDesc = in.Header.Attr.VersionDesc
		device.ProtocolVersion = in.Header.ProtocolVersion
	}
	device.AuthCode = protocol.RegisterAuthCode(in)
	return devices.SaveRegistration(ctx, device)
}

// processAuthBusiness 是 0x0102 的管控半边：鉴权通过后更新在线状态。
// 鉴权失败只在会话一致时清理未完成注册的档案，避免伪造报文删档。
func (p *Processor) processAuthBusiness(ctx context.Context, data *model.ProcessData) error {
	in, ok := data.Incoming.(*model.Msg0102)
	if !ok || in == nil {
		return nil
	}
	devices := p.repos.Devices
	phone := in.Header.PhoneNumber
	device, err := devices.GetByPhone(ctx, phone)
	if errors.Is(err, repository.ErrDeviceNotFound) {
		return repository.ErrDeviceNotFound
	}
	if err != nil {
		return err
	}

	if in.AuthCode != device.AuthCode {
		event := protocol.UplinkEventFromContext(ctx)
		if event.SessionID == "" || device.SessionID == event.SessionID {
			_ = devices.DeleteByPhone(ctx, phone)
		}
		return nil
	}

	event := protocol.UplinkEventFromContext(ctx)
	if event.SessionID != "" {
		device.SessionID = event.SessionID
	}
	device.Status = model.DeviceStatusOnline
	device.LastestComTime = time.Now()
	device.IMEI = in.IMEI
	device.SoftwareVersion = in.SoftwareVersion
	return devices.SaveAuthState(ctx, device)
}
