package media

import (
	"fmt"
	"net"
	"os"
	"path/filepath"
	"sync"
	"time"

	"github.com/rs/zerolog/log"

	"github.com/fakeyanss/jt808-server-go/pkg/routines"
)

const (
	// DefaultIdleTimeout 是单路音视频流的空闲回收时长。
	DefaultIdleTimeout = 30 * time.Second
	// maxFrameBytes 限制单帧重组后的最大字节数，防止异常终端占满内存。
	maxFrameBytes = 8 * 1024 * 1024
	// udpReadBuffer 是 UDP 套接字读缓冲，1078 视频突发包较多。
	udpReadBuffer = 4 * 1024 * 1024
)

// Config 是 1078 RTP 接入服务器配置。
type Config struct {
	Dir           string        // H.264 落盘根目录
	MaxConcurrent int           // 并发通道上限
	IdleTimeout   time.Duration // 空闲回收时长
}

func (c Config) normalize() Config {
	if c.Dir == "" {
		c.Dir = "./data/media"
	}
	if c.MaxConcurrent <= 0 {
		c.MaxConcurrent = 16
	}
	if c.IdleTimeout <= 0 {
		c.IdleTimeout = DefaultIdleTimeout
	}
	return c
}

// Server 接收终端按 0x9101 建立的 1078 RTP 音视频流。
//
// 当前实现范围：
//   - 仅支持 UDP（0x9101 的 TCP 推送暂不支持）；
//   - 仅解析 H.264；H.265 暂不支持；
//   - 只把 PS/ES 还原为 H.264 Annex B 并落成 .h264 文件，不做转码、不做分发、不写播放器索引。
type Server struct {
	cfg Config

	conn    *net.UDPConn
	close   sync.Once
	done    chan struct{}
	cleanup sync.WaitGroup

	mu        sync.Mutex
	streams   map[string]*streamState
	lastLimit time.Time
}

type streamState struct {
	assemble *frameAssembler
	sink     *fileSink
	lastSeen time.Time
}

// NewServer 创建 1078 RTP 接入服务器。
func NewServer(cfg Config) *Server {
	s := &Server{
		cfg:     cfg.normalize(),
		done:    make(chan struct{}),
		streams: map[string]*streamState{},
	}
	s.cleanup.Add(1)
	routines.GoSafe(func() {
		defer s.cleanup.Done()
		s.cleanupLoop()
	})
	return s
}

// Listen 绑定 UDP 端口。
func (s *Server) Listen(addr string) error {
	udpAddr, err := net.ResolveUDPAddr("udp", addr)
	if err != nil {
		return fmt.Errorf("resolve media udp addr %s: %w", addr, err)
	}
	conn, err := net.ListenUDP("udp", udpAddr)
	if err != nil {
		return fmt.Errorf("listen media udp addr %s: %w", addr, err)
	}
	_ = conn.SetReadBuffer(udpReadBuffer)
	s.conn = conn
	log.Info().Str("addr", conn.LocalAddr().String()).Str("dir", s.cfg.Dir).
		Int("maxConcurrent", s.cfg.MaxConcurrent).Msg("Media RTP server listening")
	return nil
}

// Addr 返回实际监听地址。
func (s *Server) Addr() string {
	if s.conn == nil {
		return ""
	}
	return s.conn.LocalAddr().String()
}

// Start 进入阻塞式收包循环，由调用方放入独立 goroutine。
func (s *Server) Start() {
	if s.conn == nil {
		log.Error().Msg("Media RTP server is not listening")
		return
	}
	buf := make([]byte, 65535)
	for {
		n, _, err := s.conn.ReadFromUDP(buf)
		if err != nil {
			select {
			case <-s.done:
				return
			default:
			}
			log.Error().Err(err).Msg("Fail to read media RTP packet")
			continue
		}
		s.handlePacket(buf[:n])
	}
}

// Stop 停止收包、回收清理任务并关闭所有落盘文件。
func (s *Server) Stop() {
	s.close.Do(func() {
		close(s.done)
		if s.conn != nil {
			_ = s.conn.Close()
		}
	})
	s.cleanup.Wait()
	s.closeAllStreams()
}

func (s *Server) handlePacket(raw []byte) {
	select {
	case <-s.done:
		return
	default:
	}

	pkt, err := ParsePacket(raw)
	if err != nil {
		log.Debug().Err(err).Msg("Ignore invalid media RTP packet")
		return
	}
	if pkt.DataType < DataTypeIFrame || pkt.DataType > DataTypeBFrame {
		return
	}
	if pkt.PayloadType != PayloadTypeH264 {
		return
	}

	st := s.streamFor(pkt)
	if st == nil {
		return
	}
	st.lastSeen = time.Now()

	frame, err := st.assemble.Push(pkt)
	if err != nil {
		// 缺片/超限：丢弃当前帧，但保留流继续接收后续帧（UDP 丢包属常态）。
		log.Debug().Err(err).Str("phone", pkt.Phone).Uint8("channel", pkt.LogicChannel).
			Msg("Drop truncated media frame")
		return
	}
	if len(frame) == 0 {
		return // 分片未收齐，正常中间状态
	}
	h264, err := ExtractH264(frame)
	if err != nil {
		// 截断/损坏同样只丢帧不断流；不支持的载荷形态也无需写盘。
		log.Debug().Err(err).Str("phone", pkt.Phone).Uint8("channel", pkt.LogicChannel).
			Msg("Drop unusable media frame")
		return
	}
	if len(h264) == 0 {
		// PS 正常结束且无 ES，不是错误。
		return
	}
	if err := st.sink.Write(h264); err != nil {
		log.Warn().Err(err).Str("path", st.sink.Path()).Msg("Fail to write media frame")
	}
}

func (s *Server) streamFor(pkt *Packet) *streamState {
	key := streamKey(pkt.Phone, pkt.LogicChannel)

	s.mu.Lock()
	defer s.mu.Unlock()
	if st, ok := s.streams[key]; ok {
		return st
	}
	if len(s.streams) >= s.cfg.MaxConcurrent {
		now := time.Now()
		if now.Sub(s.lastLimit) >= time.Second {
			s.lastLimit = now
			log.Warn().Str("phone", pkt.Phone).Uint8("channel", pkt.LogicChannel).
				Int("maxConcurrent", s.cfg.MaxConcurrent).Msg("Media stream limit reached, drop packet")
		}
		return nil
	}
	sink, err := newFileSink(s.cfg.Dir, pkt.Phone, pkt.LogicChannel)
	if err != nil {
		log.Error().Err(err).Str("phone", pkt.Phone).Uint8("channel", pkt.LogicChannel).
			Msg("Fail to create media file")
		return nil
	}
	st := &streamState{
		assemble: newFrameAssembler(maxFrameBytes),
		sink:     sink,
		lastSeen: time.Now(),
	}
	s.streams[key] = st
	log.Info().Str("phone", pkt.Phone).Uint8("channel", pkt.LogicChannel).Str("path", sink.Path()).
		Msg("Media stream started")
	return st
}

func (s *Server) cleanupLoop() {
	interval := s.cfg.IdleTimeout / 2
	if interval > time.Minute {
		interval = time.Minute
	}
	ticker := time.NewTicker(interval)
	defer ticker.Stop()
	for {
		select {
		case <-s.done:
			return
		case now := <-ticker.C:
			s.evictIdle(now)
		}
	}
}

func (s *Server) evictIdle(now time.Time) {
	s.mu.Lock()
	deleted := make([]*streamState, 0, len(s.streams))
	for key, st := range s.streams {
		if now.Sub(st.lastSeen) >= s.cfg.IdleTimeout {
			deleted = append(deleted, st)
			delete(s.streams, key)
		}
	}
	s.mu.Unlock()
	for _, st := range deleted {
		if err := st.sink.Close(); err != nil {
			log.Warn().Err(err).Str("path", st.sink.Path()).Msg("Fail to close idle media file")
		}
	}
}

func (s *Server) closeAllStreams() {
	s.mu.Lock()
	all := make([]*streamState, 0, len(s.streams))
	for key, st := range s.streams {
		all = append(all, st)
		delete(s.streams, key)
	}
	s.mu.Unlock()
	for _, st := range all {
		if err := st.sink.Close(); err != nil {
			log.Warn().Err(err).Str("path", st.sink.Path()).Msg("Fail to close media file")
		}
	}
}

func streamKey(phone string, channel uint8) string {
	return fmt.Sprintf("%s/%d", phone, channel)
}

// fileSink 把 H.264 Annex B 数据追加到单个 .h264 文件。
type fileSink struct {
	file *os.File
	path string
}

func newFileSink(baseDir, phone string, channel uint8) (*fileSink, error) {
	if !isDigits(phone) {
		return nil, fmt.Errorf("invalid phone for media file: %q", phone)
	}
	dir := filepath.Join(baseDir, phone, fmt.Sprintf("channel_%d", channel))
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return nil, fmt.Errorf("create media dir %s: %w", dir, err)
	}
	path := filepath.Join(dir, fmt.Sprintf("%d.h264", time.Now().UnixNano()))
	file, err := os.OpenFile(path, os.O_CREATE|os.O_TRUNC|os.O_WRONLY, 0o644)
	if err != nil {
		return nil, fmt.Errorf("create media file %s: %w", path, err)
	}
	return &fileSink{file: file, path: path}, nil
}

func (s *fileSink) Write(data []byte) error {
	_, err := s.file.Write(data)
	return err
}

func (s *fileSink) Close() error {
	return s.file.Close()
}

func (s *fileSink) Path() string {
	return s.path
}

func isDigits(value string) bool {
	if value == "" {
		return false
	}
	for _, c := range value {
		if c < '0' || c > '9' {
			return false
		}
	}
	return true
}
