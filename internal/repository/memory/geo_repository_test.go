package memory

import (
	"context"
	"testing"
	"time"

	"github.com/fakeyanss/jt808-server-go/internal/protocol/model"
	"github.com/fakeyanss/jt808-server-go/internal/repository"
	"github.com/stretchr/testify/require"
)

func TestGeoRepositoryAppendLatestDelete(t *testing.T) {
	repo := NewGeoRepository()
	ctx := context.Background()

	_, err := repo.Latest(ctx, "18800000001")
	require.ErrorIs(t, err, repository.ErrGisNotFound)

	require.NoError(t, repo.Append(ctx, &model.DeviceGeo{Phone: "18800000001"}))
	latest, err := repo.Latest(ctx, "18800000001")
	require.NoError(t, err)
	require.Equal(t, "18800000001", latest.Phone)

	require.NoError(t, repo.Delete(ctx, "18800000001"))
	_, err = repo.Latest(ctx, "18800000001")
	require.ErrorIs(t, err, repository.ErrGisNotFound)
}

func TestGeoRepositoryLatestIsMonotonicByTerminalTime(t *testing.T) {
	repo := NewGeoRepository()
	ctx := context.Background()
	const phone = "18800000002"
	base := time.Date(2026, 11, 1, 12, 0, 0, 0, time.UTC)

	newer := &model.DeviceGeo{Phone: phone, Time: base, Report: &model.Msg0200{Time: "newer"}}
	older := &model.DeviceGeo{Phone: phone, Time: base.Add(-time.Minute), Report: &model.Msg0200{Time: "older"}}
	equal := &model.DeviceGeo{Phone: phone, Time: base, Report: &model.Msg0200{Time: "equal-later-arrival"}}

	require.NoError(t, repo.Append(ctx, newer))
	require.NoError(t, repo.Append(ctx, older))

	latest, err := repo.Latest(ctx, phone)
	require.NoError(t, err)
	require.Same(t, newer, latest, "晚到的旧定位不得覆盖最新值")

	// 时间相等视为同一时刻，保留最后到达者。
	require.NoError(t, repo.Append(ctx, equal))
	latest, err = repo.Latest(ctx, phone)
	require.NoError(t, err)
	require.Same(t, equal, latest)

	// 零/非法终端时间按旧值处理，不得回退已有有效时间。
	zero := &model.DeviceGeo{Phone: phone}
	require.NoError(t, repo.Append(ctx, zero))
	latest, err = repo.Latest(ctx, phone)
	require.NoError(t, err)
	require.Same(t, equal, latest)
}
