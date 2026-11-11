package media

import (
	"net/http"

	"github.com/fakeyanss/jt808-server-go/internal/media/playback"
)

// ringSource 把帧环适配成播放输出层的只读 FrameSource。
//
// 边界刻意保持单向：输出层只读帧环，不参与 GOP 淘汰；录制与统计仍由
// 媒体服务维护帧环，输出模式切换不改变这些语义。
type ringSource struct {
	ring *gopRing
}

func (s ringSource) StartFrame() (playback.Frame, bool) {
	frame, ok := s.ring.startFrame()
	if !ok {
		return playback.Frame{}, false
	}
	return toPlaybackFrame(frame), true
}

func (s ringSource) FramesSince(cursor uint64) ([]playback.Frame, uint64, bool) {
	frames, next, dropped := s.ring.framesSince(cursor)
	if len(frames) == 0 {
		return nil, next, dropped
	}
	out := make([]playback.Frame, len(frames))
	for i, frame := range frames {
		out[i] = toPlaybackFrame(frame)
	}
	return out, next, dropped
}

func (s ringSource) Notify() <-chan struct{} {
	return s.ring.notifyCh()
}

func toPlaybackFrame(frame ringFrame) playback.Frame {
	return playback.Frame{
		Seq:    frame.Seq,
		PTS:    frame.PTS,
		Key:    frame.Key,
		Audio:  frame.Audio,
		Data:   frame.Data,
		Config: frame.Config,
	}
}

// playbackHandler 返回 HTTP-FLV 处理器；rtmp-only 模式返回 nil。
func (s *Server) playbackHandler() http.Handler {
	if s.playback == nil {
		return nil
	}
	return s.playback.FLVHandler()
}
