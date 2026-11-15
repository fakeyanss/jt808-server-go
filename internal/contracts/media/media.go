// Package media 定义管控与媒体节点之间、以及管理面与管控之间的媒体调度契约。
package media

import (
	"context"
	"fmt"
	"io"
	"time"
)

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
