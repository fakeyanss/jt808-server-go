package playback

import (
	"errors"
	"testing"
)

// fakePublisher 用于验证并联语义：可分别控制 Start / Push 的失败。
type fakePublisher struct {
	startErr error
	pushErr  error
	started  bool
	closed   bool
	videos   int
	audios   int
	state    State
}

func (f *fakePublisher) Start(_ Session, _ ParamSets, _ FrameSource) ([]Endpoint, error) {
	if f.startErr != nil {
		return nil, f.startErr
	}
	f.started = true
	f.state = StatePublishing
	if f.pushErr != nil {
		f.state = StateFailed
	}
	return []Endpoint{{Protocol: "fake", URL: f.endpointURL()}}, nil
}

func (f *fakePublisher) endpointURL() string {
	if f.startErr != nil {
		return "fake://failed"
	}
	return "fake://ok"
}

func (f *fakePublisher) PushVideo(_ uint64, _ []byte, _ bool, _ *ParamSets) error {
	f.videos++
	return f.pushErr
}

func (f *fakePublisher) PushAudio(_ uint64, _ AudioCodec, _ []byte, _ []byte) error {
	f.audios++
	return f.pushErr
}

func (f *fakePublisher) Close(_ string) error {
	f.closed = true
	f.state = StateClosed
	return nil
}

func (f *fakePublisher) State() State { return f.state }

func (f *fakePublisher) Endpoints() []Endpoint {
	return []Endpoint{{Protocol: "fake", URL: f.endpointURL()}}
}

func TestHandleBothKeepsWorkingWhenOnePublisherFails(t *testing.T) {
	failing := &fakePublisher{pushErr: errors.New("rtmp down")}
	healthy := &fakePublisher{}
	handle := &handle{output: OutputBoth, publishers: []Publisher{failing, healthy}}

	if _, err := handle.Start(Session{ID: "s1"}, ParamSets{}, nil); err != nil {
		t.Fatalf("start: %v", err)
	}
	if !failing.started || !healthy.started {
		t.Fatal("both publishers must be started")
	}

	err := handle.PushVideo(0, []byte{0x00, 0x00, 0x00, 0x01, 0x65}, true, nil)
	if err == nil {
		t.Fatal("push must report the failed publisher")
	}
	if healthy.videos != 1 {
		t.Fatalf("healthy publisher received %d frames, want 1", healthy.videos)
	}
	if failing.videos != 1 {
		t.Fatalf("failing publisher received %d frames, want 1", failing.videos)
	}

	if err := handle.PushAudio(0, AudioAAC, nil, nil); err == nil {
		t.Fatal("audio push must report the failed publisher")
	}
	if healthy.audios != 1 {
		t.Fatalf("healthy publisher received %d audio frames, want 1", healthy.audios)
	}

	if state := handle.State(); state != StateFailed {
		t.Fatalf("aggregate state = %s, want failed", state)
	}
	if err := handle.Close("done"); err != nil {
		t.Fatalf("close: %v", err)
	}
	if !failing.closed || !healthy.closed {
		t.Fatal("both publishers must be closed")
	}
}

func TestHandleStartFailureClosesStartedPublishers(t *testing.T) {
	started := &fakePublisher{}
	failing := &fakePublisher{startErr: errors.New("bad rtmp url")}
	handle := &handle{output: OutputBoth, publishers: []Publisher{started, failing}}

	if _, err := handle.Start(Session{ID: "s1"}, ParamSets{}, nil); err == nil {
		t.Fatal("start must fail when a publisher fails")
	}
	if !started.closed {
		t.Fatal("started publisher must be closed when a later one fails")
	}
	if handle.Endpoints() != nil {
		t.Fatalf("endpoints = %+v, want none", handle.Endpoints())
	}
}

var _ Publisher = (*fakePublisher)(nil)
