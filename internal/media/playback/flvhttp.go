package playback

import (
	"bytes"
	"encoding/binary"
	"errors"
	"fmt"
	"io"
	"net/http"
	"path"
	"strings"
	"sync"
	"time"
)

const (
	// DefaultFLVWriteTimeout 是单帧写超时；超时即断开慢客户端，避免拖住帧环。
	DefaultFLVWriteTimeout = 10 * time.Second
	// DefaultFLVWaitTimeout 是无新帧时的等待上限，用于及时发现会话关闭。
	DefaultFLVWaitTimeout = 10 * time.Second
)

// FLVHTTPOptions 是进程内 HTTP-FLV 输出配置。
type FLVHTTPOptions struct {
	// BaseURL 是媒体节点对观众可达的地址，例如 http://10.0.0.5:9102。
	BaseURL string
	// PathPrefix 是 FLV 播放路径前缀，默认 /media/。
	PathPrefix string
	// WriteTimeout 是单个 FLV 写入的超时。
	WriteTimeout time.Duration
	// WaitTimeout 是等待新帧的轮询上限。
	WaitTimeout time.Duration
}

// FLVRegistry 管理所有 HTTP-FLV 会话并对外提供 HTTP 处理器。
//
// 它不复制 GOP 缓存：新观众从帧环最近一个关键帧开始读，由帧环负责整 GOP
// 淘汰与游标失效判定。封装使用 mux 共用层，与 rtmp 实现保持一致。
type FLVRegistry struct {
	opts FLVHTTPOptions

	mu       sync.Mutex
	sessions map[string]*flvSession
}

type flvSession struct {
	info   Session
	source FrameSource
	done   chan struct{}
	once   sync.Once

	paramMu sync.Mutex
	params  ParamSets
}

// NewFLVRegistry 创建 HTTP-FLV 会话注册表。
func NewFLVRegistry(opts FLVHTTPOptions) *FLVRegistry {
	if opts.PathPrefix == "" {
		opts.PathPrefix = "/media/"
	}
	if !strings.HasPrefix(opts.PathPrefix, "/") {
		opts.PathPrefix = "/" + opts.PathPrefix
	}
	if !strings.HasSuffix(opts.PathPrefix, "/") {
		opts.PathPrefix += "/"
	}
	if opts.WriteTimeout <= 0 {
		opts.WriteTimeout = DefaultFLVWriteTimeout
	}
	if opts.WaitTimeout <= 0 {
		opts.WaitTimeout = DefaultFLVWaitTimeout
	}
	opts.BaseURL = strings.TrimRight(strings.TrimSpace(opts.BaseURL), "/")
	return &FLVRegistry{opts: opts, sessions: map[string]*flvSession{}}
}

// NewPublisher 创建一路会话的 HTTP-FLV 输出。
func (r *FLVRegistry) NewPublisher() Publisher {
	return &flvPublisher{registry: r}
}

type flvPublisher struct {
	registry *FLVRegistry
	session  *flvSession
	endpoint Endpoint
}

// Start 注册会话并返回 HTTP-FLV 播放地址。
func (p *flvPublisher) Start(session Session, params ParamSets, source FrameSource) ([]Endpoint, error) {
	if session.ID == "" {
		return nil, errors.New("playback: session id is required")
	}
	if source == nil {
		return nil, errors.New("playback: flv output requires a frame source")
	}
	st := &flvSession{
		info:   session,
		source: source,
		done:   make(chan struct{}),
		params: params.Clone(),
	}
	p.registry.mu.Lock()
	p.registry.sessions[session.ID] = st
	p.registry.mu.Unlock()
	p.session = st
	p.endpoint = Endpoint{
		Protocol: "http-flv",
		URL:      p.registry.opts.BaseURL + p.registry.opts.PathPrefix + session.ID + ".flv",
	}
	return []Endpoint{p.endpoint}, nil
}

// PushVideo 记录最新参数集；帧本身已由媒体服务写入帧环，这里不重复缓存。
func (p *flvPublisher) PushVideo(_ uint64, _ []byte, _ bool, params *ParamSets) error {
	if p.session == nil || params == nil {
		return nil
	}
	p.session.paramMu.Lock()
	p.session.params = params.Clone()
	p.session.paramMu.Unlock()
	return nil
}

// PushAudio 对 HTTP-FLV 输出无操作；音频帧同样由帧环提供。
func (p *flvPublisher) PushAudio(_ uint64, _ AudioCodec, _ []byte, _ []byte) error {
	return nil
}

// Close 终止会话；已连接的观众在下一次写/等待时退出。
func (p *flvPublisher) Close(_ string) error {
	if p.session == nil {
		return nil
	}
	p.registry.mu.Lock()
	delete(p.registry.sessions, p.session.info.ID)
	p.registry.mu.Unlock()
	p.session.once.Do(func() { close(p.session.done) })
	return nil
}

// State 返回 flv 输出状态。
func (p *flvPublisher) State() State {
	if p.session == nil {
		return StateIdle
	}
	select {
	case <-p.session.done:
		return StateClosed
	default:
		return StatePublishing
	}
}

// Endpoints 返回该会话的播放地址。
func (p *flvPublisher) Endpoints() []Endpoint {
	if p.session == nil || p.endpoint.URL == "" {
		return nil
	}
	return []Endpoint{p.endpoint}
}

// Handler 返回 HTTP-FLV 处理器，由媒体节点挂在 PathPrefix 下。
//
// 这是实现细节而非 Publisher 接口的一部分：接口不含 http.ResponseWriter，
// 桥接实现才能复用同一抽象。
func (r *FLVRegistry) Handler() http.Handler {
	return http.HandlerFunc(r.handle)
}

func (r *FLVRegistry) handle(w http.ResponseWriter, req *http.Request) {
	if req.Method != http.MethodGet && req.Method != http.MethodHead {
		w.Header().Set("Allow", "GET, HEAD")
		w.WriteHeader(http.StatusMethodNotAllowed)
		return
	}
	name := path.Base(req.URL.Path)
	if !strings.HasSuffix(name, ".flv") {
		http.NotFound(w, req)
		return
	}
	sessionID := strings.TrimSuffix(name, ".flv")
	r.mu.Lock()
	session := r.sessions[sessionID]
	r.mu.Unlock()
	if session == nil {
		http.NotFound(w, req)
		return
	}
	r.serve(w, req, session)
}

func (r *FLVRegistry) serve(w http.ResponseWriter, req *http.Request, session *flvSession) {
	start, ok := session.source.StartFrame()
	if !ok {
		writeError(w, http.StatusServiceUnavailable, "waiting_for_keyframe", "no keyframe yet")
		return
	}
	params := ExtractParamSets(start.Data)
	if !params.Complete() {
		session.paramMu.Lock()
		params = session.params.Clone()
		session.paramMu.Unlock()
	}
	if !params.Complete() {
		writeError(w, http.StatusServiceUnavailable, "waiting_for_param_sets", "SPS/PPS not available yet")
		return
	}
	config, err := AVCDecoderConfigurationRecord(params)
	if err != nil {
		writeError(w, http.StatusServiceUnavailable, "waiting_for_param_sets", err.Error())
		return
	}

	w.Header().Set("Content-Type", "video/x-flv")
	w.Header().Set("Cache-Control", "no-store")
	w.Header().Set("Connection", "close")
	w.WriteHeader(http.StatusOK)
	if req.Method == http.MethodHead {
		return
	}
	controller := http.NewResponseController(w)

	writer := newFLVWriter(w)
	if err := writer.writeHeader(); err != nil {
		return
	}
	if err := writer.writeVideoConfig(config); err != nil {
		return
	}

	timeline := &Timeline{}
	cursor := start.Seq
	var audioConfig []byte
	for {
		select {
		case <-req.Context().Done():
			return
		case <-session.done:
			return
		default:
		}
		frames, _, dropped := session.source.FramesSince(cursor)
		if dropped {
			// 客户端落后于帧环淘汰边界，继续写会得到不可解码的流。
			return
		}
		if len(frames) == 0 {
			select {
			case <-session.source.Notify():
			case <-session.done:
				return
			case <-req.Context().Done():
				return
			case <-time.After(r.opts.WaitTimeout):
				continue
			}
			continue
		}
		for _, frame := range frames {
			if err := controller.SetWriteDeadline(time.Now().Add(r.opts.WriteTimeout)); err != nil &&
				!errors.Is(err, http.ErrNotSupported) {
				return
			}
			ts := timeline.Normalize(frame.PTS)
			if frame.Audio {
				if !bytes.Equal(frame.Config, audioConfig) {
					if err := writer.writeAudioConfig(frame.Config); err != nil {
						return
					}
					audioConfig = frame.Config
				}
				if err := writer.writeAudio(ts, frame.Data); err != nil {
					return
				}
			} else {
				if frame.Key {
					if next := ExtractParamSets(frame.Data); next.Complete() &&
						(!bytes.Equal(next.SPS, params.SPS) || !bytes.Equal(next.PPS, params.PPS)) {
						nextConfig, err := AVCDecoderConfigurationRecord(next)
						if err != nil {
							return
						}
						if err := writer.writeVideoConfig(nextConfig); err != nil {
							return
						}
						params = next
					}
				}
				body, err := VideoTagBody(frame.Data, frame.Key)
				if err != nil {
					return
				}
				if len(body) == 0 {
					cursor = frame.Seq + 1
					continue
				}
				if err := writer.writeTag(9, ts, body); err != nil {
					return
				}
			}
			cursor = frame.Seq + 1
		}
		if flusher, ok := w.(http.Flusher); ok {
			flusher.Flush()
		}
	}
}

// flvWriter 只负责 FLV 文件头与 tag 框架；音视频负载编码在 mux.go 共用。
type flvWriter struct {
	w io.Writer
}

func newFLVWriter(w io.Writer) *flvWriter { return &flvWriter{w: w} }

func (f *flvWriter) writeHeader() error {
	header := []byte{'F', 'L', 'V', 0x01, 0x01, 0x00, 0x00, 0x00, 0x09, 0x00, 0x00, 0x00, 0x00}
	_, err := f.w.Write(header)
	return err
}

func (f *flvWriter) writeVideoConfig(record []byte) error {
	body := make([]byte, 0, 5+len(record))
	body = append(body, 0x17, 0x00, 0x00, 0x00, 0x00)
	body = append(body, record...)
	return f.writeTag(9, 0, body)
}

func (f *flvWriter) writeAudioConfig(config []byte) error {
	if len(config) == 0 {
		return nil
	}
	return f.writeTag(8, 0, AudioSequenceTagBody(config))
}

func (f *flvWriter) writeAudio(ts uint64, raw []byte) error {
	return f.writeTag(8, ts, AudioTagBody(raw))
}

func (f *flvWriter) writeTag(tagType byte, timestamp uint64, body []byte) error {
	header := make([]byte, 11)
	header[0] = tagType
	header[1] = byte(len(body) >> 16)
	header[2] = byte(len(body) >> 8)
	header[3] = byte(len(body))
	header[4] = byte(timestamp >> 16)
	header[5] = byte(timestamp >> 8)
	header[6] = byte(timestamp)
	header[7] = byte(timestamp >> 24)
	if _, err := f.w.Write(header); err != nil {
		return err
	}
	if _, err := f.w.Write(body); err != nil {
		return err
	}
	var previous [4]byte
	binary.BigEndian.PutUint32(previous[:], uint32(11+len(body)))
	_, err := f.w.Write(previous[:])
	return err
}

func writeError(w http.ResponseWriter, status int, code, message string) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	_, _ = fmt.Fprintf(w, `{"code":%q,"message":%q}`, code, message)
}
