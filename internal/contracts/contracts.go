// Package contracts 定义 808 网关与管控层之间跨进程的最小信封契约。
//
// 这些接口是角色拆分后的稳定边界，不是内部实现细节：
//   - UplinkSink：网关把上行业务事件交给管控；
//   - Downlink：管控把下行命令交给网关；
//   - AuthProvider：网关向管控查询设备档案与鉴权码。
//
// 媒体、附件与对象存储等按领域拆到 internal/contracts/media、
// internal/contracts/attachment、internal/storage 与 internal/media/playback。
//
// standalone 模式下这些契约由内置管控的本地实现满足，不产生网络调用。
package contracts

import (
	"context"
	"errors"
	"fmt"
	"time"

	"github.com/fakeyanss/jt808-server-go/internal/domain"
	"github.com/fakeyanss/jt808-server-go/internal/protocol/model"
)

// Reliability 描述一次上行投递的确认语义。
type Reliability string

const (
	// ReliabilityBestEffort 先应答再异步投递；出口失败时允许丢弃并计数。
	ReliabilityBestEffort Reliability = "best_effort"
	// ReliabilityAtLeastOnce 与 confirm_before_ack 同义：先处理成功再应答。
	ReliabilityAtLeastOnce Reliability = "at_least_once"
	// ReliabilityConfirmBeforeAck 必须拿到管控受理结果后才应答；失败由终端重传。
	ReliabilityConfirmBeforeAck Reliability = "confirm_before_ack"
)

// UplinkEvent 是网关交给管控的一条上行事件。
//
// Body 是已解码的原始消息体，管控用 DecodeMessageBody 单次解码，避免与网关
// 形成两套解码实现；Payload/PayloadRaw 只作为诊断快照保留。
type UplinkEvent struct {
	EventID    string
	Phone      string
	MsgID      uint16
	RawMsgID   uint16
	Serial     uint16
	OccurredAt time.Time
	GatewayID  string
	// InstanceID 是网关进程实例标识，与 GatewayID 一起确定会话归属。
	InstanceID  string
	SessionID   string
	Transport   string
	Reliability Reliability
	// Generation 是会话 claim 得到的围栏版本；鉴权前为 0，不做归属校验。
	Generation uint64

	// Event 为事件类型；空值表示普通终端报文，session.closed 表示会话结束。
	Event string
	// Reason 仅 session.closed 使用：logout / transport_closed。
	Reason string

	// 协议头信息，供管控重建报文头并解码消息体。
	ProtocolVersion     string
	HeaderAttr          uint16
	FragmentReassembled bool
	FragmentTotal       uint16

	// ReplyMsgID/ReplyResult 是网关已发给终端的应答结果（0x8100/0x8001）。
	// 管控侧据此派发与网关一致的注册/鉴权钩子事件。
	ReplyMsgID  uint16
	ReplyResult uint8

	Body       []byte
	Payload    any
	PayloadRaw []byte
	Meta       map[string]any

	// Reply 仅在终端通用应答（0x0001）时非空，是给管控应答入口的归一化字段。
	Reply *TerminalReply
}

// UplinkResult 是管控对一次上行投递的受理结果。
type UplinkResult struct {
	Accepted bool
	Reply    []byte
	Reason   string
	// Generation 是 0x0102 claim 后的会话版本，网关据此绑定本地会话。
	Generation uint64
	// Fenced 表示事件 generation 已落后于目录，网关应关闭本地会话。
	Fenced bool
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
	Generation uint64    `json:"generation,omitempty"`
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
// 为 0 时由 Downlink 实现分配（outbox 启用），off 模式则留给网关在发送时分配。
// Message 必须已带报文头。
type DownlinkRequest struct {
	Phone          string
	SessionID      string
	IdempotencyKey string
	Serial         uint16
	Message        model.JT808Msg
	// ExpectResponseMsgID 非零表示这次下发要同步等待终端的查询应答
	// （0x0104/0x0107/0x0201/0x0608），回执里带回该应答的消息体。
	ExpectResponseMsgID uint16
	// ResponseTimeout 是等待查询应答的上限；为零时使用网关默认值。
	ResponseTimeout time.Duration
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

// DownlinkReceipt 是网关对一次下发请求的即时回执。
//
// Status 与 DownlinkResult.Status 语义对齐：
//   - accepted：已写入连接，结果稍后由应答事件推进；
//   - acked/failed：同步等到终端应答；
//   - timeout：同步等待超时，命令已发出；
//   - offline：设备不在线，未发送；
//   - rejected：会话在发送前失效，可重试。
type DownlinkReceipt struct {
	ID         string
	Serial     uint16
	Status     string
	Duplicated bool
	Result     *uint8
	Reason     string
	// ResponseMsgID/ResponseBody 仅在请求声明 ExpectResponseMsgID 且拿到应答时填充。
	ResponseMsgID  uint16
	ResponseBody   []byte
	ResponseHeader *model.MsgHeader
}

// 下行回执状态。与 outbox 的 acked/failed 保持同名，避免两套词汇。
const (
	DownlinkStatusAccepted = "accepted"
	DownlinkStatusAcked    = "acked"
	DownlinkStatusFailed   = "failed"
	DownlinkStatusTimeout  = "timeout"
	DownlinkStatusOffline  = "offline"
	DownlinkStatusRejected = "rejected"
)

// DownlinkSender 是控制侧把一条已编码的下行请求交给网关的传输契约。
//
// 本地直连与 HTTP 网关都必须实现同一接口；返回 error 仅表示传输失败，
// offline/rejected 等业务结果放在 Receipt.Status 中，由 outbox 决定租约收尾。
// 传输错误应尽量包装成 *DownlinkError，让 outbox 区分确定性失败与可重试失败。
type DownlinkSender interface {
	Send(ctx context.Context, request DownlinkRequest) (DownlinkReceipt, error)
}

// DownlinkErrorKind 是下发传输失败的分类。
type DownlinkErrorKind string

const (
	// DownlinkErrorTransient 表示网络抖动、超时、429 或 5xx，重投可能成功。
	DownlinkErrorTransient DownlinkErrorKind = "transient"
	// DownlinkErrorPermanent 表示 400/401/403/413 等确定性拒绝，重投必然同样失败。
	DownlinkErrorPermanent DownlinkErrorKind = "permanent"
)

// DownlinkError 是 DownlinkSender 的类型化传输错误。
//
// StatusCode/Code 用于日志与错误信息，RetryAfter 来自网关的 Retry-After 头，
// 供 outbox 在 429 时尊重对方的限流窗口而不是按本地退避硬打。
type DownlinkError struct {
	Kind       DownlinkErrorKind
	StatusCode int
	Code       string
	RetryAfter time.Duration
	Message    string
}

func (e *DownlinkError) Error() string {
	if e == nil {
		return ""
	}
	message := e.Message
	if message == "" {
		message = "downlink request failed"
	}
	if e.StatusCode != 0 {
		return fmt.Sprintf("%s (status=%d code=%s)", message, e.StatusCode, e.Code)
	}
	if e.Code != "" {
		return fmt.Sprintf("%s (code=%s)", message, e.Code)
	}
	return message
}

// IsPermanent 表示该错误是否为确定性失败，调用方应直接终结而不是退避重投。
func (e *DownlinkError) IsPermanent() bool {
	return e != nil && e.Kind == DownlinkErrorPermanent
}

// ClassifyDownlinkError 提取类型化错误；未包装的传输错误按可重试处理。
func ClassifyDownlinkError(err error) *DownlinkError {
	var downlinkErr *DownlinkError
	if errors.As(err, &downlinkErr) {
		if downlinkErr.Kind == "" {
			downlinkErr.Kind = DownlinkErrorTransient
		}
		return downlinkErr
	}
	return &DownlinkError{Kind: DownlinkErrorTransient, Message: err.Error()}
}

// ClassifyDownlinkStatus 把网关 HTTP 状态码映射为错误分类。
// 400/401/403/413 是确定性拒绝；408/429/5xx 与其余传输失败按可重试处理。
func ClassifyDownlinkStatus(statusCode int) DownlinkErrorKind {
	switch statusCode {
	case 400, 401, 403, 413:
		return DownlinkErrorPermanent
	default:
		return DownlinkErrorTransient
	}
}

// AuthProvider 是网关向管控查询设备档案与鉴权码的只读契约。
//
// 网关不得反向写入设备档案；注册受理属于上行出口的业务结果。
type AuthProvider interface {
	Device(ctx context.Context, phone string) (*domain.Device, error)
	AuthCode(ctx context.Context, phone string) (string, error)
}
