package memory

import (
	"context"
	"testing"

	"github.com/fakeyanss/jt808-server-go/internal/domain"
	"github.com/stretchr/testify/require"
)

func TestMemoryDeviceRepositoryReturnsSnapshots(t *testing.T) {
	repo := newMemoryDeviceRepository()
	ctx := context.Background()
	device := &domain.Device{Phone: "188765439999", Plate: "CACHE-SNAPSHOT-TEST", Status: domain.DeviceStatusOffline}
	require.NoError(t, repo.Upsert(ctx, device))
	t.Cleanup(func() { _ = repo.DeleteByPhone(ctx, device.Phone) })

	device.Status = domain.DeviceStatusOnline
	got, err := repo.GetByPhone(ctx, device.Phone)
	require.NoError(t, err)
	require.Equal(t, domain.DeviceStatusOffline, got.Status)

	got.Status = domain.DeviceStatusSleeping
	got, err = repo.GetByPlate(ctx, device.Plate)
	require.NoError(t, err)
	require.Equal(t, domain.DeviceStatusOffline, got.Status)
}
