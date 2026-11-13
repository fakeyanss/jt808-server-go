package gatewaynode

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"sync"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
)

type nodeEvent struct {
	path string
	body any
}

type recorder struct {
	mu     sync.Mutex
	events []nodeEvent
}

func (r *recorder) add(path string, body any) {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.events = append(r.events, nodeEvent{path: path, body: body})
}

func (r *recorder) snapshot() []nodeEvent {
	r.mu.Lock()
	defer r.mu.Unlock()
	return append([]nodeEvent(nil), r.events...)
}

func newControlStub(t *testing.T, fenceHeartbeats int) (*recorder, *httptest.Server) {
	t.Helper()
	recorded := &recorder{}
	var mu sync.Mutex
	var heartbeats int
	mux := http.NewServeMux()
	mux.HandleFunc(PathRegister, func(w http.ResponseWriter, r *http.Request) {
		var body RegisterRequest
		require.NoError(t, json.NewDecoder(r.Body).Decode(&body))
		recorded.add(PathRegister, body)
		w.WriteHeader(http.StatusOK)
	})
	mux.HandleFunc(PathHeartbeat, func(w http.ResponseWriter, r *http.Request) {
		var body HeartbeatRequest
		require.NoError(t, json.NewDecoder(r.Body).Decode(&body))
		recorded.add(PathHeartbeat, body)
		mu.Lock()
		heartbeats++
		fence := heartbeats <= fenceHeartbeats
		mu.Unlock()
		if fence {
			w.WriteHeader(http.StatusConflict)
			return
		}
		w.WriteHeader(http.StatusOK)
	})
	mux.HandleFunc(PathDeregister, func(w http.ResponseWriter, r *http.Request) {
		var body HeartbeatRequest
		require.NoError(t, json.NewDecoder(r.Body).Decode(&body))
		recorded.add(PathDeregister, body)
		w.WriteHeader(http.StatusOK)
	})
	server := httptest.NewServer(mux)
	t.Cleanup(server.Close)
	return recorded, server
}

func waitFor(t *testing.T, condition func() bool, message string) {
	t.Helper()
	deadline := time.Now().Add(2 * time.Second)
	for time.Now().Before(deadline) {
		if condition() {
			return
		}
		time.Sleep(5 * time.Millisecond)
	}
	t.Fatalf("timed out waiting for %s", message)
}

func TestReporterRegistersHeartbeatsAndDeregisters(t *testing.T) {
	recorded, control := newControlStub(t, 0)
	reporter, err := NewReporter(ReporterOptions{
		BaseURL:     control.URL,
		Token:       "token",
		GatewayID:   "gw-1",
		InstanceID:  "inst-a",
		DownlinkURL: "http://gw-1:9001",
		Interval:    10 * time.Millisecond,
	})
	require.NoError(t, err)

	ctx, cancel := context.WithCancel(context.Background())
	go reporter.Run(ctx)

	waitFor(t, func() bool { return countEvents(recorded.snapshot(), PathHeartbeat) >= 2 }, "heartbeats")
	cancel()
	select {
	case <-reporter.Done():
	case <-time.After(2 * time.Second):
		t.Fatal("reporter did not stop")
	}

	events := recorded.snapshot()
	require.Equal(t, PathRegister, events[0].path)
	registered, ok := events[0].body.(RegisterRequest)
	require.True(t, ok)
	require.Equal(t, "gw-1", registered.GatewayID)
	require.Equal(t, "inst-a", registered.InstanceID)
	require.Equal(t, "http://gw-1:9001", registered.DownlinkURL)
	require.Equal(t, PathDeregister, events[len(events)-1].path)
}

func TestReporterReRegistersAfterFencing(t *testing.T) {
	recorded, control := newControlStub(t, 1)
	reporter, err := NewReporter(ReporterOptions{
		BaseURL:     control.URL,
		Token:       "token",
		GatewayID:   "gw-1",
		InstanceID:  "inst-a",
		DownlinkURL: "http://gw-1:9001",
		Interval:    10 * time.Millisecond,
	})
	require.NoError(t, err)

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	go reporter.Run(ctx)

	waitFor(t, func() bool { return countEvents(recorded.snapshot(), PathRegister) >= 2 }, "re-register after fencing")
}

func countEvents(events []nodeEvent, path string) int {
	count := 0
	for _, event := range events {
		if event.path == path {
			count++
		}
	}
	return count
}
