package attachmentserver

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strings"
	"time"

	"github.com/fakeyanss/jt808-server-go/internal/contracts"
)

// HTTPProgressSink 是附件节点向 control 上报进度、查询恢复点的出站客户端。
//
// 方向固定为“附件节点 → control”：control 不需要反向连接边缘节点。
// 上报失败不阻断接收；control 可通过扫描 attachment/ 前缀补齐索引。
type HTTPProgressSink struct {
	endpoint string
	token    string
	client   *http.Client
}

// NewHTTPProgressSink 构造出站进度客户端。
func NewHTTPProgressSink(endpoint, token string, timeout time.Duration) (*HTTPProgressSink, error) {
	endpoint = strings.TrimSpace(endpoint)
	if endpoint == "" {
		return nil, fmt.Errorf("attachment progress url is required")
	}
	if !strings.HasPrefix(endpoint, "http://") && !strings.HasPrefix(endpoint, "https://") {
		return nil, fmt.Errorf("attachment progress url must start with http:// or https://, got %q", endpoint)
	}
	if timeout <= 0 {
		timeout = 3 * time.Second
	}
	return &HTTPProgressSink{
		endpoint: endpoint,
		token:    strings.TrimSpace(token),
		client:   &http.Client{Timeout: timeout},
	}, nil
}

// Save 上报一份进度快照。
func (s *HTTPProgressSink) Save(ctx context.Context, progress contracts.AttachmentProgress) error {
	body, err := json.Marshal(progress)
	if err != nil {
		return fmt.Errorf("marshal attachment progress: %w", err)
	}
	request, err := http.NewRequestWithContext(ctx, http.MethodPost, s.endpoint, bytes.NewReader(body))
	if err != nil {
		return fmt.Errorf("build attachment progress request: %w", err)
	}
	request.Header.Set("Content-Type", "application/json")
	s.authorize(request)
	response, err := s.client.Do(request)
	if err != nil {
		return fmt.Errorf("post attachment progress: %w", err)
	}
	defer discardBody(response)
	if response.StatusCode < 200 || response.StatusCode >= 300 {
		return fmt.Errorf("attachment progress rejected with status %d", response.StatusCode)
	}
	return nil
}

// Load 查询恢复点；404 映射为 ErrAttachmentProgressNotFound。
func (s *HTTPProgressSink) Load(ctx context.Context, key contracts.AttachmentProgressKey) (*contracts.AttachmentProgress, error) {
	parsed, err := url.Parse(s.endpoint)
	if err != nil {
		return nil, fmt.Errorf("parse attachment progress url: %w", err)
	}
	query := parsed.Query()
	query.Set("source", key.Source)
	query.Set("phone", key.Phone)
	query.Set("alarmNo", key.AlarmNo)
	query.Set("fileName", key.FileName)
	parsed.RawQuery = query.Encode()

	request, err := http.NewRequestWithContext(ctx, http.MethodGet, parsed.String(), nil)
	if err != nil {
		return nil, fmt.Errorf("build attachment progress query: %w", err)
	}
	s.authorize(request)
	response, err := s.client.Do(request)
	if err != nil {
		return nil, fmt.Errorf("query attachment progress: %w", err)
	}
	defer discardBody(response)
	switch {
	case response.StatusCode == http.StatusNotFound:
		return nil, contracts.ErrAttachmentProgressNotFound
	case response.StatusCode < 200 || response.StatusCode >= 300:
		return nil, fmt.Errorf("attachment progress query rejected with status %d", response.StatusCode)
	}
	var progress contracts.AttachmentProgress
	if err := json.NewDecoder(io.LimitReader(response.Body, 1<<20)).Decode(&progress); err != nil {
		return nil, fmt.Errorf("decode attachment progress: %w", err)
	}
	return &progress, nil
}

func (s *HTTPProgressSink) authorize(request *http.Request) {
	if s.token != "" {
		request.Header.Set("Authorization", "Bearer "+s.token)
	}
}

func discardBody(response *http.Response) {
	_, _ = io.Copy(io.Discard, io.LimitReader(response.Body, 4<<10))
	_ = response.Body.Close()
}

var _ contracts.AttachmentProgressSink = (*HTTPProgressSink)(nil)
