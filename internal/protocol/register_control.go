package protocol

import (
	"context"
	"sync"
	"time"

	"github.com/pkg/errors"
	"github.com/rs/zerolog/log"

	"github.com/fakeyanss/jt808-server-go/internal/domain"
	"github.com/fakeyanss/jt808-server-go/internal/protocol/model"
	"github.com/fakeyanss/jt808-server-go/internal/repository"
)

// UplinkEventCtxKey 承载控制面执行器从事件带到 handler 的上下文。
type UplinkEventCtxKey struct{}

// UplinkEventContext 是 handler 在管控侧能看到的、与活连接无关的事件信息。
type UplinkEventContext struct {
	EventID     string
	SessionID   string
	Phone       string
	ReplyMsgID  uint16
	ReplyResult uint8
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
			// 重复注册不签发新码，但必须回填档案里的鉴权码：
			// 否则应答会带 GenOutgoing 的占位符，终端无法完成鉴权。
			code, codeErr := mp.deps.AuthProvider.AuthCode(ctx, in.Header.PhoneNumber)
			if codeErr != nil {
				log.Warn().Err(codeErr).Str("phone", in.Header.PhoneNumber).
					Msg("Auth provider lookup failed while returning existing auth code")
				return codeErr
			}
			out.Result = model.ResDeviceAlreadyRegister
			out.AuthCode = code
			return nil
		} else if !errors.Is(err, repository.ErrDeviceNotFound) {
			// 管控不可用时不放行注册，避免在档案未知的情况下签发鉴权码。
			log.Warn().Err(err).Str("phone", in.Header.PhoneNumber).Msg("Auth provider lookup failed during registration")
			return err
		}
	}

	code := RegisterAuthCode(in)
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

// RegisterAuthCode 计算注册鉴权码；standalone、网关半边与管控半边共用同一算法。
func RegisterAuthCode(in *model.Msg0100) string {
	device := &domain.Device{ID: in.DeviceID, Plate: in.PlateNumber, Phone: in.Header.PhoneNumber}
	return genAuthCode(device)
}
