// Package playback 是 1078 媒体流的播放输出层。
//
// 边界刻意切在“帧 + 参数集”这一层，而不是 HTTP 响应层：
//   - 输入是一路流的音视频访问单元与参数集变化事件；
//   - 输出是播放端点（URL）与发布状态。
//
// 因此同一套接口既能承载进程内 HTTP-FLV（flvhttp），也能承载向 SRS/lal
// 转推 RTMP（rtmp）。接口签名里不允许出现 http.ResponseWriter。
//
// 帧环、录制与对象存储上传不属于本包，它们与输出模式无关：
// 帧环通过 FrameSource 只读地暴露给输出层，输出模式切换不得改变帧环语义。
package playback

import (
	"fmt"
	"strings"
)

// OutputMode 是播放出口模式。
type OutputMode string

const (
	// OutputFLV 是进程内 FLV 封装 + HTTP-FLV 长连接（轻量默认）。
	OutputFLV OutputMode = "flv"
	// OutputRTMP 是向 SRS/lal 转推 RTMP，由外部媒体服务器分发 FLV/HLS。
	OutputRTMP OutputMode = "rtmp"
	// OutputBoth 同时提供两种出口；两者并联，互不阻塞。
	OutputBoth OutputMode = "both"
)

// NormalizeOutputMode 校验并归一化输出模式；空值按 flv 处理。
func NormalizeOutputMode(mode string) (OutputMode, error) {
	switch OutputMode(strings.ToLower(strings.TrimSpace(mode))) {
	case "", OutputFLV:
		return OutputFLV, nil
	case OutputRTMP:
		return OutputRTMP, nil
	case OutputBoth:
		return OutputBoth, nil
	default:
		return "", fmt.Errorf("playback.output must be flv, rtmp or both, got %q", mode)
	}
}

// Session 是输出层看到的播放会话标识。
type Session struct {
	ID      string
	Phone   string
	Channel uint8
	Kind    string
}

// ParamSets 是 Annex B 形式的 SPS/PPS（含起始码）。
type ParamSets struct {
	SPS []byte
	PPS []byte
}

// Complete 表示两组参数集都可用。
func (p ParamSets) Complete() bool {
	return len(p.SPS) > 0 && len(p.PPS) > 0
}

// Clone 返回参数集副本，避免调用方复用底层缓冲。
func (p ParamSets) Clone() ParamSets {
	return ParamSets{
		SPS: append([]byte(nil), p.SPS...),
		PPS: append([]byte(nil), p.PPS...),
	}
}

// AudioCodec 是 1078 音频编码。
type AudioCodec string

const (
	AudioAAC   AudioCodec = "aac"
	AudioG711A AudioCodec = "g711a"
	AudioG711U AudioCodec = "g711u"
)

// Endpoint 是一个可播放/可发布的端点。
//
// protocol 取值：http-flv、hls、rtmp。播放地址由媒体节点返回，管控层只透传，
// 不按输出模式硬拼 URL。
type Endpoint struct {
	Protocol string `json:"protocol"`
	URL      string `json:"url"`
}

// State 是发布状态。
type State string

const (
	StateIdle       State = "idle"
	StatePublishing State = "publishing"
	StateFailed     State = "failed"
	StateClosed     State = "closed"
)

// Frame 是帧环对外暴露的一帧。
//
// PTS 是 1078 原始时间戳（字段宽度 8 字节）；输出层必须经 Timeline 归一化，
// 不得直接写入 FLV/RTMP。Config 仅 AAC 音频使用（AudioSpecificConfig）。
type Frame struct {
	Seq    uint64
	PTS    uint64
	Key    bool
	Audio  bool
	Data   []byte
	Config []byte
}

// FrameSource 是帧环的只读视图。
//
// 读取方按 Seq 追帧；FramesSince 返回 dropped=true 表示游标已被淘汰，
// 调用方必须从新的关键帧重新开始（断连重连），不能继续写不可解码的流。
type FrameSource interface {
	StartFrame() (Frame, bool)
	FramesSince(cursor uint64) ([]Frame, uint64, bool)
	Notify() <-chan struct{}
}

// Publisher 是一路播放会话的输出实现。
//
// 实现必须保证：
//   - PushVideo/PushAudio 不阻塞收流协程；队列满时按策略丢弃并标记 desync；
//   - 参数集变化时重新下发解码配置（AVCDecoderConfigurationRecord/AudioSpecificConfig）；
//   - Close 幂等。
type Publisher interface {
	// Start 注册会话并返回播放/发布端点。source 只对进程内 flv 实现有意义，
	// rtmp 实现可以忽略；这样桥接模式不需要复制平台侧 GOP 缓存。
	Start(session Session, params ParamSets, source FrameSource) ([]Endpoint, error)
	// PushVideo 推送一个 Annex B 视频访问单元。params 非空表示参数集发生变化，
	// 实现应在下一关键帧前重发解码配置。
	PushVideo(pts uint64, annexB []byte, keyframe bool, params *ParamSets) error
	// PushAudio 推送一个音频帧。AAC 的 frame 应为已剥离 ADTS 的原始帧，
	// config 为 AudioSpecificConfig；G.711 忽略 config。
	PushAudio(pts uint64, codec AudioCodec, frame []byte, config []byte) error
	Close(reason string) error
	State() State
	Endpoints() []Endpoint
}
