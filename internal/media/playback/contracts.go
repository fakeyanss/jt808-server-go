// Package playback 定义播放输出抽象与媒体节点的播放/录像能力契约。
//
// 平台录像回放与终端回放流是两件事：终端回放走 0x9201 推流，
// 平台录像按 recording_segments 索引 + Range 直接读对象存储，不经过 0x9201。
package playback

import (
	"context"
	"errors"
	"fmt"
	"io"
	"time"

	"github.com/fakeyanss/jt808-server-go/internal/contracts/media"
)

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
	Endpoint  media.MediaEndpoint
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
