package ftpserver

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"strings"
	"time"

	"github.com/fakeyanss/jt808-server-go/internal/attachmentnode"
	"github.com/fakeyanss/jt808-server-go/internal/contracts"
)

// ErrLoginRejected 表示 control 判定账号无效或已吊销。
var ErrLoginRejected = errors.New("ftp login rejected")

// LoginResult 是 control 返回的任务配置。
type LoginResult struct {
	Phone      string
	Serial     uint16
	UploadPath string
	MaxBytes   int64
}

// Control 是附件节点对 control 的出站通道。
//
// 节点不连库：登录与进度都必须经这里发出；control 不可达时 Login 返回错误，
// 调用方拒绝新登录，但已建立的连接不受影响。
type Control interface {
	Login(ctx context.Context, nodeID, username, password string) (LoginResult, error)
	Report(ctx context.Context, progress contracts.AttachmentProgress) error
}

// HTTPControl 是 Control 的 HTTP 实现。
type HTTPControl struct {
	BaseURL string
	Token   string
	NodeID  string
	Client  *http.Client
}

// NewHTTPControl 创建出站客户端；baseURL 是 control 内部入口基址。
func NewHTTPControl(baseURL, token, nodeID string) *HTTPControl {
	return &HTTPControl{
		BaseURL: strings.TrimRight(strings.TrimSpace(baseURL), "/"),
		Token:   strings.TrimSpace(token),
		NodeID:  nodeID,
		Client:  &http.Client{Timeout: 3 * time.Second},
	}
}

// Login 校验账号并取回任务配置。
func (c *HTTPControl) Login(ctx context.Context, nodeID, username, password string) (LoginResult, error) {
	if c == nil || c.BaseURL == "" {
		return LoginResult{}, fmt.Errorf("ftp control endpoint is not configured")
	}
	body := attachmentnode.FTPLoginRequest{NodeID: nodeID, Username: username, Password: password}
	payload, err := json.Marshal(body)
	if err != nil {
		return LoginResult{}, err
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, c.BaseURL+attachmentnode.PathFTPLogin, bytes.NewReader(payload))
	if err != nil {
		return LoginResult{}, err
	}
	req.Header.Set("Content-Type", "application/json")
	if c.Token != "" {
		req.Header.Set("Authorization", "Bearer "+c.Token)
	}
	resp, err := c.client().Do(req)
	if err != nil {
		return LoginResult{}, fmt.Errorf("control login: %w", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode == http.StatusUnauthorized {
		_, _ = io.Copy(io.Discard, io.LimitReader(resp.Body, 4096))
		return LoginResult{}, ErrLoginRejected
	}
	if resp.StatusCode != http.StatusOK {
		_, _ = io.Copy(io.Discard, io.LimitReader(resp.Body, 4096))
		return LoginResult{}, fmt.Errorf("control login returned %d", resp.StatusCode)
	}
	var result attachmentnode.FTPLoginResponse
	if err := json.NewDecoder(io.LimitReader(resp.Body, 1<<20)).Decode(&result); err != nil {
		return LoginResult{}, fmt.Errorf("decode control login response: %w", err)
	}
	return LoginResult{
		Phone: result.Phone, Serial: result.Serial,
		UploadPath: result.UploadPath, MaxBytes: result.MaxBytes,
	}, nil
}

// Report 把进度快照推给 control；失败只影响索引新鲜度，不阻断接收。
func (c *HTTPControl) Report(ctx context.Context, progress contracts.AttachmentProgress) error {
	if c == nil || c.BaseURL == "" {
		return nil
	}
	payload, err := json.Marshal(progress)
	if err != nil {
		return err
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodPost,
		c.BaseURL+"/internal/v1/attachment/progress", bytes.NewReader(payload))
	if err != nil {
		return err
	}
	req.Header.Set("Content-Type", "application/json")
	if c.Token != "" {
		req.Header.Set("Authorization", "Bearer "+c.Token)
	}
	resp, err := c.client().Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	_, _ = io.Copy(io.Discard, io.LimitReader(resp.Body, 4096))
	if resp.StatusCode >= 200 && resp.StatusCode < 300 {
		return nil
	}
	return fmt.Errorf("control progress returned %d", resp.StatusCode)
}

func (c *HTTPControl) client() *http.Client {
	if c.Client != nil {
		return c.Client
	}
	return http.DefaultClient
}
