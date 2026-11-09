package repository

import (
	"context"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
)

// TestMemoryUpgradeResultRepository 校验内存实现按设备保存最近一次升级结果。
func TestMemoryUpgradeResultRepository(t *testing.T) {
	ctx := context.Background()
	repo := NewUpgradeResultRepository()

	_, err := repo.Latest(ctx, "18800000001")
	require.ErrorIs(t, err, ErrUpgradeResultNotFound)

	first := &UpgradeResult{
		Phone:       "18800000001",
		UpgradeType: 0,
		Result:      2,
		Serial:      7,
		ReceivedAt:  time.UnixMilli(1000),
	}
	require.NoError(t, repo.Save(ctx, first))
	require.Error(t, repo.Save(ctx, nil))
	require.Error(t, repo.Save(ctx, &UpgradeResult{}))

	second := &UpgradeResult{
		Phone:       first.Phone,
		UpgradeType: 1,
		Result:      0,
		Serial:      9,
		ReceivedAt:  time.UnixMilli(2000),
	}
	require.NoError(t, repo.Save(ctx, second))

	latest, err := repo.Latest(ctx, first.Phone)
	require.NoError(t, err)
	require.Equal(t, second, latest)

	// 返回快照：修改读取结果不影响仓储内部状态。
	latest.Result = 1
	again, err := repo.Latest(ctx, first.Phone)
	require.NoError(t, err)
	require.Equal(t, uint8(0), again.Result)
}
