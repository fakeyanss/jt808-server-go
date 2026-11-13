package control

import (
	"context"
	"io"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/stretchr/testify/require"

	"github.com/fakeyanss/jt808-server-go/internal/contracts"
	"github.com/fakeyanss/jt808-server-go/internal/mediaupload"
	"github.com/fakeyanss/jt808-server-go/internal/protocol/model"
	"github.com/fakeyanss/jt808-server-go/internal/repository"
	"github.com/fakeyanss/jt808-server-go/internal/repository/database"
)

func TestLocalUplinkSinkDelivers(t *testing.T) {
	sink := NewLocalUplinkSink(func(_ context.Context, event contracts.UplinkEvent) (contracts.UplinkResult, error) {
		require.Equal(t, testPhone, event.Phone)
		return contracts.UplinkResult{Accepted: true, Reply: []byte{0x00}}, nil
	})
	result, err := sink.Deliver(context.Background(), contracts.UplinkEvent{Phone: testPhone})
	require.NoError(t, err)
	require.True(t, result.Accepted)
	require.Equal(t, []byte{0x00}, result.Reply)
}

func TestLocalUplinkSinkRequiresHandler(t *testing.T) {
	_, err := NewLocalUplinkSink(nil).Deliver(context.Background(), contracts.UplinkEvent{})
	require.Error(t, err)
}

func TestLocalAuthProviderReadsDevice(t *testing.T) {
	ctx := context.Background()
	const phone = "188765499001"
	devices := database.Default().Devices
	require.NoError(t, devices.Upsert(ctx, &model.Device{Phone: phone, AuthCode: "AUTH-1"}))
	t.Cleanup(func() { _ = devices.DeleteByPhone(ctx, phone) })

	provider := NewLocalAuthProvider(devices)
	device, err := provider.Device(ctx, phone)
	require.NoError(t, err)
	require.Equal(t, phone, device.Phone)

	code, err := provider.AuthCode(ctx, phone)
	require.NoError(t, err)
	require.Equal(t, "AUTH-1", code)

	_, err = provider.AuthCode(ctx, "188765499099")
	require.Error(t, err)
}

type fakeStreams struct{ count int }

func (f fakeStreams) Streams() int { return f.count }

func TestLocalMediaReadsStreamsAndFiles(t *testing.T) {
	root := t.TempDir()
	require.NoError(t, os.WriteFile(filepath.Join(root, "image.jpg"), []byte("jpeg"), 0o644))
	store, err := mediaupload.NewStore(root)
	require.NoError(t, err)

	media := NewLocalMedia(fakeStreams{count: 3}, store)
	require.Equal(t, 3, media.Streams())

	file, err := media.Open("image.jpg")
	require.NoError(t, err)
	defer file.Close()
	content, err := io.ReadAll(file)
	require.NoError(t, err)
	require.Equal(t, []byte("jpeg"), content)

	_, err = media.Open("../outside.jpg")
	require.Error(t, err)
}

type fakeAttachmentStats struct{}

func (fakeAttachmentStats) Stats() (int64, int64) { return 2, 7 }

func TestLocalAttachmentReadsStatsAndRoot(t *testing.T) {
	attachment := NewLocalAttachment(fakeAttachmentStats{}, "/data/attachments")
	active, completed := attachment.Stats()
	require.Equal(t, int64(2), active)
	require.Equal(t, int64(7), completed)
	require.Equal(t, "/data/attachments", attachment.Root())
}

func TestStaticMediaAllocatorIsSticky(t *testing.T) {
	allocator := NewStaticMediaAllocator(contracts.MediaEndpoint{IP: "10.0.0.1", TCPPort: 7300, UDPPort: 7301}, time.Minute)
	request := contracts.MediaLeaseRequest{Phone: testPhone, Channel: 1, Source: "live"}
	first, err := allocator.Allocate(context.Background(), request)
	require.NoError(t, err)
	second, err := allocator.Allocate(context.Background(), request)
	require.NoError(t, err)
	require.Equal(t, first.Endpoint, second.Endpoint)
	require.NotEqual(t, first.LeaseID, second.LeaseID)
	require.NoError(t, allocator.Release(context.Background(), first.LeaseID))
}

func TestStaticAttachmentAllocatorIsSticky(t *testing.T) {
	allocator := NewStaticAttachmentAllocator(contracts.AttachmentEndpoint{IP: "10.0.0.2", TCPPort: 7600, UDPPort: 7601}, time.Minute)
	request := contracts.AttachmentLeaseRequest{Phone: testPhone, AlarmID: "alarm-1"}
	first, err := allocator.Allocate(context.Background(), request)
	require.NoError(t, err)
	second, err := allocator.Allocate(context.Background(), request)
	require.NoError(t, err)
	require.Equal(t, first.Endpoint, second.Endpoint)
	require.NotEqual(t, first.LeaseID, second.LeaseID)
	require.NoError(t, allocator.Release(context.Background(), first.LeaseID))
}

// TestSubmitOffModeSendsWithoutPersistence 覆盖 off 模式：直接发送、不落库。
func TestSubmitOffModeSendsWithoutPersistence(t *testing.T) {
	sender := &fakeSender{}
	outbox, err := NewOutbox(OutboxDependencies{
		Send: sender,
	}, OutboxOptions{Enabled: false})
	require.NoError(t, err)

	msg := &model.Msg8105{
		Header:      &model.MsgHeader{MsgID: 0x8105, PhoneNumber: testPhone, Attr: &model.MsgBodyAttr{VersionDesc: model.Version2013}},
		CommandWord: 2,
	}
	result, err := outbox.Submit(context.Background(), contracts.DownlinkRequest{
		SessionID: testSessionID,
		Serial:    9,
		Message:   msg,
	})
	require.NoError(t, err)
	require.True(t, result.Accepted)
	require.Equal(t, 1, sender.count())
}

// TestSubmitIsIdempotent 覆盖 durable 模式：同一幂等键只入队一次、只发送一次。
func TestSubmitIsIdempotent(t *testing.T) {
	h := newHarness(t, defaultTestOptions())
	device, err := h.devices.GetByPhone(context.Background(), testPhone)
	require.NoError(t, err)

	msg := &model.Msg8105{Header: model.GenMsgHeader(device, 0x8105, 11), CommandWord: 2}
	request := contracts.DownlinkRequest{
		SessionID:      testSessionID,
		IdempotencyKey: "idem-1",
		Serial:         11,
		Message:        msg,
	}
	first, err := h.service.Submit(context.Background(), request)
	require.NoError(t, err)
	require.False(t, first.Duplicated)
	require.NotEmpty(t, first.ID)
	require.Equal(t, 1, h.sender.count())

	second, err := h.service.Submit(context.Background(), request)
	require.NoError(t, err)
	require.True(t, second.Duplicated)
	require.Equal(t, first.ID, second.ID)
	require.Equal(t, 1, h.sender.count())

	stored, err := h.repo.Get(context.Background(), first.ID)
	require.NoError(t, err)
	require.Equal(t, testPhone, stored.Phone)
}

// TestTerminalReplyOutcomeIsTransportIndependent 覆盖 local 与 http 两种来源：
// Source 只用于日志，不参与状态机，同一应答语义必须得到同一 outcome。
func TestTerminalReplyOutcomeIsTransportIndependent(t *testing.T) {
	ctx := context.Background()
	options := defaultTestOptions()
	options.InitialWait = time.Second
	options.ExpireAfter = time.Hour
	h := newHarness(t, options)
	device, err := h.devices.GetByPhone(ctx, testPhone)
	require.NoError(t, err)

	submit := func(serial uint16, key string) string {
		msg := &model.Msg8105{Header: model.GenMsgHeader(device, 0x8105, serial), CommandWord: 2}
		result, err := h.service.Submit(ctx, contracts.DownlinkRequest{
			SessionID:      testSessionID,
			IdempotencyKey: key,
			Serial:         serial,
			Message:        msg,
		})
		require.NoError(t, err)
		return result.ID
	}
	cases := []struct {
		id     string
		serial uint16
		source string
	}{
		{id: submit(21, "local-source"), serial: 21, source: "transport"},
		{id: submit(22, "http-source"), serial: 22, source: "http"},
	}
	for _, tc := range cases {
		outcome, err := h.service.HandleTerminalReply(ctx, contracts.TerminalReply{
			Phone:        testPhone,
			AnswerMsgID:  0x8105,
			AnswerSerial: tc.serial,
			Result:       0,
			Source:       tc.source,
		})
		require.NoError(t, err)
		require.Equal(t, contracts.TerminalReplyAcked, outcome)

		stored, err := h.repo.Get(ctx, tc.id)
		require.NoError(t, err)
		require.Equal(t, repository.CommandStatusAcked, stored.Status)
	}
}
