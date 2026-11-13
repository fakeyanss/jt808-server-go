package control

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"strconv"
	"sync"
	"testing"
	"time"

	"github.com/fakeyanss/jt808-server-go/internal/contracts"
)

type fakeMediaNode struct {
	mu          sync.Mutex
	nodeID      string
	available   int
	sessions    []map[string]any
	createCalls int
	deleteCalls int
	nextID      int
	coverage    contracts.RecordingCoverage
}

func (n *fakeMediaNode) handler() http.Handler {
	mux := http.NewServeMux()
	mux.HandleFunc("/internal/media/status", func(w http.ResponseWriter, _ *http.Request) {
		n.mu.Lock()
		defer n.mu.Unlock()
		_ = json.NewEncoder(w).Encode(map[string]any{
			"nodeId":        n.nodeID,
			"active":        len(n.sessions),
			"maxConcurrent": n.available,
			"available":     n.available - len(n.sessions),
			"sessions":      n.sessions,
		})
	})
	mux.HandleFunc("/internal/media/sessions", func(w http.ResponseWriter, r *http.Request) {
		n.mu.Lock()
		defer n.mu.Unlock()
		switch r.Method {
		case http.MethodPost:
			var request struct {
				Phone   string `json:"phone"`
				Channel uint8  `json:"channel"`
				Kind    string `json:"kind"`
			}
			_ = json.NewDecoder(r.Body).Decode(&request)
			// 节点侧已有同一通道的会话时返回 409，管控据此复用而不是重复开流。
			for _, existing := range n.sessions {
				if existing["phone"] == request.Phone && existing["channel"] == request.Channel {
					w.WriteHeader(http.StatusConflict)
					_ = json.NewEncoder(w).Encode(map[string]any{
						"code": "session_conflict",
						"existing": map[string]any{
							"sessionId": existing["sessionId"],
							"kind":      existing["kind"],
							"expiresAt": existing["expiresAt"],
						},
					})
					return
				}
			}
			n.createCalls++
			n.nextID++
			session := map[string]any{
				"sessionId": n.nodeID + "-" + strconv.Itoa(n.nextID),
				"phone":     request.Phone,
				"channel":   request.Channel,
				"kind":      request.Kind,
				"expiresAt": time.Now().Add(time.Minute),
				"serverIp":  "127.0.0.1",
				"tcpPort":   7300,
			}
			n.sessions = append(n.sessions, session)
			w.WriteHeader(http.StatusCreated)
			_ = json.NewEncoder(w).Encode(session)
		default:
			w.WriteHeader(http.StatusMethodNotAllowed)
		}
	})
	mux.HandleFunc("/internal/media/sessions/", func(w http.ResponseWriter, r *http.Request) {
		n.mu.Lock()
		defer n.mu.Unlock()
		n.deleteCalls++
		w.WriteHeader(http.StatusNoContent)
	})
	mux.HandleFunc("/internal/media/recordings/coverage", func(w http.ResponseWriter, _ *http.Request) {
		n.mu.Lock()
		defer n.mu.Unlock()
		_ = json.NewEncoder(w).Encode(n.coverage)
	})
	return mux
}

func TestRemoteMediaReturnsUnavailableWithoutNodes(t *testing.T) {
	media := NewRemoteMedia(RemoteMediaOptions{})
	_, err := media.CreatePlaybackSession(context.Background(), contracts.PlaybackSessionRequest{
		Phone: "13800138000", Channel: 1, Kind: "live",
	})
	var unavailable *contracts.MediaUnavailableError
	if !errors.As(err, &unavailable) || unavailable.Code != contracts.MediaCodeNoNodes {
		t.Fatalf("expected no_media_node_configured, got %v", err)
	}
	if err := media.Check(context.Background()); err == nil {
		t.Fatal("expected readiness check to fail without nodes")
	}
}

func TestRemoteMediaCreatesAndReleasesOnSameNode(t *testing.T) {
	first := &fakeMediaNode{nodeID: "media-a", available: 2}
	second := &fakeMediaNode{nodeID: "media-b", available: 8}
	firstServer := httptest.NewServer(first.handler())
	defer firstServer.Close()
	secondServer := httptest.NewServer(second.handler())
	defer secondServer.Close()

	media := NewRemoteMedia(RemoteMediaOptions{Nodes: []MediaNode{
		{Name: "a", URL: firstServer.URL},
		{Name: "b", URL: secondServer.URL},
	}})
	ctx := context.Background()
	session, err := media.CreatePlaybackSession(ctx, contracts.PlaybackSessionRequest{
		Phone: "13800138000", Channel: 1, Kind: "live", TTL: time.Minute,
	})
	if err != nil {
		t.Fatalf("create playback session: %v", err)
	}
	// 第二个节点剩余额度更多，应被优先选中。
	if second.createCalls != 1 || first.createCalls != 0 {
		t.Fatalf("expected node b to be selected, got a=%d b=%d", first.createCalls, second.createCalls)
	}

	// 同一 (phone, channel) 再申请必须复用已有分配，且不会再向任何节点发创建请求。
	reused, err := media.CreatePlaybackSession(ctx, contracts.PlaybackSessionRequest{
		Phone: "13800138000", Channel: 1, Kind: "live",
	})
	if err != nil {
		t.Fatalf("expected reuse of existing session, got %v", err)
	}
	if reused.ID != session.ID {
		t.Fatalf("expected reused session %s, got %s", session.ID, reused.ID)
	}
	if first.createCalls != 0 || second.createCalls != 1 {
		t.Fatalf("reuse must not create a second session, got a=%d b=%d", first.createCalls, second.createCalls)
	}

	if err := media.ReleasePlaybackSession(ctx, session.ID); err != nil {
		t.Fatalf("release playback session: %v", err)
	}
	if second.deleteCalls != 1 {
		t.Fatalf("expected release on node b, got %d", second.deleteCalls)
	}
}

func TestRemoteMediaRestoresStickinessFromNodeStatus(t *testing.T) {
	first := &fakeMediaNode{nodeID: "media-a", available: 2}
	first.sessions = []map[string]any{{
		"sessionId": "existing-1",
		"phone":     "13800138000",
		"channel":   1,
		"kind":      "live",
		"expiresAt": time.Now().Add(time.Minute),
	}}
	second := &fakeMediaNode{nodeID: "media-b", available: 8}
	firstServer := httptest.NewServer(first.handler())
	defer firstServer.Close()
	secondServer := httptest.NewServer(second.handler())
	defer secondServer.Close()

	media := NewRemoteMedia(RemoteMediaOptions{Nodes: []MediaNode{
		{Name: "a", URL: firstServer.URL},
		{Name: "b", URL: secondServer.URL},
	}})
	// 管控本地没有映射时，节点返回 409；同类型会话应被复用而不是报冲突。
	session, err := media.CreatePlaybackSession(context.Background(), contracts.PlaybackSessionRequest{
		Phone: "13800138000", Channel: 1, Kind: "live",
	})
	if err != nil {
		t.Fatalf("expected reuse of node session, got %v", err)
	}
	if session.ID != "existing-1" {
		t.Fatalf("unexpected reused session: %+v", session)
	}
	if first.createCalls != 0 || second.createCalls != 0 {
		t.Fatal("reusing the node session must not create a new session")
	}
}

func TestRemoteMediaCoverageMergesNodes(t *testing.T) {
	start := time.Date(2026, 11, 12, 0, 0, 0, 0, time.UTC)
	first := &fakeMediaNode{nodeID: "media-a", available: 1, coverage: contracts.RecordingCoverage{
		Segments: []contracts.RecordingSegmentInfo{{ID: "seg-1", Start: start, End: start.Add(time.Minute)}},
		Available: []contracts.TimeRange{{
			Start: start, End: start.Add(time.Minute),
		}},
	}}
	second := &fakeMediaNode{nodeID: "media-b", available: 1, coverage: contracts.RecordingCoverage{
		Segments: []contracts.RecordingSegmentInfo{{ID: "seg-2", Start: start.Add(time.Minute), End: start.Add(2 * time.Minute)}},
		Available: []contracts.TimeRange{{
			Start: start.Add(time.Minute), End: start.Add(2 * time.Minute),
		}},
	}}
	firstServer := httptest.NewServer(first.handler())
	defer firstServer.Close()
	secondServer := httptest.NewServer(second.handler())
	defer secondServer.Close()

	media := NewRemoteMedia(RemoteMediaOptions{Nodes: []MediaNode{
		{Name: "a", URL: firstServer.URL},
		{Name: "b", URL: secondServer.URL},
	}})
	coverage, err := media.RecordingCoverage(context.Background(), "13800138000", 1, start, start.Add(2*time.Minute))
	if err != nil {
		t.Fatalf("recording coverage: %v", err)
	}
	if len(coverage.Segments) != 2 || len(coverage.Missing) != 0 || !coverage.Playable {
		t.Fatalf("unexpected coverage: %+v", coverage)
	}
}
