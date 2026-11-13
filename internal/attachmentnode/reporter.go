package attachmentnode

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

	"github.com/rs/zerolog/log"
)

// ReporterOptions 是附件节点心跳上报的构造参数。
type ReporterOptions struct {
	// BaseURL 是 control 内部入口基址。
	BaseURL string
	Token   string
	NodeID  string
	// InstanceID 每次进程启动都变，control 据此识别重启。
	InstanceID string
	// AdvertiseURL 是 control 可达的本节点内部 HTTP 基址。
	AdvertiseURL string
	Interval     time.Duration
	Client       *http.Client
}

// Reporter 负责附件节点注册、周期心跳与优雅注销。
type Reporter struct {
	baseURL    string
	token      string
	nodeID     string
	instanceID string
	// advertiseURL 是 control 可达的本节点内部 HTTP 基址。
	advertiseURL string
	interval     time.Duration
	client       *http.Client
	done         chan struct{}
}

// NewReporter 创建附件节点心跳上报器。
func NewReporter(options ReporterOptions) (*Reporter, error) {
	base, err := normalizeBaseURL(options.BaseURL)
	if err != nil {
		return nil, err
	}
	advertise := strings.TrimSpace(options.AdvertiseURL)
	if advertise == "" {
		return nil, fmt.Errorf("attachmentnode: advertise base url is required")
	}
	if _, err := normalizeBaseURL(advertise); err != nil {
		return nil, err
	}
	if strings.TrimSpace(options.NodeID) == "" || strings.TrimSpace(options.InstanceID) == "" {
		return nil, fmt.Errorf("attachmentnode: nodeId and instanceId are required")
	}
	interval := options.Interval
	if interval <= 0 {
		interval = HeartbeatInterval
	}
	client := options.Client
	if client == nil {
		client = &http.Client{Timeout: 3 * time.Second}
	}
	return &Reporter{
		baseURL:      base,
		token:        strings.TrimSpace(options.Token),
		nodeID:       strings.TrimSpace(options.NodeID),
		instanceID:   strings.TrimSpace(options.InstanceID),
		advertiseURL: strings.TrimRight(advertise, "/"),
		interval:     interval,
		client:       client,
		done:         make(chan struct{}),
	}, nil
}

func normalizeBaseURL(raw string) (string, error) {
	base := strings.TrimRight(strings.TrimSpace(raw), "/")
	parsed, err := url.Parse(base)
	if err != nil || parsed.Scheme == "" || parsed.Host == "" {
		return "", fmt.Errorf("attachmentnode: invalid control base url %q", raw)
	}
	return base, nil
}

// Done 在 Run 退出后关闭。
func (r *Reporter) Done() <-chan struct{} { return r.done }

// Run 注册节点并按周期发送心跳，ctx 取消时注销节点。
func (r *Reporter) Run(ctx context.Context) {
	defer close(r.done)
	registered := false
	ticker := time.NewTicker(r.interval)
	defer ticker.Stop()

	if err := r.register(ctx); err != nil {
		log.Warn().Err(err).Str("node", r.nodeID).Msg("Attachment node register failed; will retry")
	} else {
		registered = true
		log.Info().Str("node", r.nodeID).Str("instance_id", r.instanceID).
			Str("base_url", r.advertiseURL).Msg("Attachment node registered")
	}

	for {
		select {
		case <-ctx.Done():
			if registered {
				r.deregister()
			}
			return
		case <-ticker.C:
			if !registered {
				if err := r.register(ctx); err != nil {
					log.Warn().Err(err).Str("node", r.nodeID).Msg("Attachment node register retry failed")
					continue
				}
				registered = true
				continue
			}
			ok, err := r.heartbeat(ctx)
			if err != nil || !ok {
				if err != nil {
					log.Warn().Err(err).Str("node", r.nodeID).Msg("Attachment node heartbeat failed; re-register")
				} else {
					log.Warn().Str("node", r.nodeID).Str("instance_id", r.instanceID).
						Msg("Attachment node heartbeat fenced; re-register")
				}
				registered = false
			}
		}
	}
}

func (r *Reporter) register(ctx context.Context) error {
	body := RegisterRequest{NodeID: r.nodeID, InstanceID: r.instanceID, BaseURL: r.advertiseURL}
	return r.post(ctx, PathRegister, body, http.StatusOK)
}

func (r *Reporter) heartbeat(ctx context.Context) (bool, error) {
	body := HeartbeatRequest{NodeID: r.nodeID, InstanceID: r.instanceID}
	status, err := r.postStatus(ctx, PathHeartbeat, body)
	if err != nil {
		return false, err
	}
	if status == http.StatusConflict {
		return false, nil
	}
	if status != http.StatusOK {
		return false, fmt.Errorf("attachmentnode: heartbeat returned %d", status)
	}
	return true, nil
}

func (r *Reporter) deregister() {
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()
	body := HeartbeatRequest{NodeID: r.nodeID, InstanceID: r.instanceID}
	if err := r.post(ctx, PathDeregister, body, http.StatusOK); err != nil {
		log.Warn().Err(err).Str("node", r.nodeID).Msg("Attachment node deregister failed; it will expire by TTL")
		return
	}
	log.Info().Str("node", r.nodeID).Str("instance_id", r.instanceID).Msg("Attachment node deregistered")
}

func (r *Reporter) post(ctx context.Context, path string, body any, want int) error {
	status, err := r.postStatus(ctx, path, body)
	if err != nil {
		return err
	}
	if status != want {
		return fmt.Errorf("attachmentnode: %s returned %d", path, status)
	}
	return nil
}

func (r *Reporter) postStatus(ctx context.Context, path string, body any) (int, error) {
	payload, err := json.Marshal(body)
	if err != nil {
		return 0, err
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, r.baseURL+path, bytes.NewReader(payload))
	if err != nil {
		return 0, err
	}
	req.Header.Set("Content-Type", "application/json")
	if r.token != "" {
		req.Header.Set("Authorization", "Bearer "+r.token)
	}
	resp, err := r.client.Do(req)
	if err != nil {
		return 0, err
	}
	defer resp.Body.Close()
	_, _ = io.Copy(io.Discard, io.LimitReader(resp.Body, 4096))
	return resp.StatusCode, nil
}
