package observability

import (
	"context"
	"encoding/json"
	"errors"
	"net"
	"net/http"
	"time"

	"github.com/rs/zerolog/log"
)

// ProbeServer 是独立的存活/就绪探针监听。
//
// 只暴露 /healthz 与 /readyz：前者表示进程活着，后者逐个执行 Provider 的
// Checks 并反映真实依赖。不承载业务接口，也不做鉴权。
type ProbeServer struct {
	server   *http.Server
	listener net.Listener
}

// ListenProbes 在给定地址上监听探针端口；端口占用等错误在启动阶段返回。
func ListenProbes(addr string, provider *Provider) (*ProbeServer, error) {
	listener, err := net.Listen("tcp", addr)
	if err != nil {
		return nil, err
	}
	mux := http.NewServeMux()
	mux.HandleFunc("/healthz", func(w http.ResponseWriter, _ *http.Request) {
		writeProbeJSON(w, http.StatusOK, map[string]any{"status": "ok"})
	})
	mux.HandleFunc("/readyz", func(w http.ResponseWriter, r *http.Request) {
		results := provider.Ready(r.Context())
		checks := make([]map[string]any, 0, len(results))
		ready := true
		degraded := false
		for _, result := range results {
			check := map[string]any{"name": result.Name, "ok": result.OK()}
			if result.Err != nil {
				check["error"] = result.Err.Error()
				if result.IsDegraded() {
					check["degraded"] = true
					degraded = true
				} else {
					ready = false
				}
			}
			checks = append(checks, check)
		}
		status := http.StatusOK
		state := "ready"
		if !ready {
			status = http.StatusServiceUnavailable
			state = "not ready"
		} else if degraded {
			state = "degraded"
		}
		writeProbeJSON(w, status, map[string]any{"status": state, "checks": checks})
	})
	server := &http.Server{
		Handler:           mux,
		ReadHeaderTimeout: 5 * time.Second,
		ReadTimeout:       15 * time.Second,
		WriteTimeout:      15 * time.Second,
		IdleTimeout:       60 * time.Second,
	}
	log.Info().Str("addr", listener.Addr().String()).Msg("Probe HTTP server listening")
	return &ProbeServer{server: server, listener: listener}, nil
}

// Addr 返回实际监听地址。
func (s *ProbeServer) Addr() string {
	if s == nil || s.listener == nil {
		return ""
	}
	return s.listener.Addr().String()
}

// Start 阻塞运行探针服务；监听器关闭后返回。
func (s *ProbeServer) Start() error {
	if s == nil || s.listener == nil {
		return errors.New("probe server is not listening")
	}
	if err := s.server.Serve(s.listener); err != nil && !errors.Is(err, http.ErrServerClosed) {
		log.Error().Err(err).Msg("Probe HTTP server stopped unexpectedly")
		return err
	}
	return nil
}

// Stop 在给定超时内关闭探针服务。
func (s *ProbeServer) Stop(ctx context.Context) error {
	if s == nil || s.server == nil {
		return nil
	}
	return s.server.Shutdown(ctx)
}

func writeProbeJSON(w http.ResponseWriter, status int, body any) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(body)
}
