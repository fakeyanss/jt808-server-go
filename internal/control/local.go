package control

import (
	"context"
	"fmt"
	"io"
	"sync"
	"time"

	"github.com/fakeyanss/jt808-server-go/internal/contracts"
	"github.com/fakeyanss/jt808-server-go/internal/mediaupload"
	"github.com/fakeyanss/jt808-server-go/internal/protocol/model"
	"github.com/fakeyanss/jt808-server-go/internal/repository"
)

// LocalUplinkHandler 是内置管控对上行事件的处理函数。
type LocalUplinkHandler func(ctx context.Context, event contracts.UplinkEvent) (contracts.UplinkResult, error)

// LocalUplinkSink 是上行出口的进程内实现。
// standalone 模式下由网关直接调用，不产生网络往返。
type LocalUplinkSink struct {
	handler LocalUplinkHandler
}

// NewLocalUplinkSink 创建进程内上行出口。
func NewLocalUplinkSink(handler LocalUplinkHandler) *LocalUplinkSink {
	return &LocalUplinkSink{handler: handler}
}

// Deliver 实现 contracts.UplinkSink。
func (s *LocalUplinkSink) Deliver(ctx context.Context, event contracts.UplinkEvent) (contracts.UplinkResult, error) {
	if s == nil || s.handler == nil {
		return contracts.UplinkResult{}, fmt.Errorf("local uplink handler is not configured")
	}
	return s.handler(ctx, event)
}

// LocalAuthProvider 是鉴权提供者的进程内实现，直接读取仓储层。
type LocalAuthProvider struct {
	devices repository.DeviceRepository
}

// NewLocalAuthProvider 创建进程内鉴权提供者。
func NewLocalAuthProvider(devices repository.DeviceRepository) *LocalAuthProvider {
	return &LocalAuthProvider{devices: devices}
}

// Device 返回设备档案。
func (p *LocalAuthProvider) Device(ctx context.Context, phone string) (*model.Device, error) {
	if p == nil || p.devices == nil {
		return nil, fmt.Errorf("device repository is not configured")
	}
	return p.devices.GetByPhone(ctx, phone)
}

// AuthCode 返回设备鉴权码。
func (p *LocalAuthProvider) AuthCode(ctx context.Context, phone string) (string, error) {
	device, err := p.Device(ctx, phone)
	if err != nil {
		return "", err
	}
	return device.AuthCode, nil
}

// StreamCounter 是媒体流的只读计数接口，便于测试替换。
type StreamCounter interface {
	Streams() int
}

// LocalMedia 是媒体读取契约的进程内实现。
type LocalMedia struct {
	streams StreamCounter
	files   *mediaupload.Store
}

// NewLocalMedia 创建进程内媒体读取器。
func NewLocalMedia(streams StreamCounter, files *mediaupload.Store) *LocalMedia {
	return &LocalMedia{streams: streams, files: files}
}

// Streams 返回当前活跃流数量。
func (m *LocalMedia) Streams() int {
	if m == nil || m.streams == nil {
		return 0
	}
	return m.streams.Streams()
}

// Open 打开 0x0801 图片文件。
func (m *LocalMedia) Open(relativePath string) (io.ReadCloser, error) {
	if m == nil || m.files == nil {
		return nil, fmt.Errorf("media file store is not configured")
	}
	return m.files.Open(relativePath)
}

// AttachmentStatsSource 是附件组装状态的只读接口。
type AttachmentStatsSource interface {
	Stats() (active, completed int64)
}

// LocalAttachment 是附件读取契约的进程内实现。
type LocalAttachment struct {
	stats AttachmentStatsSource
	root  string
}

// NewLocalAttachment 创建进程内附件读取器。
func NewLocalAttachment(stats AttachmentStatsSource, root string) *LocalAttachment {
	return &LocalAttachment{stats: stats, root: root}
}

// Stats 返回进行中与已完成的上传数量。
func (a *LocalAttachment) Stats() (active, completed int64) {
	if a == nil || a.stats == nil {
		return 0, 0
	}
	return a.stats.Stats()
}

// Root 返回附件落盘目录。
func (a *LocalAttachment) Root() string {
	if a == nil {
		return ""
	}
	return a.root
}

// StaticMediaAllocator 是 standalone 形态的媒体端点分配器：
// 所有租约返回同一个本地端点，粘性天然成立。
type StaticMediaAllocator struct {
	endpoint contracts.MediaEndpoint
	ttl      time.Duration
	mu       sync.Mutex
	leases   map[string]contracts.MediaLease
	nextID   uint64
}

// NewStaticMediaAllocator 创建固定端点的媒体分配器。
func NewStaticMediaAllocator(endpoint contracts.MediaEndpoint, ttl time.Duration) *StaticMediaAllocator {
	if ttl <= 0 {
		ttl = 10 * time.Minute
	}
	return &StaticMediaAllocator{endpoint: endpoint, ttl: ttl, leases: make(map[string]contracts.MediaLease)}
}

// Allocate 返回固定端点租约。
func (a *StaticMediaAllocator) Allocate(_ context.Context, request contracts.MediaLeaseRequest) (contracts.MediaLease, error) {
	if a.endpoint.IP == "" {
		return contracts.MediaLease{}, fmt.Errorf("media endpoint is not configured")
	}
	ttl := request.TTL
	if ttl <= 0 {
		ttl = a.ttl
	}
	a.mu.Lock()
	defer a.mu.Unlock()
	a.nextID++
	lease := contracts.MediaLease{
		LeaseID:   fmt.Sprintf("media-%d", a.nextID),
		Phone:     request.Phone,
		Channel:   request.Channel,
		Endpoint:  a.endpoint,
		ExpiresAt: time.Now().Add(ttl),
	}
	a.leases[lease.LeaseID] = lease
	return lease, nil
}

// Release 释放租约。
func (a *StaticMediaAllocator) Release(_ context.Context, leaseID string) error {
	a.mu.Lock()
	defer a.mu.Unlock()
	delete(a.leases, leaseID)
	return nil
}

// StaticAttachmentAllocator 是 standalone 形态的附件端点分配器。
type StaticAttachmentAllocator struct {
	endpoint contracts.AttachmentEndpoint
	ttl      time.Duration
	mu       sync.Mutex
	leases   map[string]contracts.AttachmentLease
	nextID   uint64
}

// NewStaticAttachmentAllocator 创建固定端点的附件分配器。
func NewStaticAttachmentAllocator(endpoint contracts.AttachmentEndpoint, ttl time.Duration) *StaticAttachmentAllocator {
	if ttl <= 0 {
		ttl = 10 * time.Minute
	}
	return &StaticAttachmentAllocator{endpoint: endpoint, ttl: ttl, leases: make(map[string]contracts.AttachmentLease)}
}

// Allocate 返回固定端点租约。
func (a *StaticAttachmentAllocator) Allocate(_ context.Context, request contracts.AttachmentLeaseRequest) (contracts.AttachmentLease, error) {
	if a.endpoint.IP == "" {
		return contracts.AttachmentLease{}, fmt.Errorf("attachment endpoint is not configured")
	}
	ttl := request.TTL
	if ttl <= 0 {
		ttl = a.ttl
	}
	a.mu.Lock()
	defer a.mu.Unlock()
	a.nextID++
	lease := contracts.AttachmentLease{
		LeaseID:   fmt.Sprintf("attachment-%d", a.nextID),
		Phone:     request.Phone,
		Endpoint:  a.endpoint,
		ExpiresAt: time.Now().Add(ttl),
	}
	a.leases[lease.LeaseID] = lease
	return lease, nil
}

// Release 释放租约。
func (a *StaticAttachmentAllocator) Release(_ context.Context, leaseID string) error {
	a.mu.Lock()
	defer a.mu.Unlock()
	delete(a.leases, leaseID)
	return nil
}
