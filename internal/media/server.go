package media

import (
	"errors"
	"fmt"
	"net"
	"os"
	"path/filepath"
	"strconv"
	"sync"
	"sync/atomic"
	"time"

	"github.com/rs/zerolog/log"

	"github.com/fakeyanss/jt808-server-go/pkg/routines"
)

const (
	// DefaultIdleTimeout 是单路音视频流的空闲回收时长。
	DefaultIdleTimeout = 30 * time.Second
	// DefaultMaxTCPConns 是 1078 TCP 连接数默认上限。
	DefaultMaxTCPConns = 64
	// maxFrameBytes 限制单帧重组后的最大字节数，防止异常终端占满内存。
	maxFrameBytes = 8 * 1024 * 1024
	// udpReadBuffer 是 UDP 套接字读缓冲，1078 视频突发包较多。
	udpReadBuffer = 4 * 1024 * 1024
)

// Config 是 1078 RTP 接入服务器配置。
type Config struct {
	Dir           string        // 音视频落盘根目录
	MaxConcurrent int           // 并发流上限（UDP 与 TCP 合计）
	MaxTCPConns   int           // TCP 连接数上限
	IdleTimeout   time.Duration // 空闲回收时长
	Retention     time.Duration // 落盘文件保留时长；<=0 表示不清理
}

func (c Config) normalize() Config {
	if c.Dir == "" {
		c.Dir = "./data/media"
	}
	if c.MaxConcurrent <= 0 {
		c.MaxConcurrent = 16
	}
	if c.MaxTCPConns <= 0 {
		c.MaxTCPConns = DefaultMaxTCPConns
	}
	if c.IdleTimeout <= 0 {
		c.IdleTimeout = DefaultIdleTimeout
	}
	return c
}

// transportKind 区分 1078 音视频的承载方式。
type transportKind uint8

const (
	transportUDP transportKind = iota
	transportTCP
)

// packetSource 标识一个包的来源；TCP 用连接 ID，UDP 用对端地址。
type packetSource struct {
	transport transportKind
	remote    string
	connID    uint64
}

func (s packetSource) key() string {
	if s.transport == transportTCP {
		return fmt.Sprintf("tcp/%d", s.connID)
	}
	// UDP 只按对端 IP 区分：终端可能在 NAT 下更换源端口，
	// 但同一手机号/通道仍应视为同一路流。
	host := s.remote
	if h, _, err := net.SplitHostPort(s.remote); err == nil {
		host = h
	}
	return "udp/" + host
}

// Server 接收终端按 0x9101 建立的 1078 音视频流。
//
// 支持范围：
//   - UDP 与 TCP 承载，两者独立监听；
//   - H.264 视频、G.711A/U 与 ADTS AAC 音频；
//   - 视频还原为 Annex B H.264 并按 IDR 前置 SPS/PPS；
//   - 不做转码、不做分发、不写播放器索引。
type Server struct {
	cfg Config

	udpConn *net.UDPConn
	tcpLn   *net.TCPListener
	done    chan struct{}
	close   sync.Once
	cleanup sync.WaitGroup
	serve   sync.WaitGroup

	mu         sync.Mutex
	streams    map[string]*streamState
	lastLimit  time.Time
	conns      map[uint64]*tcpConn
	nextConnID uint64

	lastAACWarn atomic.Int64
}

// streamState 是一路“手机号 + 通道 + 来源”的音视频状态。
// 视频与音频各有独立文件，共用同一个文件名主幹。
type streamState struct {
	source  packetSource
	phone   string
	channel uint8
	dir     string
	stem    string

	assemble *frameAssembler

	mu        sync.Mutex
	videoSink *fileSink
	audioSink *fileSink
	audioExt  string

	lastSeen atomic.Int64
}

func (st *streamState) touch() {
	st.lastSeen.Store(time.Now().UnixNano())
}

func (st *streamState) idleFor(now time.Time) time.Duration {
	return now.Sub(time.Unix(0, st.lastSeen.Load()))
}

// writeVideo 应用参数集缓存后写入视频文件；文件按需延迟创建。
func (st *streamState) writeVideo(frame []byte) error {
	st.mu.Lock()
	defer st.mu.Unlock()

	if st.videoSink == nil {
		sink, err := newSink(st.dir, st.phone, st.channel, st.stem, ".h264")
		if err != nil {
			return err
		}
		st.videoSink = sink
	}
	return st.videoSink.Write(frame)
}

// writeAudio 按编码扩展名写入音频文件；编码变化时关闭旧文件并新建。
func (st *streamState) writeAudio(ext string, data []byte) error {
	st.mu.Lock()
	defer st.mu.Unlock()

	if st.audioSink != nil && st.audioExt != ext {
		if err := st.audioSink.Close(); err != nil {
			log.Warn().Err(err).Str("path", st.audioSink.Path()).Msg("Fail to close media audio file")
		}
		st.audioSink = nil
	}
	if st.audioSink == nil {
		sink, err := newSink(st.dir, st.phone, st.channel, st.stem, ext)
		if err != nil {
			return err
		}
		st.audioSink = sink
		st.audioExt = ext
		log.Info().Str("phone", st.phone).Uint8("channel", st.channel).Str("path", sink.Path()).
			Msg("Media audio stream started")
	}
	return st.audioSink.Write(data)
}

func (st *streamState) close() error {
	st.mu.Lock()
	defer st.mu.Unlock()
	var errs []error
	if st.videoSink != nil {
		if err := st.videoSink.Close(); err != nil {
			errs = append(errs, err)
		}
		st.videoSink = nil
	}
	if st.audioSink != nil {
		if err := st.audioSink.Close(); err != nil {
			errs = append(errs, err)
		}
		st.audioSink = nil
	}
	return errors.Join(errs...)
}

// NewServer 创建 1078 音视频接入服务器。
func NewServer(cfg Config) *Server {
	s := &Server{
		cfg:     cfg.normalize(),
		done:    make(chan struct{}),
		streams: map[string]*streamState{},
		conns:   map[uint64]*tcpConn{},
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
	if err := os.MkdirAll(s.cfg.Dir, 0o755); err != nil {
		return fmt.Errorf("create media dir %s: %w", s.cfg.Dir, err)
	}
	udpAddr, err := net.ResolveUDPAddr("udp", addr)
	if err != nil {
		return fmt.Errorf("resolve media udp addr %s: %w", addr, err)
	}
	conn, err := net.ListenUDP("udp", udpAddr)
	if err != nil {
		return fmt.Errorf("listen media udp addr %s: %w", addr, err)
	}
	_ = conn.SetReadBuffer(udpReadBuffer)
	s.udpConn = conn
	log.Info().Str("addr", conn.LocalAddr().String()).Str("transport", "udp").Str("dir", s.cfg.Dir).
		Int("maxConcurrent", s.cfg.MaxConcurrent).Dur("retention", s.cfg.Retention).Msg("Media server listening")
	return nil
}

// ListenTCP 绑定 TCP 端口。TCP 与 UDP 可同时监听。
func (s *Server) ListenTCP(addr string) error {
	if err := os.MkdirAll(s.cfg.Dir, 0o755); err != nil {
		return fmt.Errorf("create media dir %s: %w", s.cfg.Dir, err)
	}
	tcpAddr, err := net.ResolveTCPAddr("tcp", addr)
	if err != nil {
		return fmt.Errorf("resolve media tcp addr %s: %w", addr, err)
	}
	ln, err := net.ListenTCP("tcp", tcpAddr)
	if err != nil {
		return fmt.Errorf("listen media tcp addr %s: %w", addr, err)
	}
	s.tcpLn = ln
	log.Info().Str("addr", ln.Addr().String()).Str("transport", "tcp").Str("dir", s.cfg.Dir).
		Int("maxTCPConns", s.cfg.MaxTCPConns).Dur("retention", s.cfg.Retention).Msg("Media server listening")
	return nil
}

// Addr 返回 UDP 实际监听地址。
func (s *Server) Addr() string {
	if s.udpConn == nil {
		return ""
	}
	return s.udpConn.LocalAddr().String()
}

// TCPAddr 返回 TCP 实际监听地址。
func (s *Server) TCPAddr() string {
	if s.tcpLn == nil {
		return ""
	}
	return s.tcpLn.Addr().String()
}

// Streams 返回当前活跃的媒体流数量，供指标采集。
func (s *Server) Streams() int {
	s.mu.Lock()
	defer s.mu.Unlock()
	return len(s.streams)
}

// Start 进入阻塞式服务循环，由调用方放入独立 goroutine。
func (s *Server) Start() {
	if s.udpConn == nil && s.tcpLn == nil {
		log.Error().Msg("Media server is not listening")
		return
	}
	if s.udpConn != nil {
		s.serve.Add(1)
		routines.GoSafe(func() {
			defer s.serve.Done()
			s.readUDP()
		})
	}
	if s.tcpLn != nil {
		s.serve.Add(1)
		routines.GoSafe(func() {
			defer s.serve.Done()
			s.acceptTCP()
		})
	}
	s.serve.Wait()
}

// Stop 停止收包、回收清理任务并关闭所有落盘文件。
func (s *Server) Stop() {
	s.close.Do(func() {
		close(s.done)
		if s.udpConn != nil {
			_ = s.udpConn.Close()
		}
		if s.tcpLn != nil {
			_ = s.tcpLn.Close()
		}
		s.mu.Lock()
		conns := make([]*tcpConn, 0, len(s.conns))
		for _, c := range s.conns {
			conns = append(conns, c)
		}
		s.mu.Unlock()
		for _, c := range conns {
			_ = c.conn.Close()
		}
	})
	s.serve.Wait()
	s.cleanup.Wait()
	s.closeAllStreams()
}

func (s *Server) readUDP() {
	buf := make([]byte, 65535)
	for {
		n, addr, err := s.udpConn.ReadFromUDP(buf)
		if err != nil {
			select {
			case <-s.done:
				return
			default:
			}
			log.Error().Err(err).Msg("Fail to read media RTP packet")
			continue
		}
		s.handlePacket(buf[:n], packetSource{transport: transportUDP, remote: addr.String()})
	}
}

func (s *Server) handlePacket(raw []byte, source packetSource) {
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

	audioExt := ""
	switch pkt.DataType {
	case DataTypeIFrame, DataTypePFrame, DataTypeBFrame:
		if pkt.PayloadType != PayloadTypeH264 {
			return
		}
	case DataTypeAudio:
		ext, ok := audioExtension(pkt.PayloadType)
		if !ok {
			return
		}
		audioExt = ext
	default:
		// 透传数据不属于音视频流。
		return
	}

	st := s.streamFor(pkt, source)
	if st == nil {
		return
	}
	st.touch()

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

	if pkt.DataType == DataTypeAudio {
		audio, err := ExtractAudio(frame)
		if err != nil {
			log.Debug().Err(err).Str("phone", pkt.Phone).Uint8("channel", pkt.LogicChannel).
				Msg("Drop unusable media audio frame")
			return
		}
		if len(audio) == 0 {
			return
		}
		if audioExt == ".aac" && !hasADTS(audio) {
			s.warnAACWithoutADTS(pkt.Phone, pkt.LogicChannel)
			return
		}
		if err := st.writeAudio(audioExt, audio); err != nil {
			log.Warn().Err(err).Str("path", st.stem).Msg("Fail to write media audio frame")
		}
		return
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
	if err := st.writeVideo(h264); err != nil {
		log.Warn().Err(err).Str("stem", st.stem).Msg("Fail to write media frame")
	}
}

func (s *Server) streamFor(pkt *Packet, source packetSource) *streamState {
	key := streamKey(pkt.Phone, pkt.LogicChannel, source)

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

	st := &streamState{
		source:   source,
		phone:    pkt.Phone,
		channel:  pkt.LogicChannel,
		dir:      s.cfg.Dir,
		stem:     strconv.FormatInt(time.Now().UnixNano(), 10),
		assemble: newFrameAssembler(maxFrameBytes),
	}
	st.touch()
	s.streams[key] = st
	log.Info().Str("phone", pkt.Phone).Uint8("channel", pkt.LogicChannel).
		Str("transport", source.key()).Msg("Media stream started")
	return st
}

func (s *Server) cleanupLoop() {
	interval := s.cfg.IdleTimeout / 2
	if interval > time.Minute {
		interval = time.Minute
	}
	ticker := time.NewTicker(interval)
	defer ticker.Stop()
	var lastPrune time.Time
	for {
		select {
		case <-s.done:
			return
		case now := <-ticker.C:
			s.evictIdle(now)
			// 每小时扫一次落盘目录；保留期为 0 时不清理。
			if s.cfg.Retention > 0 && (lastPrune.IsZero() || now.Sub(lastPrune) >= time.Hour) {
				lastPrune = now
				s.pruneExpired(now)
			}
		}
	}
}

// prunableMediaExt 判断扩展名是否属于实时流落盘文件。
func prunableMediaExt(ext string) bool {
	switch ext {
	case ".h264", ".g711a", ".g711u", ".aac":
		return true
	default:
		return false
	}
}

// pruneExpired 删除超过保留期且不在写入中的音视频文件。
// 正在写入的流即使修改时间较旧也跳过，避免删除已打开的文件。
func (s *Server) pruneExpired(now time.Time) {
	if s.cfg.Retention <= 0 {
		return
	}
	cutoff := now.Add(-s.cfg.Retention)
	active := s.activeSinkPaths()

	removed := 0
	walkErr := filepath.WalkDir(s.cfg.Dir, func(path string, entry os.DirEntry, err error) error {
		if err != nil {
			log.Warn().Err(err).Str("path", path).Msg("Fail to scan media dir")
			return nil
		}
		if entry.IsDir() || !prunableMediaExt(filepath.Ext(entry.Name())) {
			return nil
		}
		if _, ok := active[path]; ok {
			return nil
		}
		info, infoErr := entry.Info()
		if infoErr != nil {
			return nil
		}
		if !info.ModTime().Before(cutoff) {
			return nil
		}
		if removeErr := os.Remove(path); removeErr != nil {
			log.Warn().Err(removeErr).Str("path", path).Msg("Fail to prune expired media file")
			return nil
		}
		removed++
		return nil
	})
	if walkErr != nil {
		log.Warn().Err(walkErr).Str("dir", s.cfg.Dir).Msg("Fail to walk media dir")
	}
	if removed > 0 {
		log.Info().Int("removed", removed).Str("dir", s.cfg.Dir).Msg("Pruned expired media files")
	}
}

// activeSinkPaths 返回所有正在写入的视频与音频文件路径。
func (s *Server) activeSinkPaths() map[string]struct{} {
	s.mu.Lock()
	states := make([]*streamState, 0, len(s.streams))
	for _, st := range s.streams {
		states = append(states, st)
	}
	s.mu.Unlock()

	active := make(map[string]struct{}, len(states)*2)
	for _, st := range states {
		st.mu.Lock()
		if st.videoSink != nil {
			active[st.videoSink.Path()] = struct{}{}
		}
		if st.audioSink != nil {
			active[st.audioSink.Path()] = struct{}{}
		}
		st.mu.Unlock()
	}
	return active
}

func (s *Server) evictIdle(now time.Time) {
	s.mu.Lock()
	expired := make([]*streamState, 0, len(s.streams))
	for key, st := range s.streams {
		if st.idleFor(now) >= s.cfg.IdleTimeout {
			expired = append(expired, st)
			delete(s.streams, key)
		}
	}
	s.mu.Unlock()
	for _, st := range expired {
		if err := st.close(); err != nil {
			log.Warn().Err(err).Str("phone", st.phone).Uint8("channel", st.channel).
				Msg("Fail to close idle media file")
		}
	}
}

// closeConnectionStreams 在 TCP 连接断开时立即收尾该连接上的所有流。
// 空闲回收按流判定，因此同一条连接上的其他通道仍在活跃时不会被误杀。
func (s *Server) closeConnectionStreams(connID uint64) {
	s.mu.Lock()
	closed := make([]*streamState, 0)
	for key, st := range s.streams {
		if st.source.transport == transportTCP && st.source.connID == connID {
			closed = append(closed, st)
			delete(s.streams, key)
		}
	}
	s.mu.Unlock()
	for _, st := range closed {
		if err := st.close(); err != nil {
			log.Warn().Err(err).Str("phone", st.phone).Uint8("channel", st.channel).
				Msg("Fail to close media file on connection close")
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
		if err := st.close(); err != nil {
			log.Warn().Err(err).Str("phone", st.phone).Uint8("channel", st.channel).
				Msg("Fail to close media file")
		}
	}
}

func (s *Server) warnAACWithoutADTS(phone string, channel uint8) {
	now := time.Now().UnixNano()
	last := s.lastAACWarn.Load()
	if now-last < int64(30*time.Second) {
		return
	}
	if s.lastAACWarn.CompareAndSwap(last, now) {
		log.Warn().Str("phone", phone).Uint8("channel", channel).
			Msg("Discard AAC frame without ADTS header")
	}
}

// audioExtension 把 1078 音频载荷类型映射为落盘扩展名。
func audioExtension(payloadType uint8) (string, bool) {
	switch payloadType {
	case PayloadTypeG711A:
		return ".g711a", true
	case PayloadTypeG711U:
		return ".g711u", true
	case PayloadTypeAAC:
		return ".aac", true
	default:
		return "", false
	}
}

// hasADTS 检查 AAC 载荷是否带 ADTS 同步字（12 位 0xFFF）。
func hasADTS(data []byte) bool {
	return len(data) >= 7 && data[0] == 0xFF && data[1]&0xF0 == 0xF0
}

func streamKeyBase(phone string, channel uint8) string {
	return fmt.Sprintf("%s/%d", phone, channel)
}

func streamKey(phone string, channel uint8, source packetSource) string {
	return streamKeyBase(phone, channel) + "/" + source.key()
}

// fileSink 把音视频数据追加到单个文件。
type fileSink struct {
	file *os.File
	path string
}

// newSink 在 <baseDir>/<phone>/channel_<n>/ 下创建 <stem><ext>。
func newSink(baseDir, phone string, channel uint8, stem, ext string) (*fileSink, error) {
	if !isDigits(phone) {
		return nil, fmt.Errorf("invalid phone for media file: %q", phone)
	}
	dir := filepath.Join(baseDir, phone, fmt.Sprintf("channel_%d", channel))
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return nil, fmt.Errorf("create media dir %s: %w", dir, err)
	}
	path := filepath.Join(dir, stem+ext)
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

// tcpConn 是一条 1078 媒体 TCP 连接。
type tcpConn struct {
	id     uint64
	conn   net.Conn
	remote string
}

// readTCP 读取一条连接并把完整帧交给 handle；抽成独立函数便于测试分帧器。
func readTCP(conn net.Conn, scanner *frameScanner, handle func([]byte)) error {
	buf := make([]byte, 64*1024)
	for {
		n, err := conn.Read(buf)
		if n > 0 {
			for _, raw := range scanner.Push(buf[:n]) {
				handle(raw)
			}
		}
		if err != nil {
			return err
		}
	}
}
