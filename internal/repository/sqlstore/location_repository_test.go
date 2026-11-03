package sqlstore

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/stretchr/testify/require"

	"github.com/fakeyanss/jt808-server-go/internal/protocol/model"
	"github.com/fakeyanss/jt808-server-go/internal/repository"
)

func sampleLocation(phone string, serial int64, receivedAt time.Time) repository.LocationRecord {
	return repository.LocationRecord{
		Phone:      phone,
		MsgID:      0x0200,
		AlarmSign:  uint32(serial),
		StatusSign: 0x03,
		Latitude:   31000000 + uint32(serial),
		Longitude:  121000000 + uint32(serial),
		Altitude:   12,
		Speed:      85,
		Direction:  90,
		ExtItems: []model.LocationExtItem{
			{ID: 0x01, Length: 4, Data: []byte{0x01, 0x02, 0x03, 0x04}},
		},
		OccurredAt: "261028204500",
		ReceivedAt: receivedAt,
	}
}

// SQLite 定位历史：批量写入、按接收时间倒序读取、关闭重开后仍在。
func TestSQLitePersistsLocationsAcrossReopen(t *testing.T) {
	const phone = "188765700001"
	ctx := context.Background()
	dbPath := filepath.Join(t.TempDir(), "jt808.db")
	migrateSQLite(t, dbPath)

	base := time.UnixMilli(time.Now().UnixMilli())
	first, err := Open(Config{Backend: BackendSQLite, Path: dbPath})
	require.NoError(t, err)
	repo := first.LocationRepository(repository.LocationRetention{})
	require.NoError(t, repo.Record(ctx, []repository.LocationRecord{
		sampleLocation(phone, 1, base),
		sampleLocation(phone, 2, base.Add(time.Second)),
		sampleLocation(phone, 3, base.Add(2*time.Second)),
	}))

	records, err := repo.List(ctx, phone, 2)
	require.NoError(t, err)
	require.Len(t, records, 2)
	require.Equal(t, uint32(3), records[0].AlarmSign)
	require.Equal(t, uint32(2), records[1].AlarmSign)
	require.Equal(t, "261028204500", records[0].OccurredAt)
	require.Len(t, records[0].ExtItems, 1)
	require.Equal(t, []byte{0x01, 0x02, 0x03, 0x04}, records[0].ExtItems[0].Data)

	require.NoError(t, first.Close())
	second, err := Open(Config{Backend: BackendSQLite, Path: dbPath})
	require.NoError(t, err)
	t.Cleanup(func() { _ = second.Close() })

	records, err = second.LocationRepository(repository.LocationRetention{}).List(ctx, phone, 0)
	require.NoError(t, err)
	require.Len(t, records, 3, "location history must survive restart")
	require.Equal(t, uint32(1), records[2].AlarmSign)
	require.Equal(t, base.UnixMilli(), records[2].ReceivedAt.UnixMilli())
}

// SQLite 定位保留策略：超过保留期的记录在写入后清理，超过每设备上限的旧记录被裁剪。
func TestSQLiteLocationRetentionCleanup(t *testing.T) {
	const phone = "188765700002"
	ctx := context.Background()
	store := openTestSQLite(t)

	repo := store.LocationRepository(repository.LocationRetention{
		MaxAge:       7 * 24 * time.Hour,
		MaxPerDevice: 2,
	})
	now := time.Now()
	require.NoError(t, repo.Record(ctx, []repository.LocationRecord{
		sampleLocation(phone, 1, now.Add(-10*24*time.Hour)), // 超出保留期
		sampleLocation(phone, 2, now.Add(-time.Minute)),
		sampleLocation(phone, 3, now.Add(-time.Second)),
		sampleLocation(phone, 4, now),
	}))

	records, err := repo.List(ctx, phone, 0)
	require.NoError(t, err)
	require.Len(t, records, 2)
	require.Equal(t, uint32(4), records[0].AlarmSign)
	require.Equal(t, uint32(3), records[1].AlarmSign)
}

// MySQL 需要外部实例；未设置 JT808_TEST_MYSQL_DSN 时跳过。
func TestMySQLLocationPersistence(t *testing.T) {
	dsn := os.Getenv("JT808_TEST_MYSQL_DSN")
	if dsn == "" {
		t.Skip("JT808_TEST_MYSQL_DSN is not set; skipping MySQL integration test")
	}
	ctx := context.Background()
	store, err := Open(Config{Backend: BackendMySQL, DSN: dsn})
	require.NoError(t, err)
	t.Cleanup(func() { _ = store.Close() })

	phone := fmt.Sprintf("188%08d", time.Now().UnixNano()%100000000)
	_, err = store.db.ExecContext(ctx, `DELETE FROM locations WHERE phone = ?`, phone)
	require.NoError(t, err)

	repo := store.LocationRepository(repository.LocationRetention{MaxPerDevice: 2})
	now := time.Now()
	require.NoError(t, repo.Record(ctx, []repository.LocationRecord{
		sampleLocation(phone, 1, now.Add(-2*time.Second)),
		sampleLocation(phone, 2, now.Add(-time.Second)),
		sampleLocation(phone, 3, now),
	}))

	records, err := repo.List(ctx, phone, 0)
	require.NoError(t, err)
	require.Len(t, records, 2)
	require.Equal(t, uint32(3), records[0].AlarmSign)

	_, err = store.db.ExecContext(ctx, `DELETE FROM locations WHERE phone = ?`, phone)
	require.NoError(t, err)
}
