package protocol

import (
	"context"
	"errors"
	"io"
	"net"
	"testing"
	"time"

	"github.com/fakeyanss/gron"
	"github.com/stretchr/testify/require"

	"github.com/fakeyanss/jt808-server-go/internal/protocol/model"
	"github.com/fakeyanss/jt808-server-go/internal/repository"
	"github.com/fakeyanss/jt808-server-go/internal/runtime"
	"github.com/fakeyanss/jt808-server-go/internal/storage"
)

// newIdleKeepaliveTimer backs a timer with a cron that is never started, so
// Register/Cancel mutate entries synchronously and assertions are race-free.
func newIdleKeepaliveTimer() *KeepaliveTimer {
	return &KeepaliveTimer{cron: gron.New()}
}

func hasKeepaliveJob(t *testing.T, timer *KeepaliveTimer, phone string) bool {
	t.Helper()
	for _, entry := range timer.Jobs() {
		job, ok := entry.Job.(*CheckDeviceJob)
		if ok && job.JobID() == phone {
			return true
		}
	}
	return false
}

// cacheKeepaliveTestDevice stores a device backed by net.Pipe ends and cleans
// both ends plus all caches when the test finishes.
func cacheKeepaliveTestDevice(t *testing.T, phone string, status model.DeviceStatus, lastCom time.Time) net.Conn {
	t.Helper()
	ctx := context.Background()
	devEnd, peerEnd := net.Pipe()
	d := &model.Device{
		ID:             "KA-" + phone,
		Plate:          "京K" + phone[len(phone)-4:],
		Phone:          phone,
		Conn:           devEnd,
		Keepalive:      time.Second,
		LastestComTime: lastCom,
		Status:         status,
		VersionDesc:    model.Version2013,
	}
	require.NoError(t, storage.Default().Devices.Upsert(ctx, d))
	t.Cleanup(func() {
		_ = storage.Default().Devices.DeleteByPhone(ctx, phone)
		_ = storage.Default().Geo.Delete(ctx, phone)
		_ = devEnd.Close()
		_ = peerEnd.Close()
	})
	return peerEnd
}

func TestKeepaliveTimerRegisterMissingDeviceIsNoop(t *testing.T) {
	timer := newIdleKeepaliveTimer()
	timer.Register("188765444999") // never cached
	require.False(t, hasKeepaliveJob(t, timer, "188765444999"))
}

func TestKeepaliveTimerRegisterAndCancelJob(t *testing.T) {
	const phone = "188765444001"
	cacheKeepaliveTestDevice(t, phone, model.DeviceStatusOnline, time.Now())
	timer := newIdleKeepaliveTimer()

	timer.Register(phone)
	require.True(t, hasKeepaliveJob(t, timer, phone))

	timer.Cancel(phone)
	require.False(t, hasKeepaliveJob(t, timer, phone))
}

func TestCheckDeviceKeepaliveTurnsExpiredOnlineDeviceOffline(t *testing.T) {
	const phone = "188765444002"
	peer := cacheKeepaliveTestDevice(t, phone, model.DeviceStatusOnline, time.Now().Add(-2*time.Second))
	timer := newIdleKeepaliveTimer()
	timer.Register(phone)

	checkDeviceKeepalive(timer, phone)

	d, err := storage.Default().Devices.GetByPhone(context.Background(), phone)
	require.NoError(t, err)
	require.Equal(t, model.DeviceStatusOffline, d.Status)
	require.True(t, hasKeepaliveJob(t, timer, phone), "offline devices keep the cleanup job")

	// The connection must still be open at the offline transition.
	require.NoError(t, peer.SetReadDeadline(time.Now().Add(200*time.Millisecond)))
	_, err = peer.Read(make([]byte, 1))
	var netErr net.Error
	require.True(t, errors.As(err, &netErr) && netErr.Timeout(), "conn should stay open, got %v", err)
}

func TestCheckDeviceKeepaliveClearsStaleOfflineDevice(t *testing.T) {
	const phone = "188765444003"
	ctx := context.Background()
	peer := cacheKeepaliveTestDevice(t, phone, model.DeviceStatusOffline, time.Now().Add(-2*time.Second))
	require.NoError(t, storage.Default().Geo.Append(ctx, &model.DeviceGeo{Phone: phone}))
	timer := newIdleKeepaliveTimer()
	timer.Register(phone)

	checkDeviceKeepalive(timer, phone)

	_, err := storage.Default().Devices.GetByPhone(ctx, phone)
	require.ErrorIs(t, err, repository.ErrDeviceNotFound)
	_, err = storage.Default().Geo.Latest(ctx, phone)
	require.ErrorIs(t, err, repository.ErrGisNotFound)
	require.False(t, hasKeepaliveJob(t, timer, phone))

	// Closing the cached pipe end unblocks the peer read with an EOF-class
	// error (exact sentinel varies with the Go net.Pipe implementation).
	_, err = peer.Read(make([]byte, 1))
	require.True(t, errors.Is(err, io.EOF) || errors.Is(err, io.ErrClosedPipe), "expected conn-closed error, got %v", err)
}

func TestCheckDeviceKeepaliveFreshDeviceNoop(t *testing.T) {
	const phone = "188765444004"
	cacheKeepaliveTestDevice(t, phone, model.DeviceStatusOnline, time.Now())
	timer := newIdleKeepaliveTimer()
	timer.Register(phone)

	checkDeviceKeepalive(timer, phone)

	d, err := storage.Default().Devices.GetByPhone(context.Background(), phone)
	require.NoError(t, err)
	require.Equal(t, model.DeviceStatusOnline, d.Status)
	require.True(t, hasKeepaliveJob(t, timer, phone))
}

// 心跳落库被节流时，保活判定以进程内最近心跳为准，不误判离线。
func TestCheckDeviceKeepaliveUsesInProcessHeartbeat(t *testing.T) {
	const phone = "188765444005"
	ctx := context.Background()
	// 库内最后交互时间已过期，但进程内刚收到心跳。
	cacheKeepaliveTestDevice(t, phone, model.DeviceStatusOnline, time.Now().Add(-2*time.Second))
	runtime.Default().Heartbeat.Touch(ctx, phone, time.Now())
	t.Cleanup(func() { runtime.Default().Heartbeat.Remove(ctx, phone) })

	timer := newIdleKeepaliveTimer()
	timer.Register(phone)
	checkDeviceKeepalive(timer, phone)

	d, err := storage.Default().Devices.GetByPhone(ctx, phone)
	require.NoError(t, err)
	require.Equal(t, model.DeviceStatusOnline, d.Status)
}

// TestCheckDeviceKeepaliveMissingDeviceIsSafe is a regression test for a nil
// dereference when the device cache entry disappeared before the check ran.
func TestCheckDeviceKeepaliveMissingDeviceIsSafe(t *testing.T) {
	// Uses the running singleton: the cache miss branch cancels the job
	// through its channel, which must not block or panic.
	require.NotPanics(t, func() {
		checkDeviceKeepalive(NewKeepaliveTimer(), "188765444998")
	})
}
