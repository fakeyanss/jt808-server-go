package control

import (
	"bytes"
	"context"
	"fmt"
	"net/http"
	"path/filepath"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/pkg/errors"
	"github.com/rs/zerolog/log"

	actionpkg "github.com/fakeyanss/jt808-server-go/internal/action"
	"github.com/fakeyanss/jt808-server-go/internal/contracts"
	"github.com/fakeyanss/jt808-server-go/internal/mediaupload"
	"github.com/fakeyanss/jt808-server-go/internal/protocol"
	"github.com/fakeyanss/jt808-server-go/internal/protocol/model"
	"github.com/fakeyanss/jt808-server-go/internal/repository"
	"github.com/fakeyanss/jt808-server-go/internal/storage"
	"github.com/fakeyanss/jt808-server-go/internal/uplink"
)

// BusinessOptions 是管控业务执行器的构造参数。
type BusinessOptions struct {
	ProcessorDeps protocol.ProcessorDeps
	// Devices 用于 409 unknown_device 判定；为空时跳过预检。
	Devices repository.DeviceRepository
	// Sessions 是会话归属目录；为空时不做 claim/fencing（standalone 或未启用）。
	Sessions repository.SessionDirectoryRepository
	// Files 是媒体文件元数据仓储。
	Files repository.MediaFileRepository
	// Blob 是 sharedVolume 文件后端；为空时 0x0801 回退到本地媒体目录。
	Blob contracts.BlobStore
	// MediaUpload 是本地媒体目录，仅 Blob 为空时使用。
	MediaUpload *mediaupload.Store
	// Hooks 是控制面识别到报警/位置时的动作出口；autoUpload 依赖它在控制面触发 0x9208。
	Hooks actionpkg.Hooks
	// OnUnknownDevice 在丢弃事件时回调（指标/日志）。
	OnUnknownDevice func(phone string)
	// OnMediaTypeRejected 在 0x0801 携带非图片类型时回调。
	OnMediaTypeRejected func(phone string, mediaType uint8)
	// OnStorageCommitFailed 在文件已写、状态标记失败时回调。
	OnStorageCommitFailed func(phone string, mediaID uint32)
}

// BusinessExecutor 是管控侧的上行事件消费者。
//
// 它复用 protocol 的 handler 实现：输入从活连接换成“事件 + 原始消息体”，
// 并按手机号串行处理；多副本下顺序不保证，但字段所有权隔离与 409 有界重投
// 保证最终状态一致（0x0100 与 0x0102 互不覆盖对方字段）。
type BusinessExecutor struct {
	opts BusinessOptions
	proc *protocol.JT808MsgProcessor

	mu       sync.Mutex
	mailbox  map[string]*phoneMailbox
	lastSeen time.Time
}

type phoneMailbox struct {
	mu   sync.Mutex
	used time.Time
}

// NewBusinessExecutor 创建管控业务执行器。
func NewBusinessExecutor(opts BusinessOptions) *BusinessExecutor {
	processorDeps := opts.ProcessorDeps
	processorDeps.UplinkSink = nil
	hooks := opts.Hooks
	if hooks == nil {
		hooks = actionpkg.Noop{}
	}
	return &BusinessExecutor{
		opts:    opts,
		proc:    protocol.NewJT808MsgProcessor(hooks, protocol.WithProcessorDeps(processorDeps)),
		mailbox: map[string]*phoneMailbox{},
	}
}

// ConsumeEvent 实现 uplink.EventConsumer。
func (b *BusinessExecutor) ConsumeEvent(ctx context.Context, event contracts.UplinkEvent) (contracts.UplinkResult, error) {
	owner, ok := protocol.OwnershipOf(event.MsgID)
	if !ok || owner == protocol.OwnerGateway {
		// 网关归属的消息不应投到这里；幂等接受，不制造重投。
		return contracts.UplinkResult{Accepted: true}, nil
	}

	header := headerFromEvent(event)
	incoming, err := protocol.DecodeMessageBody(event.MsgID, header, event.Body)
	if err != nil {
		return contracts.UplinkResult{}, &uplink.StatusError{
			Status:  http.StatusBadRequest,
			Code:    "invalid_body",
			Message: fmt.Sprintf("decode 0x%04X: %v", event.MsgID, err),
		}
	}
	data := b.proc.NewProcessData(event.MsgID)
	if data == nil {
		data = &model.ProcessData{}
	}
	data.Incoming = incoming

	eventCtx := protocol.UplinkEventContext{EventID: event.EventID, SessionID: event.SessionID, Phone: event.Phone}
	ctx = context.WithValue(ctx, protocol.UplinkEventCtxKey{}, eventCtx)

	unlock := b.lockPhone(event.Phone)
	defer unlock()

	if b.needsKnownDevice(event.MsgID) && b.opts.Devices != nil {
		if _, err := b.opts.Devices.GetByPhone(ctx, event.Phone); err != nil {
			if errors.Is(err, repository.ErrDeviceNotFound) {
				if b.opts.OnUnknownDevice != nil {
					b.opts.OnUnknownDevice(event.Phone)
				}
				log.Warn().Str("phone", event.Phone).Str("event_id", event.EventID).
					Str("msg_id", fmt.Sprintf("0x%04X", event.MsgID)).
					Msg("Uplink event rejected: device is not registered")
				return contracts.UplinkResult{}, &uplink.StatusError{
					Status:  http.StatusConflict,
					Code:    "unknown_device",
					Message: "device is not registered",
				}
			}
			return contracts.UplinkResult{}, err
		}
	}

	if event.MsgID == 0x0801 && b.opts.Blob != nil {
		return b.ingestMedia(ctx, event, incoming)
	}

	generation, err := b.claimSession(ctx, event, incoming)
	if err != nil {
		return contracts.UplinkResult{}, err
	}
	if err := b.proc.ApplyControlBusiness(ctx, event.MsgID, data); err != nil {
		if errors.Is(err, repository.ErrDeviceNotFound) {
			return contracts.UplinkResult{}, &uplink.StatusError{
				Status:  http.StatusConflict,
				Code:    "unknown_device",
				Message: "device is not registered",
			}
		}
		return contracts.UplinkResult{}, err
	}
	return contracts.UplinkResult{Accepted: true, Generation: generation}, nil
}

// claimSession 在 0x0102 业务路径内为会话建立归属。
//
// 先校验鉴权码再 claim：鉴权不通过时不建立目录，避免伪造报文覆盖归属。
// claim 幂等，重复的 0x0102 返回同一 generation。
func (b *BusinessExecutor) claimSession(ctx context.Context, event contracts.UplinkEvent, incoming model.JT808Msg) (uint64, error) {
	if b.opts.Sessions == nil || event.MsgID != 0x0102 {
		return 0, nil
	}
	in, ok := incoming.(*model.Msg0102)
	if !ok || in == nil {
		return 0, nil
	}
	if b.opts.Devices == nil {
		return 0, fmt.Errorf("session claim requires device repository")
	}
	device, err := b.opts.Devices.GetByPhone(ctx, event.Phone)
	if errors.Is(err, repository.ErrDeviceNotFound) {
		return 0, &uplink.StatusError{
			Status:  http.StatusConflict,
			Code:    "unknown_device",
			Message: "device is not registered",
		}
	}
	if err != nil {
		return 0, err
	}
	if in.AuthCode != device.AuthCode {
		return 0, &uplink.StatusError{
			Status:  http.StatusForbidden,
			Code:    "auth_rejected",
			Message: "authentication code does not match control state",
		}
	}
	owner := repository.SessionOwner{
		GatewayID:  event.GatewayID,
		InstanceID: event.InstanceID,
		SessionID:  event.SessionID,
	}
	return b.opts.Sessions.Claim(ctx, event.Phone, owner)
}

// ingestMedia 按“写文件 → 写库 → 标记已提交”的顺序处理 0x0801。
// 任一步失败都不返回成功，让网关不应答、终端重传；文件写入是幂等覆盖。
func (b *BusinessExecutor) ingestMedia(ctx context.Context, event contracts.UplinkEvent, incoming model.JT808Msg) (contracts.UplinkResult, error) {
	upload, ok := incoming.(*model.Msg0801)
	if !ok || upload == nil {
		return contracts.UplinkResult{}, &uplink.StatusError{
			Status: http.StatusBadRequest, Code: "invalid_body", Message: "expect 0x0801 body",
		}
	}
	if upload.MultiMediaType != 0 {
		// 音频/视频/保留值：按协议应答终止重传，但不落库、不静默。
		if b.opts.OnMediaTypeRejected != nil {
			b.opts.OnMediaTypeRejected(event.Phone, upload.MultiMediaType)
		}
		log.Warn().Str("phone", event.Phone).Uint32("media_id", upload.MultiMediaID).
			Uint8("media_type", upload.MultiMediaType).
			Msg("0x0801 carries non-image payload; acknowledged but not stored")
		return contracts.UplinkResult{Accepted: true}, nil
	}

	key, err := mediaupload.MediaKey(upload)
	if err != nil {
		return contracts.UplinkResult{}, err
	}
	// 对象存储按来源分区：0x0801 图片与 1078 切片、附件分开前缀，便于按来源清理。
	key = storage.MediaUploads + key
	info, err := b.opts.Blob.Put(ctx, key, bytes.NewReader(upload.FragmentData), contracts.BlobOptions{ContentType: "image/jpeg"})
	if err != nil {
		return contracts.UplinkResult{}, fmt.Errorf("put media %s: %w", key, err)
	}
	info, err = b.opts.Blob.Promote(ctx, info)
	if err != nil {
		return contracts.UplinkResult{}, fmt.Errorf("promote media %s: %w", key, err)
	}
	file := mediaupload.Metadata(upload, info.Key, info.Size, info.MD5)
	if err := b.opts.Files.Save(ctx, file); err != nil {
		// 文件已在共享卷，但没有业务行：保持 pending，交给巡检在无引用时删除。
		return contracts.UplinkResult{}, fmt.Errorf("save media metadata %s: %w", key, err)
	}
	if err := b.opts.Blob.MarkCommitted(ctx, info.Key); err != nil {
		if b.opts.OnStorageCommitFailed != nil {
			b.opts.OnStorageCommitFailed(event.Phone, upload.MultiMediaID)
		}
		log.Error().Err(err).Str("phone", event.Phone).Str("key", info.Key).
			Msg("Media file committed but state marker failed; metadata is authoritative")
	}
	return contracts.UplinkResult{Accepted: true}, nil
}

// SweepPending 清理超过 olderThan 仍为 pending 且无元数据引用的媒体对象。
// 范围限定在媒体上传前缀内，不触碰附件等其它来源。
func (b *BusinessExecutor) SweepPending(ctx context.Context, olderThan time.Duration) {
	sweeper, ok := b.opts.Blob.(interface {
		SweepPendingPrefix(context.Context, string, time.Duration, func(string) bool)
	})
	if !ok {
		return
	}
	sweeper.SweepPendingPrefix(ctx, storage.MediaUploads, olderThan, func(key string) bool {
		return mediaReferenced(ctx, b.opts.Files, key)
	})
}

// needsKnownDevice 判断是否需要先确认设备档案存在。
func (b *BusinessExecutor) needsKnownDevice(msgID uint16) bool {
	return msgID != 0x0100 && msgID != 0x0102
}

func (b *BusinessExecutor) lockPhone(phone string) func() {
	b.mu.Lock()
	box, ok := b.mailbox[phone]
	if !ok {
		box = &phoneMailbox{}
		b.mailbox[phone] = box
	}
	b.lastSeen = time.Now()
	b.mu.Unlock()
	box.mu.Lock()
	return func() {
		box.mu.Unlock()
		b.mu.Lock()
		box.used = time.Now()
		b.mu.Unlock()
	}
}

// headerFromEvent 用事件信封重建协议头，不做任何改写。
func headerFromEvent(event contracts.UplinkEvent) *model.MsgHeader {
	version := model.Version2013
	switch event.ProtocolVersion {
	case "2019":
		version = model.Version2019
	case "2013", "":
		version = model.Version2013
	case "2011":
		version = model.Version2011
	}
	attr := &model.MsgBodyAttr{VersionDesc: version, BodyLength: uint16(len(event.Body))}
	return &model.MsgHeader{
		MsgID:           event.MsgID,
		Attr:            attr,
		PhoneNumber:     event.Phone,
		SerialNumber:    event.Serial,
		ProtocolVersion: 0,
	}
}

// mediaReferenced 回查 pending 对象是否已被媒体元数据引用。
func mediaReferenced(ctx context.Context, files repository.MediaFileRepository, key string) bool {
	if files == nil {
		return false
	}
	relative := strings.TrimPrefix(filepath.ToSlash(key), storage.MediaUploads)
	parts := strings.Split(relative, "/")
	if len(parts) < 2 {
		return false
	}
	phone := parts[0]
	base := strings.TrimSuffix(filepath.Base(relative), filepath.Ext(relative))
	mediaID, err := strconv.ParseUint(base, 10, 32)
	if err != nil {
		return false
	}
	_, err = files.Get(ctx, phone, uint32(mediaID))
	return err == nil
}
