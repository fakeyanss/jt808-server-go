package protocol

import (
	"context"
	"errors"
	"sync"
	"testing"
	"time"

	"github.com/stretchr/testify/require"

	"github.com/fakeyanss/jt808-server-go/internal/contracts"
	"github.com/fakeyanss/jt808-server-go/internal/protocol/model"
	"github.com/fakeyanss/jt808-server-go/internal/repository/database"
	"github.com/fakeyanss/jt808-server-go/internal/uplink"
)

// fakeUplinkSink 记录出口事件，并可按需注入投递失败。
type fakeUplinkSink struct {
	mu     sync.Mutex
	events []contracts.UplinkEvent
	err    error
}

func (f *fakeUplinkSink) Deliver(_ context.Context, event contracts.UplinkEvent) (contracts.UplinkResult, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.events = append(f.events, event)
	if f.err != nil {
		return contracts.UplinkResult{Reason: f.err.Error()}, f.err
	}
	return contracts.UplinkResult{Accepted: true}, nil
}

func (f *fakeUplinkSink) count() int {
	f.mu.Lock()
	defer f.mu.Unlock()
	return len(f.events)
}

func (f *fakeUplinkSink) last(t *testing.T) contracts.UplinkEvent {
	t.Helper()
	f.mu.Lock()
	defer f.mu.Unlock()
	require.NotEmpty(t, f.events)
	return f.events[len(f.events)-1]
}

func newUplinkTestStores(t *testing.T) *database.Repositories {
	t.Helper()
	stores := database.NewMemory()
	require.NoError(t, stores.Devices.Upsert(context.Background(), &model.Device{
		Phone: "188765499021", Keepalive: time.Minute,
	}))
	return stores
}

func TestUplinkSinkBestEffortFailureStillAcks(t *testing.T) {
	stores := newUplinkTestStores(t)
	sink := &fakeUplinkSink{err: errors.New("hook unavailable")}
	policy := uplink.NewPolicy("best_effort", "best_effort", "at_least_once", "at_least_once")

	processErr, ackCh, cleanup := runUplinkPipelineWithSink(t, stores, policy, 0, sink)
	defer cleanup()
	require.NoError(t, processErr)
	require.Equal(t, 1, sink.count())

	event := sink.last(t)
	require.Equal(t, contracts.ReliabilityBestEffort, event.Reliability)
	require.Equal(t, "188765499021", event.Phone)
	require.Equal(t, uint16(0x0200), event.MsgID)
	require.Equal(t, uint16(33), event.Serial)
	require.Equal(t, "gw-test", event.GatewayID)
	require.IsType(t, &model.Msg0200{}, event.Payload)

	select {
	case ack := <-ackCh:
		packet, err := NewJT808PacketCodec().Decode(ack)
		require.NoError(t, err)
		require.Equal(t, uint16(0x8001), packet.Header.MsgID)
	case <-time.After(2 * time.Second):
		t.Fatal("best-effort must acknowledge even when the sink fails")
	}
}

func TestUplinkSinkConfirmFailureSkipsAck(t *testing.T) {
	stores := newUplinkTestStores(t)
	sink := &fakeUplinkSink{err: errors.New("hook timeout")}
	policy := uplink.NewPolicy("best_effort", "at_least_once", "at_least_once", "at_least_once")

	processErr, ackCh, cleanup := runUplinkPipelineWithSink(t, stores, policy, 0, sink)
	defer cleanup()
	require.Error(t, processErr)
	require.Equal(t, 1, sink.count())
	require.Equal(t, contracts.ReliabilityConfirmBeforeAck, sink.last(t).Reliability)

	select {
	case ack := <-ackCh:
		t.Fatalf("unexpected acknowledgement on sink failure: %x", ack)
	case <-time.After(300 * time.Millisecond):
	}
}

func TestUplinkSinkConfirmSuccessAcks(t *testing.T) {
	stores := newUplinkTestStores(t)
	sink := &fakeUplinkSink{}
	policy := uplink.NewPolicy("best_effort", "at_least_once", "at_least_once", "at_least_once")

	processErr, ackCh, cleanup := runUplinkPipelineWithSink(t, stores, policy, 0, sink)
	defer cleanup()
	require.NoError(t, processErr)
	require.Equal(t, 1, sink.count())

	select {
	case ack := <-ackCh:
		packet, err := NewJT808PacketCodec().Decode(ack)
		require.NoError(t, err)
		require.Equal(t, uint16(0x8001), packet.Header.MsgID)
	case <-time.After(2 * time.Second):
		t.Fatal("confirm-before-ack must acknowledge after successful delivery")
	}
}

// 配置出口后，事件交给出口，进程内落库不再执行。
func TestUplinkSinkReplacesLocalProcessing(t *testing.T) {
	stores := newUplinkTestStores(t)
	stores.Locations = failingLocationRepo{}
	sink := &fakeUplinkSink{}
	policy := uplink.NewPolicy("best_effort", "at_least_once", "at_least_once", "at_least_once")

	processErr, _, cleanup := runUplinkPipelineWithSink(t, stores, policy, 0, sink)
	defer cleanup()
	require.NoError(t, processErr)
	require.Equal(t, 1, sink.count())
}

func TestBuildUplinkEventIsStableAcrossRetransmission(t *testing.T) {
	header := &model.MsgHeader{
		MsgID: 0x0200, SerialNumber: 33, PhoneNumber: "188765499021", Attr: &model.MsgBodyAttr{},
	}
	incoming := &model.Msg0200{
		Header: header,
		Time:   "261101120000",
	}
	data := &model.ProcessData{Incoming: incoming}

	first := buildUplinkEvent(context.Background(), 0x0200, data, contracts.ReliabilityBestEffort, "gw-test", "")
	second := buildUplinkEvent(context.Background(), 0x0200, data, contracts.ReliabilityBestEffort, "gw-test", "")
	require.Equal(t, first.EventID, second.EventID)
	require.Equal(t, int64(1793505600000), first.OccurredAt.UnixMilli())
	// eventId 是内容哈希，重传同一报文必须得到同一值。
	require.Len(t, first.EventID, 16)
	require.Regexp(t, "^[0-9a-f]{16}$", first.EventID)
	require.False(t, first.OccurredAt.IsZero())
}

func TestBuildUplinkEventTerminalReply(t *testing.T) {
	header := &model.MsgHeader{
		MsgID: 0x0001, SerialNumber: 44, PhoneNumber: "188765499021", Attr: &model.MsgBodyAttr{},
	}
	reply := &model.Msg0001{
		Header:             header,
		AnswerSerialNumber: 77,
		AnswerMessageID:    0x8105,
		Result:             1,
	}
	event := buildUplinkEvent(context.Background(), 0x0001, &model.ProcessData{Incoming: reply},
		contracts.ReliabilityBestEffort, "gw-test", "")

	require.NotNil(t, event.Reply)
	require.Equal(t, "188765499021", event.Reply.Phone)
	require.Equal(t, uint16(0x8105), event.Reply.AnswerMsgID)
	require.Equal(t, uint16(77), event.Reply.AnswerSerial)
	require.Equal(t, uint8(1), event.Reply.Result)
	require.Equal(t, event.EventID, event.Reply.EventID)
	// 0x0001 没有协议时间，事件 ID 仍是内容哈希，重传时保持稳定。
	require.Len(t, event.EventID, 16)
	require.Regexp(t, "^[0-9a-f]{16}$", event.EventID)
}

func TestBuildUplinkEventMediaUsesRawPayload(t *testing.T) {
	header := &model.MsgHeader{
		MsgID: 0x0801, SerialNumber: 5, PhoneNumber: "188765499021", Attr: &model.MsgBodyAttr{},
	}
	media := &model.Msg0801{
		Header:              header,
		MultiMediaID:        9,
		MultiMediaType:      0,
		MultiMediaContainer: 0,
		EventID:             1,
		LogicChannelID:      2,
		FragmentData:        []byte{0xff, 0xd8, 0xff},
	}
	event := buildUplinkEvent(context.Background(), 0x0801, &model.ProcessData{Incoming: media},
		contracts.ReliabilityBestEffort, "gw-test", "")

	require.Equal(t, []byte{0xff, 0xd8, 0xff}, event.PayloadRaw)
	payload, ok := event.Payload.(map[string]any)
	require.True(t, ok)
	require.Equal(t, uint32(9), payload["multiMediaId"])
	require.Equal(t, uint32(9), event.Meta["mediaId"])
}

func TestApplySessionOutcomeBindsGenerationAndFences(t *testing.T) {
	session := &model.Session{ID: "sess-1"}
	ctx := context.WithValue(context.Background(), model.SessionCtxKey{}, session)
	processor := &JT808MsgProcessor{}

	processor.applySessionOutcome(ctx, contracts.UplinkResult{Generation: 5})
	if got := session.Generation(); got != 5 {
		t.Fatalf("generation = %d, want 5", got)
	}
	if session.ShouldCloseAfterResponse() {
		t.Fatal("session must stay open before fencing")
	}

	processor.applySessionOutcome(ctx, contracts.UplinkResult{Fenced: true})
	if !session.ShouldCloseAfterResponse() {
		t.Fatal("fenced session must close after current response")
	}
}
