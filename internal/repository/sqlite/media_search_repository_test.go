package sqlite

import (
	"context"
	"path/filepath"
	"testing"
	"time"

	"github.com/stretchr/testify/require"

	"github.com/fakeyanss/jt808-server-go/internal/repository"
)

// TestSQLiteMediaSearchRepository 校验 0x0802 检索结果在 SQLite 下的覆盖写入、
// 查询排序以及进程重启后的持久性。
func TestSQLiteMediaSearchRepository(t *testing.T) {
	ctx := context.Background()
	dbPath := filepath.Join(t.TempDir(), "jt808.db")
	migrateSQLite(t, dbPath)

	store, err := Open(dbPath, 0)
	require.NoError(t, err)
	searches := store.MediaSearchRepository()

	const phone = "188765477004"
	base := time.Date(2026, 11, 14, 9, 30, 0, 0, time.Local)
	records := []repository.MediaSearchRecord{
		{
			MediaID: 6001, MediaType: 0, ChannelID: 1, EventID: 0,
			AlarmSign: 0x4, StatusSign: 2, Latitude: 30000000, Longitude: 120000000,
			Altitude: 50, Speed: 600, Direction: 90, LocationTime: base, ReceivedAt: base,
		},
		{
			MediaID: 6002, MediaType: 2, ChannelID: 2, EventID: 3,
			LocationTime: base.Add(time.Minute), ReceivedAt: base.Add(time.Second),
		},
	}
	require.NoError(t, searches.Replace(ctx, phone, 11, records))

	got, err := searches.List(ctx, phone, 10)
	require.NoError(t, err)
	require.Len(t, got, 2)
	require.Equal(t, uint16(11), got[0].AnswerSerial)
	require.Equal(t, uint32(6002), got[0].MediaID)
	require.Equal(t, uint8(2), got[0].ChannelID)
	require.Equal(t, base.Add(time.Minute), got[0].LocationTime)
	require.Equal(t, uint32(0x4), got[1].AlarmSign)
	require.Equal(t, uint32(120000000), got[1].Longitude)

	// 同一应答流水号重传且条目变少时，旧条目必须被清理。
	require.NoError(t, searches.Replace(ctx, phone, 11, records[:1]))
	got, err = searches.List(ctx, phone, 10)
	require.NoError(t, err)
	require.Len(t, got, 1)
	require.Equal(t, uint32(6001), got[0].MediaID)

	require.NoError(t, store.Close())

	// 关闭再打开同一文件，检索结果仍然存在。
	reopened, err := Open(dbPath, 0)
	require.NoError(t, err)
	defer reopened.Close()
	got, err = reopened.MediaSearchRepository().List(ctx, phone, 10)
	require.NoError(t, err)
	require.Len(t, got, 1)
	require.Equal(t, uint32(6001), got[0].MediaID)
	require.Equal(t, 0, got[0].Index)
}
