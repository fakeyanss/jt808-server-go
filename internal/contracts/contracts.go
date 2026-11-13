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
	"errors"
	"fmt"
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

	// 协议头信息，供管控重建报文头并解码消息体。
	ProtocolVersion     string
	HeaderAttr          uint16
	FragmentReassembled bool
	FragmentTotal       uint16

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
	Device(ctx context.Context, phone string) (*model.Device, error)
	AuthCode(ctx context.Context, phone string) (string, error)
}

// BlobState 是对象在“提级完成、业务元数据落库”之间的状态。
type BlobState string

const (
	// BlobStatePending 对象已可见但业务元数据尚未落库，只能等待对账或过期。
	BlobStatePending BlobState = "pending"
	// BlobStateCommitted 业务元数据已落库，对象可被引用。
	BlobStateCommitted BlobState = "committed"
	// BlobStateMissing 业务元数据存在但对象丢失，保留引用依据供排查。
	BlobStateMissing BlobState = "missing"
)

// BlobInfo 描述一次暂存或提级后的对象。
type BlobInfo struct {
	Key        string
	StagingKey string
	Size       int64
	MD5        string
	ETag       string
	State      BlobState
}

// BlobOptions 是写入对象的可选属性。
type BlobOptions struct {
	ContentType string
	MD5         string
	Metadata    map[string]string
}

// BlobStore 是媒体/附件文件的统一写入契约。
//
// Put 只写暂存区，不使最终 key 可见；Promote 负责原子提级；
// MarkCommitted 只表达业务引用状态，不参与提级。三者分开，
// 避免“对象已可见”与“业务已提交”两个事实被混为一个 Commit。
type BlobStore interface {
	Put(ctx context.Context, key string, r io.Reader, opts BlobOptions) (BlobInfo, error)
	Promote(ctx context.Context, info BlobInfo) (BlobInfo, error)
	MarkCommitted(ctx context.Context, key string) error
	Open(ctx context.Context, key string) (io.ReadCloser, error)
	// OpenRange 读取 [offset, offset+length) 区间；length<=0 表示读到对象末尾。
	//
	// 这是录像回放的必需能力，不是可选优化：平台录像按片段索引 seek，
	// 退化成“读全量再丢弃前缀”会在长录像上放大成倍带宽与内存。
	OpenRange(ctx context.Context, key string, offset, length int64) (io.ReadCloser, error)
	Delete(ctx context.Context, key string) error
	Stat(ctx context.Context, key string) (BlobInfo, error)
}

// BlobObject 是列举结果中的一个对象。
type BlobObject struct {
	Key          string
	Size         int64
	LastModified time.Time
}

// BlobLister 是可选能力：能列举指定前缀下的对象。
//
// control 依赖它从 media/segments/ 的对象 key 重建切片索引；
// 不要求所有 BlobStore 实现，测试替身缺失时扫描侧会显式跳过。
type BlobLister interface {
	ListObjects(ctx context.Context, prefix string) ([]BlobObject, error)
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

// 媒体调度失败的错误码。调用方据此返回稳定的结构化错误，而不是空泛的 503。
const (
	// MediaCodeNoNodes 表示没有配置任何媒体节点。
	MediaCodeNoNodes = "no_media_node_configured"
	// MediaCodeNodesUnavailable 表示配置了节点但没有一个可接受新会话。
	MediaCodeNodesUnavailable = "no_media_node_available"
)

// MediaUnavailableError 表示当前没有可用于承载播放会话的媒体节点。
//
// 这是稳定契约错误：API 层返回 503 + code + message，禁止把“起了但什么都不通”
// 伪装成成功。
type MediaUnavailableError struct {
	Code   string
	Reason string
}

func (e *MediaUnavailableError) Error() string {
	if e == nil {
		return "media is unavailable"
	}
	if e.Reason == "" {
		return e.Code
	}
	return fmt.Sprintf("%s: %s", e.Code, e.Reason)
}

// MediaReader 提供媒体流与文件的只读视图。
type MediaReader interface {
	Streams() int
	Open(relativePath string) (io.ReadCloser, error)
}

// PlaybackSessionRequest 是一次播放会话申请。
type PlaybackSessionRequest struct {
	Phone   string
	Channel uint8
	// Kind 为 live 或 terminal_playback；两者数据面相同，只影响下行信令。
	Kind string
	TTL  time.Duration
}

// PlaybackSession 是播放会话与终端应推流的端点。
type PlaybackSession struct {
	ID        string
	Phone     string
	Channel   uint8
	Kind      string
	Endpoint  MediaEndpoint
	ExpiresAt time.Time
	FLVPath   string
	// Output 是媒体节点的播放出口模式（flv/rtmp/both）。
	Output string
	// PlaybackURL 是媒体节点返回的播放地址；管控层只透传，不按模式硬拼。
	PlaybackURL string
	// Endpoints 是全部可用播放/发布端点。
	Endpoints []PlaybackEndpoint
	// Reused 表示本次申请复用了已有分配，调用方不得重复下发 0x9101/0x9201，
	// 否则会让终端重新协商并可能把已有流顶掉。
	Reused bool
}

// PlaybackEndpoint 是一个可播放/可发布端点。
type PlaybackEndpoint struct {
	Protocol string
	URL      string
}

// PlaybackLibrary 是媒体节点的播放与录像能力。
//
// 平台录像回放与终端回放流是两件事：终端回放走 0x9201 推流，
// 平台录像按 recording_segments 索引 + Range 直接读对象存储，不经过 0x9201。
type PlaybackLibrary interface {
	PlaybackSessionManager
	// RecordingCoverage 返回请求区间内的可用/缺失区间，始终是成功语义；
	// 只有参数非法（时间倒置、通道不存在）才返回 error。
	RecordingCoverage(ctx context.Context, phone string, channel uint8, start, end time.Time) (RecordingCoverage, error)
	// OpenRecordingSegment 打开一段平台录像；offset/length 用于 Range 读取。
	// 返回读取器与切片总长度。
	OpenRecordingSegment(ctx context.Context, phone, segmentID string, offset, length int64) (io.ReadCloser, int64, error)
}

// TimeRange 是闭开区间 [Start, End)。
type TimeRange struct {
	Start time.Time `json:"start"`
	End   time.Time `json:"end"`
}

// RecordingSegmentInfo 是平台录像切片索引。
type RecordingSegmentInfo struct {
	ID        string    `json:"id"`
	Phone     string    `json:"phone"`
	Channel   uint8     `json:"channel"`
	Start     time.Time `json:"start"`
	End       time.Time `json:"end"`
	Size      int64     `json:"size"`
	ObjectKey string    `json:"objectKey,omitempty"`
}

// RecordingCoverage 是平台录像的覆盖查询结果。
//
// 不完整覆盖不是错误：返回 200 + 结构化区间，调用方按数据结构判断，
// 不需要特判 HTTP 409。
type RecordingCoverage struct {
	Requested TimeRange              `json:"requested"`
	Available []TimeRange            `json:"available"`
	Missing   []TimeRange            `json:"missing"`
	Playable  bool                   `json:"playable"`
	Segments  []RecordingSegmentInfo `json:"segments"`
}

// ErrRecordingNotFound 表示切片不存在或不属于该终端。
var ErrRecordingNotFound = errors.New("recording segment not found")

// ErrRangeNotSatisfiable 表示 Range 起点超出切片长度，对应 416。
var ErrRangeNotSatisfiable = errors.New("range not satisfiable")

// PlaybackSessionConflictError 表示同一 (phone, channel) 已存在会话。
//
// 这是一个稳定契约错误：调用方据此返回结构化冲突，不得静默顶替。
type PlaybackSessionConflictError struct {
	Existing PlaybackSession
}

func (e *PlaybackSessionConflictError) Code() string { return "session_conflict" }

func (e *PlaybackSessionConflictError) Error() string {
	return fmt.Sprintf("playback session conflict for %s/%d: existing session %s (%s) expires at %s",
		e.Existing.Phone, e.Existing.Channel, e.Existing.ID, e.Existing.Kind,
		e.Existing.ExpiresAt.Format(time.RFC3339))
}

// PlaybackSessionManager 是管控申请/释放媒体播放会话的契约。
type PlaybackSessionManager interface {
	CreatePlaybackSession(ctx context.Context, request PlaybackSessionRequest) (PlaybackSession, error)
	ReleasePlaybackSession(ctx context.Context, sessionID string) error
	// PlaybackSession 返回会话快照，found=false 表示会话已释放或不存在。
	// 停机路径需要在释放前拿到手机号、通道号与会话类型，用于选择 0x9102/0x9202。
	PlaybackSession(ctx context.Context, sessionID string) (PlaybackSession, bool)
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
