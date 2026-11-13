package control

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/fakeyanss/jt808-server-go/internal/repository"
)

// authServer 是可切换可用性的 control 档案查询桩。
type authServer struct {
	mu        sync.Mutex
	available bool
	lookups   atomic.Int64
	device    map[string]string
}

func (s *authServer) setAvailable(available bool) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.available = available
}

func (s *authServer) handler() http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		s.lookups.Add(1)
		var body struct {
			Phone string `json:"phone"`
		}
		_ = json.NewDecoder(r.Body).Decode(&body)
		s.mu.Lock()
		available := s.available
		profile, ok := s.device[body.Phone]
		s.mu.Unlock()
		if !available {
			w.WriteHeader(http.StatusServiceUnavailable)
			return
		}
		if !ok {
			w.WriteHeader(http.StatusNotFound)
			return
		}
		_ = json.NewEncoder(w).Encode(map[string]string{
			"phone": body.Phone, "id": profile, "authCode": "auth-" + body.Phone,
		})
	})
}

func TestHTTPAuthProviderServesCachedProfileWithinGrace(t *testing.T) {
	stub := &authServer{available: true, device: map[string]string{"13800138000": "dev-1"}}
	server := httptest.NewServer(stub.handler())
	defer server.Close()

	provider, err := NewHTTPAuthProvider(server.URL, "token", time.Second, 10*time.Second, 30*time.Second)
	if err != nil {
		t.Fatalf("new provider: %v", err)
	}
	now := time.Now()
	provider.now = func() time.Time { return now }

	device, err := provider.Device(context.Background(), "13800138000")
	if err != nil || device == nil || device.AuthCode != "auth-13800138000" {
		t.Fatalf("initial lookup: device=%+v err=%v", device, err)
	}
	if got := stub.lookups.Load(); got != 1 {
		t.Fatalf("expected one control lookup, got %d", got)
	}

	// 缓存新鲜期：不应再访问 control。
	now = now.Add(5 * time.Second)
	if _, err := provider.Device(context.Background(), "13800138000"); err != nil {
		t.Fatalf("fresh cache lookup: %v", err)
	}
	if got := stub.lookups.Load(); got != 1 {
		t.Fatalf("fresh cache must not hit control, got %d lookups", got)
	}

	// 缓存过期 + control 不可达，但仍在宽限期内：按缓存档案放行。
	stub.setAvailable(false)
	now = now.Add(10 * time.Second)
	device, err = provider.Device(context.Background(), "13800138000")
	if err != nil || device == nil {
		t.Fatalf("expected cached profile within grace, device=%+v err=%v", device, err)
	}
	if device.Phone != "13800138000" {
		t.Fatalf("unexpected cached device: %+v", device)
	}

	// 宽限期耗尽：必须回到拒绝，并清掉条目。
	now = now.Add(31 * time.Second)
	if _, err := provider.Device(context.Background(), "13800138000"); err == nil {
		t.Fatal("expected rejection after grace expires")
	}

	// 从未拿到过档案的终端：control 不可达时始终拒绝。
	if _, err := provider.Device(context.Background(), "13900139000"); err == nil {
		t.Fatal("unknown terminal must be rejected while control is unavailable")
	}
}

func TestHTTPAuthProviderRecoversWhenControlReturns(t *testing.T) {
	stub := &authServer{available: true, device: map[string]string{"13800138000": "dev-1"}}
	server := httptest.NewServer(stub.handler())
	defer server.Close()

	provider, err := NewHTTPAuthProvider(server.URL, "token", time.Second, 10*time.Second, time.Minute)
	if err != nil {
		t.Fatalf("new provider: %v", err)
	}
	now := time.Now()
	provider.now = func() time.Time { return now }

	if _, err := provider.Device(context.Background(), "13800138000"); err != nil {
		t.Fatalf("initial lookup: %v", err)
	}
	stub.setAvailable(false)
	now = now.Add(15 * time.Second)
	if _, err := provider.Device(context.Background(), "13800138000"); err != nil {
		t.Fatalf("expected grace serving: %v", err)
	}

	// control 恢复后必须重新查询并更新档案，而不是继续用旧缓存。
	stub.setAvailable(true)
	stub.mu.Lock()
	stub.device["13800138000"] = "dev-2"
	stub.mu.Unlock()
	now = now.Add(15 * time.Second)
	device, err := provider.Device(context.Background(), "13800138000")
	if err != nil {
		t.Fatalf("lookup after recovery: %v", err)
	}
	if device.ID != "dev-2" {
		t.Fatalf("expected refreshed profile after recovery, got %+v", device)
	}
}

func TestHTTPAuthProviderRejectsUnknownDeviceFromControl(t *testing.T) {
	stub := &authServer{available: true, device: map[string]string{"13800138000": "dev-1"}}
	server := httptest.NewServer(stub.handler())
	defer server.Close()

	provider, err := NewHTTPAuthProvider(server.URL, "token", time.Second, time.Minute, time.Minute)
	if err != nil {
		t.Fatalf("new provider: %v", err)
	}
	_, err = provider.Device(context.Background(), "13900139000")
	if !errors.Is(err, repository.ErrDeviceNotFound) {
		t.Fatalf("expected ErrDeviceNotFound, got %v", err)
	}
}
