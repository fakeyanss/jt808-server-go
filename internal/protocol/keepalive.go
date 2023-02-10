package protocol

import (
	"time"

	"github.com/roylee0704/gron"
	"github.com/rs/zerolog/log"

	"github.com/fakeYanss/jt808-server-go/internal/protocol/model"
	"github.com/fakeYanss/jt808-server-go/internal/storage"
)

type KeepaliveTimer struct {
	cron *gron.Cron
}

func NewKeepaliveTimer() *KeepaliveTimer {
	return &KeepaliveTimer{
		cron: gron.New(),
	}
}

// 检查设备保活
// 1. 当前在线，保活失效，改为离线，断开tcp连接
// 2. 当前离线，缓存保留3倍保活时间
func checkDeviceKeepalive() {
	cache := storage.GetDeviceCache()
	gisCache := storage.GetGisCache()
	for _, d := range cache.ListDevice() {
		if d.ShouleTurnOffline() {
			// 保活失效
			d.Status = model.DeviceStatusOffline
			cache.CacheDevice(d)
			log.Debug().
				Str("device", d.PhoneNumber).
				Msg("Turn offline for device keepalive expired")
		} else if d.ShouldClear() {
			d.Conn.Close()
			cache.DelDeviceByPhone(d.PhoneNumber)
			gisCache.DelGisByPhone(d.PhoneNumber)
			log.Debug().
				Str("device", d.PhoneNumber).
				Msg("Clear cache and close connection after device being offline for a long time")
		}
	}
}

func CheckDeviceKeepaliveTimer() {
	c := gron.New()
	c.AddFunc(gron.Every(1*time.Minute), func() { checkDeviceKeepalive() })
	c.Start()
}
