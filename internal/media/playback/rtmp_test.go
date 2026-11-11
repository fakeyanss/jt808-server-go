package playback

import (
	"bufio"
	"bytes"
	"encoding/binary"
	"io"
	"net"
	"strings"
	"sync"
	"testing"
	"time"
)

// fakeRTMPServer 是协议级假服务端：只做 simple handshake 与最小的
// connect/createStream/publish 应答，用来验证客户端发出的字节。
//
// 它不是 SRS，也不校验 SigV4 之外的媒体语义；真实 SRS 互操作仍需部署验证。
type fakeRTMPServer struct {
	ln        net.Listener
	addr      string
	published chan string
	media     chan rtmpMessage
	mu        sync.Mutex
	conn      net.Conn
}

func newFakeRTMPServer(t *testing.T) *fakeRTMPServer {
	t.Helper()
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatalf("listen: %v", err)
	}
	server := &fakeRTMPServer{
		ln:        ln,
		addr:      ln.Addr().String(),
		published: make(chan string, 8),
		media:     make(chan rtmpMessage, 64),
	}
	t.Cleanup(func() { _ = ln.Close() })
	go server.serve()
	return server
}

// serve 接受多次连接：重连测试需要第二次 publish。
func (s *fakeRTMPServer) serve() {
	for {
		conn, err := s.ln.Accept()
		if err != nil {
			return
		}
		s.mu.Lock()
		s.conn = conn
		s.mu.Unlock()
		// 单条连接的失败（含测试主动断连）不终止监听，继续接受下一次连接。
		_ = s.handleConn(conn)
	}
}

// dropConnection 模拟媒体服务器断连。
func (s *fakeRTMPServer) dropConnection() {
	s.mu.Lock()
	conn := s.conn
	s.mu.Unlock()
	if conn != nil {
		_ = conn.Close()
	}
}

func (s *fakeRTMPServer) handleConn(conn net.Conn) error {
	defer conn.Close()
	_ = conn.SetDeadline(time.Now().Add(10 * time.Second))

	// C0+C1。
	c0c1 := make([]byte, 1+1536)
	if _, err := io.ReadFull(conn, c0c1); err != nil {
		return err
	}
	if c0c1[0] != 0x03 {
		return io.ErrUnexpectedEOF
	}
	reply := make([]byte, 1+1536+1536)
	reply[0] = 0x03
	copy(reply[1+1536:], c0c1[1:]) // S2 回显 C1
	if _, err := conn.Write(reply); err != nil {
		return err
	}
	// C2 回显 S1。
	c2 := make([]byte, 1536)
	if _, err := io.ReadFull(conn, c2); err != nil {
		return err
	}

	reader := bufio.NewReader(conn)
	for {
		typ, ts, payload, err := readTestMessage(reader)
		if err != nil {
			return err
		}
		switch {
		case typ == 1: // Set Chunk Size
			continue
		case bytes.Contains(payload, []byte("connect")):
			if err := writeRTMPMessage(conn, 3, 20, 0, 0, amf0Command("_result", 1, amf0Null()), 4096); err != nil {
				return err
			}
		case bytes.Contains(payload, []byte("createStream")):
			if err := writeRTMPMessage(conn, 3, 20, 0, 0, amf0Command("_result", 2, amf0Null()), 4096); err != nil {
				return err
			}
		case bytes.Contains(payload, []byte("publish")):
			name := ""
			if index := bytes.Index(payload, []byte("publish")); index >= 0 {
				name = string(payload[index:])
			}
			s.published <- name
			if err := writeRTMPMessage(conn, 5, 20, 1, 0,
				amf0Command("onStatus", 0, amf0String("NetStream.Publish.Start")), 4096); err != nil {
				return err
			}
		default:
			s.media <- rtmpMessage{typ: typ, ts: ts, body: payload}
		}
	}
}

// readTestMessage 读取客户端一条 fmt0 消息并返回时间戳。
//
// 客户端对媒体消息固定用 fmt0、chunkSize=4096；测试帧远小于该值，
// 因此不需要处理续块。
func readTestMessage(reader *bufio.Reader) (byte, uint32, []byte, error) {
	basic, err := reader.ReadByte()
	if err != nil {
		return 0, 0, nil, err
	}
	if basic>>6 != 0 {
		return 0, 0, nil, io.ErrUnexpectedEOF
	}
	header := make([]byte, 11)
	if _, err := io.ReadFull(reader, header); err != nil {
		return 0, 0, nil, err
	}
	ts := uint32(header[0])<<16 | uint32(header[1])<<8 | uint32(header[2])
	length := int(header[3])<<16 | int(header[4])<<8 | int(header[5])
	typ := header[6]
	if ts == 0xFFFFFF {
		var ext [4]byte
		if _, err := io.ReadFull(reader, ext[:]); err != nil {
			return 0, 0, nil, err
		}
		ts = binary.BigEndian.Uint32(ext[:])
	}
	payload := make([]byte, length)
	if _, err := io.ReadFull(reader, payload); err != nil {
		return 0, 0, nil, err
	}
	return typ, ts, payload, nil
}

func (s *fakeRTMPServer) nextMedia(t *testing.T) rtmpMessage {
	t.Helper()
	select {
	case msg := <-s.media:
		return msg
	case <-time.After(5 * time.Second):
		t.Fatal("timeout waiting for media message")
	}
	return rtmpMessage{}
}

func (s *fakeRTMPServer) waitPublished(t *testing.T) string {
	t.Helper()
	select {
	case name := <-s.published:
		return name
	case <-time.After(5 * time.Second):
		t.Fatal("timeout waiting for publish")
	}
	return ""
}

func TestRTMPPublishesConfigKeyframeAndAudio(t *testing.T) {
	server := newFakeRTMPServer(t)
	factory, err := NewRTMPFactory(RTMPConfig{
		URL:           "rtmp://" + server.addr + "/live",
		StreamPrefix:  "jt_",
		PlayURLPrefix: "http://127.0.0.1:8080/live",
	})
	if err != nil {
		t.Fatalf("factory: %v", err)
	}
	publisher := factory.NewPublisher()
	defer publisher.Close("test done")

	params := ParamSets{SPS: testSPS, PPS: testPPS}
	endpoints, err := publisher.Start(Session{ID: "sess1", Phone: "13800138000", Channel: 1, Kind: "live"}, params, nil)
	if err != nil {
		t.Fatalf("start: %v", err)
	}
	if len(endpoints) != 2 {
		t.Fatalf("endpoints = %+v, want publish + play", endpoints)
	}
	if endpoints[0].Protocol != "rtmp" || !strings.Contains(endpoints[0].URL, "/jt_sess1") {
		t.Fatalf("publish endpoint = %+v", endpoints[0])
	}
	if endpoints[1].Protocol != "http-flv" || !strings.Contains(endpoints[1].URL, "/jt_sess1.flv") {
		t.Fatalf("play endpoint = %+v", endpoints[1])
	}
	if name := server.waitPublished(t); !strings.Contains(name, "jt_sess1") {
		t.Fatalf("publish payload = %q, want stream name", name)
	}

	idr := append(append([]byte{}, testSPS...), testPPS...)
	idr = append(idr, 0x00, 0x00, 0x00, 0x01, 0x65, 0x88, 0x84, 0x00)
	pFrame := []byte{0x00, 0x00, 0x00, 0x01, 0x41, 0x9a, 0x00}

	// 第一条关键帧用于确认连接已进入写循环（run 在 connect 之后会重置 desync）。
	if err := publisher.PushVideo(1000, idr, true, &params); err != nil {
		t.Fatalf("push first keyframe: %v", err)
	}
	first := server.nextMedia(t)
	if first.typ != 9 || first.ts != 0 || first.body[0] != 0x17 || first.body[1] != 0x00 {
		t.Fatalf("first message = type %d ts %d body %x, want avc config at ts 0", first.typ, first.ts, first.body[:2])
	}
	if second := server.nextMedia(t); second.typ != 9 || second.body[1] != 0x01 {
		t.Fatalf("second message = type %d body %x, want keyframe", second.typ, second.body[:2])
	}

	// 之后消息顺序：配置、关键帧、P 帧、AAC 配置、AAC 帧。
	if err := publisher.PushVideo(2000, idr, true, &params); err != nil {
		t.Fatalf("push keyframe: %v", err)
	}
	if err := publisher.PushVideo(2040, pFrame, false, nil); err != nil {
		t.Fatalf("push p frame: %v", err)
	}
	if err := publisher.PushAudio(2050, AudioAAC, []byte{0x21, 0x10, 0x05}, []byte{0x12, 0x10}); err != nil {
		t.Fatalf("push audio: %v", err)
	}

	config := server.nextMedia(t)
	frame := server.nextMedia(t)
	p := server.nextMedia(t)
	audioConfig := server.nextMedia(t)
	audio := server.nextMedia(t)

	if config.typ != 9 || config.ts != 0 || config.body[0] != 0x17 || config.body[1] != 0x00 {
		t.Fatalf("config = type %d ts %d body %x", config.typ, config.ts, config.body[:2])
	}
	if frame.typ != 9 || frame.ts != 1000 || frame.body[1] != 0x01 {
		t.Fatalf("keyframe = type %d ts %d body %x", frame.typ, frame.ts, frame.body[:2])
	}
	if p.typ != 9 || p.ts != 1040 || p.body[1] != 0x01 {
		t.Fatalf("p frame = type %d ts %d body %x", p.typ, p.ts, p.body[:2])
	}
	if audioConfig.typ != 8 || audioConfig.ts != 0 || audioConfig.body[0] != 0xAF || audioConfig.body[1] != 0x00 {
		t.Fatalf("audio config = type %d ts %d body %x", audioConfig.typ, audioConfig.ts, audioConfig.body[:2])
	}
	if audio.typ != 8 || audio.ts != 1050 || audio.body[0] != 0xAF || audio.body[1] != 0x01 {
		t.Fatalf("audio = type %d ts %d body %x", audio.typ, audio.ts, audio.body[:2])
	}
}

func TestRTMPReconnectStartsWithConfigAndKeyframe(t *testing.T) {
	server := newFakeRTMPServer(t)
	factory, err := NewRTMPFactory(RTMPConfig{URL: "rtmp://" + server.addr + "/live"})
	if err != nil {
		t.Fatalf("factory: %v", err)
	}
	publisher := factory.NewPublisher()
	defer publisher.Close("test done")

	params := ParamSets{SPS: testSPS, PPS: testPPS}
	if _, err := publisher.Start(Session{ID: "sess3"}, params, nil); err != nil {
		t.Fatalf("start: %v", err)
	}
	server.waitPublished(t)

	idr := append(append([]byte{}, testSPS...), testPPS...)
	idr = append(idr, 0x00, 0x00, 0x00, 0x01, 0x65, 0x88, 0x84, 0x00)
	pFrame := []byte{0x00, 0x00, 0x00, 0x01, 0x41, 0x9a, 0x00}
	if err := publisher.PushVideo(1000, idr, true, &params); err != nil {
		t.Fatalf("push keyframe: %v", err)
	}
	if msg := server.nextMedia(t); msg.body[1] != 0x00 {
		t.Fatalf("first = %x, want avc config", msg.body[:2])
	}
	if msg := server.nextMedia(t); msg.body[1] != 0x01 {
		t.Fatalf("second = %x, want keyframe", msg.body[:2])
	}

	// 断连后自动重连：写失败才会发现对端已断开，持续推帧触发。
	server.dropConnection()
	stop := make(chan struct{})
	defer close(stop)
	go func() {
		pts := uint64(2000)
		for {
			select {
			case <-stop:
				return
			default:
			}
			pts += 40
			_ = publisher.PushVideo(pts, pFrame, false, nil)
			time.Sleep(20 * time.Millisecond)
		}
	}()
	server.waitPublished(t)
	time.Sleep(200 * time.Millisecond)
	// 重连后、关键帧之前的 P 帧必须被丢弃，不能让新连接以 P 帧开流。
	if err := publisher.PushVideo(3000, idr, true, &params); err != nil {
		t.Fatalf("push keyframe after reconnect: %v", err)
	}
	first := server.nextMedia(t)
	second := server.nextMedia(t)
	if first.typ != 9 || first.body[1] != 0x00 {
		t.Fatalf("first after reconnect = type %d body %x, want avc config", first.typ, first.body[:2])
	}
	if second.typ != 9 || second.body[1] != 0x01 {
		t.Fatalf("second after reconnect = type %d body %x, want keyframe", second.typ, second.body[:2])
	}
}

func TestRTMPDropsFramesUntilKeyframe(t *testing.T) {
	server := newFakeRTMPServer(t)
	factory, err := NewRTMPFactory(RTMPConfig{URL: "rtmp://" + server.addr + "/live"})
	if err != nil {
		t.Fatalf("factory: %v", err)
	}
	publisher := factory.NewPublisher()
	defer publisher.Close("test done")
	if _, err := publisher.Start(Session{ID: "sess2"}, ParamSets{SPS: testSPS, PPS: testPPS}, nil); err != nil {
		t.Fatalf("start: %v", err)
	}
	server.waitPublished(t)

	pFrame := []byte{0x00, 0x00, 0x00, 0x01, 0x41, 0x9a, 0x00}
	if err := publisher.PushVideo(1000, pFrame, false, nil); err != nil {
		t.Fatalf("push p frame: %v", err)
	}

	// 推送关键帧后，队列里只应出现关键帧相关消息，不能有更早的 P 帧。
	idr := append(append([]byte{}, testSPS...), testPPS...)
	idr = append(idr, 0x00, 0x00, 0x00, 0x01, 0x65, 0x88, 0x84, 0x00)
	if err := publisher.PushVideo(2000, idr, true, &ParamSets{SPS: testSPS, PPS: testPPS}); err != nil {
		t.Fatalf("push keyframe: %v", err)
	}
	first := server.nextMedia(t)
	second := server.nextMedia(t)
	if first.typ != 9 || first.body[1] != 0x00 {
		t.Fatalf("first = type %d body %x, want avc config", first.typ, first.body[:2])
	}
	if second.typ != 9 || second.body[1] != 0x01 {
		t.Fatalf("second = type %d body %x, want keyframe", second.typ, second.body[:2])
	}
	want, err := VideoTagBody(idr, true)
	if err != nil {
		t.Fatalf("build video body: %v", err)
	}
	if !bytes.Equal(second.body, want) {
		t.Fatal("keyframe payload mismatch")
	}
}
