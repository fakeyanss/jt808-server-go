// Package attachment 定义附件节点、管控与管理面之间的附件调度与进度契约。
package attachment

import (
	"context"
	"errors"
	"fmt"
	"time"
)

// AttachmentEndpoint 是苏标附件节点的可达地址。
type AttachmentEndpoint struct {
	IP      string
	TCPPort uint16
	UDPPort uint16
}

// AttachmentLeaseRequest 描述一次附件端点申请。
type AttachmentLeaseRequest struct {
	Phone       string
	AlarmID     string
	AlarmSerial uint16
	TTL         time.Duration
}

// AttachmentLease 是一段时间内有效的附件端点租约。
type AttachmentLease struct {
	LeaseID   string
	Phone     string
	Endpoint  AttachmentEndpoint
	ExpiresAt time.Time
}

// AttachmentAllocator 决定附件上传落在哪个节点，并保证粘性。
type AttachmentAllocator interface {
	Allocate(ctx context.Context, request AttachmentLeaseRequest) (AttachmentLease, error)
	Release(ctx context.Context, leaseID string) error
}

// 附件调度失败的错误码。API 返回 503 + code + reason，禁止静默降级。
const (
	// AttachmentCodeNoNodes 表示没有配置任何附件节点。
	AttachmentCodeNoNodes = "no_attachment_node_configured"
	// AttachmentCodeNodesUnavailable 表示配置了节点但没有一个可接受新的上传。
	AttachmentCodeNodesUnavailable = "no_attachment_node_available"
)

// AttachmentUnavailableError 表示当前没有可承载附件上传的节点。
type AttachmentUnavailableError struct {
	Code   string
	Reason string
}

func (e *AttachmentUnavailableError) Error() string {
	if e == nil {
		return "attachment service is unavailable"
	}
	if e.Reason == "" {
		return e.Code
	}
	return fmt.Sprintf("%s: %s", e.Code, e.Reason)
}

// AttachmentReader 提供附件组装状态与文件目录的只读视图。
type AttachmentReader interface {
	Stats() (active, completed int64)
	Root() string
}

// ErrFTPLoginRejected 表示 FTP 一次性账号无效、已吊销或任务已终态。
//
// 附件节点用它区分“账号被拒”（530）与“control 不可达”（421/503）：
// 前者不应重试，后者必须继续重试但不得放行新登录。
var ErrFTPLoginRejected = errors.New("ftp login rejected")

// ErrAttachmentProgressNotFound 表示 control 侧没有该文件的进度记录。
//
// 数据面必须把“没有记录”和“control 不可达”区分开：前者按新文件登记，
// 后者不得阻断接收，改用本地暂存与对象存储扫描恢复。
var ErrAttachmentProgressNotFound = errors.New("attachment progress not found")

// 附件进度状态。与 attachment_files.status 同值，跨进程传输不做二次映射。
const (
	AttachmentStatusReceiving = "receiving"
	AttachmentStatusCompleted = "completed"
	AttachmentStatusFailed    = "failed"
	AttachmentStatusExpired   = "expired"
)

// AttachmentProgressKey 是附件进度的业务主键。
type AttachmentProgressKey struct {
	Source   string `json:"source"`
	Phone    string `json:"phone"`
	AlarmNo  string `json:"alarmNo"`
	FileName string `json:"fileName"`
}

// AttachmentProgressSegment 是一个已提交（或正在提交）的对象切片。
type AttachmentProgressSegment struct {
	Index  int    `json:"index"`
	Key    string `json:"key"`
	Offset int64  `json:"offset"`
	Size   int64  `json:"size"`
	MD5    string `json:"md5,omitempty"`
	State  string `json:"state"`
}

// AttachmentProgress 是附件节点向 control 上报的进度快照，也是 control 返回的恢复点。
//
// 它只是“可重建的索引”：对象存储里的切片才是事实来源。因此上报允许丢失，
// control 侧扫描 attachment/ 前缀即可按对象 key 补齐缺失记录。
type AttachmentProgress struct {
	Source      string `json:"source"`
	Phone       string `json:"phone"`
	TerminalID  string `json:"terminalId"`
	AlarmSign   string `json:"alarmSign"`
	AlarmNo     string `json:"alarmNo"`
	AlarmSerial string `json:"alarmSerial"`
	FileName    string `json:"fileName"`
	FileType    uint8  `json:"fileType"`

	TotalSize     int64 `json:"totalSize"`
	ReceivedBytes int64 `json:"receivedBytes"`
	// Watermark 是已提交对象覆盖到的连续字节前缀。
	Watermark int64 `json:"watermark"`

	// StagingPath/StagingBase 是上报节点的本地暂存位置，仅用于排查；
	// control 不得据此删除文件——边缘节点目录对中心不可见。
	StagingPath string `json:"stagingPath,omitempty"`
	StagingBase string `json:"stagingBase,omitempty"`
	// Ranges 是已落本地暂存但尚未切换成对象的覆盖区间（JSON 数组）。
	Ranges []byte `json:"ranges,omitempty"`

	Segments []AttachmentProgressSegment `json:"segments,omitempty"`
	Status   string                      `json:"status"`

	CreatedAt   time.Time `json:"createdAt"`
	UpdatedAt   time.Time `json:"updatedAt"`
	CompletedAt time.Time `json:"completedAt"`
}

// Key 返回业务主键。
func (p AttachmentProgress) Key() AttachmentProgressKey {
	return AttachmentProgressKey{Source: p.Source, Phone: p.Phone, AlarmNo: p.AlarmNo, FileName: p.FileName}
}

// AttachmentProgressSink 是数据面节点的出站进度通道。
//
// Save 是尽力而为：失败只记指标/日志，不阻塞接收；Load 用于断线重连或重启后
// 从 control 取回恢复点。control 不可达时实现应返回非 ErrAttachmentProgressNotFound
// 的错误，由调用方降级到对象存储扫描。
type AttachmentProgressSink interface {
	Save(ctx context.Context, progress AttachmentProgress) error
	Load(ctx context.Context, key AttachmentProgressKey) (*AttachmentProgress, error)
}
