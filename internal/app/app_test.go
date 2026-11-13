package app

import (
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/fakeyanss/jt808-server-go/internal/storage"
)

// 附件角色缺少共享对象存储时必须返回稳定错误，而不是继续走到 attachmentserver.New 触发空指针。
// 这里绕过 config.ValidateServerMode（它会把该场景提前拦下），直接向守卫传 nil 依赖。
func TestCheckAttachmentBlobRejectsNil(t *testing.T) {
	err := checkAttachmentBlob(nil)
	require.ErrorIs(t, err, errAttachmentBlobRequired)
	require.Contains(t, err.Error(), "cluster.storage.backend=objectStore")
	require.Contains(t, err.Error(), "local|sharedVolume|objectStore")
}

// 有共享对象存储时守卫必须放行。
func TestCheckAttachmentBlobAcceptsBlobStore(t *testing.T) {
	blob, err := storage.NewSharedVolume(t.TempDir(), false)
	require.NoError(t, err)
	require.NoError(t, checkAttachmentBlob(blob))
}
