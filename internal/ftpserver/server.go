// Package ftpserver 实现 JT/T 1078 文件上传使用的 FTP 服务端。
//
// 设计边界：
//   - 只支持 PASV/EPSV，明确拒绝 PORT/EPRT，避免服务端反向连终端；
//   - 上传内容是终端录像的不透明字节，不转封装、不解析厂商文件名；
//   - 切片写入与苏标附件共用同一对象存储契约（Put → Promote → MarkCommitted），
//     key 自带终端、流水号、文件名、偏移与段序号，扫描即可重建进度；
//   - 不连数据库：登录校验与进度上报都是节点到 control 的出站请求。
package ftpserver

import (
	"context"
	"fmt"
	"net"
	"strings"
	"sync"
	"sync/atomic"
	"time"

	"github.com/rs/zerolog/log"

	"github.com/fakeyanss/jt808-server-go/internal/clock"
	"github.com/fakeyanss/jt808-server-go/internal/contracts"
	"github.com/fakeyanss/jt808-server-go/internal/observability"
)

const (
	// DefaultControlPort 是 FTP 控制连接默认端口。
	DefaultControlPort = 7500
	// DefaultPassiveFrom/DefaultPassiveTo 是被动数据连接的默认端口段。
	DefaultPassiveFrom = 7501
	DefaultPassiveTo   = 7510
	// DefaultSegmentSize 是单个对象切片默认大小（8 MiB）。
	DefaultSegmentSize = 8 * 1024 * 1024
	// DefaultIdleTimeout 是控制连接无命令的回收阈值。
	DefaultIdleTimeout = 5 * time.Minute
	// DefaultDataTimeout 是等待数据连接建立的超时。
	DefaultDataTimeout = 30 * time.Second
	// DefaultMaxStagingBytes 是本地暂存区默认上限（2 GiB）。
	DefaultMaxStagingBytes = 2 * 1024 * 1024 * 1024

	partSuffix = ".part"
)

// Config 是 FTP 服务端的运行参数。
type Config struct {
	Dir             string
	ControlPort     uint16
	PassivePorts    []int
	AdvertiseIP     string
	NodeID          string
	SegmentSize     int64
	MaxStagingBytes int64
	MaxConcurrent   int
	IdleTimeout     time.Duration
	DataTimeout     time.Duration

	Blob    contracts.BlobStore
	Control Control
	Clock   clock.Clock
	Metrics *observability.Metrics
}

// Server 是 FTP 控制端：监听、会话登记与 0x9207 下发。
type Server struct {
	cfg Config

	mu        sync.Mutex
	sessions  map[*session]struct{}
	transfers map[string]*transferHandle
	lastSeq   map[string]uint32
	// taskStates 记住没有活跃传输时的控制状态：暂停后再登录仍应保持暂停，取消后拒绝新传。
	taskStates  map[string]taskState
	ln          net.Listener
	passiveNext atomic.Uint32
	staging     atomic.Int64
	active      atomic.Int64

	reportCh     chan contracts.AttachmentProgress
	reportDone   chan struct{}
	reportClosed atomic.Bool
	reportWG     sync.WaitGroup

	wg   sync.WaitGroup
	done chan struct{}
	once sync.Once
}

type transferHandle struct {
	upload *upload
	data   net.Conn
}

// taskState 是控制指令在任务级留下的状态，独立于当前是否已有传输。
type taskState struct {
	paused    bool
	cancelled bool
}

// New 创建 FTP 服务端；Blob 与 Control 都是必需依赖。
func New(cfg Config) (*Server, error) {
	if cfg.Blob == nil {
		return nil, fmt.Errorf("ftpserver: blob store is required")
	}
	if cfg.Control == nil {
		return nil, fmt.Errorf("ftpserver: control channel is required")
	}
	if strings.TrimSpace(cfg.Dir) == "" {
		return nil, fmt.Errorf("ftpserver: staging dir is required")
	}
	if cfg.SegmentSize <= 0 {
		cfg.SegmentSize = DefaultSegmentSize
	}
	if cfg.MaxStagingBytes <= 0 {
		cfg.MaxStagingBytes = DefaultMaxStagingBytes
	}
	if cfg.IdleTimeout <= 0 {
		cfg.IdleTimeout = DefaultIdleTimeout
	}
	if cfg.DataTimeout <= 0 {
		cfg.DataTimeout = DefaultDataTimeout
	}
	if len(cfg.PassivePorts) == 0 {
		for port := DefaultPassiveFrom; port <= DefaultPassiveTo; port++ {
			cfg.PassivePorts = append(cfg.PassivePorts, port)
		}
	}
	if cfg.Clock == nil {
		cfg.Clock = clock.System()
	}
	s := &Server{
		cfg:        cfg,
		sessions:   make(map[*session]struct{}),
		transfers:  make(map[string]*transferHandle),
		lastSeq:    make(map[string]uint32),
		taskStates: make(map[string]taskState),
		reportCh:   make(chan contracts.AttachmentProgress, 256),
		reportDone: make(chan struct{}),
		done:       make(chan struct{}),
	}
	s.startReporter()
	return s, nil
}

func (s *Server) now() time.Time { return s.cfg.Clock.Now() }

func (s *Server) segmentSize() int64 { return s.cfg.SegmentSize }

// Listen 监听 FTP 控制端口；addr 为空时按配置端口监听。
func (s *Server) Listen(addr string) error {
	if addr == "" {
		addr = fmt.Sprintf(":%d", s.cfg.ControlPort)
	}
	ln, err := net.Listen("tcp", addr)
	if err != nil {
		return fmt.Errorf("listen ftp control: %w", err)
	}
	s.ln = ln
	return nil
}

// Addr 返回控制连接实际监听地址。
func (s *Server) Addr() string {
	if s.ln == nil {
		return ""
	}
	return s.ln.Addr().String()
}

// Start 接收控制连接，直到 Stop。
func (s *Server) Start() {
	if s.ln == nil {
		return
	}
	s.wg.Add(1)
	defer s.wg.Done()
	for {
		conn, err := s.ln.Accept()
		if err != nil {
			select {
			case <-s.done:
				return
			default:
			}
			if ne, ok := err.(net.Error); ok && ne.Timeout() {
				continue
			}
			log.Warn().Err(err).Msg("FTP control accept failed")
			return
		}
		sess := newSession(s, conn)
		s.mu.Lock()
		s.sessions[sess] = struct{}{}
		s.mu.Unlock()
		s.wg.Add(1)
		go func() {
			defer s.wg.Done()
			sess.serve()
			s.mu.Lock()
			delete(s.sessions, sess)
			s.mu.Unlock()
		}()
	}
}

// Stop 关闭监听与全部会话，等待协程退出。
func (s *Server) Stop() {
	s.once.Do(func() {
		close(s.done)
		if s.ln != nil {
			_ = s.ln.Close()
		}
		s.mu.Lock()
		for sess := range s.sessions {
			_ = sess.conn.Close()
		}
		for _, handle := range s.transfers {
			if handle.upload != nil {
				handle.upload.cancel()
			}
			if handle.data != nil {
				_ = handle.data.Close()
			}
		}
		s.mu.Unlock()
		s.stopReporter()
	})
	s.wg.Wait()
}

// ActiveTransfers 返回当前进行中的上传数量。
func (s *Server) ActiveTransfers() int64 { return s.active.Load() }

// ApplyControl 应用 control 推来的暂停/继续/取消指令。
//
// 同一动作可能重复推送：lastSeq 保证只有更新的序号才生效。
func (s *Server) ApplyControl(req ControlCommand) bool {
	key := taskKey(req.Phone, req.Serial)
	s.mu.Lock()
	if req.Seq <= s.lastSeq[key] {
		s.mu.Unlock()
		return false
	}
	s.lastSeq[key] = req.Seq
	state := s.taskStates[key]
	switch req.Action {
	case ControlPause:
		state.paused = true
	case ControlResume:
		state.paused = false
	case ControlCancel:
		state.cancelled = true
	}
	s.taskStates[key] = state
	handle := s.transfers[key]
	s.mu.Unlock()
	if handle == nil || handle.upload == nil {
		return true
	}
	switch req.Action {
	case ControlPause:
		handle.upload.pause()
	case ControlResume:
		handle.upload.resume()
	case ControlCancel:
		handle.upload.cancel()
		if handle.data != nil {
			_ = handle.data.Close()
		}
	}
	return true
}

// taskControlState 返回任务级控制状态；取消优先于暂停。
func (s *Server) taskControlState(key string) (paused, cancelled bool) {
	s.mu.Lock()
	defer s.mu.Unlock()
	state := s.taskStates[key]
	return state.paused, state.cancelled
}

// ControlCommand 是 control 推送到节点的指令。
type ControlCommand struct {
	Phone  string `json:"phone"`
	Serial uint16 `json:"serial"`
	Action uint8  `json:"action"`
	Seq    uint32 `json:"seq"`
}

// 控制动作，与 0x9207 同值。
const (
	ControlPause  uint8 = 0
	ControlResume uint8 = 1
	ControlCancel uint8 = 2
)

func (s *Server) beginTransfer(key string, up *upload, data net.Conn) bool {
	max := s.cfg.MaxConcurrent
	if max > 0 && s.active.Load() >= int64(max) {
		return false
	}
	s.mu.Lock()
	s.transfers[key] = &transferHandle{upload: up, data: data}
	s.mu.Unlock()
	s.active.Add(1)
	return true
}

func (s *Server) endTransfer(key string) {
	s.mu.Lock()
	delete(s.transfers, key)
	s.mu.Unlock()
	s.active.Add(-1)
}

func (s *Server) reserveStaging(size int64) bool {
	if size <= 0 {
		return true
	}
	for {
		current := s.staging.Load()
		if s.cfg.MaxStagingBytes > 0 && current+size > s.cfg.MaxStagingBytes {
			return false
		}
		if s.staging.CompareAndSwap(current, current+size) {
			return true
		}
	}
}

func (s *Server) releaseStaging(size int64) {
	if size <= 0 {
		return
	}
	for {
		current := s.staging.Load()
		next := current - size
		if next < 0 {
			next = 0
		}
		if s.staging.CompareAndSwap(current, next) {
			return
		}
	}
}

func (s *Server) startReporter() {
	s.reportWG.Add(1)
	go func() {
		defer s.reportWG.Done()
		for {
			select {
			case <-s.reportDone:
				return
			case progress := <-s.reportCh:
				ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
				err := s.cfg.Control.Report(ctx, progress)
				cancel()
				if err != nil {
					log.Warn().Err(err).Str("phone", progress.Phone).Str("file", progress.FileName).
						Str("status", progress.Status).Msg("Fail to report ftp progress to control")
				}
			}
		}
	}()
}

func (s *Server) enqueueProgress(progress contracts.AttachmentProgress) {
	if s.reportClosed.Load() {
		return
	}
	select {
	case s.reportCh <- progress:
	default:
		log.Warn().Str("phone", progress.Phone).Str("file", progress.FileName).
			Msg("FTP progress queue is full; drop snapshot and rely on object scan")
	}
}

func (s *Server) stopReporter() {
	if s.reportClosed.Swap(true) {
		return
	}
	close(s.reportDone)
	s.reportWG.Wait()
}

func taskKey(phone string, serial uint16) string {
	return fmt.Sprintf("%s\x00%d", phone, serial)
}

// openPassive 在配置的端口段内轮转尝试，避免固定单端口被占满。
func (s *Server) openPassive() (net.Listener, int, error) {
	ports := s.cfg.PassivePorts
	if len(ports) == 0 {
		return nil, 0, fmt.Errorf("ftpserver: no passive ports configured")
	}
	for attempt := 0; attempt < len(ports); attempt++ {
		index := int(s.passiveNext.Add(1)-1) % len(ports)
		port := ports[index]
		ln, err := net.Listen("tcp", fmt.Sprintf(":%d", port))
		if err == nil {
			return ln, port, nil
		}
	}
	return nil, 0, fmt.Errorf("ftpserver: passive port range exhausted")
}

// advertiseIP 返回 PASV 应答中的地址：优先配置值，否则取控制连接的本地地址。
func (s *Server) advertiseIP(local net.Addr) string {
	if ip := strings.TrimSpace(s.cfg.AdvertiseIP); ip != "" {
		return ip
	}
	if host, _, err := net.SplitHostPort(local.String()); err == nil && host != "" {
		return host
	}
	return "127.0.0.1"
}
