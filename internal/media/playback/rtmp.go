package playback

import (
	"bufio"
	"bytes"
	"encoding/binary"
	"errors"
	"fmt"
	"io"
	"math"
	"math/rand"
	"net"
	"net/url"
	"strings"
	"sync"
	"time"
)

const (
	// DefaultRTMPQueueFrames 是转推队列长度；队列满时丢 P 帧而不是阻塞收流。
	DefaultRTMPQueueFrames = 120
	defaultRTMPChunkSize   = 4096
)

// RTMPConfig 是桥接 SRS/lal 的转推配置。
type RTMPConfig struct {
	// URL 是 RTMP 接入地址，例如 rtmp://127.0.0.1:1935/live。
	URL string
	// StreamPrefix 是流名前缀，最终流名为 prefix + sessionID。
	StreamPrefix string
	// PlayURLPrefix 是外部媒体服务器对观众的播放地址前缀，
	// 例如 http://127.0.0.1:8080/live；为空时不返回播放端点。
	PlayURLPrefix string
	DialTimeout   time.Duration
	WriteTimeout  time.Duration
	QueueFrames   int
}

func (c RTMPConfig) normalize() RTMPConfig {
	if c.DialTimeout <= 0 {
		c.DialTimeout = 5 * time.Second
	}
	if c.WriteTimeout <= 0 {
		c.WriteTimeout = 10 * time.Second
	}
	if c.QueueFrames <= 0 {
		c.QueueFrames = DefaultRTMPQueueFrames
	}
	c.URL = strings.TrimRight(strings.TrimSpace(c.URL), "/")
	c.PlayURLPrefix = strings.TrimRight(strings.TrimSpace(c.PlayURLPrefix), "/")
	return c
}

// RTMPFactory 创建 RTMP 转推实现。
type RTMPFactory struct {
	cfg RTMPConfig
}

// NewRTMPFactory 校验配置并创建工厂。
func NewRTMPFactory(cfg RTMPConfig) (*RTMPFactory, error) {
	cfg = cfg.normalize()
	if cfg.URL == "" {
		return nil, errors.New("playback.rtmp.url is required")
	}
	parsed, err := url.Parse(cfg.URL)
	if err != nil || parsed.Scheme != "rtmp" || parsed.Host == "" {
		return nil, fmt.Errorf("playback.rtmp.url must be rtmp://host[:port]/app, got %q", cfg.URL)
	}
	if strings.Trim(parsed.Path, "/") == "" {
		return nil, fmt.Errorf("playback.rtmp.url must include an app path, got %q", cfg.URL)
	}
	return &RTMPFactory{cfg: cfg}, nil
}

// NewPublisher 创建一路会话的 RTMP 转推。
func (f *RTMPFactory) NewPublisher() Publisher {
	return &rtmpPublisher{cfg: f.cfg, done: make(chan struct{}), wake: make(chan struct{}, 1)}
}

type rtmpMessage struct {
	typ  byte
	ts   uint32
	body []byte
}

type rtmpPublisher struct {
	cfg RTMPConfig

	mu       sync.Mutex
	session  Session
	stream   string
	endpoint Endpoint
	publish  Endpoint
	queue    chan rtmpMessage
	timeline Timeline
	desync   bool
	params   ParamSets
	audioCfg []byte
	state    State
	done     chan struct{}
	wake     chan struct{}
	closeOne sync.Once
}

// Start 启动转推协程并返回发布/播放端点。
//
// 连接失败不影响 Start：转推在后台按退避重连，自研 flv 出口仍可工作。
func (p *rtmpPublisher) Start(session Session, params ParamSets, _ FrameSource) ([]Endpoint, error) {
	if session.ID == "" {
		return nil, errors.New("playback: session id is required")
	}
	stream := p.cfg.StreamPrefix + session.ID
	p.mu.Lock()
	p.session = session
	p.stream = stream
	p.params = params.Clone()
	p.queue = make(chan rtmpMessage, p.cfg.QueueFrames)
	p.timeline.Reset()
	p.desync = true
	p.state = StatePublishing
	p.publish = Endpoint{Protocol: "rtmp", URL: p.cfg.URL + "/" + stream}
	if p.cfg.PlayURLPrefix != "" {
		p.endpoint = Endpoint{Protocol: "http-flv", URL: p.cfg.PlayURLPrefix + "/" + stream + ".flv"}
	}
	p.mu.Unlock()

	go p.run()
	return p.Endpoints(), nil
}

// PushVideo 编码为 FLV/RTMP 共用的 AVCC 消息体并投递到有界队列。
func (p *rtmpPublisher) PushVideo(pts uint64, annexB []byte, keyframe bool, params *ParamSets) error {
	p.mu.Lock()
	defer p.mu.Unlock()
	if p.isClosedLocked() {
		return nil
	}
	ts := uint32(p.timeline.Normalize(pts))
	if params != nil && params.Complete() {
		p.params = params.Clone()
	}
	if !keyframe && p.desync {
		return nil
	}
	if keyframe {
		if !p.params.Complete() {
			return nil
		}
		record, err := AVCDecoderConfigurationRecord(p.params)
		if err != nil {
			return err
		}
		config := make([]byte, 0, 5+len(record))
		config = append(config, 0x17, 0x00, 0x00, 0x00, 0x00)
		config = append(config, record...)
		if !p.enqueueLocked(rtmpMessage{typ: 9, ts: 0, body: config}) {
			return nil
		}
		// 只有配置真正入队后才解除 desync，否则后续帧会缺参数集。
		p.desync = false
	}
	body, err := VideoTagBody(annexB, keyframe)
	if err != nil || len(body) == 0 {
		return err
	}
	p.enqueueLocked(rtmpMessage{typ: 9, ts: ts, body: body})
	return nil
}

// PushAudio 编码 AAC 消息体。G.711 没有 FLV/RTMP 原生编码，直接忽略。
func (p *rtmpPublisher) PushAudio(pts uint64, codec AudioCodec, frame []byte, config []byte) error {
	if codec != AudioAAC {
		return nil
	}
	p.mu.Lock()
	defer p.mu.Unlock()
	if p.isClosedLocked() {
		return nil
	}
	if p.desync {
		// 音频不能先于关键帧到达观众，否则会先听到一段没有画面的声音。
		return nil
	}
	if len(config) > 0 && !bytes.Equal(config, p.audioCfg) {
		p.audioCfg = append([]byte(nil), config...)
		p.enqueueLocked(rtmpMessage{typ: 8, ts: 0, body: AudioSequenceTagBody(p.audioCfg)})
	}
	body := AudioTagBody(frame)
	ts := uint32(p.timeline.Normalize(pts))
	p.enqueueLocked(rtmpMessage{typ: 8, ts: ts, body: body})
	return nil
}

// enqueueLocked 投递消息；队列满时丢弃当前帧并等待下一个关键帧。
func (p *rtmpPublisher) enqueueLocked(msg rtmpMessage) bool {
	if p.queue == nil {
		return false
	}
	select {
	case p.queue <- msg:
		p.signal()
		return true
	default:
		p.desync = true
		return false
	}
}

// Close 停止转推；连接由 run 协程自行关闭。
func (p *rtmpPublisher) Close(_ string) error {
	p.closeOne.Do(func() {
		close(p.done)
	})
	p.mu.Lock()
	p.state = StateClosed
	p.mu.Unlock()
	p.signal()
	return nil
}

// State 返回转推状态。
func (p *rtmpPublisher) State() State {
	p.mu.Lock()
	defer p.mu.Unlock()
	return p.state
}

// Endpoints 返回发布与播放端点。
func (p *rtmpPublisher) Endpoints() []Endpoint {
	p.mu.Lock()
	defer p.mu.Unlock()
	out := make([]Endpoint, 0, 2)
	if p.publish.URL != "" {
		out = append(out, p.publish)
	}
	if p.endpoint.URL != "" {
		out = append(out, p.endpoint)
	}
	return out
}

func (p *rtmpPublisher) isClosedLocked() bool {
	select {
	case <-p.done:
		return true
	default:
		return false
	}
}

func (p *rtmpPublisher) signal() {
	select {
	case p.wake <- struct{}{}:
	default:
	}
}

// run 持续重连并写队列；后台协程是唯一持有网络连接的地方。
func (p *rtmpPublisher) run() {
	backoff := time.Second
	for {
		select {
		case <-p.done:
			return
		default:
		}
		conn, err := p.connect()
		if err != nil {
			p.setState(StateFailed)
			if !p.wait(backoff) {
				return
			}
			if backoff < 30*time.Second {
				backoff *= 2
			}
			continue
		}
		backoff = time.Second
		p.setState(StatePublishing)
		p.mu.Lock()
		p.desync = true
		p.mu.Unlock()
		p.writeLoop(conn)
		_ = conn.Close()
		if !p.wait(time.Second) {
			return
		}
	}
}

func (p *rtmpPublisher) wait(d time.Duration) bool {
	timer := time.NewTimer(d)
	defer timer.Stop()
	select {
	case <-p.done:
		return false
	case <-timer.C:
		return true
	}
}

func (p *rtmpPublisher) setState(state State) {
	p.mu.Lock()
	p.state = state
	p.mu.Unlock()
}

func (p *rtmpPublisher) nextMessage() (rtmpMessage, bool) {
	select {
	case msg := <-p.queue:
		return msg, true
	case <-p.done:
		return rtmpMessage{}, false
	case <-p.wake:
		select {
		case msg := <-p.queue:
			return msg, true
		default:
			return rtmpMessage{}, true
		}
	}
}

func (p *rtmpPublisher) writeLoop(conn net.Conn) {
	for {
		msg, ok := p.nextMessage()
		if !ok {
			return
		}
		if msg.body == nil {
			continue
		}
		_ = conn.SetWriteDeadline(time.Now().Add(p.cfg.WriteTimeout))
		if err := writeRTMPMessage(conn, 5, msg.typ, 1, msg.ts, msg.body, defaultRTMPChunkSize); err != nil {
			return
		}
	}
}

// connect 完成握手与 publish 握手。
func (p *rtmpPublisher) connect() (net.Conn, error) {
	parsed, err := url.Parse(p.cfg.URL)
	if err != nil {
		return nil, err
	}
	addr := parsed.Host
	if !strings.Contains(addr, ":") {
		addr += ":1935"
	}
	conn, err := net.DialTimeout("tcp", addr, p.cfg.DialTimeout)
	if err != nil {
		return nil, err
	}
	_ = conn.SetDeadline(time.Now().Add(p.cfg.DialTimeout))
	if err := rtmpHandshake(conn); err != nil {
		_ = conn.Close()
		return nil, err
	}
	// 整个连接只用一个 bufio.Reader：读到 _result 时可能已把下一条消息读进缓冲，
	// 每次新建 reader 会丢掉缓冲区里的字节。
	reader := bufio.NewReader(conn)
	app := strings.Trim(parsed.Path, "/")
	// 设置 chunk size，后续音视频分块按 4096 发送。
	if err := writeRTMPMessage(conn, 2, 1, 0, 0, []byte{0x00, 0x00, 0x10, 0x00}, defaultRTMPChunkSize); err != nil {
		_ = conn.Close()
		return nil, err
	}
	tcURL := fmt.Sprintf("rtmp://%s/%s", parsed.Host, app)
	connect := amf0Command("connect", 1,
		amf0Object(map[string]any{
			"app":      app,
			"type":     "nonprivate",
			"flashVer": "FMLE/3.0 (compatible; FMSc/1.0)",
			"tcUrl":    tcURL,
		}))
	if err := writeRTMPMessage(conn, 3, 20, 0, 0, connect, defaultRTMPChunkSize); err != nil {
		_ = conn.Close()
		return nil, err
	}
	if !readUntil(conn, reader, "_result", 5*time.Second) {
		_ = conn.Close()
		return nil, errors.New("rtmp connect: no _result")
	}
	create := amf0Command("createStream", 2, amf0Null())
	if err := writeRTMPMessage(conn, 3, 20, 0, 0, create, defaultRTMPChunkSize); err != nil {
		_ = conn.Close()
		return nil, err
	}
	_ = readUntil(conn, reader, "_result", 5*time.Second)
	publish := amf0Command("publish", 3, amf0Null(), amf0String(p.stream), amf0String("live"))
	if err := writeRTMPMessage(conn, 3, 20, 1, 0, publish, defaultRTMPChunkSize); err != nil {
		_ = conn.Close()
		return nil, err
	}
	if !readUntil(conn, reader, "NetStream.Publish.Start", 5*time.Second) {
		_ = conn.Close()
		return nil, errors.New("rtmp publish: not started")
	}
	_ = conn.SetDeadline(time.Time{})
	return conn, nil
}

// rtmpHandshake 实现 simple handshake：C0/C1 → S0/S1/S2 → C2。
func rtmpHandshake(conn net.Conn) error {
	c1 := make([]byte, 1536)
	binary.BigEndian.PutUint32(c1[0:4], 0)
	rng := rand.New(rand.NewSource(time.Now().UnixNano()))
	_, _ = rng.Read(c1[8:])
	if _, err := conn.Write(append([]byte{0x03}, c1...)); err != nil {
		return err
	}
	reply := make([]byte, 1+1536+1536)
	if _, err := io.ReadFull(conn, reply); err != nil {
		return err
	}
	if reply[0] != 0x03 {
		return fmt.Errorf("rtmp handshake: unsupported version %d", reply[0])
	}
	// C2 回显 S1。
	if _, err := conn.Write(reply[1 : 1+1536]); err != nil {
		return err
	}
	return nil
}

// readUntil 读取 RTMP 消息直到 payload 含指定子串或超时。
func readUntil(conn net.Conn, reader *bufio.Reader, needle string, timeout time.Duration) bool {
	deadline := time.Now().Add(timeout)
	for time.Now().Before(deadline) {
		_ = conn.SetReadDeadline(deadline)
		payload, err := readRTMPMessage(reader, defaultRTMPChunkSize)
		if err != nil {
			return false
		}
		if bytes.Contains(payload, []byte(needle)) {
			return true
		}
	}
	return false
}

// writeRTMPMessage 写一条 fmt0 消息并按 chunkSize 分块。
func writeRTMPMessage(w io.Writer, csid byte, typ byte, streamID uint32, ts uint32, payload []byte, chunkSize int) error {
	header := make([]byte, 12)
	header[0] = csid // fmt=0
	// 时间戳 >= 0xFFFFFF 时字段本身填 0xFFFFFF，真实值放到扩展字段。
	tsField := ts
	if tsField >= 0xFFFFFF {
		tsField = 0xFFFFFF
	}
	header[1] = byte(tsField >> 16)
	header[2] = byte(tsField >> 8)
	header[3] = byte(tsField)
	header[4] = byte(len(payload) >> 16)
	header[5] = byte(len(payload) >> 8)
	header[6] = byte(len(payload))
	header[7] = typ
	binary.LittleEndian.PutUint32(header[8:12], streamID)
	if ts >= 0xFFFFFF {
		header = append(header, byte(ts>>24))
	}
	if _, err := w.Write(header); err != nil {
		return err
	}
	for len(payload) > 0 {
		n := len(payload)
		if n > chunkSize {
			n = chunkSize
		}
		if _, err := w.Write(payload[:n]); err != nil {
			return err
		}
		payload = payload[n:]
		if len(payload) == 0 {
			break
		}
		cont := []byte{0xC0 | csid}
		if ts >= 0xFFFFFF {
			var ext [4]byte
			binary.BigEndian.PutUint32(ext[:], ts)
			cont = append(cont, ext[:]...)
		}
		if _, err := w.Write(cont); err != nil {
			return err
		}
	}
	return nil
}

type chunkState struct {
	ts       uint32
	length   int
	typ      byte
	streamID uint32
	buffer   []byte
}

// readRTMPMessage 读取一条完整消息，只支持 fmt0/3 与小于 64 的 csid。
func readRTMPMessage(r *bufio.Reader, chunkSize int) ([]byte, error) {
	states := map[byte]*chunkState{}
	for {
		basic, err := r.ReadByte()
		if err != nil {
			return nil, err
		}
		format := basic >> 6
		csid := basic & 0x3f
		state := states[csid]
		if state == nil {
			state = &chunkState{}
			states[csid] = state
		}
		var ts uint32
		switch format {
		case 0:
			header := make([]byte, 11)
			if _, err := io.ReadFull(r, header); err != nil {
				return nil, err
			}
			ts = uint32(header[0])<<16 | uint32(header[1])<<8 | uint32(header[2])
			state.length = int(header[3])<<16 | int(header[4])<<8 | int(header[5])
			state.typ = header[6]
			state.streamID = binary.LittleEndian.Uint32(header[7:11])
			state.ts = ts
			state.buffer = nil
			if ts == 0xFFFFFF {
				var ext [4]byte
				if _, err := io.ReadFull(r, ext[:]); err != nil {
					return nil, err
				}
				state.ts = binary.BigEndian.Uint32(ext[:])
			}
		case 1:
			header := make([]byte, 7)
			if _, err := io.ReadFull(r, header); err != nil {
				return nil, err
			}
			state.length = int(header[3])<<16 | int(header[4])<<8 | int(header[5])
			state.typ = header[6]
			state.buffer = nil
		case 2:
			header := make([]byte, 3)
			if _, err := io.ReadFull(r, header); err != nil {
				return nil, err
			}
			state.buffer = nil
		case 3:
			if state.length == 0 {
				return nil, errors.New("rtmp: unexpected continuation chunk")
			}
		}
		remaining := state.length - len(state.buffer)
		n := remaining
		if n > chunkSize {
			n = chunkSize
		}
		if n < 0 {
			n = 0
		}
		buf := make([]byte, n)
		if _, err := io.ReadFull(r, buf); err != nil {
			return nil, err
		}
		state.buffer = append(state.buffer, buf...)
		if len(state.buffer) >= state.length {
			payload := state.buffer
			state.buffer = nil
			return payload, nil
		}
	}
}

// ---- AMF0 编码 ----

func amf0Command(name string, transaction float64, args ...[]byte) []byte {
	out := amf0String(name)
	out = append(out, amf0Number(transaction)...)
	for _, arg := range args {
		out = append(out, arg...)
	}
	return out
}

func amf0String(s string) []byte {
	out := []byte{0x02, byte(len(s) >> 8), byte(len(s))}
	return append(out, s...)
}

func amf0Number(v float64) []byte {
	out := make([]byte, 9)
	out[0] = 0x00
	binary.BigEndian.PutUint64(out[1:], math.Float64bits(v))
	return out
}

func amf0Null() []byte { return []byte{0x05} }

func amf0Object(values map[string]any) []byte {
	out := []byte{0x03}
	for key, value := range values {
		out = append(out, amf0String(key)...)
		switch v := value.(type) {
		case string:
			out = append(out, amf0String(v)...)
		case float64:
			out = append(out, amf0Number(v)...)
		case bool:
			if v {
				out = append(out, 0x01, 0x01)
			} else {
				out = append(out, 0x01, 0x00)
			}
		default:
			out = append(out, amf0Null()...)
		}
	}
	return append(out, 0x00, 0x00, 0x09)
}
