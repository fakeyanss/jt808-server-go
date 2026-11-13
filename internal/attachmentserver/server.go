// Package attachmentserver 是独立附件节点的装配层。
//
// 它复用 alarmserver 的协议接入实现（0x7e 转义、校验、0x1210/0x1211/0x1212），
// 但存储走对象存储模式：本地只保留有界暂存窗口，按段提交到全集群共享的 objectStore。
//
// 附件节点不连数据库：水位、段列表与终态由 control 落库，节点通过出站 HTTP
// 上报进度并在重连时向 control 查询恢复点；control 不可达时退回对象存储扫描。
package attachmentserver

import (
	"context"
	"encoding/json"
	"fmt"
	"net"
	"net/http"
	"strconv"
	"sync"
	"time"

	"github.com/rs/zerolog/log"

	"github.com/fakeyanss/jt808-server-go/internal/alarmserver"
	"github.com/fakeyanss/jt808-server-go/internal/contracts"
	"github.com/fakeyanss/jt808-server-go/internal/storage"
	"github.com/fakeyanss/jt808-server-go/pkg/routines"
)

// Config 是附件节点的运行参数。
type Config struct {
	Dir            string
	TCPPort        uint16
	UDPPort        uint16
	AdvertiseIP    string
	NodeID         string
	MaxFileSize    int64
	MaxConcurrent  int
	SessionTimeout time.Duration
	CompletedTTL   time.Duration
	Retention      time.Duration

	Blob contracts.BlobStore
	// Progress 是向 control 上报进度/查询恢复点的出站通道；为空时只写对象存储。
	Progress contracts.AttachmentProgressSink
	// StagingDir 是本地暂存根目录；集群下必须有足够空间容纳有界窗口。
	StagingDir string
	// SegmentSize 是单个对象切片大小。
	SegmentSize int64
	// MaxStagingBytes 是整个暂存区上限；到顶拒绝新的文件登记。
	MaxStagingBytes int64

	HTTPAddr string
}

// Server 是附件节点：协议监听 + 内部状态 HTTP。
type Server struct {
	cfg   Config
	alarm *alarmserver.Server
	http  *http.Server
	ln    net.Listener
	done  chan struct{}
	once  sync.Once
	wg    sync.WaitGroup
}

// New 创建附件节点。
func New(cfg Config) (*Server, error) {
	if cfg.Blob == nil {
		return nil, fmt.Errorf("attachment blob store is required: cluster attachment writes to the shared object store")
	}
	if cfg.StagingDir == "" {
		return nil, fmt.Errorf("attachment staging dir is required")
	}
	alarmCfg := alarmserver.Config{
		Dir:            cfg.Dir,
		MaxFileSize:    cfg.MaxFileSize,
		MaxConcurrent:  cfg.MaxConcurrent,
		SessionTimeout: cfg.SessionTimeout,
		CompletedTTL:   cfg.CompletedTTL,
		Retention:      cfg.Retention,
		Storage: &alarmserver.StorageConfig{
			Blob:            cfg.Blob,
			Progress:        cfg.Progress,
			Source:          storage.SourceSubia,
			SegmentSize:     cfg.SegmentSize,
			StagingDir:      cfg.StagingDir,
			MaxStagingBytes: cfg.MaxStagingBytes,
			NodeID:          cfg.NodeID,
		},
	}
	return &Server{
		cfg:   cfg,
		alarm: alarmserver.NewServer(alarmCfg),
		done:  make(chan struct{}),
	}, nil
}

// AlarmServer 返回底层协议服务器，供测试或装配层读取监听地址。
func (s *Server) AlarmServer() *alarmserver.Server { return s.alarm }

// Listen 监听 TCP 附件端口。
func (s *Server) Listen(addr string) error {
	return s.alarm.Listen(addr)
}

// ListenUDP 监听 UDP 附件端口。
func (s *Server) ListenUDP(addr string) error {
	return s.alarm.ListenUDP(addr)
}

// ListenHTTP 监听内部状态端口。
func (s *Server) ListenHTTP(addr string) error {
	listener, err := net.Listen("tcp", addr)
	if err != nil {
		return fmt.Errorf("listen attachment internal http: %w", err)
	}
	s.ln = listener
	s.http = &http.Server{Handler: s.routes(), ReadHeaderTimeout: 5 * time.Second}
	return nil
}

// HTTPAddr 返回内部 HTTP 的实际监听地址。
func (s *Server) HTTPAddr() string {
	if s.ln == nil {
		return ""
	}
	return s.ln.Addr().String()
}

// Start 启动协议监听与内部 HTTP，由调用方放入独立 goroutine。
func (s *Server) Start() {
	if s.ln != nil {
		s.wg.Add(1)
		routines.GoSafe(func() {
			defer s.wg.Done()
			if err := s.http.Serve(s.ln); err != nil && err != http.ErrServerClosed {
				log.Error().Err(err).Msg("Attachment internal HTTP stopped")
			}
		})
	}
	s.alarm.Start()
}

// Stop 关闭协议监听与内部 HTTP。
func (s *Server) Stop() {
	s.once.Do(func() {
		close(s.done)
		if s.http != nil {
			ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
			_ = s.http.Shutdown(ctx)
			cancel()
		}
		s.alarm.Stop()
	})
	s.wg.Wait()
}

// ActiveFiles 返回当前接收中的附件数量，用于状态接口与探针。
//
// 统计的是本进程的活动会话，不是共享库里其它节点正在接收的文件；
// 这样多附件节点部署时每个节点的剩余额度互不干扰。
func (s *Server) ActiveFiles(_ context.Context) (int, error) {
	return s.alarm.Assembler().ActiveReceiving(), nil
}

func (s *Server) routes() http.Handler {
	mux := http.NewServeMux()
	mux.HandleFunc("/internal/attachment/status", s.handleStatus)
	return mux
}

func (s *Server) handleStatus(w http.ResponseWriter, r *http.Request) {
	active, err := s.ActiveFiles(r.Context())
	if err != nil {
		writeJSON(w, http.StatusServiceUnavailable, map[string]any{"err": err.Error()})
		return
	}
	maxConcurrent := s.cfg.MaxConcurrent
	if maxConcurrent <= 0 {
		maxConcurrent = alarmserver.DefaultMaxConcurrent
	}
	available := maxConcurrent - active
	if available < 0 {
		available = 0
	}
	writeJSON(w, http.StatusOK, map[string]any{
		"nodeId":        s.cfg.NodeID,
		"active":        active,
		"maxConcurrent": maxConcurrent,
		"available":     available,
		"advertiseIp":   s.advertiseIP(),
		"tcpPort":       s.tcpPort(),
		"udpPort":       s.udpPort(),
		"stagingDir":    s.cfg.StagingDir,
	})
}

func (s *Server) advertiseIP() string {
	if s.cfg.AdvertiseIP != "" {
		return s.cfg.AdvertiseIP
	}
	addr := s.alarm.Addr()
	if host, _, err := net.SplitHostPort(addr); err == nil && host != "" {
		return host
	}
	return "127.0.0.1"
}

func (s *Server) tcpPort() uint16 {
	if s.cfg.TCPPort != 0 {
		return s.cfg.TCPPort
	}
	return portOf(s.alarm.Addr())
}

func (s *Server) udpPort() uint16 {
	if s.cfg.UDPPort != 0 {
		return s.cfg.UDPPort
	}
	return portOf(s.alarm.UDPAddr())
}

func portOf(addr string) uint16 {
	_, portText, err := net.SplitHostPort(addr)
	if err != nil {
		return 0
	}
	parsed, err := strconv.Atoi(portText)
	if err != nil || parsed < 1 || parsed > 65535 {
		return 0
	}
	return uint16(parsed)
}

func writeJSON(w http.ResponseWriter, status int, body any) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(body)
}
