package gatewaynode

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

// ReporterOptions 是网关心跳上报的构造参数。
type ReporterOptions struct {
	BaseURL     string
	Token       string
	GatewayID   string
	InstanceID  string
	DownlinkURL string
	Interval    time.Duration
	Client      *http.Client
}

// Reporter 负责网关注册、周期心跳和优雅注销。
type Reporter struct {
	baseURL     string
	token       string
	gatewayID   string
	instanceID  string
	downlinkURL string
	interval    time.Duration
	client      *http.Client
	done        chan struct{}
}

// NewReporter 创建网关心跳上报器。
func NewReporter(options ReporterOptions) (*Reporter, error) {
	base := strings.TrimRight(strings.TrimSpace(options.BaseURL), "/")
	parsed, err := url.Parse(base)
	if err != nil || parsed.Scheme == "" || parsed.Host == "" {
		return nil, fmt.Errorf("gatewaynode: invalid control base url %q", options.BaseURL)
	}
	if strings.TrimSpace(options.GatewayID) == "" {
		return nil, fmt.Errorf("gatewaynode: gatewayId is required")
	}
	if strings.TrimSpace(options.InstanceID) == "" {
		return nil, fmt.Errorf("gatewaynode: instanceId is required")
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
		baseURL:     base,
		token:       strings.TrimSpace(options.Token),
		gatewayID:   strings.TrimSpace(options.GatewayID),
		instanceID:  strings.TrimSpace(options.InstanceID),
		downlinkURL: strings.TrimSpace(options.DownlinkURL),
		interval:    interval,
		client:      client,
		done:        make(chan struct{}),
	}, nil
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
		log.Warn().Err(err).Str("gateway_id", r.gatewayID).Msg("Gateway node register failed; will retry")
	} else {
		registered = true
		log.Info().Str("gateway_id", r.gatewayID).Str("instance_id", r.instanceID).
			Str("downlink_url", r.downlinkURL).Msg("Gateway node registered")
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
					log.Warn().Err(err).Str("gateway_id", r.gatewayID).Msg("Gateway node register retry failed")
					continue
				}
				registered = true
				continue
			}
			ok, err := r.heartbeat(ctx)
			if err != nil || !ok {
				if err != nil {
					log.Warn().Err(err).Str("gateway_id", r.gatewayID).Msg("Gateway node heartbeat failed; re-register")
				} else {
					log.Warn().Str("gateway_id", r.gatewayID).Str("instance_id", r.instanceID).
						Msg("Gateway node heartbeat fenced; re-register")
				}
				registered = false
			}
		}
	}
}

func (r *Reporter) register(ctx context.Context) error {
	body := RegisterRequest{
		GatewayID:   r.gatewayID,
		InstanceID:  r.instanceID,
		DownlinkURL: r.downlinkURL,
	}
	return r.post(ctx, PathRegister, body, http.StatusOK)
}

func (r *Reporter) heartbeat(ctx context.Context) (bool, error) {
	body := HeartbeatRequest{GatewayID: r.gatewayID, InstanceID: r.instanceID}
	status, err := r.postStatus(ctx, PathHeartbeat, body)
	if err != nil {
		return false, err
	}
	if status == http.StatusConflict {
		return false, nil
	}
	if status != http.StatusOK {
		return false, fmt.Errorf("gatewaynode: heartbeat returned %d", status)
	}
	return true, nil
}

func (r *Reporter) deregister() {
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()
	body := HeartbeatRequest{GatewayID: r.gatewayID, InstanceID: r.instanceID}
	if err := r.post(ctx, PathDeregister, body, http.StatusOK); err != nil {
		log.Warn().Err(err).Str("gateway_id", r.gatewayID).Msg("Gateway node deregister failed; it will expire by TTL")
		return
	}
	log.Info().Str("gateway_id", r.gatewayID).Str("instance_id", r.instanceID).Msg("Gateway node deregistered")
}

func (r *Reporter) post(ctx context.Context, path string, body any, want int) error {
	status, err := r.postStatus(ctx, path, body)
	if err != nil {
		return err
	}
	if status != want {
		return fmt.Errorf("gatewaynode: %s returned %d", path, status)
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
