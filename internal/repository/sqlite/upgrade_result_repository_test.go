package sqlite

import (
	"context"
	"path/filepath"
	"testing"
	"time"

	"github.com/stretchr/testify/require"

	"github.com/fakeyanss/jt808-server-go/internal/repository"
)

// TestSQLiteUpgradeResultRepository 校验升级结果在 SQLite 下可跨重启查询。
func TestSQLiteUpgradeResultRepository(t *testing.T) {
	ctx := context.Background()
	dbPath := filepath.Join(t.TempDir(), "jt808.db")
	migrateSQLite(t, dbPath)

	store, err := Open(dbPath, 0)
	require.NoError(t, err)
	repo := store.UpgradeResultRepository()

	const phone = "188765477002"
	_, err = repo.Latest(ctx, phone)
	require.ErrorIs(t, err, repository.ErrUpgradeResultNotFound)

	first := &repository.UpgradeResult{
		Phone:       phone,
		UpgradeType: 0,
		Result:      2,
		Serial:      7,
		ReceivedAt:  time.UnixMilli(1000),
	}
	require.NoError(t, repo.Save(ctx, first))
	require.Error(t, repo.Save(ctx, nil))

	second := &repository.UpgradeResult{
		Phone:       phone,
		UpgradeType: 1,
		Result:      0,
		Serial:      9,
		ReceivedAt:  time.UnixMilli(2000),
	}
	require.NoError(t, repo.Save(ctx, second))

	latest, err := repo.Latest(ctx, phone)
	require.NoError(t, err)
	require.Equal(t, second, latest)
	require.NoError(t, store.Close())

	// 重新打开数据库，确认结果已落盘而不是只存在进程内。
	reopened, err := Open(dbPath, 0)
	require.NoError(t, err)
	t.Cleanup(func() { _ = reopened.Close() })

	persisted, err := reopened.UpgradeResultRepository().Latest(ctx, phone)
	require.NoError(t, err)
	require.Equal(t, second, persisted)
}
