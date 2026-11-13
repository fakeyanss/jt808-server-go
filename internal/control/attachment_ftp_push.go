package control

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"strings"
	"time"

	"github.com/fakeyanss/jt808-server-go/internal/attachmentnode"
	"github.com/fakeyanss/jt808-server-go/internal/clock"
	"github.com/fakeyanss/jt808-server-go/internal/observability"
	"github.com/fakeyanss/jt808-server-go/internal/repository"
)

// FTPControlPusher 把 0x9207 控制指令推送到绑定附件节点。
//
// 推送方向是 control → 节点：节点注册时给出了 control 可达的 baseUrl。
// 节点离线或地址过期时不重试到死地址，交由调用方记录指标。
type FTPControlPusher struct {
	Nodes    repository.AttachmentNodeRepository
	Client   *http.Client
	Token    string
	Clock    clock.Clock
	Metrics  *observability.Metrics
	Attempts int
}

// Push 推送到指定节点；重试次数有限，失败计入指标。
func (p *FTPControlPusher) Push(ctx context.Context, nodeID string, payload attachmentnode.FTPControlRequest) error {
	if p == nil || p.Nodes == nil {
		return nil
	}
	now := time.Now()
	if p.Clock != nil {
		now = p.Clock.Now()
	}
	node, err := p.Nodes.Get(ctx, nodeID)
	if err != nil {
		return err
	}
	if node == nil || node.Status != "online" || !node.ExpiresAt.After(now) {
		p.recordFailure()
		return fmt.Errorf("attachment node %s is offline or expired", nodeID)
	}
	base := strings.TrimRight(strings.TrimSpace(node.BaseURL), "/")
	if base == "" {
		p.recordFailure()
		return fmt.Errorf("attachment node %s has no base url", nodeID)
	}
	body, err := json.Marshal(payload)
	if err != nil {
		return err
	}
	attempts := p.Attempts
	if attempts <= 0 {
		attempts = 3
	}
	client := p.Client
	if client == nil {
		client = &http.Client{Timeout: 3 * time.Second}
	}
	var lastErr error
	for attempt := 0; attempt < attempts; attempt++ {
		if attempt > 0 {
			select {
			case <-ctx.Done():
				return ctx.Err()
			case <-time.After(time.Duration(attempt) * 100 * time.Millisecond):
			}
		}
		req, reqErr := http.NewRequestWithContext(ctx, http.MethodPost,
			base+attachmentnode.PathFTPControl, bytes.NewReader(body))
		if reqErr != nil {
			return reqErr
		}
		req.Header.Set("Content-Type", "application/json")
		if p.Token != "" {
			req.Header.Set("Authorization", "Bearer "+p.Token)
		}
		resp, doErr := client.Do(req)
		if doErr != nil {
			lastErr = doErr
			continue
		}
		_, _ = io.Copy(io.Discard, io.LimitReader(resp.Body, 4096))
		_ = resp.Body.Close()
		if resp.StatusCode >= 200 && resp.StatusCode < 300 {
			return nil
		}
		lastErr = fmt.Errorf("control push returned %d", resp.StatusCode)
	}
	p.recordFailure()
	return lastErr
}

func (p *FTPControlPusher) recordFailure() {
	if p == nil || p.Metrics == nil {
		return
	}
	p.Metrics.IncFTPControlPushFailed()
}
