//go:build srs

package playback

// 真实 SRS 互操作验证（手动执行，默认不参与 go test）。
//
// 前置：本机有可运行的 SRS 5/6 二进制与配置（daemon off，1935/8080/1985 端口）。
//
//	SRS_BIN=/path/to/srs SRS_CONF=/path/to/verify.conf \
//	  go test -tags srs -run TestSRSManual -v ./internal/media/playback/
//
// 覆盖：publish 握手、AVC/AAC 序列头与关键帧、时间戳、SRS /api/v1/streams
// 可见性、HTTP-FLV 新观众（gop_cache）拿到 SPS/PPS 与关键帧、SRS 重启后的
// 自动重连与“配置帧 + 关键帧”首帧顺序。

import (
	"bufio"
	"bytes"
	"encoding/json"
	"io"
	"net"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"sync"
	"testing"
	"time"
)

var srsSPS = []byte{0x67, 0x42, 0xc0, 0x1e, 0xda, 0x02, 0x80, 0xf6, 0x80, 0x6d, 0x0a, 0x13, 0x50}
var srsPPS = []byte{0x68, 0xce, 0x3c, 0x80}

// srsIDR 故意超过 4096 字节：客户端必须用 fmt3 续块发送，
// SRS 侧收到的完整关键帧才能证明续块重组正确。
var srsIDR = append([]byte{0x00, 0x00, 0x00, 0x01, 0x65}, bytes.Repeat([]byte{0xAA}, 6000)...)

// srsStreamName 是 StreamPrefix("jt") + 会话 ID("srscheck") 的结果。
const srsStreamName = "jtsrscheck"

func TestSRSManual(t *testing.T) {
	srsBin := envOr("SRS_BIN", "/tmp/srs/trunk/objs/srs")
	srsConf := envOr("SRS_CONF", "/tmp/srs/trunk/conf/verify.conf")
	// SRS 会把 pid 与日志写到相对工作目录的 ./objs 下，必须在其源码根目录启动。
	srsDir := envOr("SRS_DIR", filepath.Dir(filepath.Dir(srsBin)))
	httpAddr := "127.0.0.1:8080"

	cmd := startSRS(t, srsBin, srsConf, srsDir)
	// 闭包捕获变量本身：重启后 cmd 会指向新进程，必须清理最后那一个。
	defer func() { stopSRS(cmd) }()
	waitPort(t, "127.0.0.1:1935")
	waitPort(t, "127.0.0.1:1985")

	factory, err := NewRTMPFactory(RTMPConfig{
		URL:           "rtmp://127.0.0.1:1935/live",
		StreamPrefix:  "jt",
		PlayURLPrefix: "http://" + httpAddr + "/live",
	})
	if err != nil {
		t.Fatalf("factory: %v", err)
	}
	publisher := factory.NewPublisher()
	defer publisher.Close("srs manual done")
	if _, err := publisher.Start(Session{
		ID: "srscheck", Phone: "13800138000", Channel: 1, Kind: "live",
	}, ParamSets{SPS: srsSPS, PPS: srsPPS}, nil); err != nil {
		t.Fatalf("start: %v", err)
	}

	stop := make(chan struct{})
	var wg sync.WaitGroup
	wg.Add(1)
	go func() {
		defer wg.Done()
		pushSRSTestFrames(publisher, stop)
	}()

	waitSRSStream(t, "127.0.0.1:1985", srsStreamName)
	verifyHTTPFLV(t, "http://"+httpAddr+"/live/"+srsStreamName+".flv")

	// 重启 SRS：客户端应自动重连，重连后首帧仍是配置帧 + 关键帧。
	stopSRS(cmd)
	time.Sleep(500 * time.Millisecond)
	cmd = startSRS(t, srsBin, srsConf, srsDir)
	waitPort(t, "127.0.0.1:1935")
	waitPort(t, "127.0.0.1:1985")
	waitSRSStream(t, "127.0.0.1:1985", srsStreamName)
	verifyHTTPFLV(t, "http://"+httpAddr+"/live/"+srsStreamName+".flv")

	close(stop)
	wg.Wait()
}

func envOr(name, fallback string) string {
	if value := os.Getenv(name); value != "" {
		return value
	}
	return fallback
}

func startSRS(t *testing.T, bin, conf, dir string) *exec.Cmd {
	t.Helper()
	cmd := exec.Command(bin, "-c", conf)
	cmd.Dir = dir
	cmd.Stdout = os.Stdout
	cmd.Stderr = os.Stderr
	if err := cmd.Start(); err != nil {
		t.Fatalf("start srs: %v", err)
	}
	return cmd
}

func stopSRS(cmd *exec.Cmd) {
	if cmd == nil || cmd.Process == nil {
		return
	}
	_ = cmd.Process.Kill()
	_, _ = cmd.Process.Wait()
}

func waitPort(t *testing.T, addr string) {
	t.Helper()
	deadline := time.Now().Add(15 * time.Second)
	for time.Now().Before(deadline) {
		conn, err := net.DialTimeout("tcp", addr, 300*time.Millisecond)
		if err == nil {
			_ = conn.Close()
			return
		}
		time.Sleep(100 * time.Millisecond)
	}
	t.Fatalf("port %s not ready", addr)
}

// pushSRSTestFrames 每 40ms 推一帧，每 25 帧一个关键帧（约 1 秒 GOP）。
func pushSRSTestFrames(publisher Publisher, stop <-chan struct{}) {
	params := ParamSets{SPS: srsSPS, PPS: srsPPS}
	pts := uint64(0)
	for frame := 0; ; frame++ {
		select {
		case <-stop:
			return
		default:
		}
		pts += 40
		key := frame%25 == 0
		_ = publisher.PushVideo(pts, srsIDR, key, &params)
		if frame%12 == 0 {
			_ = publisher.PushAudio(pts, AudioAAC, []byte{0x21, 0x10, 0x05}, []byte{0x12, 0x10})
		}
		time.Sleep(40 * time.Millisecond)
	}
}

func waitSRSStream(t *testing.T, apiAddr, name string) {
	t.Helper()
	deadline := time.Now().Add(20 * time.Second)
	var last string
	for time.Now().Before(deadline) {
		resp, err := http.Get("http://" + apiAddr + "/api/v1/streams")
		if err == nil {
			var payload struct {
				Streams []struct {
					Name  string `json:"name"`
					Video *struct {
						Codec string `json:"codec"`
					} `json:"video"`
					Audio *struct {
						Codec string `json:"codec"`
					} `json:"audio"`
					Publish struct {
						Active bool `json:"active"`
					} `json:"publish"`
				} `json:"streams"`
			}
			body, _ := io.ReadAll(resp.Body)
			_ = resp.Body.Close()
			last = string(body)
			if err := json.Unmarshal(body, &payload); err == nil {
				for _, stream := range payload.Streams {
					if stream.Name != name || !stream.Publish.Active {
						continue
					}
					// 关键帧到达前 SRS 还解析不出编解码，继续等而不是直接失败。
					if stream.Video == nil || stream.Video.Codec != "H264" {
						continue
					}
					if stream.Audio == nil || stream.Audio.Codec != "AAC" {
						continue
					}
					return
				}
			}
		}
		time.Sleep(200 * time.Millisecond)
	}
	t.Fatalf("stream %s not publishing H264/AAC; /api/v1/streams = %s", name, last)
}

// verifyHTTPFLV 以新观众身份拉 HTTP-FLV，断言首包是 AVC 配置帧、随后是关键帧。
func verifyHTTPFLV(t *testing.T, url string) {
	t.Helper()
	client := &http.Client{Timeout: 8 * time.Second}
	resp, err := client.Get(url)
	if err != nil {
		t.Fatalf("pull flv: %v", err)
	}
	defer resp.Body.Close()
	tags := readFLVVideoTags(t, resp.Body, 8)
	if len(tags) < 2 {
		t.Fatalf("got %d video tags, want at least config + keyframe", len(tags))
	}
	if tags[0].data[0] != 0x17 || tags[0].data[1] != 0x00 {
		t.Fatalf("first video tag = % x, want AVC sequence header", tags[0].data[:2])
	}
	if !bytes.Contains(tags[0].data, srsSPS) || !bytes.Contains(tags[0].data, srsPPS) {
		t.Fatal("sequence header must carry SPS/PPS (gop_cache must be on)")
	}
	if tags[1].data[0] != 0x17 || tags[1].data[1] != 0x01 {
		t.Fatalf("second video tag = % x, want keyframe", tags[1].data[:2])
	}
	// 关键帧来自 >4096 字节的 IDR：长度正确说明续块被 SRS 完整重组。
	if len(tags[1].data) <= defaultRTMPChunkSize {
		t.Fatalf("keyframe tag len = %d, want > %d (continuation chunk lost)", len(tags[1].data), defaultRTMPChunkSize)
	}
	if !bytes.Equal(tags[1].data[len(tags[1].data)-64:], bytes.Repeat([]byte{0xAA}, 64)) {
		t.Fatal("keyframe tail corrupted, continuation chunk not reassembled")
	}
	for i := 1; i < len(tags); i++ {
		if tags[i].ts < tags[i-1].ts {
			t.Fatalf("timestamps not monotonic: %d then %d", tags[i-1].ts, tags[i].ts)
		}
	}
}

type flvTag struct {
	typ  byte
	ts   uint32
	data []byte
}

// readFLVVideoTags 跳过音频/脚本，收集前 limit 个视频 tag。
func readFLVVideoTags(t *testing.T, reader io.Reader, limit int) []flvTag {
	t.Helper()
	buffered := bufio.NewReader(reader)
	header := make([]byte, 9)
	if _, err := io.ReadFull(buffered, header); err != nil {
		t.Fatalf("read flv header: %v", err)
	}
	if string(header[:3]) != "FLV" {
		t.Fatalf("not an flv stream: % x", header[:3])
	}
	if _, err := io.ReadFull(buffered, make([]byte, 4)); err != nil { // PreviousTagSize0
		t.Fatalf("read previous tag size: %v", err)
	}
	var tags []flvTag
	for len(tags) < limit {
		var raw [11]byte
		if _, err := io.ReadFull(buffered, raw[:]); err != nil {
			t.Fatalf("read tag header: %v", err)
		}
		size := int(raw[1])<<16 | int(raw[2])<<8 | int(raw[3])
		ts := uint32(raw[4])<<16 | uint32(raw[5])<<8 | uint32(raw[6]) | uint32(raw[7])<<24
		data := make([]byte, size)
		if _, err := io.ReadFull(buffered, data); err != nil {
			t.Fatalf("read tag body: %v", err)
		}
		if _, err := io.ReadFull(buffered, make([]byte, 4)); err != nil {
			t.Fatalf("read previous tag size: %v", err)
		}
		if raw[0] == 9 {
			tags = append(tags, flvTag{typ: raw[0], ts: ts, data: data})
		}
	}
	return tags
}
