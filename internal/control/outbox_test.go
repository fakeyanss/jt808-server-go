package control

import (
	"context"
	"errors"
	"sync"
	"testing"
	"time"

	"github.com/stretchr/testify/require"

	"github.com/fakeyanss/jt808-server-go/internal/contracts"
	"github.com/fakeyanss/jt808-server-go/internal/observability"
	"github.com/fakeyanss/jt808-server-go/internal/protocol/model"
	"github.com/fakeyanss/jt808-server-go/internal/repository"
)

const (
	testPhone     = "188765470101"
	testSessionID = "session-1"
)

// fakeClock 让重投测试使用固定时间，避免真实时钟导致的偶发失败。
type fakeClock struct {
	now time.Time
}

func (c *fakeClock) Now() time.Time { return c.now }

func (c *fakeClock) advance(d time.Duration) { c.now = c.now.Add(d) }

type sentMessage struct {
	sessionID string
	msgID     uint16
	serial    uint16
	body      []byte
}

// fakeSender 记录重投下发的报文，并可按需注入离线、拒绝或传输错误。
type fakeSender struct {
	mu       sync.Mutex
	sent     []sentMessage
	sendErr  error
	offline  bool
	rejected bool
}

func (f *fakeSender) Send(_ context.Context, request contracts.DownlinkRequest) (contracts.DownlinkReceipt, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	if f.sendErr != nil {
		return contracts.DownlinkReceipt{}, f.sendErr
	}
	if f.offline {
		return contracts.DownlinkReceipt{Status: contracts.DownlinkStatusOffline, Reason: "device offline"}, nil
	}
	if f.rejected {
		return contracts.DownlinkReceipt{Status: contracts.DownlinkStatusRejected, Reason: "gateway rejected downlink"}, nil
	}
	header := request.Message.GetHeader()
	body := []byte(nil)
	if raw, ok := request.Message.(*model.MsgRawBody); ok {
		body = append([]byte(nil), raw.Body...)
	}
	f.sent = append(f.sent, sentMessage{
		sessionID: request.SessionID,
		msgID:     header.MsgID,
		serial:    header.SerialNumber,
		body:      body,
	})
	return contracts.DownlinkReceipt{Status: contracts.DownlinkStatusAccepted, Serial: request.Serial}, nil
}

func (f *fakeSender) count() int {
	f.mu.Lock()
	defer f.mu.Unlock()
	return len(f.sent)
}

type harness struct {
	service *Outbox
	repo    repository.CommandRepository
	devices repository.DeviceRepository
	sender  *fakeSender
	clock   *fakeClock
	metrics *observability.Metrics
	options OutboxOptions
}

func newHarness(t *testing.T, options OutboxOptions) *harness {
	t.Helper()
	ctx := context.Background()
	repo := repository.NewCommandRepository()
	devices := repository.NewDeviceRepository()
	sender := &fakeSender{}
	metrics := observability.NewMetrics()
	clock := &fakeClock{now: time.Date(2026, 11, 8, 12, 0, 0, 0, time.UTC)}
	require.NoError(t, devices.Upsert(ctx, &model.Device{
		Phone:           testPhone,
		SessionID:       testSessionID,
		VersionDesc:     model.Version2013,
		ProtocolVersion: 0,
	}))
	service, err := NewOutbox(OutboxDependencies{
		Commands: repo,
		Devices:  devices,
		Metrics:  metrics,
		Send:     sender,
		Owner:    "test-node",
		Clock:    clock.Now,
	}, options)
	require.NoError(t, err)
	return &harness{
		service: service,
		repo:    repo,
		devices: devices,
		sender:  sender,
		clock:   clock,
		metrics: metrics,
		options: options,
	}
}

func (h *harness) enqueue(t *testing.T, commandID string, attempts int, next, expires time.Time) *repository.DownlinkCommand {
	t.Helper()
	command, created, err := h.repo.Enqueue(context.Background(), &repository.DownlinkCommand{
		CommandID:      commandID,
		IdempotencyKey: commandID,
		Phone:          testPhone,
		MsgID:          0x8105,
		Serial:         77,
		Payload:        []byte{0x02},
		Attempts:       attempts,
		NextAttemptAt:  next,
		ExpiresAt:      expires,
	})
	require.NoError(t, err)
	require.True(t, created)
	return command
}

func defaultTestOptions() OutboxOptions {
	return OutboxOptions{
		Enabled:       true,
		PollInterval:  time.Second,
		BatchSize:     10,
		MaxAttempts:   5,
		LeaseDuration: time.Minute,
		RetryBase:     5 * time.Second,
		RetryMax:      time.Minute,
	}
}

// TestReplayAfterRestart 覆盖进程重启后的重投：持久化的 pending 命令重新下发，
// 并复用原始流水号，使终端应答还能回填到原记录。
func TestReplayAfterRestart(t *testing.T) {
	ctx := context.Background()
	h := newHarness(t, defaultTestOptions())
	command := h.enqueue(t, "cmd-restart", 1, h.clock.now.Add(-time.Minute), h.clock.now.Add(time.Hour))

	require.NoError(t, h.service.Tick(ctx))
	require.Equal(t, 1, h.sender.count())

	h.sender.mu.Lock()
	sent := h.sender.sent[0]
	h.sender.mu.Unlock()
	// outbox 不再依赖本地会话：网关按 phone 路由，SessionID 由网关侧解析。
	require.Empty(t, sent.sessionID)
	require.Equal(t, uint16(0x8105), sent.msgID)
	// 重投复用原始流水号。
	require.Equal(t, command.Serial, sent.serial)
	require.Equal(t, []byte{0x02}, sent.body)

	stored, err := h.repo.Get(ctx, command.CommandID)
	require.NoError(t, err)
	require.Equal(t, 2, stored.Attempts)
	require.Equal(t, h.clock.now, stored.LastAttemptAt)
	require.NotEmpty(t, stored.LeaseOwner)

	h.service.HandleResult(model.ReliableResult{
		MsgID:  command.MsgID,
		Serial: command.Serial,
		Phone:  command.Phone,
		Status: model.ReliableStatusAcked,
	})
	stored, err = h.repo.Get(ctx, command.CommandID)
	require.NoError(t, err)
	require.Equal(t, repository.CommandStatusAcked, stored.Status)
	require.Empty(t, stored.LeaseOwner)
}

// TestReplayKeepsWaitingWhileDeviceOffline 覆盖设备离线：命令保持 pending 等待上线，
// 不消耗重投次数，设备上线后补发。
func TestReplayKeepsWaitingWhileDeviceOffline(t *testing.T) {
	ctx := context.Background()
	h := newHarness(t, defaultTestOptions())
	h.sender.offline = true
	command := h.enqueue(t, "cmd-offline", 1, h.clock.now.Add(-time.Minute), h.clock.now.Add(time.Hour))

	require.NoError(t, h.service.Tick(ctx))
	require.Equal(t, 0, h.sender.count())

	stored, err := h.repo.Get(ctx, command.CommandID)
	require.NoError(t, err)
	require.Equal(t, repository.CommandStatusPending, stored.Status)
	require.Equal(t, 1, stored.Attempts)
	require.Equal(t, "device offline", stored.LastError)
	require.Empty(t, stored.LeaseOwner)

	// 设备上线后同一轮即可补发。
	h.sender.offline = false
	require.NoError(t, h.service.Tick(ctx))
	require.Equal(t, 1, h.sender.count())
	stored, err = h.repo.Get(ctx, command.CommandID)
	require.NoError(t, err)
	require.Equal(t, 2, stored.Attempts)
}

// TestReplayOfflineDoesNotConsumeAttempts 覆盖长期离线：反复释放租约不得递增
// 或清零 attempts，否则 MaxAttempts 这道闸会在设备恢复前失效。
func TestReplayOfflineDoesNotConsumeAttempts(t *testing.T) {
	ctx := context.Background()
	options := defaultTestOptions()
	options.MaxAttempts = 2
	h := newHarness(t, options)
	h.sender.offline = true
	command := h.enqueue(t, "cmd-offline-long", 1, h.clock.now.Add(-time.Minute), h.clock.now.Add(time.Hour))

	for i := 0; i < options.MaxAttempts+5; i++ {
		require.NoError(t, h.service.Tick(ctx))
		h.clock.advance(time.Second)
	}
	stored, err := h.repo.Get(ctx, command.CommandID)
	require.NoError(t, err)
	require.Equal(t, repository.CommandStatusPending, stored.Status)
	require.Equal(t, 1, stored.Attempts)
	require.Empty(t, stored.LeaseOwner)

	// 恢复在线后仍能正常发出，且只把 attempts 推进到 2。
	h.sender.offline = false
	require.NoError(t, h.service.Tick(ctx))
	require.Equal(t, 1, h.sender.count())
	stored, err = h.repo.Get(ctx, command.CommandID)
	require.NoError(t, err)
	require.Equal(t, 2, stored.Attempts)
}

// TestReplayMarksFailedWhenAttemptsExhausted 覆盖重投上限：达到 maxAttempts 后转 failed。
func TestReplayMarksFailedWhenAttemptsExhausted(t *testing.T) {
	ctx := context.Background()
	options := defaultTestOptions()
	options.MaxAttempts = 2
	h := newHarness(t, options)
	command := h.enqueue(t, "cmd-exhausted", 1, h.clock.now.Add(-time.Minute), h.clock.now.Add(time.Hour))

	// 第一次重投把 attempts 推到 2，即上限。
	require.NoError(t, h.service.Tick(ctx))
	require.Equal(t, 1, h.sender.count())
	stored, err := h.repo.Get(ctx, command.CommandID)
	require.NoError(t, err)
	require.Equal(t, 2, stored.Attempts)

	// 会话重传耗尽后的失败事件不再安排重投，而是转 failed。
	h.service.HandleResult(model.ReliableResult{
		MsgID:  command.MsgID,
		Serial: command.Serial,
		Phone:  command.Phone,
		Status: model.ReliableStatusFailed,
		Reason: "retransmission exhausted",
	})
	stored, err = h.repo.Get(ctx, command.CommandID)
	require.NoError(t, err)
	require.Equal(t, repository.CommandStatusFailed, stored.Status)
	require.Equal(t, "retransmission exhausted", stored.LastError)
}

// TestReplayIsIdempotentAcrossInstances 覆盖多实例/重复启动：租约有效期内同一命令
// 只会被一个实例下发，后续 Tick 不会重复投递。
func TestReplayIsIdempotentAcrossInstances(t *testing.T) {
	ctx := context.Background()
	options := defaultTestOptions()
	h := newHarness(t, options)
	h.enqueue(t, "cmd-idempotent", 1, h.clock.now.Add(-time.Minute), h.clock.now.Add(time.Hour))

	second, err := NewOutbox(OutboxDependencies{
		Commands: h.repo,
		Devices:  h.devices,
		Send:     h.sender,
		Owner:    "other-node",
		Clock:    h.clock.Now,
	}, options)
	require.NoError(t, err)

	require.NoError(t, h.service.Tick(ctx))
	require.Equal(t, 1, h.sender.count())

	// 另一个实例抢不到已租约的命令。
	require.NoError(t, second.Tick(ctx))
	require.Equal(t, 1, h.sender.count())

	// 同一实例在租约内再次 Tick 也不会重复下发。
	require.NoError(t, h.service.Tick(ctx))
	require.Equal(t, 1, h.sender.count())

	// 租约到期后允许重新占用，体现 at-least-once。
	h.clock.advance(options.LeaseDuration + time.Second)
	require.NoError(t, second.Tick(ctx))
	require.Equal(t, 2, h.sender.count())
}

// TestReplayExpiresCommand 覆盖过期兜底：长期离线且超过有效期后转 failed。
func TestReplayExpiresCommand(t *testing.T) {
	ctx := context.Background()
	h := newHarness(t, defaultTestOptions())
	command := h.enqueue(t, "cmd-expired", 1, h.clock.now.Add(-time.Minute), h.clock.now.Add(-time.Second))

	require.NoError(t, h.service.Tick(ctx))
	require.Equal(t, 0, h.sender.count())

	stored, err := h.repo.Get(ctx, command.CommandID)
	require.NoError(t, err)
	require.Equal(t, repository.CommandStatusFailed, stored.Status)
	require.Equal(t, "expired before delivery", stored.LastError)
}

// TestReplaySchedulesBackoffOnFailure 覆盖失败后的退避重排：未到上限时保持 pending，
// 并按尝试次数设置下一次可投时刻。
func TestReplaySchedulesBackoffOnFailure(t *testing.T) {
	ctx := context.Background()
	options := defaultTestOptions()
	h := newHarness(t, options)
	command := h.enqueue(t, "cmd-backoff", 1, h.clock.now.Add(-time.Minute), h.clock.now.Add(time.Hour))

	require.NoError(t, h.service.Tick(ctx))
	h.service.HandleResult(model.ReliableResult{
		MsgID:  command.MsgID,
		Serial: command.Serial,
		Phone:  command.Phone,
		Status: model.ReliableStatusFailed,
		Reason: "write failed",
	})

	stored, err := h.repo.Get(ctx, command.CommandID)
	require.NoError(t, err)
	require.Equal(t, repository.CommandStatusPending, stored.Status)
	require.Equal(t, "write failed", stored.LastError)
	require.Empty(t, stored.LeaseOwner)
	// attempts=2 时退避为 2×RetryBase。
	require.Equal(t, h.clock.now.Add(2*options.RetryBase), stored.NextAttemptAt)
}

// TestReplaySynchronousSendFailureSchedulesRetry 覆盖同步写失败：不直接判死，保持 pending。
func TestReplaySynchronousSendFailureSchedulesRetry(t *testing.T) {
	ctx := context.Background()
	h := newHarness(t, defaultTestOptions())
	h.sender.sendErr = errors.New("write failed")
	command := h.enqueue(t, "cmd-send-fail", 1, h.clock.now.Add(-time.Minute), h.clock.now.Add(time.Hour))

	require.NoError(t, h.service.Tick(ctx))
	stored, err := h.repo.Get(ctx, command.CommandID)
	require.NoError(t, err)
	require.Equal(t, repository.CommandStatusPending, stored.Status)
	require.Equal(t, "write failed", stored.LastError)
	require.Empty(t, stored.LeaseOwner)
}

// TestReplayPermanentErrorMarksFailedImmediately 覆盖确定性失败：400/401/403/413
// 重投必然同样失败，应直接终结并记录原因，不进入退避。
func TestReplayPermanentErrorMarksFailedImmediately(t *testing.T) {
	ctx := context.Background()
	h := newHarness(t, defaultTestOptions())
	h.sender.sendErr = &contracts.DownlinkError{
		Kind:       contracts.DownlinkErrorPermanent,
		StatusCode: 403,
		Message:    "forbidden",
	}
	command := h.enqueue(t, "cmd-permanent", 1, h.clock.now.Add(-time.Minute), h.clock.now.Add(time.Hour))

	require.NoError(t, h.service.Tick(ctx))
	stored, err := h.repo.Get(ctx, command.CommandID)
	require.NoError(t, err)
	require.Equal(t, repository.CommandStatusFailed, stored.Status)
	require.Contains(t, stored.LastError, "403")
	require.Empty(t, stored.LeaseOwner)
}

// TestReplayTransientHonorsRetryAfter 覆盖 429 限流：退避采用网关给出的 Retry-After，
// 不按本地指数退避立刻重投。
func TestReplayTransientHonorsRetryAfter(t *testing.T) {
	ctx := context.Background()
	h := newHarness(t, defaultTestOptions())
	h.sender.sendErr = &contracts.DownlinkError{
		Kind:       contracts.DownlinkErrorTransient,
		StatusCode: 429,
		RetryAfter: 30 * time.Second,
		Message:    "too many requests",
	}
	command := h.enqueue(t, "cmd-429", 1, h.clock.now.Add(-time.Minute), h.clock.now.Add(time.Hour))

	require.NoError(t, h.service.Tick(ctx))
	stored, err := h.repo.Get(ctx, command.CommandID)
	require.NoError(t, err)
	require.Equal(t, repository.CommandStatusPending, stored.Status)
	require.Contains(t, stored.LastError, "429")
	require.True(t, stored.NextAttemptAt.Equal(h.clock.now.Add(30*time.Second)),
		"next attempt should honor Retry-After, got %s", stored.NextAttemptAt)
	require.Empty(t, stored.LeaseOwner)
}

// TestSubmitPermanentErrorFailsImmediately 覆盖首次下发即确定性失败：命令不进入
// 重投窗口，直接终结，避免占用 in-flight 名额。
func TestSubmitPermanentErrorFailsImmediately(t *testing.T) {
	ctx := context.Background()
	h := newHarness(t, defaultTestOptions())
	h.sender.sendErr = &contracts.DownlinkError{
		Kind:       contracts.DownlinkErrorPermanent,
		StatusCode: 400,
		Message:    "bad request",
	}
	result, err := h.service.Submit(ctx, contracts.DownlinkRequest{
		Phone: testPhone,
		Message: &model.MsgRawBody{
			Header: model.GenMsgHeader(&model.Device{Phone: testPhone, VersionDesc: model.Version2013}, 0x8105, 0),
			Body:   []byte{0x02},
		},
	})
	require.Error(t, err)
	require.NotEmpty(t, result.ID)
	require.Equal(t, 0, h.sender.count())

	stored, getErr := h.repo.Get(ctx, result.ID)
	require.NoError(t, getErr)
	require.Equal(t, repository.CommandStatusFailed, stored.Status)
	require.Contains(t, stored.LastError, "400")
}
