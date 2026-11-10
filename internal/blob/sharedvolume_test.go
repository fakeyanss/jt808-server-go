package blob

import (
	"bytes"
	"context"
	"io"
	"os"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/fakeyanss/jt808-server-go/internal/contracts"
)

func newTestVolume(t *testing.T, withMarker bool) *SharedVolume {
	t.Helper()
	root := t.TempDir()
	if withMarker {
		require.NoError(t, os.WriteFile(filepath.Join(root, MarkerFile), nil, 0o640))
	}
	volume, err := NewSharedVolume(root, withMarker)
	require.NoError(t, err)
	return volume
}

func TestNewSharedVolumeRequiresMarker(t *testing.T) {
	root := t.TempDir()
	_, err := NewSharedVolume(root, true)
	require.Error(t, err)

	require.NoError(t, os.WriteFile(filepath.Join(root, MarkerFile), nil, 0o640))
	volume, err := NewSharedVolume(root, true)
	require.NoError(t, err)
	require.Equal(t, root, volume.Root())
}

// Put 不暴露最终 key；Promote 后可按 key 读取并留下 pending 标记。
func TestSharedVolumePutPromoteAndCommit(t *testing.T) {
	volume := newTestVolume(t, true)
	ctx := context.Background()

	info, err := volume.Put(ctx, "188765432105/20261112/1.jpg", bytes.NewReader([]byte("jpeg-bytes")), contracts.BlobOptions{})
	require.NoError(t, err)
	require.Equal(t, contracts.BlobStatePending, info.State)

	_, err = volume.Open(ctx, "188765432105/20261112/1.jpg")
	require.ErrorIs(t, err, os.ErrNotExist, "staging object must not be visible before promote")

	info, err = volume.Promote(ctx, info)
	require.NoError(t, err)
	require.Equal(t, "188765432105/20261112/1.jpg", info.Key)
	require.Equal(t, int64(len("jpeg-bytes")), info.Size)

	reader, err := volume.Open(ctx, info.Key)
	require.NoError(t, err)
	content, err := io.ReadAll(reader)
	require.NoError(t, err)
	require.NoError(t, reader.Close())
	require.Equal(t, []byte("jpeg-bytes"), content)

	stat, err := volume.Stat(ctx, info.Key)
	require.NoError(t, err)
	require.Equal(t, contracts.BlobStatePending, stat.State)

	require.NoError(t, volume.MarkCommitted(ctx, info.Key))
	stat, err = volume.Stat(ctx, info.Key)
	require.NoError(t, err)
	require.Equal(t, contracts.BlobStateCommitted, stat.State)
}

// 同一 key 重传时覆盖旧文件，不产生第二份对象。
func TestSharedVolumePutIsIdempotent(t *testing.T) {
	volume := newTestVolume(t, true)
	ctx := context.Background()

	first, err := volume.Put(ctx, "188765432105/20261112/1.jpg", bytes.NewReader([]byte("v1")), contracts.BlobOptions{})
	require.NoError(t, err)
	_, err = volume.Promote(ctx, first)
	require.NoError(t, err)

	second, err := volume.Put(ctx, "188765432105/20261112/1.jpg", bytes.NewReader([]byte("v2")), contracts.BlobOptions{})
	require.NoError(t, err)
	_, err = volume.Promote(ctx, second)
	require.NoError(t, err)

	reader, err := volume.Open(ctx, second.Key)
	require.NoError(t, err)
	content, err := io.ReadAll(reader)
	require.NoError(t, err)
	require.NoError(t, reader.Close())
	require.Equal(t, []byte("v2"), content)
}

// 巡检删除无引用的 pending 对象，保留仍被业务表引用的对象。
func TestSharedVolumeSweepPending(t *testing.T) {
	volume := newTestVolume(t, true)
	ctx := context.Background()

	referenced := putAndPromote(t, volume, "188765432105/20261112/1.jpg", "referenced")
	orphan := putAndPromote(t, volume, "188765432105/20261112/2.jpg", "orphan")

	pending, err := volume.PendingBefore(ctx, 0)
	require.NoError(t, err)
	require.ElementsMatch(t, []string{referenced, orphan}, pending)

	volume.SweepPending(ctx, 0, func(key string) bool { return key == referenced })

	_, err = volume.Open(ctx, referenced)
	require.NoError(t, err)
	_, err = volume.Open(ctx, orphan)
	require.ErrorIs(t, err, os.ErrNotExist)
}

// 已提交对象不会被巡检列入待清理集合。
func TestSharedVolumeSweepSkipsCommitted(t *testing.T) {
	volume := newTestVolume(t, true)
	ctx := context.Background()

	key := putAndPromote(t, volume, "188765432105/20261112/3.jpg", "committed")
	require.NoError(t, volume.MarkCommitted(ctx, key))

	pending, err := volume.PendingBefore(ctx, 0)
	require.NoError(t, err)
	require.Empty(t, pending)
}

func putAndPromote(t *testing.T, volume *SharedVolume, key, content string) string {
	t.Helper()
	info, err := volume.Put(context.Background(), key, bytes.NewReader([]byte(content)), contracts.BlobOptions{})
	require.NoError(t, err)
	info, err = volume.Promote(context.Background(), info)
	require.NoError(t, err)
	return info.Key
}

// 目录穿越与绝对路径必须拒绝。
func TestSharedVolumeRejectsUnsafeKeys(t *testing.T) {
	volume := newTestVolume(t, true)
	for _, key := range []string{"", "../escape", "/abs/path", "a/../../escape"} {
		_, err := volume.Put(context.Background(), key, bytes.NewReader(nil), contracts.BlobOptions{})
		require.Error(t, err, "key %q must be rejected", key)
	}
}
