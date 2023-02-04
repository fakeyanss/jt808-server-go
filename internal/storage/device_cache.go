package storage

import (
	"errors"
	"sync"
	"time"

	"github.com/fakeYanss/jt808-server-go/internal/protocol/model"
)

var ErrDeviceNotFound = errors.New("device not found")

var deviceCache sync.Map

func GetDevice(id string) (*model.Device, error) {
	if d, ok := deviceCache.Load(id); ok {
		return d.(*model.Device), nil
	}
	return nil, ErrDeviceNotFound
}

func CacheDevice(d *model.Device) {
	d.LastComTime = time.Now().UnixMilli()
	deviceCache.Store(d.ID, d)
}

func DelDevice(id string) {
	deviceCache.Delete(id)
}
