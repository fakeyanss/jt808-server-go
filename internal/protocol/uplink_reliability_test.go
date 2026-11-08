package protocol

import (
	"context"
	"errors"
	"net"
	"testing"
	"time"

	"github.com/stretchr/testify/require"

	"github.com/fakeyanss/jt808-server-go/internal/protocol/model"
	"github.com/fakeyanss/jt808-server-go/internal/repository"
	"github.com/fakeyanss/jt808-server-go/internal/runtime"
	"github.com/fakeyanss/jt808-server-go/internal/storage"
	"github.com/fakeyanss/jt808-server-go/internal/uplink"
)

// failingLocationRepo 模拟落库失败，用于验证 at_least_once 不应答。
type failingLocationRepo struct{}

func (failingLocationRepo) Record(context.Context, []repository.LocationRecord) error {
	return errors.New("storage unavailable")
}

func (failingLocationRepo) List(context.Context, string, int) ([]repository.LocationRecord, error) {
	return nil, nil
}

func (failingLocationRepo) ListPage(context.Context, repository.HistoryQuery) ([]repository.LocationRecord, int, error) {
	return nil, 0, nil
}

// recordingLocationRepo 记录成功落库的定位，用于验证先落库后应答。
type recordingLocationRepo struct {
	records []repository.LocationRecord
}

func (r *recordingLocationRepo) Record(_ context.Context, records []repository.LocationRecord) error {
	r.records = append(r.records, records...)
	return nil
}

func (r *recordingLocationRepo) List(context.Context, string, int) ([]repository.LocationRecord, error) {
	return nil, nil
}

func (r *recordingLocationRepo) ListPage(context.Context, repository.HistoryQuery) ([]repository.LocationRecord, int, error) {
	return nil, 0, nil
}

// runUplinkPipeline 启动一次完整上行链路，返回应答帧通道。
// peerEnd 上的读取协程只取第一帧（通用应答），用于判断本轮是否应答。
func runUplinkPipeline(t *testing.T, stores *storage.Stores, policy uplink.Policy, alarmSign uint32) (error, <-chan []byte, func()) {
	t.Helper()

	serverEnd, peerEnd := net.Pipe()
	ackCh := make(chan []byte, 1)
	go func() {
		buf := make([]byte, 4096)
		n, readErr := peerEnd.Read(buf)
		if readErr == nil && n > 0 {
			ackCh <- append([]byte(nil), buf[:n]...)
		}
	}()

	header := &model.MsgHeader{MsgID: 0x0200, SerialNumber: 33, PhoneNumber: "188765499021", Attr: &model.MsgBodyAttr{}}
	frame, err := NewJT808PacketCodec().EncodePacketData(header, locationReportBody(alarmSign))
	require.NoError(t, err)

	pipeline := NewPipeline(serverEnd, WithDeps(ProcessorDeps{
		Storage: stores,
		Runtime: runtime.NewStores(),
		Uplink:  &policy,
	}))
	session := &model.Session{ID: "uplink-reliability", Conn: serverEnd, AckTimeout: time.Hour, MaxRetries: 1}

	writeDone := make(chan struct{})
	go func() {
		defer close(writeDone)
		_, _ = peerEnd.Write(frame)
	}()

	ctx := context.WithValue(context.Background(), model.SessionCtxKey{}, session)
	processErr := pipeline.ProcessConnRead(ctx)
	<-writeDone

	return processErr, ackCh, func() {
		session.ClosePending()
		_ = serverEnd.Close()
		_ = peerEnd.Close()
	}
}

// 落库成功时才发出 0x8001 通用应答。
func TestAtLeastOnceAcksAfterLocationPersisted(t *testing.T) {
	stores := storage.NewMemory()
	require.NoError(t, stores.Devices.Upsert(context.Background(), &model.Device{Phone: "188765499021", Keepalive: time.Minute}))
	repo := &recordingLocationRepo{}
	stores.Locations = repo

	policy := uplink.NewPolicy("best_effort", "best_effort", "at_least_once", "at_least_once")
	processErr, ackCh, cleanup := runUplinkPipeline(t, stores, policy, 1)
	defer cleanup()
	require.NoError(t, processErr)
	require.NotEmpty(t, repo.records, "location must be persisted before acknowledgement")

	select {
	case ack := <-ackCh:
		packet, err := NewJT808PacketCodec().Decode(ack)
		require.NoError(t, err)
		require.Equal(t, uint16(0x8001), packet.Header.MsgID)
	case <-time.After(2 * time.Second):
		t.Fatal("expected a 0x8001 acknowledgement")
	}
}

// 落库失败时本轮不发出任何应答，让终端按协议重传。
func TestAtLeastOnceSkipsAckWhenLocationPersistFails(t *testing.T) {
	stores := storage.NewMemory()
	require.NoError(t, stores.Devices.Upsert(context.Background(), &model.Device{Phone: "188765499021", Keepalive: time.Minute}))
	stores.Locations = failingLocationRepo{}

	policy := uplink.NewPolicy("best_effort", "best_effort", "at_least_once", "at_least_once")
	processErr, ackCh, cleanup := runUplinkPipeline(t, stores, policy, 1)
	defer cleanup()
	require.Error(t, processErr)

	select {
	case ack := <-ackCh:
		t.Fatalf("unexpected acknowledgement on persistence failure: %x", ack)
	case <-time.After(300 * time.Millisecond):
	}
}

// best_effort 保留旧语义：先应答，落库失败只记日志、不影响连接。
func TestBestEffortAcksBeforeLocationPersistFailure(t *testing.T) {
	stores := storage.NewMemory()
	require.NoError(t, stores.Devices.Upsert(context.Background(), &model.Device{Phone: "188765499021", Keepalive: time.Minute}))
	stores.Locations = failingLocationRepo{}

	policy := uplink.NewPolicy("best_effort", "best_effort", "at_least_once", "at_least_once")
	processErr, ackCh, cleanup := runUplinkPipeline(t, stores, policy, 0)
	defer cleanup()
	require.NoError(t, processErr)

	select {
	case ack := <-ackCh:
		packet, err := NewJT808PacketCodec().Decode(ack)
		require.NoError(t, err)
		require.Equal(t, uint16(0x8001), packet.Header.MsgID)
	case <-time.After(2 * time.Second):
		t.Fatal("best_effort must acknowledge before persisting")
	}
}
