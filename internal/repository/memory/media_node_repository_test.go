package memory

import (
	"context"
	"testing"
	"time"

	"github.com/fakeyanss/jt808-server-go/internal/repository"
)

func TestMediaNodeRepositoryReserveAndHeartbeat(t *testing.T) {
	ctx := context.Background()
	repo := NewMediaNodeRepository()
	now := time.Now()
	node := &repository.MediaNode{
		NodeID: "media-1", InstanceID: "i1", InternalURL: "http://127.0.0.1:7301",
		EndpointCapacity: 2, MaxBitrateKbps: 8000,
	}
	if err := repo.Register(ctx, node, now); err != nil {
		t.Fatalf("register: %v", err)
	}

	ok, err := repo.Reserve(ctx, "media-1", "i1", 1, now)
	if err != nil || !ok {
		t.Fatalf("first reserve: ok=%v err=%v", ok, err)
	}
	cpu := 42.5
	memory := uint64(1024)
	// 心跳只更新小指标，不覆盖 control 维护的端点占用。
	if ok, err := repo.Heartbeat(ctx, "media-1", "i1", repository.MediaNodeLoad{
		ActiveChannels: 3, ActiveTCPConns: 1, InboundBitrateKbps: 2000,
		CPUPercent: &cpu, MemoryBytes: &memory,
	}, now); err != nil || !ok {
		t.Fatalf("heartbeat: ok=%v err=%v", ok, err)
	}
	stored, err := repo.Get(ctx, "media-1")
	if err != nil || stored == nil {
		t.Fatalf("get: %v", err)
	}
	if stored.EndpointsUsed != 1 {
		t.Fatalf("heartbeat must not overwrite endpoints_used, got %d", stored.EndpointsUsed)
	}
	if stored.InboundBitrateKbps != 2000 || stored.ActiveChannels != 3 {
		t.Fatalf("load not applied: %+v", stored)
	}
	if stored.CPUPercent == nil || *stored.CPUPercent != cpu {
		t.Fatalf("cpu percent not applied: %+v", stored.CPUPercent)
	}

	ok, err = repo.Reserve(ctx, "media-1", "i1", 1, now)
	if err != nil || !ok {
		t.Fatalf("second reserve: ok=%v err=%v", ok, err)
	}
	// 额度已满，后续占位必须失败。
	ok, err = repo.Reserve(ctx, "media-1", "i1", 1, now)
	if err != nil {
		t.Fatalf("third reserve: %v", err)
	}
	if ok {
		t.Fatal("reserve beyond capacity must fail")
	}

	if err := repo.Release(ctx, "media-1", "i1", 5, now); err != nil {
		t.Fatalf("release: %v", err)
	}
	stored, _ = repo.Get(ctx, "media-1")
	if stored.EndpointsUsed != 0 {
		t.Fatalf("release must not go negative, got %d", stored.EndpointsUsed)
	}

	// 新实例注册即顶替旧实例：旧实例心跳被围栏，端点从零重新计数。
	if err := repo.Register(ctx, &repository.MediaNode{
		NodeID: "media-1", InstanceID: "i2", InternalURL: "http://127.0.0.1:7301",
	}, now); err != nil {
		t.Fatalf("re-register: %v", err)
	}
	if ok, err := repo.Heartbeat(ctx, "media-1", "i1", repository.MediaNodeLoad{}, now); err != nil || ok {
		t.Fatalf("stale instance heartbeat must be fenced, ok=%v err=%v", ok, err)
	}
	stored, _ = repo.Get(ctx, "media-1")
	if stored.InstanceID != "i2" || stored.EndpointsUsed != 0 {
		t.Fatalf("re-register must reset instance and endpoints: %+v", stored)
	}
}

func TestMediaNodeRepositorySkipsExpiredNodes(t *testing.T) {
	ctx := context.Background()
	repo := NewMediaNodeRepository()
	registeredAt := time.Now().Add(-2 * repository.MediaNodeTTL)
	if err := repo.Register(ctx, &repository.MediaNode{
		NodeID: "media-1", InstanceID: "i1", InternalURL: "http://127.0.0.1:7301", EndpointCapacity: 1,
	}, registeredAt); err != nil {
		t.Fatalf("register: %v", err)
	}
	nodes, err := repo.ListOnline(ctx, time.Now())
	if err != nil {
		t.Fatalf("list online: %v", err)
	}
	if len(nodes) != 0 {
		t.Fatalf("expired node must not be listed online, got %d", len(nodes))
	}
	ok, err := repo.Reserve(ctx, "media-1", "i1", 1, time.Now())
	if err != nil {
		t.Fatalf("reserve: %v", err)
	}
	if ok {
		t.Fatal("expired node must not accept reservations")
	}
}
