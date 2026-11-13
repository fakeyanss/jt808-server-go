package control

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"strings"
	"sync"
	"time"

	"github.com/rs/zerolog/log"

	"github.com/fakeyanss/jt808-server-go/internal/contracts"
	"github.com/fakeyanss/jt808-server-go/internal/protocol/model"
	"github.com/fakeyanss/jt808-server-go/internal/repository"
	"github.com/fakeyanss/jt808-server-go/internal/uplink"
)

// HTTPAuthProvider 是网关侧的只读鉴权来源：只查档案，不写档案。
//
// 缓存过期后若 control 不可达，在宽限期内继续按缓存档案放行，避免控制面抖动
// 直接把在网终端踢下线；宽限期耗尽后仍回到拒绝，保证陈旧凭据不会无限期生效。
// 默认宽限期由 config.DefaultUplinkAuthGrace 提供。
//
// 降级语义（有界）：
//   - 缓存新鲜时直接命中；
//   - 缓存过期且 control 不可达（网络错误或 5xx）时，若仍在该条目的宽限期内，
//     按过期缓存放行；从未查到过档案的终端一律拒绝；
//   - control 返回 404 视为档案不存在，删除缓存并拒绝，不进入宽限；
//   - 宽限期耗尽或 control 明确返回非 5xx 错误时回到拒绝。
type HTTPAuthProvider struct {
	url    string
	token  string
	client *http.Client
	ttl    time.Duration
	grace  time.Duration

	// now 便于测试推进时间；生产使用系统时钟。
	now func() time.Time

	mu       sync.Mutex
	cache    map[string]authCacheEntry
	degraded bool
}

type authCacheEntry struct {
	device     *model.Device
	expiresAt  time.Time
	graceUntil time.Time
}

// NewHTTPAuthProvider 构造只读鉴权客户端。baseURL 是管控内部入口的根地址，
// 例如 http://control:8081；cacheTTL 用于吸收注册/鉴权的连续查询，
// grace 是缓存过期后 control 不可达时的有限放行窗口。
func NewHTTPAuthProvider(baseURL, token string, timeout, cacheTTL, grace time.Duration) (*HTTPAuthProvider, error) {
	if strings.TrimSpace(baseURL) == "" {
		return nil, fmt.Errorf("auth provider: base url is required")
	}
	if strings.TrimSpace(token) == "" {
		return nil, fmt.Errorf("auth provider: token is required")
	}
	if timeout <= 0 {
		timeout = 3 * time.Second
	}
	if cacheTTL <= 0 {
		cacheTTL = 5 * time.Minute
	}
	if grace < 0 {
		grace = 0
	}
	return &HTTPAuthProvider{
		url:    strings.TrimRight(baseURL, "/") + uplink.PathDeviceLookup,
		token:  token,
		client: &http.Client{Timeout: timeout},
		ttl:    cacheTTL,
		grace:  grace,
		now:    time.Now,
		cache:  map[string]authCacheEntry{},
	}, nil
}

// Device 返回设备档案。
func (p *HTTPAuthProvider) Device(ctx context.Context, phone string) (*model.Device, error) {
	if p == nil {
		return nil, repository.ErrDeviceNotFound
	}
	if device, ok := p.fresh(phone); ok {
		return device, nil
	}

	payload, _ := json.Marshal(map[string]string{"phone": phone})
	request, err := http.NewRequestWithContext(ctx, http.MethodPost, p.url, bytes.NewReader(payload))
	if err != nil {
		return nil, err
	}
	request.Header.Set("Content-Type", "application/json")
	request.Header.Set("Authorization", "Bearer "+p.token)
	response, err := p.client.Do(request)
	if err != nil {
		return p.degrade(phone, err)
	}
	defer response.Body.Close()
	if response.StatusCode == http.StatusNotFound {
		// 档案被 control 明确判定不存在：清掉陈旧条目，不进入宽限。
		p.drop(phone)
		return nil, repository.ErrDeviceNotFound
	}
	if response.StatusCode >= http.StatusInternalServerError {
		return p.degrade(phone, fmt.Errorf("auth provider lookup status %d", response.StatusCode))
	}
	if response.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("auth provider lookup status %d", response.StatusCode)
	}
	var body struct {
		Phone    string `json:"phone"`
		ID       string `json:"id"`
		Plate    string `json:"plate"`
		AuthCode string `json:"authCode"`
	}
	if err := json.NewDecoder(response.Body).Decode(&body); err != nil {
		return p.degrade(phone, err)
	}
	device := &model.Device{Phone: body.Phone, ID: body.ID, Plate: body.Plate, AuthCode: body.AuthCode}
	p.store(device)
	p.markRecovered()
	return device, nil
}

// AuthCode 只取鉴权码。
func (p *HTTPAuthProvider) AuthCode(ctx context.Context, phone string) (string, error) {
	device, err := p.Device(ctx, phone)
	if err != nil {
		return "", err
	}
	return device.AuthCode, nil
}

// degrade 在 control 不可达时按过期缓存有界放行。
// 没有缓存条目或宽限期已耗尽则返回原始错误，保持 fail-closed。
func (p *HTTPAuthProvider) degrade(phone string, cause error) (*model.Device, error) {
	now := p.now()
	p.mu.Lock()
	entry, ok := p.cache[phone]
	if ok && now.After(entry.graceUntil) {
		delete(p.cache, phone)
		ok = false
	}
	first := false
	if ok && !p.degraded {
		p.degraded = true
		first = true
	}
	var device *model.Device
	if ok {
		copied := *entry.device
		device = &copied
	}
	remaining := time.Duration(0)
	if ok {
		remaining = entry.graceUntil.Sub(now)
	}
	p.mu.Unlock()

	if !ok {
		return nil, cause
	}
	if first {
		log.Warn().Err(cause).Str("phone", phone).Dur("grace_remaining", remaining).
			Msg("Control is unavailable: gateway serves cached device profile within the configured grace period")
	} else {
		log.Debug().Err(cause).Str("phone", phone).Dur("grace_remaining", remaining).
			Msg("Control is unavailable: serving cached device profile")
	}
	return device, nil
}

// fresh 只返回仍在正常有效期内的缓存。
func (p *HTTPAuthProvider) fresh(phone string) (*model.Device, bool) {
	now := p.now()
	p.mu.Lock()
	defer p.mu.Unlock()
	entry, ok := p.cache[phone]
	if !ok || now.After(entry.expiresAt) {
		return nil, false
	}
	copied := *entry.device
	return &copied, true
}

func (p *HTTPAuthProvider) drop(phone string) {
	p.mu.Lock()
	defer p.mu.Unlock()
	delete(p.cache, phone)
}

func (p *HTTPAuthProvider) store(device *model.Device) {
	now := p.now()
	p.mu.Lock()
	defer p.mu.Unlock()
	if len(p.cache) > 50000 {
		// 清理宽限期也过期的条目；新鲜条目不受影响。
		for key, entry := range p.cache {
			if now.After(entry.graceUntil) {
				delete(p.cache, key)
			}
		}
	}
	copied := *device
	p.cache[device.Phone] = authCacheEntry{
		device:     &copied,
		expiresAt:  now.Add(p.ttl),
		graceUntil: now.Add(p.ttl + p.grace),
	}
}

// markRecovered 在 control 恢复后结束降级并记录一次恢复日志。
func (p *HTTPAuthProvider) markRecovered() {
	p.mu.Lock()
	wasDegraded := p.degraded
	p.degraded = false
	p.mu.Unlock()
	if wasDegraded {
		log.Info().Msg("Control is reachable again: gateway device lookup returned to the normal path")
	}
}

var _ contracts.AuthProvider = (*HTTPAuthProvider)(nil)
