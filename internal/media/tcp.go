package media

import (
	"bytes"
	"encoding/binary"
	"errors"
	"io"
	"net"
	"time"

	"github.com/rs/zerolog/log"

	"github.com/fakeyanss/jt808-server-go/pkg/routines"
)

// acceptTCP 接受 1078 媒体 TCP 连接，并独立限制连接数。
func (s *Server) acceptTCP() {
	for {
		conn, err := s.tcpLn.AcceptTCP()
		if err != nil {
			select {
			case <-s.done:
				return
			default:
			}
			log.Error().Err(err).Msg("Fail to accept media TCP connection")
			continue
		}
		_ = conn.SetKeepAlive(true)
		_ = conn.SetKeepAlivePeriod(30 * time.Second)

		remote := conn.RemoteAddr().String()
		s.mu.Lock()
		if len(s.conns) >= s.cfg.MaxTCPConns {
			s.mu.Unlock()
			log.Warn().Str("remote", remote).Int("maxTCPConns", s.cfg.MaxTCPConns).
				Msg("Media TCP connection limit reached, reject connection")
			_ = conn.Close()
			continue
		}
		s.nextConnID++
		tc := &tcpConn{id: s.nextConnID, conn: conn, remote: remote}
		s.conns[tc.id] = tc
		s.mu.Unlock()

		log.Debug().Str("remote", remote).Uint64("conn", tc.id).Msg("Media TCP connection accepted")
		s.serve.Add(1)
		routines.GoSafe(func() {
			defer s.serve.Done()
			s.serveTCP(tc)
		})
	}
}

func (s *Server) serveTCP(tc *tcpConn) {
	scanner := newFrameScanner(maxFrameBytes)
	source := packetSource{transport: transportTCP, connID: tc.id, remote: tc.remote}
	err := readTCP(tc.conn, scanner, func(raw []byte) {
		s.handlePacket(raw, source)
	})
	if err != nil && !errors.Is(err, io.EOF) && !errors.Is(err, net.ErrClosed) {
		log.Debug().Err(err).Str("remote", tc.remote).Msg("Media TCP read failed")
	}

	_ = tc.conn.Close()
	s.mu.Lock()
	delete(s.conns, tc.id)
	s.mu.Unlock()
	s.closeConnectionStreams(tc.id)
	log.Debug().Str("remote", tc.remote).Uint64("conn", tc.id).Msg("Media TCP connection closed")
}

// frameScanner 从一条 TCP 连接的字节流中切出完整的 1078 包。
//
// 帧边界由固定 magic 与包体长度共同确定：
//   - 丢弃 magic 之前的垃圾数据，并保留末尾 3 字节以处理被拆开的 magic；
//   - 首包探测 SIM 宽度（6 或 10 字节）并在连接内锁定；
//   - 包体未收齐时保留缓冲区等待半包；
//   - 包体长度异常时丢弃 1 字节重新同步。
type frameScanner struct {
	buf     []byte
	simLen  int
	maxBody int
}

func newFrameScanner(maxBody int) *frameScanner {
	return &frameScanner{maxBody: maxBody}
}

// Push 追加字节流并返回本次可交付的完整包。
func (fs *frameScanner) Push(data []byte) [][]byte {
	fs.buf = append(fs.buf, data...)
	frames := make([][]byte, 0, 4)

	for {
		if len(fs.buf) < len(jt1078Magic) {
			break
		}
		if !bytes.HasPrefix(fs.buf, jt1078Magic[:]) {
			idx := bytes.Index(fs.buf[1:], jt1078Magic[:])
			if idx < 0 {
				keep := len(fs.buf)
				if keep > len(jt1078Magic)-1 {
					keep = len(jt1078Magic) - 1
				}
				fs.buf = append(fs.buf[:0], fs.buf[len(fs.buf)-keep:]...)
				break
			}
			dropped := idx + 1
			fs.buf = fs.buf[dropped:]
			log.Debug().Int("discarded", dropped).Msg("Resync media TCP stream")
			continue
		}

		if fs.simLen == 0 {
			switch detected := fs.detectSIM(); {
			case detected < 0:
				fs.buf = fs.buf[1:]
				continue
			case detected == 0:
				break
			default:
				fs.simLen = detected
			}
			if fs.simLen == 0 {
				break
			}
		}

		flagOffset := 8 + fs.simLen + 1
		if len(fs.buf) < flagOffset+1 {
			break
		}
		dataType := fs.buf[flagOffset] >> 4
		headerLen, ok := packetHeaderSize(dataType, fs.simLen)
		if !ok {
			fs.buf = fs.buf[1:]
			continue
		}
		if len(fs.buf) < headerLen {
			break
		}
		bodyLen := int(binary.BigEndian.Uint16(fs.buf[headerLen-2 : headerLen]))
		if bodyLen > fs.maxBody {
			log.Warn().Int("bodyLength", bodyLen).Int("max", fs.maxBody).
				Msg("Discard oversized media TCP frame")
			fs.buf = fs.buf[1:]
			continue
		}
		total := headerLen + bodyLen
		if len(fs.buf) < total {
			break
		}

		raw := make([]byte, total)
		copy(raw, fs.buf[:total])
		frames = append(frames, raw)
		fs.buf = fs.buf[total:]
	}

	// 长时间凑不齐一帧时重置缓冲，避免异常终端占满内存。
	if len(fs.buf) > fs.maxBody+64*1024 {
		log.Warn().Int("buffered", len(fs.buf)).Msg("Media TCP buffer overflow, reset connection buffer")
		fs.buf = fs.buf[:0]
		fs.simLen = 0
	}
	return frames
}

// SIM 宽度探测结果。
const (
	simProbeNeedMore = iota // 字节不足以判断
	simProbeInvalid         // 按该宽度解释不是合法包头
	simProbePending         // 包头合法但整包未收齐
	simProbeComplete        // 已能收出完整包
)

// detectSIM 在首包上探测 SIM 宽度。
// 返回 6/10 表示已确定，0 表示数据不足需继续等待，-1 表示当前字节无法构成合法头需重同步。
//
// 只有两种宽度都被判定为非法时才重同步，避免半包被误判成垃圾数据丢弃。
func (fs *frameScanner) detectSIM() int {
	short := fs.probeSIM(simLengthShort)
	long := fs.probeSIM(simLengthLong)

	switch {
	case short == simProbeComplete:
		// 两种解释都能收出完整包时，优先采用规范定义的 6 字节手机号。
		return simLengthShort
	case long == simProbeComplete:
		return simLengthLong
	case short == simProbePending && long == simProbeInvalid:
		return simLengthShort
	case long == simProbePending && short == simProbeInvalid:
		return simLengthLong
	case short == simProbeInvalid && long == simProbeInvalid:
		return -1
	default:
		return 0
	}
}

// probeSIM 按指定 SIM 宽度检查当前缓冲区。
func (fs *frameScanner) probeSIM(simLen int) int {
	flagOffset := 8 + simLen + 1
	if len(fs.buf) < flagOffset+1 {
		return simProbeNeedMore
	}
	dataType := fs.buf[flagOffset] >> 4
	headerLen, ok := packetHeaderSize(dataType, simLen)
	if !ok || len(fs.buf) < headerLen {
		if !ok {
			return simProbeInvalid
		}
		return simProbeNeedMore
	}
	// 这里只判断结构是否可解，不套用 maxBody：包体超限由主循环丢弃并重同步，
	// 否则超限包会被误判成 10 字节 SIM 解释下的小包，产出错误帧。
	bodyLen := int(binary.BigEndian.Uint16(fs.buf[headerLen-2 : headerLen]))
	if headerLen+bodyLen <= len(fs.buf) {
		return simProbeComplete
	}
	return simProbePending
}
