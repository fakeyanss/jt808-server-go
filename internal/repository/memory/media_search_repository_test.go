package memory

import (
	"context"
	"strings"
	"testing"
	"time"

	"github.com/fakeyanss/jt808-server-go/internal/repository"
	"github.com/stretchr/testify/require"
)

func TestMemoryMediaSearchRepository(t *testing.T) {
	ctx := context.Background()
	repo := NewMediaSearchRepository()
	base := time.Date(2026, 11, 14, 10, 0, 0, 0, time.Local)

	records := []repository.MediaSearchRecord{
		{MediaID: 1001, MediaType: 0, ChannelID: 1, EventID: 1, ReceivedAt: base},
		{MediaID: 1002, MediaType: 2, ChannelID: 2, EventID: 3, ReceivedAt: base.Add(time.Second)},
	}
	require.NoError(t, repo.Replace(ctx, "188765400001", 7, records))

	got, err := repo.List(ctx, "188765400001", 10)
	require.NoError(t, err)
	require.Len(t, got, 2)
	require.Equal(t, uint16(7), got[0].AnswerSerial)
	require.Equal(t, uint32(1002), got[0].MediaID, "newer results are listed first")
	require.Equal(t, 1, got[0].Index)

	// 同一应答流水号重传时整体覆盖，旧条目不能残留。
	require.NoError(t, repo.Replace(ctx, "188765400001", 7, records[:1]))
	got, err = repo.List(ctx, "188765400001", 10)
	require.NoError(t, err)
	require.Len(t, got, 1)
	require.Equal(t, uint32(1001), got[0].MediaID)

	// 新的应答流水号按接收时间排在前面，并可被 limit 截断。
	require.NoError(t, repo.Replace(ctx, "188765400001", 8, []repository.MediaSearchRecord{
		{MediaID: 1003, ReceivedAt: base.Add(2 * time.Second)},
	}))
	got, err = repo.List(ctx, "188765400001", 1)
	require.NoError(t, err)
	require.Len(t, got, 1)
	require.Equal(t, uint32(1003), got[0].MediaID)

	// 不同设备互不影响。
	got, err = repo.List(ctx, "188765400002", 10)
	require.NoError(t, err)
	require.Empty(t, got)
}

func TestMediaSearchRepositoryRejectsTruncatedFields(t *testing.T) {
	ctx := context.Background()
	repo := NewMediaSearchRepository()

	require.Error(t, repo.Replace(ctx, "", 1, nil))
	require.Error(t, repo.Replace(ctx, strings.Repeat("9", 33), 1, nil))
	require.Error(t, repo.Replace(ctx, "188765400001", 1, []repository.MediaSearchRecord{{MediaID: 0}}))
}

func TestParseMediaSearchRecordDefaults(t *testing.T) {
	// 空切片必须返回非 nil 结果，便于 API 直接序列化为 []。
	repo := NewMediaSearchRepository()
	got, err := repo.List(context.Background(), "188765400009", 10)
	require.NoError(t, err)
	require.NotNil(t, got)
}
