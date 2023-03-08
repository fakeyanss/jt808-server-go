package protocol

import (
	"sync"

	"github.com/fakeyanss/gron"
	"github.com/pkg/errors"
	"github.com/rs/zerolog/log"

	"github.com/fakeyanss/jt808-server-go/internal/protocol/model"
	"github.com/fakeyanss/jt808-server-go/internal/storage"
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
	cache := storage.GetDeviceCache()
	device, err := cache.GetDeviceByPhone(devicePhone)
	if errors.Is(err, storage.ErrDeviceNotFound) {
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
	cache := storage.GetDeviceCache()
	gisCache := storage.GetGeoCache()
	d, err := cache.GetDeviceByPhone(devicePhone)
	if errors.Is(err, storage.ErrDeviceNotFound) {
		log.Debug().Str("device", devicePhone).Msg("Fail to find device cache")
		t.Cancel(devicePhone)
	}
	if d.ShouleTurnOffline() {
		// 保活失效
		d.Status = model.DeviceStatusOffline
		cache.CacheDevice(d)
		log.Debug().Str("device", devicePhone).Msg("Turn offline for device keepalive expired")
	} else if d.ShouldClear() {
		d.Conn.Close()
		cache.DelDeviceByPhone(devicePhone)
		gisCache.DelGeoByPhone(devicePhone)
		log.Debug().Str("device", d.Phone).Msg("Clear cache and close connection after device being offline for a long time")
		t.Cancel(devicePhone)
	}
}
