package protocol

import (
	"context"
	"sync"
	"time"

	"github.com/rs/zerolog/log"

	"github.com/fakeyanss/jt808-server-go/internal/protocol/model"
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

// DeviceProfileCache 是网关进程内的 0x0100 档案缓存。
//
// 只保存本次注册报文携带的静态字段（设备 ID、车牌、协议版本），0x0102 鉴权时
// 随事件交给管控落库。鉴权码由手机号现算，不进入缓存。
type DeviceProfileCache struct {
	mu      sync.Mutex
	ttl     time.Duration
	entries map[string]profileEntry
}

type profileEntry struct {
	profile   model.DeviceProfile
	expiresAt time.Time
}

// NewDeviceProfileCache 创建档案缓存；ttl 为零时使用 5 分钟。
func NewDeviceProfileCache(ttl time.Duration) *DeviceProfileCache {
	if ttl <= 0 {
		ttl = 5 * time.Minute
	}
	return &DeviceProfileCache{ttl: ttl, entries: map[string]profileEntry{}}
}

// Put 记录一次注册携带的档案快照。
func (c *DeviceProfileCache) Put(phone string, profile model.DeviceProfile) {
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
	c.entries[phone] = profileEntry{profile: profile, expiresAt: now.Add(c.ttl)}
}

// Get 返回未过期的档案快照。
func (c *DeviceProfileCache) Get(phone string) (model.DeviceProfile, bool) {
	if c == nil {
		return model.DeviceProfile{}, false
	}
	c.mu.Lock()
	defer c.mu.Unlock()
	entry, found := c.entries[phone]
	if !found {
		return model.DeviceProfile{}, false
	}
	if time.Now().After(entry.expiresAt) {
		delete(c.entries, phone)
		return model.DeviceProfile{}, false
	}
	return entry.profile, true
}

// processRegisterProtocol 是 0x0100 的网关半边：只写本进程档案缓存并回 0x8100。
// 不查库、不写库；档案由管控在 0x0102 鉴权通过后落库。
func (mp *JT808MsgProcessor) processRegisterProtocol(ctx context.Context, data *model.ProcessData) error {
	in, ok := data.Incoming.(*model.Msg0100)
	if !ok || in == nil {
		return nil
	}
	out, _ := data.Outgoing.(*model.Msg8100)
	if out == nil {
		return nil
	}

	phone := in.Header.PhoneNumber
	out.Result = model.ResSuccess
	out.AuthCode = AuthCodeForPhone(phone)

	// 先写缓存再应答：随后的 0x0102 依赖它把档案带给管控。
	if mp.deps.DeviceProfileCache != nil {
		mp.deps.DeviceProfileCache.Put(phone, profileFromMsg0100(in))
	}

	if session, ok := ctx.Value(model.SessionCtxKey{}).(*model.Session); ok && session != nil {
		// 手机号未被其它活会话占用时立即绑定；已被占用则留到鉴权通过后再顶替。
		if bound, bindErr := mp.deps.Runtime.Conn.BindPhoneIfFree(ctx, phone, session.ID); bindErr != nil {
			log.Warn().Err(bindErr).Str("phone", phone).Str("session", session.ID).
				Msg("Fail to bind phone on registration")
		} else if !bound {
			log.Info().Str("phone", phone).Str("session", session.ID).
				Msg("Phone is already bound, deferring takeover until authentication")
		}
	}
	return nil
}

// processAuthProtocol 是 0x0102 的网关半边：只比 f(phone)，并把命中的档案随事件上行。
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
	if in.AuthCode != AuthCodeForPhone(phone) {
		out.Result = model.ResultFail
		return nil
	}

	out.Result = model.ResultSuccess
	if mp.deps.DeviceProfileCache != nil {
		if profile, cached := mp.deps.DeviceProfileCache.Get(phone); cached {
			data.UplinkProfile = &profile
		}
	}
	if session, ok := ctx.Value(model.SessionCtxKey{}).(*model.Session); ok && session != nil {
		if bindErr := mp.bindPhoneSession(ctx, phone, session); bindErr != nil {
			log.Warn().Err(bindErr).Str("phone", phone).Str("session", session.ID).
				Msg("Session was superseded during authentication, skip device update")
			return nil
		}
	}
	return nil
}

// profileFromMsg0100 提取注册报文携带的档案静态字段。
func profileFromMsg0100(in *model.Msg0100) model.DeviceProfile {
	profile := model.DeviceProfile{
		DeviceID: in.DeviceID,
		Plate:    in.PlateNumber,
	}
	if in.Header != nil {
		if in.Header.Attr != nil {
			profile.VersionDesc = in.Header.Attr.VersionDesc
		}
		profile.ProtocolVersion = in.Header.ProtocolVersion
	}
	return profile
}
