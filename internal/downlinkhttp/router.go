package downlinkhttp

import (
	"context"
	"fmt"
	"net/http"
	"strings"
	"sync"
	"time"

	"github.com/fakeyanss/jt808-server-go/internal/contracts"
	"github.com/fakeyanss/jt808-server-go/internal/observability"
	"github.com/fakeyanss/jt808-server-go/internal/repository"
)

// RouterOptions 是跨节点下行路由器的构造参数。
type RouterOptions struct {
	// Directory 是 phone -> 网关节点的归属索引，判定由它联表完成。
	Directory repository.SessionDirectoryRepository
	// Token 是 control -> gateway 的共享密钥。
	Token string
	// Timeout 是单次 HTTP 调用的超时。
	Timeout time.Duration
	// HTTPClient 便于测试注入；为空时按 Timeout 新建。
	HTTPClient *http.Client
	// Metrics 是目录命中/离线的观测计数器；可为空。
	Metrics *observability.Metrics
}

// Router 按会话目录把下行请求发到归属网关。
//
// 它只做“查目录 -> 取节点入口 -> 发送”：目录缺失、节点离线或过期
// 都返回 offline，交给 outbox 重试，不跨节点猜测，也不做轮询兜底。
type Router struct {
	directory  repository.SessionDirectoryRepository
	token      string
	timeout    time.Duration
	httpClient *http.Client
	metrics    *observability.Metrics

	mu      sync.Mutex
	clients map[string]*Client
}

var _ contracts.DownlinkSender = (*Router)(nil)

// NewRouter 构造跨节点下行路由器。
func NewRouter(options RouterOptions) (*Router, error) {
	if options.Directory == nil {
		return nil, fmt.Errorf("downlink router: session directory is required")
	}
	if strings.TrimSpace(options.Token) == "" {
		return nil, fmt.Errorf("downlink router: token is required")
	}
	return &Router{
		directory:  options.Directory,
		token:      strings.TrimSpace(options.Token),
		timeout:    options.Timeout,
		httpClient: options.HTTPClient,
		metrics:    options.Metrics,
		clients:    make(map[string]*Client),
	}, nil
}

// Send 实现 contracts.DownlinkSender。
func (r *Router) Send(ctx context.Context, request contracts.DownlinkRequest) (contracts.DownlinkReceipt, error) {
	if r == nil {
		return contracts.DownlinkReceipt{}, fmt.Errorf("downlink router is not configured")
	}
	if request.Message == nil || request.Message.GetHeader() == nil {
		return contracts.DownlinkReceipt{}, fmt.Errorf("downlink message header is required")
	}
	if strings.TrimSpace(request.Phone) == "" {
		r.metrics.IncDownlinkOffline()
		return contracts.DownlinkReceipt{Status: contracts.DownlinkStatusOffline}, nil
	}
	baseURL, ok, err := r.directory.Route(ctx, request.Phone, request.SessionID)
	if err != nil {
		return contracts.DownlinkReceipt{}, &contracts.DownlinkError{
			Kind:    contracts.DownlinkErrorTransient,
			Message: fmt.Sprintf("resolve downlink node for %s: %v", request.Phone, err),
		}
	}
	if !ok {
		r.metrics.IncDownlinkOffline()
		return contracts.DownlinkReceipt{Status: contracts.DownlinkStatusOffline}, nil
	}
	r.metrics.IncDownlinkDirectoryHit()
	client, err := r.client(baseURL)
	if err != nil {
		// 节点登记的入口不可用属于可自愈的配置抖动：节点重新注册后即可恢复。
		return contracts.DownlinkReceipt{}, &contracts.DownlinkError{
			Kind:    contracts.DownlinkErrorTransient,
			Message: fmt.Sprintf("downlink node %q: %v", baseURL, err),
		}
	}
	return client.Send(ctx, request)
}

// client 按节点入口缓存客户端，避免每次下发都新建 http.Client。
func (r *Router) client(baseURL string) (*Client, error) {
	r.mu.Lock()
	defer r.mu.Unlock()
	if cached, ok := r.clients[baseURL]; ok {
		return cached, nil
	}
	client, err := NewClient(ClientOptions{
		BaseURL:    baseURL,
		Token:      r.token,
		Timeout:    r.timeout,
		HTTPClient: r.httpClient,
	})
	if err != nil {
		return nil, err
	}
	r.clients[baseURL] = client
	return client, nil
}
