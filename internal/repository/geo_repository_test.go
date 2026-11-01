package repository

import (
	"context"
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/fakeyanss/jt808-server-go/internal/protocol/model"
)

func TestGeoRepositoryAppendLatestDelete(t *testing.T) {
	repo := NewGeoRepository()
	ctx := context.Background()

	_, err := repo.Latest(ctx, "18800000001")
	require.ErrorIs(t, err, ErrGisNotFound)

	require.NoError(t, repo.Append(ctx, &model.DeviceGeo{Phone: "18800000001"}))
	latest, err := repo.Latest(ctx, "18800000001")
	require.NoError(t, err)
	require.Equal(t, "18800000001", latest.Phone)

	require.NoError(t, repo.Delete(ctx, "18800000001"))
	_, err = repo.Latest(ctx, "18800000001")
	require.ErrorIs(t, err, ErrGisNotFound)
}
