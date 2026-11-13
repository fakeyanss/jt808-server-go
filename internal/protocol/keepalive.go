package protocol

import (
	"context"
	"sync"
	"time"

	"github.com/fakeyanss/gron"
	"github.com/pkg/errors"
	"github.com/rs/zerolog/log"

	"github.com/fakeyanss/jt808-server-go/internal/protocol/model"
	"github.com/fakeyanss/jt808-server-go/internal/repository"
	"github.com/fakeyanss/jt808-server-go/internal/repository/database"
	"github.com/fakeyanss/jt808-server-go/internal/runtime"
)

type KeepaliveTimer struct {
	cron *gron.Cron
}

var timerSingleton *KeepaliveTimer
var timerInitOnce sync.Once

func NewKeepaliveTimer() *KeepaliveTimer {
	timerInitOnce.Do(func() {
		timerSingleton = &KeepaliveTimer{
			cron: gron.New(),
		}
		timerSingleton.cron.Start()
	})
	return timerSingleton
}

func (t *KeepaliveTimer) Register(devicePhone string) {
	ctx := context.Background()
	device, err := database.Default().Devices.GetByPhone(ctx, devicePhone)
	if errors.Is(err, repository.ErrDeviceNotFound) {
		return
	}
	job := &CheckDeviceJob{phone: devicePhone}
	t.cron.Add(gron.Every(device.Keepalive), job)
	log.Debug().Str("device", devicePhone).Msg("Register device keepalive check job")
}

func (t *KeepaliveTimer) Cancel(devicePhone string) {
	t.cron.Cancel(devicePhone)
}

func (t *KeepaliveTimer) Jobs() []*gron.Entry {
	return t.cron.Entries()
}

type CheckDeviceJob struct {
	phone string
}

func (j *CheckDeviceJob) JobID() string {
	return j.phone
}

func (j *CheckDeviceJob) Run() {
	checkDeviceKeepalive(timerSingleton, j.phone)
}

// 检查设备保活
// 1. 当前在线，保活失效，改为离线，断开tcp连接
// 2. 当前离线，缓存保留3倍保活时间
func checkDeviceKeepalive(t *KeepaliveTimer, devicePhone string) {
	log.Debug().Str("device", devicePhone).Msg("Check device keepalive status")
	ctx := context.Background()
	stores := database.Default()
	d, err := stores.Devices.GetByPhone(ctx, devicePhone)
	if errors.Is(err, repository.ErrDeviceNotFound) {
		log.Debug().Str("device", devicePhone).Msg("Fail to find device cache")
		t.Cancel(devicePhone)
		return
	}
	// 心跳落库可能被节流，进程内最近心跳更实时；取两者较新值判断保活。
	if last, ok := runtime.Default().Heartbeat.Last(ctx, devicePhone); ok && last.After(d.LastestComTime) {
		d.LastestComTime = last
	}
	// SQL 后端不再持久化运行时状态，读出的 Conn/SessionID/Status 为零值。
	// 此时不能按库内状态清理静态档案，只用进程内连接与最近通信时间判定在线。
	if d.Conn == nil {
		checkSQLBackedKeepalive(ctx, devicePhone, d.Keepalive, d.LastestComTime)
		return
	}
	if d.ShouleTurnOffline() {
		// 保活失效
		d.Status = model.DeviceStatusOffline
		_ = stores.Devices.Upsert(ctx, d)
		runtime.Default().Heartbeat.Remove(ctx, devicePhone)
		log.Debug().Str("device", devicePhone).Msg("Turn offline for device keepalive expired")
	} else if d.ShouldClear() {
		_ = d.Conn.Close()
		_ = stores.Devices.DeleteByPhone(ctx, devicePhone)
		runtime.Default().Heartbeat.Remove(ctx, devicePhone)
		_ = stores.Geo.Delete(ctx, devicePhone)
		log.Debug().Str("device", d.Phone).Msg("Clear cache and close connection after device being offline for a long time")
		t.Cancel(devicePhone)
	}
}

// checkSQLBackedKeepalive 处理运行时状态不落库的后端：
// 只关闭进程内在线会话，绝不删除 devices 静态档案（避免重启或误判丢注册信息）。
func checkSQLBackedKeepalive(ctx context.Context, phone string, keepalive time.Duration, lastCom time.Time) {
	if lastCom.IsZero() {
		// 进程内没有可用的最近通信时间（新进程且无心跳），无法判定，保留档案。
		return
	}
	if keepalive <= 0 {
		keepalive = time.Minute
	}
	if time.Now().Before(lastCom.Add(keepalive)) {
		return
	}
	// SQL 后端的连接信息不在库内：这里只收敛进程内最近通信时间，
	// 在线会话的关闭由连接注册表在后续按手机号查找后完成。
	runtime.Default().Heartbeat.Remove(ctx, phone)
	log.Debug().Str("device", phone).Msg("Turn offline for device keepalive expired")
}
