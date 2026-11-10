package downlinkhttp

import (
	"bytes"
	"context"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"strconv"
	"strings"
	"time"

	"github.com/fakeyanss/jt808-server-go/internal/contracts"
	"github.com/fakeyanss/jt808-server-go/internal/protocol/model"
)

// ClientOptions 是管控侧下行客户端的构造参数。
type ClientOptions struct {
	// BaseURL 是网关下行入口基址，例如 http://127.0.0.1:9101。
	BaseURL string
	// Token 是 control -> gateway 的共享密钥。
	Token string
	// Timeout 是单次 HTTP 调用的超时；sync 模式由请求体决定网关等待。
	Timeout time.Duration
	// HTTPClient 便于测试注入；为空时按 Timeout 新建。
	HTTPClient *http.Client
}

// Client 是管控侧对网关下行入口的调用端。
//
// 它只负责传输：返回 error 表示请求未得到网关回执；offline/rejected 等
// 业务结果放在 DownlinkReceipt.Status，由 outbox 决定租约收尾。
type Client struct {
	url    string
	token  string
	client *http.Client
}

var _ contracts.DownlinkSender = (*Client)(nil)

// NewClient 构造下行客户端。
func NewClient(options ClientOptions) (*Client, error) {
	base := strings.TrimRight(strings.TrimSpace(options.BaseURL), "/")
	if base == "" {
		return nil, fmt.Errorf("downlink http client: base url is required")
	}
	if strings.TrimSpace(options.Token) == "" {
		return nil, fmt.Errorf("downlink http client: token is required")
	}
	httpClient := options.HTTPClient
	if httpClient == nil {
		timeout := options.Timeout
		if timeout <= 0 {
			timeout = 8 * time.Second
		}
		httpClient = &http.Client{Timeout: timeout}
	}
	return &Client{
		url:    base + PathDownlink,
		token:  strings.TrimSpace(options.Token),
		client: httpClient,
	}, nil
}

// Send 实现 contracts.DownlinkSender。
func (c *Client) Send(ctx context.Context, request contracts.DownlinkRequest) (contracts.DownlinkReceipt, error) {
	if c == nil {
		return contracts.DownlinkReceipt{}, fmt.Errorf("downlink http client is not configured")
	}
	if request.Message == nil || request.Message.GetHeader() == nil {
		return contracts.DownlinkReceipt{}, fmt.Errorf("downlink message header is required")
	}
	encoded, err := request.Message.Encode()
	if err != nil {
		return contracts.DownlinkReceipt{}, fmt.Errorf("encode downlink message: %w", err)
	}
	mode := ModeAsync
	responseTimeoutMS := int64(0)
	if request.ExpectResponseMsgID != 0 {
		mode = ModeSyncAck
		responseTimeoutMS = request.ResponseTimeout.Milliseconds()
	}
	body, err := json.Marshal(downlinkRequest{
		Phone:               request.Phone,
		SessionID:           request.SessionID,
		IdempotencyKey:      request.IdempotencyKey,
		Serial:              request.Serial,
		Mode:                mode,
		MessageBase64:       base64.StdEncoding.EncodeToString(encoded),
		ExpectResponseMsgID: request.ExpectResponseMsgID,
		ResponseTimeoutMS:   responseTimeoutMS,
	})
	if err != nil {
		return contracts.DownlinkReceipt{}, fmt.Errorf("marshal downlink request: %w", err)
	}

	httpRequest, err := http.NewRequestWithContext(ctx, http.MethodPost, c.url, bytes.NewReader(body))
	if err != nil {
		return contracts.DownlinkReceipt{}, fmt.Errorf("build downlink request: %w", err)
	}
	httpRequest.Header.Set("Content-Type", "application/json")
	httpRequest.Header.Set("Authorization", "Bearer "+c.token)
	if request.IdempotencyKey != "" {
		httpRequest.Header.Set("Idempotency-Key", request.IdempotencyKey)
	}

	client := c.client
	if timeout := request.ResponseTimeout; timeout > 0 && (client.Timeout == 0 || client.Timeout < timeout) {
		clone := *client
		clone.Timeout = timeout + 3*time.Second
		client = &clone
	}
	response, err := client.Do(httpRequest)
	if err != nil {
		return contracts.DownlinkReceipt{}, fmt.Errorf("call gateway downlink: %w", err)
	}
	defer response.Body.Close()
	payload, _ := io.ReadAll(io.LimitReader(response.Body, 64*1024))
	if response.StatusCode != http.StatusOK && response.StatusCode != http.StatusAccepted {
		// 400/401/403/413 是确定性拒绝，429/5xx 可重试；带上 Retry-After
		// 让 outbox 在限流时按网关节奏退避，而不是按本地指数退避硬打。
		failure := &contracts.DownlinkError{
			Kind:       contracts.ClassifyDownlinkStatus(response.StatusCode),
			StatusCode: response.StatusCode,
			Code:       strings.TrimSpace(string(payload)),
			Message:    fmt.Sprintf("gateway downlink returned status %d", response.StatusCode),
		}
		if retryAfter := parseRetryAfter(response.Header.Get("Retry-After")); retryAfter > 0 {
			failure.RetryAfter = retryAfter
		}
		return contracts.DownlinkReceipt{}, failure
	}
	var parsed downlinkResponse
	if err := json.Unmarshal(payload, &parsed); err != nil {
		return contracts.DownlinkReceipt{}, fmt.Errorf("decode gateway downlink response: %w", err)
	}
	var responseBody []byte
	if parsed.ResponseBodyBase64 != "" {
		responseBody, err = base64.StdEncoding.DecodeString(parsed.ResponseBodyBase64)
		if err != nil {
			return contracts.DownlinkReceipt{}, fmt.Errorf("decode gateway response body: %w", err)
		}
	}
	return contracts.DownlinkReceipt{
		ID:            parsed.DownlinkID,
		Serial:        parsed.Serial,
		Status:        parsed.Status,
		Duplicated:    parsed.Duplicated,
		Result:        parsed.Result,
		Reason:        parsed.Reason,
		ResponseMsgID: parsed.ResponseMsgID,
		ResponseBody:  responseBody,
	}, nil
}

// parseRetryAfter 解析 Retry-After（秒数或 HTTP 日期）；无法解析时返回 0。
func parseRetryAfter(value string) time.Duration {
	value = strings.TrimSpace(value)
	if value == "" {
		return 0
	}
	if seconds, err := strconv.Atoi(value); err == nil {
		if seconds <= 0 {
			return 0
		}
		return time.Duration(seconds) * time.Second
	}
	if when, err := http.ParseTime(value); err == nil {
		if delay := time.Until(when); delay > 0 {
			return delay
		}
	}
	return 0
}

// 确保 model 包被使用：编译期校验请求消息实现了 JT808Msg。
var _ model.JT808Msg = (*model.MsgRawBody)(nil)
