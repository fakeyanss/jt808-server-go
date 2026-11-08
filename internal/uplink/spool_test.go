package uplink

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/fakeyanss/jt808-server-go/internal/repository"
)

func TestSpoolPutIsDurableAndAckRemoves(t *testing.T) {
	dir := filepath.Join(t.TempDir(), "spool")
	spool, err := NewSpool(dir)
	require.NoError(t, err)

	id, err := spool.Put(SpoolRecord{
		LocationRecord: repository.LocationRecord{
			Phone:      "18800001111",
			MsgID:      0x0704,
			Latitude:   100,
			Longitude:  200,
			OccurredAt: "241108120000",
		},
	})
	require.NoError(t, err)

	// Put 返回时文件必须已经在磁盘上，而不是只写进内存缓冲。
	path := filepath.Join(dir, id+".json")
	info, statErr := os.Stat(path)
	require.NoError(t, statErr)
	require.Positive(t, info.Size())

	pending, err := spool.Pending()
	require.NoError(t, err)
	require.Len(t, pending, 1)
	require.Equal(t, id, pending[0].ID)
	require.Equal(t, "18800001111", pending[0].Record.Phone)

	require.NoError(t, spool.Ack(id))
	pending, err = spool.Pending()
	require.NoError(t, err)
	require.Empty(t, pending)

	// 重复确认是幂等的。
	require.NoError(t, spool.Ack(id))
}

func TestSpoolSurvivesReopen(t *testing.T) {
	dir := filepath.Join(t.TempDir(), "spool")
	first, err := NewSpool(dir)
	require.NoError(t, err)
	id, err := first.Put(SpoolRecord{
		LocationRecord: repository.LocationRecord{Phone: "18800001111", MsgID: 0x0200},
		Serial:         7,
	})
	require.NoError(t, err)

	// 重新打开目录模拟进程重启，记录必须仍在。
	second, err := NewSpool(dir)
	require.NoError(t, err)
	pending, err := second.Pending()
	require.NoError(t, err)
	require.Len(t, pending, 1)
	require.Equal(t, id, pending[0].ID)
	require.Equal(t, uint16(7), pending[0].Record.Serial)
}

func TestSpoolPendingSkipsTempFiles(t *testing.T) {
	dir := filepath.Join(t.TempDir(), "spool")
	spool, err := NewSpool(dir)
	require.NoError(t, err)
	require.NoError(t, os.WriteFile(filepath.Join(dir, "unfinished.tmp"), []byte("{"), 0o644))

	pending, err := spool.Pending()
	require.NoError(t, err)
	require.Empty(t, pending)
}
