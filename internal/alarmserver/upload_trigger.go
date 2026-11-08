package alarmserver

import (
	"context"
	"encoding/hex"
	"strings"
	"sync"
	"time"

	"github.com/rs/zerolog/log"

	actionpkg "github.com/fakeyanss/jt808-server-go/internal/action"
	"github.com/fakeyanss/jt808-server-go/internal/protocol/model"
)

// DefaultUploadDedupWindow 是同一报警标识号的去重窗口。
// 与报警仓储默认去重窗口保持一致，避免内存中的去重记录无界增长。
const DefaultUploadDedupWindow = 30 * time.Minute

// UploadSender 负责把 0x9208 下发到当前在线终端。
// 设备不在线、会话已关闭或编码失败均通过 error 返回，由触发方记录日志。
type UploadSender func(phone string, msg *model.Msg9208) error

// UploadTriggerOptions 是自动下发 0x9208 所需的附件服务器地址参数。
type UploadTriggerOptions struct {
	IP          string        // 附件服务器 IP，直接写入 0x9208
	TCPPort     uint16        // 附件服务器 TCP 端口
	UDPPort     uint16        // 附件服务器 UDP 端口
	DedupWindow time.Duration // 同一报警标识号的去重窗口，<=0 时使用默认值
}

// UploadTrigger 在报警首次上报时自动下发 0x9208 报警附件上传指令。
//
// 它装饰既有 Hooks：先转发原始事件，再根据 0x0200 扩展项中提取的报警标识号
// 决定是否下发。所有失败都只记录日志，不影响协议主流程。
type UploadTrigger struct {
	next actionpkg.Hooks
	opts UploadTriggerOptions
	send UploadSender

	mu   sync.Mutex
	seen map[string]time.Time

	// now 可在测试中替换，用于验证去重窗口而不依赖真实时间。
	now func() time.Time
}

// NewUploadTrigger 创建自动下发装饰器。next 为 nil 时使用 action.Noop。
func NewUploadTrigger(next actionpkg.Hooks, opts UploadTriggerOptions, send UploadSender) *UploadTrigger {
	if next == nil {
		next = actionpkg.Noop{}
	}
	if opts.DedupWindow <= 0 {
		opts.DedupWindow = DefaultUploadDedupWindow
	}
	return &UploadTrigger{
		next: next,
		opts: opts,
		send: send,
		seen: make(map[string]time.Time),
		now:  time.Now,
	}
}

func (t *UploadTrigger) OnRegister(ctx context.Context, event actionpkg.RegisterEvent) {
	t.next.OnRegister(ctx, event)
}

func (t *UploadTrigger) OnAuth(ctx context.Context, event actionpkg.AuthEvent) {
	t.next.OnAuth(ctx, event)
}

func (t *UploadTrigger) OnLocation(ctx context.Context, event actionpkg.LocationEvent) {
	t.next.OnLocation(ctx, event)
}

func (t *UploadTrigger) OnDisconnect(ctx context.Context, event actionpkg.DisconnectEvent) {
	t.next.OnDisconnect(ctx, event)
}

// OnAlarm 只在报警首次上报时下发一次 0x9208。
//
// 报警标识号必须来自 0x0200 扩展项（0x64~0x67），取不到时跳过并记 warn，
// 不生成任何替代值。同一报警标识号在去重窗口内不会重复下发。
func (t *UploadTrigger) OnAlarm(ctx context.Context, event actionpkg.AlarmEvent) {
	if t == nil {
		return
	}
	t.next.OnAlarm(ctx, event)
	if t.send == nil {
		return
	}
	if len(event.AlarmID) != 16 {
		log.Warn().Str("phone", event.Phone).Uint32("alarm_sign", event.AlarmSign).
			Msg("Skip 0x9208: 16-byte alarm ID is unavailable from 0x0200 extension items")
		return
	}
	alarmIDHex := hex.EncodeToString(event.AlarmID)
	if !t.markSeen(event.Phone + "/" + alarmIDHex) {
		return
	}

	msg := &model.Msg9208{
		IP:        t.opts.IP,
		TCPPort:   t.opts.TCPPort,
		UDPPort:   t.opts.UDPPort,
		AlarmSign: append([]byte(nil), event.AlarmID...),
		// 0x9208 的报警编号是 32 字节文本标识；用报警标识号的十六进制串保证
		// 同一报警稳定可追踪，不参与报警标识号本身的构造。
		AlarmNo: strings.ToUpper(alarmIDHex),
	}
	if err := t.send(event.Phone, msg); err != nil {
		log.Warn().Err(err).Str("phone", event.Phone).Str("alarm_id", alarmIDHex).
			Msg("Failed to send 0x9208 attachment upload command")
		return
	}
	log.Info().Str("phone", event.Phone).Str("alarm_id", alarmIDHex).
		Str("ip", msg.IP).Uint16("tcp_port", msg.TCPPort).Msg("Sent 0x9208 attachment upload command")
}

// markSeen 记录报警标识号并清理过期项。返回 false 表示窗口内已下发过。
func (t *UploadTrigger) markSeen(key string) bool {
	t.mu.Lock()
	defer t.mu.Unlock()
	now := t.now()
	for seenKey, seenAt := range t.seen {
		if now.Sub(seenAt) >= t.opts.DedupWindow {
			delete(t.seen, seenKey)
		}
	}
	if _, ok := t.seen[key]; ok {
		return false
	}
	t.seen[key] = now
	return true
}

// 编译期确认装饰器满足 Hooks 接口。
var _ actionpkg.Hooks = (*UploadTrigger)(nil)
