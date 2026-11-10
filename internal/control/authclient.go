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

	"github.com/fakeyanss/jt808-server-go/internal/contracts"
	"github.com/fakeyanss/jt808-server-go/internal/protocol/model"
	"github.com/fakeyanss/jt808-server-go/internal/repository"
	"github.com/fakeyanss/jt808-server-go/internal/uplink"
)

// HTTPAuthProvider 是网关侧的只读鉴权来源：只查档案，不写档案。
type HTTPAuthProvider struct {
	url    string
	token  string
	client *http.Client
	ttl    time.Duration

	mu    sync.Mutex
	cache map[string]authCacheEntry
}

type authCacheEntry struct {
	device    *model.Device
	expiresAt time.Time
}

// NewHTTPAuthProvider 构造只读鉴权客户端。baseURL 是管控内部入口的根地址，
// 例如 http://control:8081；缓存 TTL 用于吸收注册/鉴权的连续查询。
func NewHTTPAuthProvider(baseURL, token string, timeout time.Duration, cacheTTL time.Duration) (*HTTPAuthProvider, error) {
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
	return &HTTPAuthProvider{
		url:    strings.TrimRight(baseURL, "/") + uplink.PathDeviceLookup,
		token:  token,
		client: &http.Client{Timeout: timeout},
		ttl:    cacheTTL,
		cache:  map[string]authCacheEntry{},
	}, nil
}

// Device 返回设备档案；管控不可用时返回错误，由调用方决定是否放行。
func (p *HTTPAuthProvider) Device(ctx context.Context, phone string) (*model.Device, error) {
	if p == nil {
		return nil, repository.ErrDeviceNotFound
	}
	if device, ok := p.cached(phone); ok {
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
		return nil, err
	}
	defer response.Body.Close()
	if response.StatusCode == http.StatusNotFound {
		return nil, repository.ErrDeviceNotFound
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
		return nil, err
	}
	device := &model.Device{Phone: body.Phone, ID: body.ID, Plate: body.Plate, AuthCode: body.AuthCode}
	p.store(device)
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

func (p *HTTPAuthProvider) cached(phone string) (*model.Device, bool) {
	p.mu.Lock()
	defer p.mu.Unlock()
	entry, ok := p.cache[phone]
	if !ok || time.Now().After(entry.expiresAt) {
		if ok {
			delete(p.cache, phone)
		}
		return nil, false
	}
	copied := *entry.device
	return &copied, true
}

func (p *HTTPAuthProvider) store(device *model.Device) {
	p.mu.Lock()
	defer p.mu.Unlock()
	if len(p.cache) > 50000 {
		now := time.Now()
		for key, entry := range p.cache {
			if now.After(entry.expiresAt) {
				delete(p.cache, key)
			}
		}
	}
	copied := *device
	p.cache[device.Phone] = authCacheEntry{device: &copied, expiresAt: time.Now().Add(p.ttl)}
}

var _ contracts.AuthProvider = (*HTTPAuthProvider)(nil)
