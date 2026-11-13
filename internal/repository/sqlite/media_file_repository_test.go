package sqlite

import (
	"context"
	"path/filepath"
	"testing"
	"time"

	"github.com/stretchr/testify/require"

	"github.com/fakeyanss/jt808-server-go/internal/repository"
)

// TestSQLiteMediaFileRepository 校验媒体文件与拍摄应答在 SQLite 下的读写与关联。
func TestSQLiteMediaFileRepository(t *testing.T) {
	ctx := context.Background()
	dbPath := filepath.Join(t.TempDir(), "jt808.db")
	migrateSQLite(t, dbPath)

	store, err := Open(dbPath, 0)
	require.NoError(t, err)
	defer store.Close()
	files := store.MediaFileRepository()
	captures := store.MediaCaptureRepository()

	const phone = "188765477003"
	base := time.Date(2026, 11, 12, 9, 30, 0, 0, time.Local)
	first := &repository.MediaFile{
		Phone: phone, MediaID: 5001, MediaType: 0, MediaFormat: 0, EventID: 0,
		ChannelID: 1, AlarmSign: 0x00000004, StatusSign: 2,
		Latitude: 30000000, Longitude: 120000000, Altitude: 50, Speed: 600, Direction: 90,
		LocationTime: base, RelativePath: phone + "/20261112/5001.jpg",
		FileSize: 1024, MD5: "d41d8cd98f00b204e9800998ecf8427e", ReceivedAt: base,
	}
	require.NoError(t, files.Save(ctx, first))
	require.Error(t, files.Save(ctx, nil))

	got, err := files.Get(ctx, phone, 5001)
	require.NoError(t, err)
	require.Equal(t, uint8(1), got.ChannelID)
	require.Equal(t, uint32(0x00000004), got.AlarmSign)
	require.Equal(t, int64(1024), got.FileSize)
	require.Equal(t, base, got.LocationTime)

	// 相同 (phone, mediaId) 重复上报应覆盖。
	first.FileSize = 2048
	first.RelativePath = phone + "/20261112/5001.jpg"
	require.NoError(t, files.Save(ctx, first))
	got, err = files.Get(ctx, phone, 5001)
	require.NoError(t, err)
	require.Equal(t, int64(2048), got.FileSize)

	second := &repository.MediaFile{
		Phone: phone, MediaID: 5002, MediaType: 1, MediaFormat: 2, ChannelID: 2,
		LocationTime: base.Add(time.Hour), RelativePath: phone + "/20261112/5002.mp3",
		FileSize: 512, ReceivedAt: base.Add(time.Hour),
	}
	require.NoError(t, files.Save(ctx, second))

	_, err = files.Get(ctx, phone, 9999)
	require.ErrorIs(t, err, repository.ErrMediaFileNotFound)

	channel := uint8(2)
	list, err := files.List(ctx, phone, repository.MediaFileFilter{ChannelID: &channel})
	require.NoError(t, err)
	require.Len(t, list, 1)
	require.Equal(t, uint32(5002), list[0].MediaID)

	list, err = files.List(ctx, phone, repository.MediaFileFilter{AlarmOnly: true})
	require.NoError(t, err)
	require.Len(t, list, 1)
	require.Equal(t, uint32(5001), list[0].MediaID)

	start := base.Add(30 * time.Minute)
	list, err = files.List(ctx, phone, repository.MediaFileFilter{Start: &start})
	require.NoError(t, err)
	require.Len(t, list, 1)
	require.Equal(t, uint32(5002), list[0].MediaID)

	count, bytes, err := files.Stats(ctx)
	require.NoError(t, err)
	require.Equal(t, int64(2), count)
	require.Equal(t, int64(2560), bytes)

	// 0x0805 拍摄记录：先落记录，再通过 media_files 判断文件是否到达。
	require.NoError(t, captures.SaveCapture(ctx, &repository.MediaCapture{
		Phone: phone, RecordID: "88", AnswerSerial: 88, Result: 0, ReceivedAt: base,
		Items: []repository.MediaCaptureItem{{MediaID: 5001}, {MediaID: 5003}},
	}))
	listed, err := captures.ListCaptures(ctx, phone, 10)
	require.NoError(t, err)
	require.Len(t, listed, 1)
	require.Len(t, listed[0].Items, 2)
	require.Equal(t, uint32(5001), listed[0].Items[0].MediaID)
	require.True(t, listed[0].Items[0].Arrived)
	require.Equal(t, int64(2048), listed[0].Items[0].FileSize)
	require.False(t, listed[0].Items[1].Arrived)

	// 重放同一 recordId 覆盖条目，不产生重复。
	require.NoError(t, captures.SaveCapture(ctx, &repository.MediaCapture{
		Phone: phone, RecordID: "88", AnswerSerial: 88, Result: 0, ReceivedAt: base,
		Items: []repository.MediaCaptureItem{{MediaID: 5002}},
	}))
	listed, err = captures.ListCaptures(ctx, phone, 10)
	require.NoError(t, err)
	require.Len(t, listed, 1)
	require.Len(t, listed[0].Items, 1)
	require.Equal(t, uint32(5002), listed[0].Items[0].MediaID)
	require.True(t, listed[0].Items[0].Arrived)
}
