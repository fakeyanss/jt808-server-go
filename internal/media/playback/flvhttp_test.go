package playback

import (
	"bytes"
	"context"
	"net/http"
	"net/http/httptest"
	"sync"
	"testing"
	"time"
)

type fakeSource struct {
	mu     sync.Mutex
	frames []Frame
	notify chan struct{}
}

func (f *fakeSource) StartFrame() (Frame, bool) {
	f.mu.Lock()
	defer f.mu.Unlock()
	for _, frame := range f.frames {
		if frame.Key {
			return frame, true
		}
	}
	return Frame{}, false
}

func (f *fakeSource) FramesSince(cursor uint64) ([]Frame, uint64, bool) {
	f.mu.Lock()
	defer f.mu.Unlock()
	var out []Frame
	var next uint64
	for _, frame := range f.frames {
		next = frame.Seq + 1
		if frame.Seq >= cursor {
			out = append(out, frame)
		}
	}
	return out, next, false
}

func (f *fakeSource) Notify() <-chan struct{} { return f.notify }

func TestFLVHTTPResendsConfigOnParamSetChange(t *testing.T) {
	sps2 := []byte{0x00, 0x00, 0x00, 0x01, 0x67, 0x4d, 0x40, 0x1f, 0xec, 0x80, 0x80, 0x2d, 0x50}
	pps2 := []byte{0x00, 0x00, 0x00, 0x01, 0x68, 0xee, 0x3c, 0x80}
	frame0 := append(append(append([]byte{}, testSPS...), testPPS...), 0x00, 0x00, 0x00, 0x01, 0x65, 0x88)
	frame1 := append(append(append([]byte{}, sps2...), pps2...), 0x00, 0x00, 0x00, 0x01, 0x65, 0x90)

	source := &fakeSource{
		notify: make(chan struct{}),
		frames: []Frame{
			{Seq: 0, PTS: 0, Key: true, Data: frame0},
			{Seq: 1, PTS: 1000, Key: true, Data: frame1},
		},
	}
	registry := NewFLVRegistry(FLVHTTPOptions{BaseURL: "http://127.0.0.1:9102", WaitTimeout: 10 * time.Millisecond})
	publisher := registry.NewPublisher()
	if _, err := publisher.Start(Session{ID: "s1"}, ParamSets{SPS: testSPS, PPS: testPPS}, source); err != nil {
		t.Fatalf("start: %v", err)
	}
	defer func() { _ = publisher.Close("test done") }()

	ctx, cancel := context.WithCancel(context.Background())
	time.AfterFunc(300*time.Millisecond, cancel)
	req := httptest.NewRequest(http.MethodGet, "/media/s1.flv", nil).WithContext(ctx)
	recorder := httptest.NewRecorder()
	registry.Handler().ServeHTTP(recorder, req)

	body := recorder.Body.Bytes()
	configs := collectVideoConfigs(t, body)
	if len(configs) != 2 {
		t.Fatalf("video config count = %d, want 2 (initial + param set change)", len(configs))
	}
	if !bytes.Contains(configs[0], NALBody(testSPS)) {
		t.Fatalf("first config does not carry the original SPS")
	}
	if !bytes.Contains(configs[1], NALBody(sps2)) {
		t.Fatalf("second config does not carry the new SPS")
	}
}

// collectVideoConfigs 解析 FLV 字节流，返回所有 AVC 解码配置记录。
func collectVideoConfigs(t *testing.T, data []byte) [][]byte {
	t.Helper()
	if len(data) < 13 {
		t.Fatalf("flv body too short: %d", len(data))
	}
	offset := 13
	var configs [][]byte
	for offset+11 <= len(data) {
		tagType := data[offset]
		size := int(data[offset+1])<<16 | int(data[offset+2])<<8 | int(data[offset+3])
		bodyStart := offset + 11
		bodyEnd := bodyStart + size
		if bodyEnd > len(data) {
			break
		}
		body := data[bodyStart:bodyEnd]
		if tagType == 9 && len(body) >= 5 && body[0] == 0x17 && body[1] == 0x00 {
			configs = append(configs, body[5:])
		}
		offset = bodyEnd + 4
	}
	return configs
}

func TestFLVHTTPEndpointURL(t *testing.T) {
	registry := NewFLVRegistry(FLVHTTPOptions{BaseURL: "http://10.0.0.5:9102/"})
	endpoints, err := registry.NewPublisher().Start(Session{ID: "abc"}, ParamSets{}, &fakeSource{notify: make(chan struct{})})
	if err != nil {
		t.Fatalf("start: %v", err)
	}
	if len(endpoints) != 1 || endpoints[0].URL != "http://10.0.0.5:9102/media/abc.flv" {
		t.Fatalf("endpoints = %#v", endpoints)
	}
}
