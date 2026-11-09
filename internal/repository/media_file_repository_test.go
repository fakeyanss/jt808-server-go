package repository

import (
	"context"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
)

// 内存仓储需要同时满足媒体文件与拍摄应答查询，便于测试中共享同一实例。
func TestMemoryMediaFileRepositorySaveGetListAndStats(t *testing.T) {
	ctx := context.Background()
	repo := NewMediaFileRepository()
	const phone = "188765432101"

	base := time.Date(2026, 11, 12, 10, 0, 0, 0, time.Local)
	first := &MediaFile{
		Phone: phone, MediaID: 1001, MediaType: 0, MediaFormat: 0, ChannelID: 1,
		AlarmSign: 0x00000002, LocationTime: base, RelativePath: "188765432101/20261112/1001.jpg",
		FileSize: 100, MD5: "a", ReceivedAt: base.Add(time.Second),
	}
	second := &MediaFile{
		Phone: phone, MediaID: 1002, MediaType: 1, MediaFormat: 2, ChannelID: 2,
		LocationTime: base.Add(time.Hour), RelativePath: "188765432101/20261112/1002.mp3",
		FileSize: 200, MD5: "b", ReceivedAt: base.Add(time.Hour),
	}
	other := &MediaFile{
		Phone: "188765432102", MediaID: 2001, MediaType: 0, ChannelID: 1,
		LocationTime: base, RelativePath: "188765432102/20261112/2001.jpg",
		FileSize: 300, MD5: "c", ReceivedAt: base,
	}
	for _, file := range []*MediaFile{first, second, other} {
		require.NoError(t, repo.Save(ctx, file))
	}

	got, err := repo.Get(ctx, phone, 1001)
	require.NoError(t, err)
	require.Equal(t, uint32(1001), got.MediaID)
	require.Equal(t, int64(100), got.FileSize)

	_, err = repo.Get(ctx, phone, 9999)
	require.ErrorIs(t, err, ErrMediaFileNotFound)

	channel := uint8(1)
	list, err := repo.List(ctx, phone, MediaFileFilter{ChannelID: &channel})
	require.NoError(t, err)
	require.Len(t, list, 1)
	require.Equal(t, uint32(1001), list[0].MediaID)

	mediaType := uint8(1)
	list, err = repo.List(ctx, phone, MediaFileFilter{MediaType: &mediaType})
	require.NoError(t, err)
	require.Len(t, list, 1)
	require.Equal(t, uint32(1002), list[0].MediaID)

	list, err = repo.List(ctx, phone, MediaFileFilter{AlarmOnly: true})
	require.NoError(t, err)
	require.Len(t, list, 1)
	require.Equal(t, uint32(1001), list[0].MediaID)

	start := base.Add(30 * time.Minute)
	list, err = repo.List(ctx, phone, MediaFileFilter{Start: &start})
	require.NoError(t, err)
	require.Len(t, list, 1)
	require.Equal(t, uint32(1002), list[0].MediaID)

	list, err = repo.List(ctx, phone, MediaFileFilter{Limit: 1})
	require.NoError(t, err)
	require.Len(t, list, 1)
	require.Equal(t, uint32(1002), list[0].MediaID, "按接收时间倒序返回")

	count, bytes, err := repo.Stats(ctx)
	require.NoError(t, err)
	require.Equal(t, int64(3), count)
	require.Equal(t, int64(600), bytes)

	// 同一 mediaId 重复上报按幂等覆盖处理。
	first.FileSize = 150
	require.NoError(t, repo.Save(ctx, first))
	count, bytes, err = repo.Stats(ctx)
	require.NoError(t, err)
	require.Equal(t, int64(3), count)
	require.Equal(t, int64(650), bytes)

	require.Error(t, repo.Save(ctx, nil))
	require.Error(t, repo.Save(ctx, &MediaFile{Phone: phone}))
}

// 0x0805 返回的 mediaId 在 0x0801 落盘后应被标记为已到达。
func TestMemoryMediaCaptureRepositoryTracksArrival(t *testing.T) {
	ctx := context.Background()
	repo := NewMediaFileRepository()
	const phone = "188765432103"
	now := time.Date(2026, 11, 12, 12, 0, 0, 0, time.Local)

	require.NoError(t, repo.SaveCapture(ctx, &MediaCapture{
		Phone: phone, RecordID: "77", AnswerSerial: 77, Result: 0, ReceivedAt: now,
		Items: []MediaCaptureItem{{MediaID: 3001}, {MediaID: 3002}},
	}))
	require.NoError(t, repo.Save(ctx, &MediaFile{
		Phone: phone, MediaID: 3002, RelativePath: "188765432103/20261112/3002.jpg",
		FileSize: 888, ReceivedAt: now,
	}))

	list, err := repo.ListCaptures(ctx, phone, 10)
	require.NoError(t, err)
	require.Len(t, list, 1)
	require.Equal(t, uint16(77), list[0].AnswerSerial)
	require.Len(t, list[0].Items, 2)
	require.False(t, list[0].Items[0].Arrived)
	require.True(t, list[0].Items[1].Arrived)
	require.Equal(t, int64(888), list[0].Items[1].FileSize)

	// 同一 recordId 重放应覆盖而不是追加。
	require.NoError(t, repo.SaveCapture(ctx, &MediaCapture{
		Phone: phone, RecordID: "77", AnswerSerial: 77, Result: 0, ReceivedAt: now,
		Items: []MediaCaptureItem{{MediaID: 3001}},
	}))
	list, err = repo.ListCaptures(ctx, phone, 10)
	require.NoError(t, err)
	require.Len(t, list, 1)
	require.Len(t, list[0].Items, 1)

	// 设备之间互不可见。
	list, err = repo.ListCaptures(ctx, "188765432104", 10)
	require.NoError(t, err)
	require.Empty(t, list)

	require.Error(t, repo.SaveCapture(ctx, nil))
	require.Error(t, repo.SaveCapture(ctx, &MediaCapture{Phone: phone}))
}
