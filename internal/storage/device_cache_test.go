package storage

import (
	"testing"

	"github.com/fakeyanss/jt808-server-go/internal/protocol/model"
	"github.com/stretchr/testify/require"
)

func TestDeviceCacheReturnsSnapshots(t *testing.T) {
	cache := GetDeviceCache()
	device := &model.Device{Phone: "188765439999", Plate: "CACHE-SNAPSHOT-TEST", Status: model.DeviceStatusOffline}
	cache.CacheDevice(device)
	t.Cleanup(func() { cache.DelDeviceByPhone(device.Phone) })

	device.Status = model.DeviceStatusOnline
	got, err := cache.GetDeviceByPhone(device.Phone)
	require.NoError(t, err)
	require.Equal(t, model.DeviceStatusOffline, got.Status)

	got.Status = model.DeviceStatusSleeping
	got, err = cache.GetDeviceByPlate(device.Plate)
	require.NoError(t, err)
	require.Equal(t, model.DeviceStatusOffline, got.Status)
}
