package playback

import (
	"errors"
	"fmt"
	"net/http"
	"sync"
)

// Config 是播放输出层配置。
type Config struct {
	Output OutputMode
	FLV    FLVHTTPOptions
	RTMP   RTMPConfig
}

// Hub 按配置创建播放输出实现。
//
// 输出模式只影响这一层：帧环、录制与对象存储上传不感知模式，
// 切换 flv/rtmp/both 不会改变媒体管线语义。
type Hub struct {
	cfg  Config
	flv  *FLVRegistry
	rtmp *RTMPFactory
}

// NewHub 校验配置并创建输出中心。
func NewHub(cfg Config) (*Hub, error) {
	mode, err := NormalizeOutputMode(string(cfg.Output))
	if err != nil {
		return nil, err
	}
	cfg.Output = mode
	hub := &Hub{cfg: cfg}
	if mode == OutputFLV || mode == OutputBoth {
		if cfg.FLV.BaseURL == "" && mode == OutputFLV {
			return nil, errors.New("playback flv output requires a media node base URL")
		}
		hub.flv = NewFLVRegistry(cfg.FLV)
	}
	if mode == OutputRTMP || mode == OutputBoth {
		if cfg.RTMP.URL == "" {
			return nil, errors.New("playback rtmp output requires playback.rtmp.url")
		}
		factory, err := NewRTMPFactory(cfg.RTMP)
		if err != nil {
			return nil, err
		}
		hub.rtmp = factory
	}
	return hub, nil
}

// Output 返回归一化后的输出模式。
func (h *Hub) Output() OutputMode { return h.cfg.Output }

// FLVHandler 返回 HTTP-FLV 处理器；非 flv 输出时为 nil。
func (h *Hub) FLVHandler() http.Handler {
	if h.flv == nil {
		return nil
	}
	return h.flv.Handler()
}

// NewPublisher 为一路播放会话创建并联输出实现；调用方随后调用 Start。
func (h *Hub) NewPublisher() Publisher {
	var publishers []Publisher
	if h.flv != nil {
		publishers = append(publishers, h.flv.NewPublisher())
	}
	if h.rtmp != nil {
		publishers = append(publishers, h.rtmp.NewPublisher())
	}
	return &handle{output: h.cfg.Output, publishers: publishers}
}

// handle 把多个输出实现并联为一个 Publisher。
//
// 并联语义：任一实现出错只影响自身；单个实现阻塞不得拖慢收流协程，
// 因此各实现的 Push 都必须是“入队或丢弃”，不允许同步写网络。
type handle struct {
	output     OutputMode
	publishers []Publisher

	mu        sync.Mutex
	endpoints []Endpoint
	closed    bool
}

// Start 启动所有并联输出并汇总端点。
func (h *handle) Start(session Session, params ParamSets, source FrameSource) ([]Endpoint, error) {
	h.mu.Lock()
	if h.closed {
		h.mu.Unlock()
		return nil, errors.New("playback: publisher is closed")
	}
	publishers := append([]Publisher(nil), h.publishers...)
	h.mu.Unlock()
	if len(publishers) == 0 {
		return nil, fmt.Errorf("playback output %q is not configured", h.output)
	}
	var endpoints []Endpoint
	for _, publisher := range publishers {
		eps, err := publisher.Start(session, params, source)
		if err != nil {
			_ = h.Close("start failed")
			return nil, err
		}
		endpoints = append(endpoints, eps...)
	}
	h.mu.Lock()
	h.endpoints = endpoints
	h.mu.Unlock()
	return endpoints, nil
}

// PushVideo 向所有输出实现分发视频帧。
func (h *handle) PushVideo(pts uint64, annexB []byte, keyframe bool, params *ParamSets) error {
	h.mu.Lock()
	if h.closed {
		h.mu.Unlock()
		return nil
	}
	publishers := append([]Publisher(nil), h.publishers...)
	h.mu.Unlock()
	var errs []error
	for _, publisher := range publishers {
		if err := publisher.PushVideo(pts, annexB, keyframe, params); err != nil {
			// 自研与桥接互不阻塞：单个输出失败不终止另一路。
			errs = append(errs, err)
		}
	}
	return errors.Join(errs...)
}

// PushAudio 向所有输出实现分发音频帧。
func (h *handle) PushAudio(pts uint64, codec AudioCodec, frame []byte, config []byte) error {
	h.mu.Lock()
	if h.closed {
		h.mu.Unlock()
		return nil
	}
	publishers := append([]Publisher(nil), h.publishers...)
	h.mu.Unlock()
	var errs []error
	for _, publisher := range publishers {
		if err := publisher.PushAudio(pts, codec, frame, config); err != nil {
			errs = append(errs, err)
		}
	}
	return errors.Join(errs...)
}

// Close 关闭所有输出实现。
func (h *handle) Close(reason string) error {
	h.mu.Lock()
	if h.closed {
		h.mu.Unlock()
		return nil
	}
	h.closed = true
	publishers := h.publishers
	h.mu.Unlock()
	var errs []error
	for _, publisher := range publishers {
		if err := publisher.Close(reason); err != nil {
			errs = append(errs, err)
		}
	}
	return errors.Join(errs...)
}

// State 返回聚合状态：任一实现失败即 failed，全部关闭即 closed。
func (h *handle) State() State {
	h.mu.Lock()
	closed := h.closed
	publishers := append([]Publisher(nil), h.publishers...)
	h.mu.Unlock()
	if closed {
		return StateClosed
	}
	state := StateIdle
	for _, publisher := range publishers {
		switch publisher.State() {
		case StateFailed:
			return StateFailed
		case StatePublishing:
			state = StatePublishing
		}
	}
	return state
}

// Endpoints 返回所有输出实现的端点。
func (h *handle) Endpoints() []Endpoint {
	h.mu.Lock()
	defer h.mu.Unlock()
	return append([]Endpoint(nil), h.endpoints...)
}
