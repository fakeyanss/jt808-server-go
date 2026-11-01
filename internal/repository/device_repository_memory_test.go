package repository

import (
	"context"
	"testing"

	"github.com/fakeyanss/jt808-server-go/internal/protocol/model"
	"github.com/stretchr/testify/require"
)

func TestMemoryDeviceRepositoryReturnsSnapshots(t *testing.T) {
	repo := newMemoryDeviceRepository()
	ctx := context.Background()
	device := &model.Device{Phone: "188765439999", Plate: "CACHE-SNAPSHOT-TEST", Status: model.DeviceStatusOffline}
	require.NoError(t, repo.Upsert(ctx, device))
	t.Cleanup(func() { _ = repo.DeleteByPhone(ctx, device.Phone) })

	device.Status = model.DeviceStatusOnline
	got, err := repo.GetByPhone(ctx, device.Phone)
	require.NoError(t, err)
	require.Equal(t, model.DeviceStatusOffline, got.Status)

	got.Status = model.DeviceStatusSleeping
	got, err = repo.GetByPlate(ctx, device.Plate)
	require.NoError(t, err)
	require.Equal(t, model.DeviceStatusOffline, got.Status)
}
