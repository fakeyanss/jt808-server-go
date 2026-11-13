package control

import (
	"context"
	"encoding/hex"
	"strings"
	"time"

	"github.com/rs/zerolog/log"

	actionpkg "github.com/fakeyanss/jt808-server-go/internal/action"
	"github.com/fakeyanss/jt808-server-go/internal/contracts"
	"github.com/fakeyanss/jt808-server-go/internal/coordination"
	"github.com/fakeyanss/jt808-server-go/internal/protocol/model"
)

// AutoUploadTrigger 在控制面收到带报警标识号的 0x0200 时自动下发 0x9208。
//
// 与进程内 UploadTrigger 的差异：附件节点地址来自 AttachmentAllocator（HTTP 节点状态），
// 下行经 DownlinkSender 走网关，因此 gateway 与 control 可以分进程部署。
// 没有可用附件节点时只记录明确告警，不影响报警入库。
type AutoUploadTrigger struct {
	next       actionpkg.Hooks
	allocator  contracts.AttachmentAllocator
	sender     contracts.DownlinkSender
	dedupStore coordination.Store
	dedupTTL   time.Duration
}

// NewAutoUploadTrigger 创建自动下发装饰器。
// dedupStore 为 nil 时退化为进程内去重；多副本必须传入共享 Store。
func NewAutoUploadTrigger(next actionpkg.Hooks, allocator contracts.AttachmentAllocator, sender contracts.DownlinkSender,
	dedupStore coordination.Store, dedupTTL time.Duration) *AutoUploadTrigger {
	if next == nil {
		next = actionpkg.Noop{}
	}
	if dedupStore == nil {
		dedupStore = coordination.NewMemoryStore()
	}
	if dedupTTL <= 0 {
		dedupTTL = 30 * time.Minute
	}
	return &AutoUploadTrigger{
		next:       next,
		allocator:  allocator,
		sender:     sender,
		dedupStore: dedupStore,
		dedupTTL:   dedupTTL,
	}
}

func (t *AutoUploadTrigger) OnRegister(ctx context.Context, event actionpkg.RegisterEvent) {
	t.next.OnRegister(ctx, event)
}

func (t *AutoUploadTrigger) OnAuth(ctx context.Context, event actionpkg.AuthEvent) {
	t.next.OnAuth(ctx, event)
}

func (t *AutoUploadTrigger) OnLocation(ctx context.Context, event actionpkg.LocationEvent) {
	t.next.OnLocation(ctx, event)
}

func (t *AutoUploadTrigger) OnDisconnect(ctx context.Context, event actionpkg.DisconnectEvent) {
	t.next.OnDisconnect(ctx, event)
}

// OnAlarm 只在同一报警标识号首次上报时触发一次。
func (t *AutoUploadTrigger) OnAlarm(ctx context.Context, event actionpkg.AlarmEvent) {
	t.next.OnAlarm(ctx, event)
	if t == nil || t.allocator == nil || t.sender == nil {
		return
	}
	if len(event.AlarmID) != 16 {
		// 没有报警标识号时无法构造合法的 0x9208；这是协议数据缺失，不是配置问题。
		log.Warn().Str("phone", event.Phone).Uint32("alarm_sign", event.AlarmSign).
			Msg("Skip automatic 0x9208: 16-byte alarm ID is unavailable from 0x0200 extension items")
		return
	}
	alarmIDHex := strings.ToUpper(hex.EncodeToString(event.AlarmID))
	first, err := t.dedupStore.MarkOnce(ctx, "auto-9208:"+event.Phone+":"+alarmIDHex, t.dedupTTL)
	if err != nil {
		// 去重存储不可用时按 at-least-once 处理：继续下发，重复命令由 outbox
		// 幂等键 (phone + idempotency_key) 兜底，避免漏掉报警附件上传。
		log.Warn().Err(err).Str("phone", event.Phone).Str("alarm_id", alarmIDHex).
			Msg("Automatic 0x9208 dedup unavailable; continue with at-least-once semantics")
	} else if !first {
		return
	}
	lease, err := t.allocator.Allocate(ctx, contracts.AttachmentLeaseRequest{
		Phone: event.Phone, AlarmID: alarmIDHex,
	})
	if err != nil {
		log.Warn().Err(err).Str("phone", event.Phone).Str("alarm_id", alarmIDHex).
			Msg("Automatic 0x9208 skipped: no attachment node is available")
		return
	}
	msg := &model.Msg9208{
		Header: &model.MsgHeader{
			MsgID:       0x9208,
			Attr:        &model.MsgBodyAttr{VersionDesc: model.Version2013},
			PhoneNumber: event.Phone,
		},
		IP:        lease.Endpoint.IP,
		TCPPort:   lease.Endpoint.TCPPort,
		UDPPort:   lease.Endpoint.UDPPort,
		AlarmSign: append([]byte(nil), event.AlarmID...),
		AlarmNo:   alarmIDHex,
	}
	if _, err := t.sender.Send(ctx, contracts.DownlinkRequest{
		Phone:          event.Phone,
		IdempotencyKey: "auto-9208-" + event.Phone + "-" + alarmIDHex,
		Message:        msg,
	}); err != nil {
		log.Warn().Err(err).Str("phone", event.Phone).Str("alarm_id", alarmIDHex).
			Msg("Fail to send automatic 0x9208 attachment upload command")
		return
	}
	log.Info().Str("phone", event.Phone).Str("alarm_id", alarmIDHex).
		Str("ip", lease.Endpoint.IP).Uint16("tcp_port", lease.Endpoint.TCPPort).
		Uint16("udp_port", lease.Endpoint.UDPPort).
		Msg("Sent automatic 0x9208 attachment upload command via downlink")
}

var _ actionpkg.Hooks = (*AutoUploadTrigger)(nil)
