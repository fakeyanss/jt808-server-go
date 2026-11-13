package media

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"strings"
	"time"
)

// HTTPSegmentSink 把切片完成事件以出站 HTTP 通知 control。
//
// 通知允许丢失：control 会扫描 media/segments/ 前缀，从对象 key 重建索引。
// 因此这里只做一次短超时投递，失败由调用方记日志，不阻塞录制。
type HTTPSegmentSink struct {
	url    string
	token  string
	client *http.Client
}

// NewHTTPSegmentSink 构造通知客户端；url 为空表示禁用通知。
func NewHTTPSegmentSink(url, token string, timeout time.Duration) (*HTTPSegmentSink, error) {
	url = strings.TrimSpace(url)
	if url == "" {
		return nil, nil
	}
	if !strings.HasPrefix(url, "http://") && !strings.HasPrefix(url, "https://") {
		return nil, fmt.Errorf("media segment report url must start with http:// or https://, got %q", url)
	}
	if timeout <= 0 {
		timeout = 3 * time.Second
	}
	return &HTTPSegmentSink{
		url:    url,
		token:  strings.TrimSpace(token),
		client: &http.Client{Timeout: timeout},
	}, nil
}

// OnSegment 上报一片已完成的切片。
func (s *HTTPSegmentSink) OnSegment(ctx context.Context, info SegmentInfo) error {
	body, err := json.Marshal(info)
	if err != nil {
		return fmt.Errorf("marshal media segment report: %w", err)
	}
	request, err := http.NewRequestWithContext(ctx, http.MethodPost, s.url, bytes.NewReader(body))
	if err != nil {
		return fmt.Errorf("build media segment report: %w", err)
	}
	request.Header.Set("Content-Type", "application/json")
	if s.token != "" {
		request.Header.Set("Authorization", "Bearer "+s.token)
	}
	response, err := s.client.Do(request)
	if err != nil {
		return fmt.Errorf("post media segment report: %w", err)
	}
	defer func() {
		_, _ = io.Copy(io.Discard, io.LimitReader(response.Body, 4<<10))
		_ = response.Body.Close()
	}()
	if response.StatusCode < 200 || response.StatusCode >= 300 {
		return fmt.Errorf("media segment report rejected with status %d", response.StatusCode)
	}
	return nil
}

var _ SegmentSink = (*HTTPSegmentSink)(nil)
