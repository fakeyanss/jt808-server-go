package storage

import (
	"context"
	"fmt"
	"sync"
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/fakeyanss/jt808-server-go/internal/protocol/model"
)

func TestDeviceRepositoryNotFoundAndDeleteMissing(t *testing.T) {
	repo := NewDeviceRepository()
	ctx := context.Background()

	_, err := repo.GetByPhone(ctx, "18800000000")
	require.ErrorIs(t, err, ErrDeviceNotFound)
	_, err = repo.GetByPlate(ctx, "MISSING")
	require.ErrorIs(t, err, ErrDeviceNotFound)

	require.NoError(t, repo.DeleteByPhone(ctx, "18800000000"))
	require.NoError(t, repo.DeleteByPlate(ctx, "MISSING"))

	list, err := repo.List(ctx)
	require.NoError(t, err)
	require.Empty(t, list)
}

func TestDeviceRepositoryConcurrentUpsertAndRead(t *testing.T) {
	repo := NewDeviceRepository()
	ctx := context.Background()
	const count = 64

	var wg sync.WaitGroup
	for i := 0; i < count; i++ {
		wg.Add(1)
		go func(i int) {
			defer wg.Done()
			_ = repo.Upsert(ctx, &model.Device{
				Phone:  fmt.Sprintf("188%08d", i),
				Plate:  fmt.Sprintf("P%08d", i),
				Status: model.DeviceStatusOffline,
			})
		}(i)
	}
	wg.Wait()

	errs := make([]error, count)
	for i := 0; i < count; i++ {
		wg.Add(1)
		go func(i int) {
			defer wg.Done()
			_, errs[i] = repo.GetByPhone(ctx, fmt.Sprintf("188%08d", i))
		}(i)
	}
	wg.Wait()

	for i, err := range errs {
		require.NoErrorf(t, err, "device %d should be readable after concurrent upsert", i)
	}
	list, err := repo.List(ctx)
	require.NoError(t, err)
	require.Len(t, list, count)
}

func TestDeviceRepositoryReturnsSnapshots(t *testing.T) {
	repo := NewDeviceRepository()
	ctx := context.Background()
	device := &model.Device{Phone: "188765400001", Plate: "REPO-SNAPSHOT", Status: model.DeviceStatusOffline}
	require.NoError(t, repo.Upsert(ctx, device))

	device.Status = model.DeviceStatusOnline
	got, err := repo.GetByPhone(ctx, device.Phone)
	require.NoError(t, err)
	require.Equal(t, model.DeviceStatusOffline, got.Status)

	got.Status = model.DeviceStatusSleeping
	byPlate, err := repo.GetByPlate(ctx, device.Plate)
	require.NoError(t, err)
	require.Equal(t, model.DeviceStatusOffline, byPlate.Status)
}
