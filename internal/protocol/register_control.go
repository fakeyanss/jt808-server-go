package protocol

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"sync"
	"time"

	"github.com/pkg/errors"
	"github.com/rs/zerolog/log"

	"github.com/fakeyanss/jt808-server-go/internal/protocol/model"
	"github.com/fakeyanss/jt808-server-go/internal/repository"
)

// UplinkEventCtxKey 承载控制面执行器从事件带到 handler 的上下文。
type UplinkEventCtxKey struct{}

// UplinkEventContext 是 handler 在管控侧能看到的、与活连接无关的事件信息。
type UplinkEventContext struct {
	EventID   string
	SessionID string
	Phone     string
}

// UplinkEventFromContext 返回事件上下文；standalone 下为空。
func UplinkEventFromContext(ctx context.Context) UplinkEventContext {
	value, _ := ctx.Value(UplinkEventCtxKey{}).(UplinkEventContext)
	return value
}

// RegisterCache 是网关本地的注册缓存。
//
// 它只保存 0x0100 当次计算出的鉴权码与占用会话，TTL 覆盖一次注册/鉴权往返；
// 设备档案的权威副本仍在管控，缓存丢失只会让网关回退到只读查询。
type RegisterCache struct {
	mu      sync.Mutex
	ttl     time.Duration
	entries map[string]registerEntry
}

type registerEntry struct {
	authCode  string
	sessionID string
	expiresAt time.Time
}

// NewRegisterCache 创建注册缓存；ttl 为零时使用 5 分钟。
func NewRegisterCache(ttl time.Duration) *RegisterCache {
	if ttl <= 0 {
		ttl = 5 * time.Minute
	}
	return &RegisterCache{ttl: ttl, entries: map[string]registerEntry{}}
}

// Put 记录一次注册计算出的鉴权码。
func (c *RegisterCache) Put(phone, authCode, sessionID string) {
	if c == nil || phone == "" {
		return
	}
	c.mu.Lock()
	defer c.mu.Unlock()
	now := time.Now()
	if len(c.entries) > 100000 {
		for key, entry := range c.entries {
			if now.After(entry.expiresAt) {
				delete(c.entries, key)
			}
		}
	}
	c.entries[phone] = registerEntry{authCode: authCode, sessionID: sessionID, expiresAt: now.Add(c.ttl)}
}

// Get 返回未过期的缓存项。
func (c *RegisterCache) Get(phone string) (authCode, sessionID string, ok bool) {
	if c == nil {
		return "", "", false
	}
	c.mu.Lock()
	defer c.mu.Unlock()
	entry, found := c.entries[phone]
	if !found {
		return "", "", false
	}
	if time.Now().After(entry.expiresAt) {
		delete(c.entries, phone)
		return "", "", false
	}
	return entry.authCode, entry.sessionID, true
}

// processRegisterProtocol 是 0x0100 的网关半边：只读判断、计算鉴权码并回 0x8100。
// 不做任何档案写入，档案由管控消费注册事件后落库。
func (mp *JT808MsgProcessor) processRegisterProtocol(ctx context.Context, data *model.ProcessData) error {
	in, ok := data.Incoming.(*model.Msg0100)
	if !ok || in == nil {
		return nil
	}
	out, _ := data.Outgoing.(*model.Msg8100)
	if out == nil {
		return nil
	}

	// 已知档案：沿用既有语义，拒绝重复注册。
	if mp.deps.AuthProvider != nil {
		if _, err := mp.deps.AuthProvider.Device(ctx, in.Header.PhoneNumber); err == nil {
			out.Result = model.ResDeviceAlreadyRegister
			return nil
		} else if !errors.Is(err, repository.ErrDeviceNotFound) {
			// 管控不可用时不放行注册，避免在档案未知的情况下签发鉴权码。
			log.Warn().Err(err).Str("phone", in.Header.PhoneNumber).Msg("Auth provider lookup failed during registration")
			return err
		}
	}

	code := registerAuthCode(in)
	out.AuthCode = code
	if mp.deps.RegisterCache != nil {
		sessionID := ""
		if session, ok := ctx.Value(model.SessionCtxKey{}).(*model.Session); ok && session != nil {
			sessionID = session.ID
			// 手机号未被其它活会话占用时立即绑定；已被占用则留到鉴权通过后再顶替。
			if bound, bindErr := mp.deps.Runtime.Conn.BindPhoneIfFree(ctx, in.Header.PhoneNumber, session.ID); bindErr != nil {
				log.Warn().Err(bindErr).Str("phone", in.Header.PhoneNumber).Str("session", session.ID).
					Msg("Fail to bind phone on registration")
			} else if !bound {
				log.Info().Str("phone", in.Header.PhoneNumber).Str("session", session.ID).
					Msg("Phone is already bound, deferring takeover until authentication")
			}
		}
		mp.deps.RegisterCache.Put(in.Header.PhoneNumber, code, sessionID)
	}
	return nil
}

// processRegisterBusiness 是 0x0100 的管控半边：把注册信息与鉴权码落库。
//
// 只写注册拥有的静态字段，不做整行覆盖：多副本下 0x0100 可能晚于 0x0102
// 到达，整行写回会冲掉鉴权刚置上的在线状态、会话与最后通信时间。
func (mp *JT808MsgProcessor) processRegisterBusiness(ctx context.Context, data *model.ProcessData) error {
	in, ok := data.Incoming.(*model.Msg0100)
	if !ok || in == nil {
		return nil
	}
	devices := mp.deps.Storage.Devices

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
	device.AuthCode = registerAuthCode(in)
	return devices.SaveRegistration(ctx, device)
}

// processAuthProtocol 是 0x0102 的网关半边：本地校验鉴权码并回 0x8001。
func (mp *JT808MsgProcessor) processAuthProtocol(ctx context.Context, data *model.ProcessData) error {
	in, ok := data.Incoming.(*model.Msg0102)
	if !ok || in == nil {
		return nil
	}
	out, _ := data.Outgoing.(*model.Msg8001)
	if out == nil {
		return nil
	}
	phone := in.Header.PhoneNumber
	expected, _, cached := "", "", false
	if mp.deps.RegisterCache != nil {
		expected, _, cached = mp.deps.RegisterCache.Get(phone)
	}
	if !cached && mp.deps.AuthProvider != nil {
		code, err := mp.deps.AuthProvider.AuthCode(ctx, phone)
		if err != nil && !errors.Is(err, repository.ErrDeviceNotFound) {
			log.Warn().Err(err).Str("phone", phone).Msg("Auth provider lookup failed during authentication")
			return err
		}
		if err == nil {
			expected, cached = code, true
		}
	}
	if !cached {
		// 没有注册缓存也没有档案：交给管控 409，由网关重投注册事件后让终端重传。
		out.Result = model.ResultFail
		return nil
	}
	if in.AuthCode != expected {
		out.Result = model.ResultFail
		return nil
	}

	out.Result = model.ResultSuccess
	if session, ok := ctx.Value(model.SessionCtxKey{}).(*model.Session); ok && session != nil {
		if bindErr := mp.bindPhoneSession(ctx, phone, session); bindErr != nil {
			log.Warn().Err(bindErr).Str("phone", phone).Str("session", session.ID).
				Msg("Session was superseded during authentication, skip device update")
			return nil
		}
	}
	return nil
}

// processAuthBusiness 是 0x0102 的管控半边：鉴权通过后更新在线状态。
// 鉴权失败只在会话一致时清理未完成注册的档案，避免伪造报文删档。
func (mp *JT808MsgProcessor) processAuthBusiness(ctx context.Context, data *model.ProcessData) error {
	in, ok := data.Incoming.(*model.Msg0102)
	if !ok || in == nil {
		return nil
	}
	devices := mp.deps.Storage.Devices
	phone := in.Header.PhoneNumber
	device, err := devices.GetByPhone(ctx, phone)
	if errors.Is(err, repository.ErrDeviceNotFound) {
		return repository.ErrDeviceNotFound
	}
	if err != nil {
		return err
	}

	if in.AuthCode != device.AuthCode {
		event := UplinkEventFromContext(ctx)
		if event.SessionID == "" || device.SessionID == event.SessionID {
			_ = devices.DeleteByPhone(ctx, phone)
		}
		return nil
	}

	event := UplinkEventFromContext(ctx)
	if event.SessionID != "" {
		device.SessionID = event.SessionID
	}
	device.Status = model.DeviceStatusOnline
	device.LastestComTime = time.Now()
	device.IMEI = in.IMEI
	device.SoftwareVersion = in.SoftwareVersion
	return devices.SaveAuthState(ctx, device)
}

// processTerminalReplyBusiness 是 0x0001 的管控半边占位实现。
//
// 应答不在这里落库：管控的 uplink HTTP 入口在解析出 TerminalReply 后直接调用
// TerminalReplyHandler（outbox），此处只用于归属声明与注册表一致性校验。
func (mp *JT808MsgProcessor) processTerminalReplyBusiness(context.Context, *model.ProcessData) error {
	return nil
}

// registerAuthCode 计算注册鉴权码；standalone 与网关半边共用同一算法。
func registerAuthCode(in *model.Msg0100) string {
	device := &model.Device{ID: in.DeviceID, Plate: in.PlateNumber, Phone: in.Header.PhoneNumber}
	return genAuthCode(device)
}

// locationDedupKey 把事件 ID 与数据项下标压成短幂等键；没有事件上下文时返回空串，
// 写入 NULL 后不参与唯一约束。
func locationDedupKey(eventID string, itemIndex int) string {
	if eventID == "" {
		return ""
	}
	sum := sha256.Sum256([]byte(fmt.Sprintf("%s|%d", eventID, itemIndex)))
	return hex.EncodeToString(sum[:])[:32]
}
