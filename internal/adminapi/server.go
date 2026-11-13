// Package adminapi 提供独立的管理面 HTTP 服务：鉴权、限流、会话运维与历史查询。
//
// 设计边界：
//   - 仅在内置管理端口上提供接口，不改变终端接入端口与既有 HTTP API；
//   - 只读取 runtime / repository 暴露的接口，不改造终端消息处理主路径；
//   - 会话与在线状态只存在于进程内，因此运维动作只作用于当前实例。
package adminapi

import (
	"context"
	"errors"
	"net"
	"net/http"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/rs/zerolog/log"

	"github.com/fakeyanss/jt808-server-go/internal/config"
	"github.com/fakeyanss/jt808-server-go/internal/observability"
	"github.com/fakeyanss/jt808-server-go/internal/repository"
	"github.com/fakeyanss/jt808-server-go/internal/runtime"
)

// Dependencies 是管理面读取的运行时与持久化依赖。
// 全部使用接口，便于单测注入内存实现。
type Dependencies struct {
	Conn      runtime.ConnectionRegistry
	Heartbeat runtime.HeartbeatRegistry
	Devices   repository.DeviceRepository
	Locations repository.LocationRepository
	Alarms    repository.AlarmRepository
	// Probes 提供就绪检查与指标采集；nil 时 /readyz 恒为就绪、/metrics 输出零值。
	Probes *observability.Provider
}

// Server 是管理面 HTTP 服务。
type Server struct {
	deps       Dependencies
	engine     *gin.Engine
	httpServer *http.Server
	listener   net.Listener
}

// NewServer 按配置构造管理面路由，不开始监听。
func NewServer(opts config.AdminOptions, deps Dependencies) *Server {
	gin.SetMode(gin.ReleaseMode)
	engine := gin.New()
	engine.Use(recoveryMiddleware())

	engine.GET("/healthz", func(c *gin.Context) {
		c.JSON(http.StatusOK, gin.H{"status": "ok"})
	})

	// 就绪探针公开，供编排系统直接调用；不返回任何业务数据。
	engine.GET("/readyz", func(c *gin.Context) {
		results := deps.Probes.Ready(c.Request.Context())
		checks := make([]gin.H, 0, len(results))
		ready := true
		degraded := false
		for _, result := range results {
			check := gin.H{"name": result.Name, "ok": result.OK()}
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
		c.JSON(status, gin.H{"status": state, "checks": checks})
	})

	limiter := newRateLimiter(opts.RatePerSecond, opts.Burst)
	secured := engine.Group("/")
	secured.Use(authMiddleware(opts.Token), rateLimitMiddleware(limiter, opts.KeyBy))
	secured.GET("/ping", func(c *gin.Context) {
		c.JSON(http.StatusOK, gin.H{"status": "ok"})
	})
	// 指标含连接与命令规模，与管理面其它接口一样要求 Bearer token。
	secured.GET("/metrics", func(c *gin.Context) {
		c.Header("Content-Type", "text/plain; version=0.0.4; charset=utf-8")
		snapshot := deps.Probes.Snapshot(c.Request.Context())
		if err := observability.WritePrometheus(c.Writer, snapshot); err != nil {
			log.Warn().Err(err).Msg("Fail to write metrics response")
		}
	})
	registerSessionRoutes(secured, deps)
	registerHistoryRoutes(secured, deps)

	engine.NoRoute(func(c *gin.Context) {
		writeJSONError(c, http.StatusNotFound, codeNotFound, "route not found")
	})
	engine.NoMethod(func(c *gin.Context) {
		writeJSONError(c, http.StatusMethodNotAllowed, codeBadRequest, "method not allowed")
	})

	serv := &Server{deps: deps, engine: engine}
	serv.httpServer = &http.Server{
		Handler:           engine,
		ReadHeaderTimeout: 5 * time.Second,
		ReadTimeout:       15 * time.Second,
		WriteTimeout:      30 * time.Second,
		IdleTimeout:       60 * time.Second,
	}
	return serv
}

// Handler 返回管理面路由，供单测直接使用 httptest。
func (s *Server) Handler() http.Handler { return s.engine }

// Listen 在配置的地址上开始监听；端口占用等错误在启动阶段直接返回。
func (s *Server) Listen(addr string) error {
	listener, err := net.Listen("tcp", addr)
	if err != nil {
		return err
	}
	s.listener = listener
	return nil
}

// Addr 返回实际监听地址，未监听时返回空串。
func (s *Server) Addr() string {
	if s.listener == nil {
		return ""
	}
	return s.listener.Addr().String()
}

// Start 阻塞运行 HTTP 服务；监听器关闭后返回。
func (s *Server) Start() error {
	if s.listener == nil {
		return errors.New("admin server is not listening")
	}
	log.Info().Str("addr", s.listener.Addr().String()).Msg("Admin API server started")
	if err := s.httpServer.Serve(s.listener); err != nil && !errors.Is(err, http.ErrServerClosed) {
		log.Error().Err(err).Msg("Admin API server stopped unexpectedly")
		return err
	}
	return nil
}

// Stop 在给定超时内优雅关闭管理面，不影响终端连接。
func (s *Server) Stop(ctx context.Context) error {
	if s.httpServer == nil {
		return nil
	}
	return s.httpServer.Shutdown(ctx)
}
