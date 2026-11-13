package sqlite

import (
	"context"
	"path/filepath"
	"testing"
	"time"

	"github.com/stretchr/testify/require"

	"github.com/fakeyanss/jt808-server-go/internal/codec/hex"
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
	first, err := Open(dbPath, 0)
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
	second, err := Open(dbPath, 0)
	require.NoError(t, err)
	t.Cleanup(func() { _ = second.Close() })

	records, err = second.LocationRepository(repository.LocationRetention{}).List(ctx, phone, 0)
	require.NoError(t, err)
	require.Len(t, records, 3, "location history must survive restart")
	require.Equal(t, uint32(1), records[2].AlarmSign)
	require.Equal(t, base.UnixMilli(), records[2].ReceivedAt.UnixMilli())
}

// dedup_key 是 E2 的幂等键：同一手机同一 key 只落一条；空 key 视为老数据不去重。
func TestSQLiteLocationDedupKeyIsIdempotent(t *testing.T) {
	const phone = "188765700009"
	ctx := context.Background()
	store := openTestSQLite(t)
	repo := store.LocationRepository(repository.LocationRetention{})
	base := time.UnixMilli(time.Now().UnixMilli())

	duplicated := sampleLocation(phone, 1, base)
	duplicated.DedupKey = "hash-1"
	replay := sampleLocation(phone, 2, base.Add(time.Second))
	replay.DedupKey = "hash-1"
	legacyFirst := sampleLocation(phone, 3, base.Add(2*time.Second))
	legacySecond := sampleLocation(phone, 4, base.Add(3*time.Second))

	require.NoError(t, repo.Record(ctx, []repository.LocationRecord{duplicated, replay, legacyFirst, legacySecond}))

	records, err := repo.List(ctx, phone, 0)
	require.NoError(t, err)
	require.Len(t, records, 3, "dedup_key rejects the replay, NULL keys stay independent")
}

// SQLite 定位分页：按上报时间半开区间过滤，返回倒序页与总数。
func TestSQLiteLocationListPageFiltersByReportTime(t *testing.T) {
	const phone = "188765700003"
	ctx := context.Background()
	store := openTestSQLite(t)
	repo := store.LocationRepository(repository.LocationRetention{})

	base := time.Date(2026, 10, 28, 20, 45, 0, 0, time.UTC)
	occurred := make([]string, 5)
	records := make([]repository.LocationRecord, 5)
	for i := range records {
		occurred[i] = hex.FormatTime(base.Add(time.Duration(i) * time.Second))
		records[i] = sampleLocation(phone, int64(i+1), base.Add(time.Duration(i)*time.Second))
		records[i].OccurredAt = occurred[i]
	}
	require.NoError(t, repo.Record(ctx, records))

	items, total, err := repo.ListPage(ctx, repository.HistoryQuery{
		Phone:  phone,
		From:   hex.ParseTime(occurred[1]),
		To:     hex.ParseTime(occurred[4]),
		Offset: 1,
		Limit:  1,
	})
	require.NoError(t, err)
	require.Equal(t, 3, total, "half-open range [t1, t4) must contain t1, t2, t3")
	require.Len(t, items, 1)
	require.Equal(t, occurred[2], items[0].OccurredAt, "page must be ordered by report time desc")

	items, total, err = repo.ListPage(ctx, repository.HistoryQuery{
		Phone:  phone,
		From:   hex.ParseTime(occurred[1]),
		To:     hex.ParseTime(occurred[4]),
		Offset: 3,
		Limit:  10,
	})
	require.NoError(t, err)
	require.Equal(t, 3, total)
	require.Empty(t, items)
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
