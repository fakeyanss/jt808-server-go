// Package contracts 定义 808 网关与管控层之间的四类边界契约。
//
// 这些接口是角色拆分后的稳定边界，不是内部实现细节：
//   - UplinkSink：网关把上行业务事件交给管控；
//   - Downlink：管控把下行命令交给网关；
//   - AuthProvider：网关向管控查询设备档案与鉴权码；
//   - MediaAllocator/MediaReader、AttachmentAllocator/AttachmentReader：
//     管控申请媒体/附件端点并读取其结果，网关不参与。
//
// standalone 模式下这些契约由内置管控的本地实现满足，不产生网络调用。
package contracts

import (
	"context"
	"io"
	"time"

	"github.com/fakeyanss/jt808-server-go/internal/protocol/model"
)

// Reliability 描述一次上行投递的确认语义。
type Reliability string

const (
	// ReliabilityBestEffort 先应答再异步投递；出口失败时允许丢弃并计数。
	ReliabilityBestEffort Reliability = "best_effort"
	// ReliabilityConfirmBeforeAck 必须拿到管控受理结果后才应答；失败由终端重传。
	ReliabilityConfirmBeforeAck Reliability = "confirm_before_ack"
)

// UplinkEvent 是网关交给管控的一条上行事件。
type UplinkEvent struct {
	EventID     string
	Phone       string
	MsgID       uint16
	Serial      uint16
	OccurredAt  time.Time
	GatewayID   string
	SessionID   string
	Transport   string
	Reliability Reliability
	Payload     any
	PayloadRaw  []byte
	Meta        map[string]any

	// Reply 仅在终端通用应答（0x0001）时非空，是给管控应答入口的归一化字段。
	Reply *TerminalReply
}

// UplinkResult 是管控对一次上行投递的受理结果。
type UplinkResult struct {
	Accepted bool
	Reply    []byte
	Reason   string
}

// UplinkSink 是网关向上的统一出口。
type UplinkSink interface {
	Deliver(ctx context.Context, event UplinkEvent) (UplinkResult, error)
}

// TerminalReply 是终端对下行命令的通用应答（0x0001）归一化结果。
//
// 匹配键是 (Phone, AnswerMsgID, AnswerSerial)：AnswerMsgID/AnswerSerial
// 来自 0x0001 报文体，指向被应答的下行消息；Phone 来自报文头。
type TerminalReply struct {
	EventID      string `json:"eventId"`
	Phone        string `json:"phone"`
	AnswerMsgID  uint16 `json:"answerMsgId"`
	AnswerSerial uint16 `json:"answerSerial"`
	// Result 为 0 表示成功；1 执行失败；2 消息有误；3 不支持。
	Result     uint8     `json:"result"`
	GatewayID  string    `json:"gatewayId,omitempty"`
	SessionID  string    `json:"sessionId,omitempty"`
	ReceivedAt time.Time `json:"receivedAt,omitempty"`
	// Source 标记来源（transport / http），仅用于日志与排查。
	Source string `json:"source,omitempty"`
}

// TerminalReplyOutcome 是一次应答处理对 outbox 的实际影响。
type TerminalReplyOutcome string

const (
	// TerminalReplyAcked 命令由 pending 推进为 acked。
	TerminalReplyAcked TerminalReplyOutcome = "acked"
	// TerminalReplyFailed 命令由 pending 推进为 failed。
	TerminalReplyFailed TerminalReplyOutcome = "failed"
	// TerminalReplyRetryScheduled 命令保持 pending 并安排了退避重投。
	TerminalReplyRetryScheduled TerminalReplyOutcome = "retry_scheduled"
	// TerminalReplyDuplicate 应答与终态一致，未改变状态、未重复计数。
	TerminalReplyDuplicate TerminalReplyOutcome = "duplicate"
	// TerminalReplyLate 应答迟到或与终态矛盾，未改变状态。
	TerminalReplyLate TerminalReplyOutcome = "late"
	// TerminalReplyUnmatched 找不到对应的 outbox 记录。
	TerminalReplyUnmatched TerminalReplyOutcome = "unmatched"
	// TerminalReplyIgnored outbox 未启用，应答不参与状态推进。
	TerminalReplyIgnored TerminalReplyOutcome = "ignored"
)

// TerminalReplyHandler 是终端应答的统一处理入口。
// 本地直连与会话回调、HTTP 事件消费方都必须走这一入口。
type TerminalReplyHandler interface {
	HandleTerminalReply(ctx context.Context, reply TerminalReply) (TerminalReplyOutcome, error)
}

// DownlinkRequest 是一次下行下发请求。
//
// IdempotencyKey 由调用方生成并保持稳定；Serial 为调用方期望使用的流水号，
// 为 0 时由网关在发送时分配（集群形态）。Message 必须已带报文头。
type DownlinkRequest struct {
	Phone          string
	SessionID      string
	IdempotencyKey string
	Serial         uint16
	Message        model.JT808Msg
}

// DownlinkResult 描述一次下发的受理结果，同时也是命令状态的快照。
//
// Submit 只填 ID、Serial、Status、Duplicated、Accepted；
// Query 额外填 Phone、MsgID、Attempts、LastError 与各时间戳，
// 供命令状态查询接口展示重投进度。
type DownlinkResult struct {
	ID         string
	Serial     uint16
	Status     string
	Duplicated bool
	Accepted   bool

	Phone     string
	MsgID     uint16
	Attempts  int
	LastError string

	CreatedAt     time.Time
	UpdatedAt     time.Time
	LastAttemptAt time.Time
	NextAttemptAt time.Time
	ExpiresAt     time.Time
}

// Downlink 是管控调用网关下发的契约。
type Downlink interface {
	// Enabled 表示实现是否提供持久化下发与命令状态查询。
	// 关闭时 Submit 直接发送且不落库，Query 返回未找到；
	// 调用方据此决定流水号归属与是否暴露状态查询接口。
	Enabled() bool
	Submit(ctx context.Context, request DownlinkRequest) (DownlinkResult, error)
	Query(ctx context.Context, id string) (DownlinkResult, error)
}

// AuthProvider 是网关向管控查询设备档案与鉴权码的只读契约。
//
// 网关不得反向写入设备档案；注册受理属于上行出口的业务结果。
type AuthProvider interface {
	Device(ctx context.Context, phone string) (*model.Device, error)
	AuthCode(ctx context.Context, phone string) (string, error)
}

// MediaEndpoint 是 1078 媒体节点的可达地址。
type MediaEndpoint struct {
	IP      string
	TCPPort uint16
	UDPPort uint16
}

// MediaLeaseRequest 描述一次媒体端点申请。
type MediaLeaseRequest struct {
	Phone   string
	Channel uint8
	Source  string
	TTL     time.Duration
}

// MediaLease 是一段时间内有效的媒体端点租约。
type MediaLease struct {
	LeaseID   string
	Phone     string
	Channel   uint8
	Endpoint  MediaEndpoint
	ExpiresAt time.Time
}

// MediaAllocator 决定媒体端点落在哪个节点，并保证粘性。
type MediaAllocator interface {
	Allocate(ctx context.Context, request MediaLeaseRequest) (MediaLease, error)
	Release(ctx context.Context, leaseID string) error
}

// MediaReader 提供媒体流与文件的只读视图。
type MediaReader interface {
	Streams() int
	Open(relativePath string) (io.ReadCloser, error)
}

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

// AttachmentReader 提供附件组装状态与文件目录的只读视图。
type AttachmentReader interface {
	Stats() (active, completed int64)
	Root() string
}
