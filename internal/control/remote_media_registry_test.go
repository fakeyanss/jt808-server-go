package control

import (
	"context"
	"errors"
	"net/http/httptest"
	"sync"
	"testing"
	"time"

	"github.com/fakeyanss/jt808-server-go/internal/contracts"
	"github.com/fakeyanss/jt808-server-go/internal/repository"
	"github.com/fakeyanss/jt808-server-go/internal/repository/memory"
)

func TestRemoteMediaSelectsLowestLoadRegisteredNode(t *testing.T) {
	repo := memory.NewMediaNodeRepository()
	heavy := &fakeMediaNode{nodeID: "media-heavy", available: 4}
	light := &fakeMediaNode{nodeID: "media-light", available: 4}
	heavyServer := httptest.NewServer(heavy.handler())
	defer heavyServer.Close()
	lightServer := httptest.NewServer(light.handler())
	defer lightServer.Close()

	now := time.Now()
	for _, node := range []*repository.MediaNode{
		{NodeID: "media-heavy", InstanceID: "i1", InternalURL: heavyServer.URL, EndpointCapacity: 4, MaxBitrateKbps: 10000, InboundBitrateKbps: 8000},
		{NodeID: "media-light", InstanceID: "i2", InternalURL: lightServer.URL, EndpointCapacity: 4, MaxBitrateKbps: 10000, InboundBitrateKbps: 1000},
	} {
		if err := repo.Register(context.Background(), node, now); err != nil {
			t.Fatalf("register %s: %v", node.NodeID, err)
		}
	}

	media := NewRemoteMedia(RemoteMediaOptions{NodeRepo: repo})
	session, err := media.CreatePlaybackSession(context.Background(), contracts.PlaybackSessionRequest{
		Phone: "13800138000", Channel: 1, Kind: "live",
	})
	if err != nil {
		t.Fatalf("create playback session: %v", err)
	}
	if heavy.createCalls != 0 || light.createCalls != 1 {
		t.Fatalf("expected lowest-load node to be selected, got heavy=%d light=%d", heavy.createCalls, light.createCalls)
	}
	stored, err := repo.Get(context.Background(), "media-light")
	if err != nil || stored == nil {
		t.Fatalf("get media node: %v", err)
	}
	if stored.EndpointsUsed != 1 {
		t.Fatalf("expected one reserved endpoint, got %d", stored.EndpointsUsed)
	}

	if err := media.ReleasePlaybackSession(context.Background(), session.ID); err != nil {
		t.Fatalf("release playback session: %v", err)
	}
	stored, err = repo.Get(context.Background(), "media-light")
	if err != nil || stored == nil {
		t.Fatalf("get media node after release: %v", err)
	}
	if stored.EndpointsUsed != 0 {
		t.Fatalf("expected endpoint reservation to be released, got %d", stored.EndpointsUsed)
	}
}

func TestRemoteMediaReservationSerializesConcurrentAllocations(t *testing.T) {
	node := &fakeMediaNode{nodeID: "media-only", available: 1}
	server := httptest.NewServer(node.handler())
	defer server.Close()

	repo := memory.NewMediaNodeRepository()
	if err := repo.Register(context.Background(), &repository.MediaNode{
		NodeID: "media-only", InstanceID: "i1", InternalURL: server.URL, EndpointCapacity: 1,
	}, time.Now()); err != nil {
		t.Fatalf("register media node: %v", err)
	}
	media := NewRemoteMedia(RemoteMediaOptions{NodeRepo: repo})

	const callers = 8
	var wg sync.WaitGroup
	var mu sync.Mutex
	succeeded := 0
	var lastErr error
	for i := 0; i < callers; i++ {
		wg.Add(1)
		go func(index int) {
			defer wg.Done()
			_, err := media.CreatePlaybackSession(context.Background(), contracts.PlaybackSessionRequest{
				Phone: "1380013800" + string(rune('0'+index)), Channel: 1, Kind: "live",
			})
			mu.Lock()
			defer mu.Unlock()
			if err == nil {
				succeeded++
				return
			}
			lastErr = err
		}(i)
	}
	wg.Wait()

	if succeeded != 1 {
		t.Fatalf("expected exactly one allocation to win the last endpoint, got %d", succeeded)
	}
	var unavailable *contracts.MediaUnavailableError
	if lastErr == nil || !errors.As(lastErr, &unavailable) || unavailable.Code != contracts.MediaCodeNodesUnavailable {
		t.Fatalf("expected nodes_unavailable for losers, got %v", lastErr)
	}
	stored, err := repo.Get(context.Background(), "media-only")
	if err != nil || stored == nil {
		t.Fatalf("get media node: %v", err)
	}
	if stored.EndpointsUsed != 1 {
		t.Fatalf("expected exactly one reserved endpoint, got %d", stored.EndpointsUsed)
	}
	if node.createCalls != 1 {
		t.Fatalf("expected one session on the node, got %d", node.createCalls)
	}
}

func TestRemoteMediaReusesSessionForConcurrentSameChannel(t *testing.T) {
	node := &fakeMediaNode{nodeID: "media-1", available: 4}
	server := httptest.NewServer(node.handler())
	defer server.Close()

	repo := memory.NewMediaNodeRepository()
	if err := repo.Register(context.Background(), &repository.MediaNode{
		NodeID: "media-1", InstanceID: "i1", InternalURL: server.URL, EndpointCapacity: 4,
	}, time.Now()); err != nil {
		t.Fatalf("register media node: %v", err)
	}
	media := NewRemoteMedia(RemoteMediaOptions{NodeRepo: repo})

	const callers = 6
	ids := make([]string, callers)
	errs := make([]error, callers)
	var wg sync.WaitGroup
	for i := 0; i < callers; i++ {
		wg.Add(1)
		go func(index int) {
			defer wg.Done()
			session, err := media.CreatePlaybackSession(context.Background(), contracts.PlaybackSessionRequest{
				Phone: "13800138000", Channel: 1, Kind: "live",
			})
			ids[index] = session.ID
			errs[index] = err
		}(i)
	}
	wg.Wait()

	for index := range ids {
		if errs[index] != nil {
			t.Fatalf("caller %d failed: %v", index, errs[index])
		}
		if ids[index] != ids[0] {
			t.Fatalf("concurrent same-channel requests must reuse one session, got %v", ids)
		}
	}
	if node.createCalls != 1 {
		t.Fatalf("expected exactly one session on the node, got %d", node.createCalls)
	}
}

func TestRemoteMediaSkipsExpiredRegisteredNodes(t *testing.T) {
	node := &fakeMediaNode{nodeID: "media-stale", available: 1}
	server := httptest.NewServer(node.handler())
	defer server.Close()

	repo := memory.NewMediaNodeRepository()
	registeredAt := time.Now().Add(-2 * repository.MediaNodeTTL)
	if err := repo.Register(context.Background(), &repository.MediaNode{
		NodeID: "media-stale", InstanceID: "i1", InternalURL: server.URL, EndpointCapacity: 1,
	}, registeredAt); err != nil {
		t.Fatalf("register media node: %v", err)
	}
	media := NewRemoteMedia(RemoteMediaOptions{NodeRepo: repo})

	_, err := media.CreatePlaybackSession(context.Background(), contracts.PlaybackSessionRequest{
		Phone: "13800138000", Channel: 1, Kind: "live",
	})
	var unavailable *contracts.MediaUnavailableError
	if !errors.As(err, &unavailable) || unavailable.Code != contracts.MediaCodeNodesUnavailable {
		t.Fatalf("expected expired node to be skipped, got %v", err)
	}
	if node.createCalls != 0 {
		t.Fatalf("expired node must not receive a session, got %d", node.createCalls)
	}
}
