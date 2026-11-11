package media

import (
	"fmt"
	"time"

	"github.com/fakeyanss/jt808-server-go/internal/media/playback"
)

// StreamKind 标识播放会话的业务类型。
//
// 数据面不区分实时与回放：1078 的 RTP 负载头没有流类型字段，两者复用同一
// RTP 封装（§5.6.3）。kind 只用于 API 展示与下行信令构造，不参与分流。
type StreamKind string

const (
	// StreamKindLive 是实时音视频（0x9101/0x9102）。
	StreamKindLive StreamKind = "live"
	// StreamKindTerminalPlayback 是终端本地录像回放（0x9201/0x9202）。
	StreamKindTerminalPlayback StreamKind = "terminal_playback"
)

// NormalizeStreamKind 校验并归一化播放类型；空值按实时处理。
func NormalizeStreamKind(kind StreamKind) (StreamKind, error) {
	switch kind {
	case "", StreamKindLive:
		return StreamKindLive, nil
	case StreamKindTerminalPlayback:
		return StreamKindTerminalPlayback, nil
	default:
		return "", fmt.Errorf("kind must be %s or %s, got %q", StreamKindLive, StreamKindTerminalPlayback, kind)
	}
}

// SessionRequest 是一次显式播放会话申请。
type SessionRequest struct {
	Phone   string
	Channel uint8
	Kind    StreamKind
	TTL     time.Duration
}

// SessionInfo 是播放会话的只读快照。
type SessionInfo struct {
	ID        string     `json:"sessionId"`
	Phone     string     `json:"phone"`
	Channel   uint8      `json:"channel"`
	Kind      StreamKind `json:"kind"`
	ExpiresAt time.Time  `json:"expiresAt"`
	FLVPath   string     `json:"flvPath,omitempty"`
	Active    bool       `json:"active"`
	ServerIP  string     `json:"serverIp"`
	TCPPort   uint16     `json:"tcpPort"`
	UDPPort   uint16     `json:"udpPort"`
	// Output 是播放出口模式（flv/rtmp/both）；播放地址由媒体节点返回，
	// 管控层只透传，不按模式硬拼 URL。
	Output      string `json:"output,omitempty"`
	PlaybackURL string `json:"playbackUrl,omitempty"`
	// Endpoints 是全部可用播放/发布端点；flv 模式是 HTTP-FLV，
	// rtmp 模式是 RTMP 发布地址与外部媒体服务器播放地址。
	Endpoints []playback.Endpoint `json:"endpoints,omitempty"`
}

// SessionConflictError 表示同一 (phone, channel) 已存在未过期的会话。
//
// 按设计不做静默顶替、不排队：调用方必须显式释放旧会话后再申请。
type SessionConflictError struct {
	Phone             string
	Channel           uint8
	ExistingSessionID string
	ExistingKind      StreamKind
	ExistingExpiresAt time.Time
}

// Code 是 API 契约中的稳定错误码。
func (e *SessionConflictError) Code() string { return "session_conflict" }

func (e *SessionConflictError) Error() string {
	return fmt.Sprintf("media session conflict for %s/%d: existing session %s (%s) expires at %s",
		e.Phone, e.Channel, e.ExistingSessionID, e.ExistingKind, e.ExistingExpiresAt.Format(time.RFC3339))
}

// SessionNotFoundError 表示释放/查询的会话不存在或已过期。
type SessionNotFoundError struct {
	SessionID string
}

func (e *SessionNotFoundError) Code() string { return "session_not_found" }

func (e *SessionNotFoundError) Error() string {
	return fmt.Sprintf("media session %s not found", e.SessionID)
}
