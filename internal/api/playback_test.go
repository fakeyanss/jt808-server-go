package api

import (
	"context"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"sync"
	"testing"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/require"

	"github.com/fakeyanss/jt808-server-go/internal/contracts"
	"github.com/fakeyanss/jt808-server-go/internal/protocol/model"
	"github.com/fakeyanss/jt808-server-go/internal/repository"
	"github.com/fakeyanss/jt808-server-go/internal/runtime"
	"github.com/fakeyanss/jt808-server-go/internal/storage"
)

type fakeStopDownlink struct {
	enabled bool
	result  contracts.DownlinkResult
	err     error

	mu       sync.Mutex
	requests []contracts.DownlinkRequest
}

func (f *fakeStopDownlink) Enabled() bool { return f.enabled }

func (f *fakeStopDownlink) Submit(_ context.Context, request contracts.DownlinkRequest) (contracts.DownlinkResult, error) {
	f.mu.Lock()
	f.requests = append(f.requests, request)
	f.mu.Unlock()
	if f.err != nil {
		return contracts.DownlinkResult{}, f.err
	}
	if !f.enabled {
		return contracts.DownlinkResult{}, nil
	}
	return f.result, nil
}

func (f *fakeStopDownlink) Query(_ context.Context, _ string) (contracts.DownlinkResult, error) {
	return f.result, nil
}

func (f *fakeStopDownlink) captured() []contracts.DownlinkRequest {
	f.mu.Lock()
	defer f.mu.Unlock()
	return append([]contracts.DownlinkRequest(nil), f.requests...)
}

type fakePlaybackLibrary struct {
	mu         sync.Mutex
	sessions   map[string]contracts.PlaybackSession
	releaseErr error
}

func newFakePlaybackLibrary(sessions ...contracts.PlaybackSession) *fakePlaybackLibrary {
	f := &fakePlaybackLibrary{sessions: make(map[string]contracts.PlaybackSession)}
	for _, session := range sessions {
		f.sessions[session.ID] = session
	}
	return f
}

func (f *fakePlaybackLibrary) CreatePlaybackSession(context.Context, contracts.PlaybackSessionRequest) (contracts.PlaybackSession, error) {
	return contracts.PlaybackSession{}, nil
}

func (f *fakePlaybackLibrary) PlaybackSession(_ context.Context, sessionID string) (contracts.PlaybackSession, bool) {
	f.mu.Lock()
	defer f.mu.Unlock()
	session, ok := f.sessions[sessionID]
	return session, ok
}

func (f *fakePlaybackLibrary) ReleasePlaybackSession(_ context.Context, sessionID string) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	if f.releaseErr != nil {
		return f.releaseErr
	}
	delete(f.sessions, sessionID)
	return nil
}

func (f *fakePlaybackLibrary) RecordingCoverage(context.Context, string, uint8, time.Time, time.Time) (contracts.RecordingCoverage, error) {
	return contracts.RecordingCoverage{}, nil
}

func (f *fakePlaybackLibrary) OpenRecordingSegment(context.Context, string, string, int64, int64) (io.ReadCloser, int64, error) {
	return nil, 0, contracts.ErrRecordingNotFound
}

type playbackTestEnv struct {
	server   *httptest.Server
	downlink *fakeStopDownlink
	playback *fakePlaybackLibrary
	registry runtime.ConnectionRegistry
	devices  repository.DeviceRepository
}

func newPlaybackTestEnv(t *testing.T, downlink *fakeStopDownlink, playback *fakePlaybackLibrary) *playbackTestEnv {
	t.Helper()
	gin.SetMode(gin.TestMode)
	stores, err := storage.Open(storage.Config{Type: storage.BackendMemory})
	require.NoError(t, err)
	t.Cleanup(func() { _ = stores.Close() })

	registry := runtime.NewConnectionRegistry()
	router := gin.New()
	registerPlaybackRoutes(router.Group("/api"), stores.Devices, registry, downlink, playback)
	server := httptest.NewServer(router)
	t.Cleanup(server.Close)
	return &playbackTestEnv{
		server:   server,
		downlink: downlink,
		playback: playback,
		registry: registry,
		devices:  stores.Devices,
	}
}

func (e *playbackTestEnv) deletePlayback(t *testing.T, sessionID string) map[string]any {
	t.Helper()
	request, err := http.NewRequest(http.MethodDelete, e.server.URL+"/api/playback/"+sessionID, nil)
	require.NoError(t, err)
	response, err := http.DefaultClient.Do(request)
	require.NoError(t, err)
	defer response.Body.Close()
	require.Equal(t, http.StatusOK, response.StatusCode)
	var body map[string]any
	require.NoError(t, json.NewDecoder(response.Body).Decode(&body))
	return body
}

func (e *playbackTestEnv) addDevice(t *testing.T, phone string) {
	t.Helper()
	require.NoError(t, e.devices.Upsert(context.Background(), &model.Device{
		Phone:     phone,
		Plate:     "测A00001",
		Keepalive: time.Minute,
	}))
}

func (e *playbackTestEnv) bind808(t *testing.T, phone string) {
	t.Helper()
	ctx := context.Background()
	session := &model.Session{ID: "sess-" + phone, Transport: model.TCPProto}
	require.NoError(t, e.registry.Bind(ctx, session))
	_, err := e.registry.BindPhone(ctx, phone, session.ID)
	require.NoError(t, err)
}

// TestDeletePlaybackSendsLiveStop 验证实时会话停止走 0x9102，控制字为关闭音视频传输。
func TestDeletePlaybackSendsLiveStop(t *testing.T) {
	downlink := &fakeStopDownlink{}
	playback := newFakePlaybackLibrary(contracts.PlaybackSession{
		ID: "ms-live", Phone: "18800001111", Channel: 3, Kind: playbackKindLive,
	})
	env := newPlaybackTestEnv(t, downlink, playback)
	env.addDevice(t, "18800001111")
	env.bind808(t, "18800001111")

	body := env.deletePlayback(t, "ms-live")
	require.Equal(t, true, body["released"])
	require.Equal(t, false, body["alreadyReleased"])
	stop := body["stopCommand"].(map[string]any)
	require.Equal(t, "0x9102", stop["messageId"])
	require.Equal(t, true, stop["notified"])
	require.Equal(t, false, stop["queued"])

	requests := env.downlink.captured()
	require.Len(t, requests, 1)
	msg, ok := requests[0].Message.(*model.Msg9102)
	require.True(t, ok, "unexpected message type %T", requests[0].Message)
	require.Equal(t, uint16(0x9102), msg.Header.MsgID)
	require.Equal(t, "18800001111", msg.Header.PhoneNumber)
	require.Equal(t, uint8(3), msg.LogicChannelID)
	require.Equal(t, uint8(0), msg.ControlCmd)
	require.Equal(t, uint8(0), msg.CloseMediaType)
	require.Equal(t, uint8(0), msg.StreamType)

	_, stillThere := playback.PlaybackSession(context.Background(), "ms-live")
	require.False(t, stillThere, "本地会话必须已释放")
}

// TestDeletePlaybackSendsTerminalPlaybackStop 验证终端回放会话走 0x9202，控制字为结束回放。
func TestDeletePlaybackSendsTerminalPlaybackStop(t *testing.T) {
	downlink := &fakeStopDownlink{}
	playback := newFakePlaybackLibrary(contracts.PlaybackSession{
		ID: "ms-terminal", Phone: "18800002222", Channel: 2, Kind: playbackKindTerminal,
	})
	env := newPlaybackTestEnv(t, downlink, playback)
	env.addDevice(t, "18800002222")
	env.bind808(t, "18800002222")

	body := env.deletePlayback(t, "ms-terminal")
	stop := body["stopCommand"].(map[string]any)
	require.Equal(t, "0x9202", stop["messageId"])

	requests := env.downlink.captured()
	require.Len(t, requests, 1)
	msg, ok := requests[0].Message.(*model.Msg9202)
	require.True(t, ok, "unexpected message type %T", requests[0].Message)
	require.Equal(t, uint16(0x9202), msg.Header.MsgID)
	require.Equal(t, uint8(2), msg.LogicChannelID)
	require.Equal(t, uint8(2), msg.PlayControl)
	require.Equal(t, uint8(0), msg.PlaySpeed)
	require.Nil(t, msg.DragPosition)
}

// TestDeletePlaybackIsIdempotent 验证重复 DELETE 不报错、不重复下发。
func TestDeletePlaybackIsIdempotent(t *testing.T) {
	downlink := &fakeStopDownlink{}
	playback := newFakePlaybackLibrary(contracts.PlaybackSession{
		ID: "ms-once", Phone: "18800003333", Channel: 1, Kind: playbackKindLive,
	})
	env := newPlaybackTestEnv(t, downlink, playback)
	env.addDevice(t, "18800003333")
	env.bind808(t, "18800003333")

	first := env.deletePlayback(t, "ms-once")
	require.Equal(t, false, first["alreadyReleased"])
	second := env.deletePlayback(t, "ms-once")
	require.Equal(t, true, second["alreadyReleased"])
	require.Equal(t, true, second["released"])
	require.Empty(t, second["stopCommand"].(map[string]any)["messageId"])
	require.Len(t, env.downlink.captured(), 1, "重复 DELETE 不得再次下发")
}

// TestDeletePlaybackReleasesWhenTerminalOffline 验证终端离线时仍释放，并标记未下发。
func TestDeletePlaybackReleasesWhenTerminalOffline(t *testing.T) {
	downlink := &fakeStopDownlink{}
	playback := newFakePlaybackLibrary(contracts.PlaybackSession{
		ID: "ms-offline", Phone: "18800004444", Channel: 1, Kind: playbackKindLive,
	})
	env := newPlaybackTestEnv(t, downlink, playback)
	env.addDevice(t, "18800004444")

	body := env.deletePlayback(t, "ms-offline")
	require.Equal(t, true, body["released"])
	stop := body["stopCommand"].(map[string]any)
	require.Equal(t, false, stop["notified"])
	require.Equal(t, false, stop["queued"])
	require.NotEmpty(t, stop["reason"])
	require.Empty(t, env.downlink.captured(), "离线时不应尝试发送")

	_, stillThere := playback.PlaybackSession(context.Background(), "ms-offline")
	require.False(t, stillThere)
}

// TestDeletePlaybackQueuesStopWithOutbox 验证 outbox 模式返回 commandId 并标记为已受理。
func TestDeletePlaybackQueuesStopWithOutbox(t *testing.T) {
	downlink := &fakeStopDownlink{
		enabled: true,
		result:  contracts.DownlinkResult{ID: "cmd-stop-1", Serial: 9, Accepted: true, Status: "pending"},
	}
	playback := newFakePlaybackLibrary(contracts.PlaybackSession{
		ID: "ms-queue", Phone: "18800005555", Channel: 5, Kind: playbackKindTerminal,
	})
	env := newPlaybackTestEnv(t, downlink, playback)
	env.addDevice(t, "18800005555")

	body := env.deletePlayback(t, "ms-queue")
	stop := body["stopCommand"].(map[string]any)
	require.Equal(t, false, stop["notified"])
	require.Equal(t, true, stop["queued"])
	require.Equal(t, "cmd-stop-1", stop["commandId"])
	require.Equal(t, float64(9), stop["serial"])

	requests := env.downlink.captured()
	require.Len(t, requests, 1)
	require.Equal(t, "playback-stop:ms-queue", requests[0].IdempotencyKey)
	require.Equal(t, uint16(0x9202), requests[0].Message.GetHeader().MsgID)
}

// TestDeletePlaybackReleasesWhenDispatchFails 验证下发失败不阻塞本地释放。
func TestDeletePlaybackReleasesWhenDispatchFails(t *testing.T) {
	downlink := &fakeStopDownlink{err: context.DeadlineExceeded}
	playback := newFakePlaybackLibrary(contracts.PlaybackSession{
		ID: "ms-fail", Phone: "18800006666", Channel: 1, Kind: playbackKindLive,
	})
	env := newPlaybackTestEnv(t, downlink, playback)
	env.addDevice(t, "18800006666")
	env.bind808(t, "18800006666")

	body := env.deletePlayback(t, "ms-fail")
	require.Equal(t, true, body["released"])
	stop := body["stopCommand"].(map[string]any)
	require.Equal(t, false, stop["notified"])
	require.NotEmpty(t, stop["reason"])
	_, stillThere := playback.PlaybackSession(context.Background(), "ms-fail")
	require.False(t, stillThere)
}
