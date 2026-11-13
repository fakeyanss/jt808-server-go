package sqlite

import (
	"context"
	"path/filepath"
	"testing"
	"time"

	"github.com/stretchr/testify/require"

	"github.com/fakeyanss/jt808-server-go/internal/repository"
)

// SQLite 报警：去重窗口内只更新报警标志，重启后历史与确认结果仍在。
func TestSQLitePersistsAlarmsAcrossReopen(t *testing.T) {
	const phone = "188765600001"
	ctx := context.Background()
	dbPath := filepath.Join(t.TempDir(), "jt808.db")
	migrateSQLite(t, dbPath)

	policy := repository.AlarmRetention{
		DedupWindow:   time.Minute,
		ConfirmExpire: time.Minute,
		MaxPerDevice:  100,
	}
	first, err := Open(dbPath, 0)
	require.NoError(t, err)
	repo := first.AlarmRepository(policy)

	require.NoError(t, repo.Record(ctx, phone, 10, 0x01, 100, 200, "261028204500"))
	// 同一流水号在去重窗口内重复上报：只更新报警标志，不新增行。
	require.NoError(t, repo.Record(ctx, phone, 10, 0x03, 100, 200, "261028204500"))
	require.NoError(t, repo.Record(ctx, phone, 11, 0x02, 110, 210, "261028204501"))
	require.NoError(t, repo.Record(ctx, phone, 12, 0, 120, 220, "261028204502")) // 无报警标志不入库

	events, err := repo.List(ctx, phone)
	require.NoError(t, err)
	require.Len(t, events, 2)
	require.Equal(t, uint16(10), events[0].MessageSerial)
	require.Equal(t, uint32(0x03), events[0].AlarmSign)
	require.False(t, events[0].Confirmed)
	require.Equal(t, uint16(11), events[1].MessageSerial)

	require.NoError(t, first.Close())
	second, err := Open(dbPath, 0)
	require.NoError(t, err)
	repo = second.AlarmRepository(policy)

	events, err = repo.List(ctx, phone)
	require.NoError(t, err)
	require.Len(t, events, 2, "alarm history must survive restart")
	require.Equal(t, uint32(0x03), events[0].AlarmSign)
	require.Equal(t, "261028204500", events[0].OccurredAt)

	// 重启后未确认的命令关联应失效。
	confirmed, err := repo.ConfirmByCommand(ctx, phone, 80)
	require.NoError(t, err)
	require.False(t, confirmed)

	require.NoError(t, repo.RegisterConfirmation(ctx, phone, 81, 11, 0x02))
	confirmed, err = repo.ConfirmByCommand(ctx, phone, 81)
	require.NoError(t, err)
	require.True(t, confirmed)
	events, err = repo.List(ctx, phone)
	require.NoError(t, err)
	require.True(t, events[1].Confirmed)
	require.False(t, events[0].Confirmed)
	require.NoError(t, second.Close())

	third, err := Open(dbPath, 0)
	require.NoError(t, err)
	t.Cleanup(func() { _ = third.Close() })
	events, err = third.AlarmRepository(policy).List(ctx, phone)
	require.NoError(t, err)
	require.True(t, events[1].Confirmed, "confirmed flag must survive restart")
}

// SQLite 报警每设备上限：超过上限时按接收时间保留最新记录。
func TestSQLiteAlarmMaxPerDevice(t *testing.T) {
	const phone = "188765600002"
	ctx := context.Background()
	store := openTestSQLite(t)
	repo := store.AlarmRepository(repository.AlarmRetention{DedupWindow: time.Minute, MaxPerDevice: 2})

	require.NoError(t, repo.Record(ctx, phone, 1, 0x01, 0, 0, "261028204500"))
	require.NoError(t, repo.Record(ctx, phone, 2, 0x01, 0, 0, "261028204500"))
	require.NoError(t, repo.Record(ctx, phone, 3, 0x01, 0, 0, "261028204500"))

	events, err := repo.List(ctx, phone)
	require.NoError(t, err)
	require.Len(t, events, 2)
	require.Equal(t, uint16(2), events[0].MessageSerial)
	require.Equal(t, uint16(3), events[1].MessageSerial)
}

// SQLite 报警分页：按接收时间半开区间过滤，返回倒序页与总数。
func TestSQLiteAlarmListPageFiltersAndPaginates(t *testing.T) {
	const phone = "188765600003"
	ctx := context.Background()
	store := openTestSQLite(t)
	repo := store.AlarmRepository(repository.AlarmRetention{})

	from := time.Now().Add(-time.Second)
	for serial := uint16(1); serial <= 3; serial++ {
		require.NoError(t, repo.Record(ctx, phone, serial, 1, 0, 0, "261028204500"))
	}
	to := time.Now().Add(time.Second)

	items, total, err := repo.ListPage(ctx, repository.HistoryQuery{
		Phone: phone, From: from, To: to, Offset: 1, Limit: 1,
	})
	require.NoError(t, err)
	require.Equal(t, 3, total)
	require.Len(t, items, 1)
	require.Equal(t, uint16(2), items[0].MessageSerial, "list must be ordered by received time desc")

	items, total, err = repo.ListPage(ctx, repository.HistoryQuery{
		Phone: phone, From: time.Now().Add(time.Hour), To: time.Now().Add(2 * time.Hour), Limit: 10,
	})
	require.NoError(t, err)
	require.Equal(t, 0, total)
	require.Empty(t, items)
}
