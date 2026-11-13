package alarmserver

import (
	"bytes"
	"context"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/stretchr/testify/require"

	"github.com/fakeyanss/jt808-server-go/internal/contracts"
	"github.com/fakeyanss/jt808-server-go/internal/storage"
)

func newBlobTestAssembler(t *testing.T, root string, volume contracts.BlobStore,
	sink contracts.AttachmentProgressSink, segmentSize, stagingMax int64) *Assembler {
	t.Helper()
	assembler := NewAssembler(Config{
		Dir:            filepath.Join(root, "alarms"),
		MaxFileSize:    1 << 20,
		MaxConcurrent:  4,
		SessionTimeout: time.Hour,
		Storage: &StorageConfig{
			Blob:            volume,
			Progress:        sink,
			Source:          storage.SourceSubia,
			SegmentSize:     segmentSize,
			StagingDir:      filepath.Join(root, "staging"),
			MaxStagingBytes: stagingMax,
		},
	})
	t.Cleanup(assembler.Close)
	return assembler
}

// 对象存储模式：切片按顺序提交，进度异步上报 control，文件收完才 marked committed。
func TestBlobModeStagesSegmentsAndReportsProgress(t *testing.T) {
	root := t.TempDir()
	volume, err := storage.NewSharedVolume(filepath.Join(root, "blob"), false)
	require.NoError(t, err)
	sink := newFakeProgressSink()
	assembler := newBlobTestAssembler(t, root, volume, sink, 4, 1<<20)

	info := AttachmentInfo{
		Phone:      "13800138000",
		TerminalID: "T1",
		AlarmNo:    "A1",
		AlarmSign:  bytes.Repeat([]byte{0x11}, 16),
	}
	require.NoError(t, assembler.RegisterAlarm(info))
	state, err := assembler.RegisterFile(info, "clip.bin", 1, 8)
	require.NoError(t, err)

	key := contracts.AttachmentProgressKey{Source: storage.SourceSubia, Phone: info.Phone, AlarmNo: info.AlarmNo, FileName: "clip.bin"}
	require.NoError(t, state.writeAt(0, []byte("abcd")))
	progress := sink.waitStatus(t, key, contracts.AttachmentStatusReceiving)
	require.EqualValues(t, 4, progress.Watermark)
	require.Len(t, progress.Segments, 1)
	require.Equal(t,
		storage.AttachmentSegmentObjectKey(storage.SourceSubia, info.Phone, info.AlarmNo, "clip.bin", 0, 4, 8, 0),
		progress.Segments[0].Key)
	info0, err := volume.Stat(context.Background(), progress.Segments[0].Key)
	require.NoError(t, err)
	require.Equal(t, contracts.BlobStatePending, info0.State)
	require.Equal(t, 1, assembler.ActiveReceiving())

	// 第二个切片补齐后文件完成：所有对象转 committed，暂存文件删除。
	require.NoError(t, state.writeAt(4, []byte("efgh")))
	progress = sink.waitStatus(t, key, contracts.AttachmentStatusCompleted)
	require.Len(t, progress.Segments, 2)
	require.EqualValues(t, 8, progress.Watermark)
	for _, segment := range progress.Segments {
		stored, statErr := volume.Stat(context.Background(), segment.Key)
		require.NoError(t, statErr)
		require.Equal(t, contracts.BlobStateCommitted, stored.State)
	}
	_, statErr := os.Stat(state.partPath)
	require.True(t, os.IsNotExist(statErr))
	require.Equal(t, 0, assembler.ActiveReceiving())
}

// control 不可达时用对象存储 key 恢复水位与完成状态，不阻断接收。
func TestBlobModeRestoresFromObjectKeysWhenControlIsDown(t *testing.T) {
	root := t.TempDir()
	volume, err := storage.NewSharedVolume(filepath.Join(root, "blob"), false)
	require.NoError(t, err)
	sink := newFakeProgressSink()
	sink.disabled = true

	// 预先在对象存储里放入第一片，模拟上一进程已提交的切片。
	key0 := storage.AttachmentSegmentObjectKey(storage.SourceSubia, "13800138000", "A1", "clip.bin", 0, 4, 8, 0)
	info0, err := volume.Put(context.Background(), key0, bytes.NewReader([]byte("abcd")), contracts.BlobOptions{})
	require.NoError(t, err)
	_, err = volume.Promote(context.Background(), info0)
	require.NoError(t, err)

	assembler := newBlobTestAssembler(t, root, volume, sink, 4, 1<<20)
	info := AttachmentInfo{
		Phone:      "13800138000",
		TerminalID: "T1",
		AlarmNo:    "A1",
		AlarmSign:  bytes.Repeat([]byte{0x11}, 16),
	}
	state, err := assembler.RegisterFile(info, "clip.bin", 1, 8)
	require.NoError(t, err)
	require.EqualValues(t, 4, state.flushCursor)
	require.Len(t, state.segments, 1)

	// 已有切片视为已覆盖：0x1212 只需补第二片。
	result, err := assembler.CompleteCheck(info.Phone, "clip.bin")
	require.NoError(t, err)
	require.False(t, result.Complete)
	require.Len(t, result.Missing, 1)
	require.EqualValues(t, 4, result.Missing[0].Offset)

	require.NoError(t, state.writeAt(4, []byte("efgh")))
	require.True(t, state.completed)
	_, statErr := os.Stat(state.partPath)
	require.True(t, os.IsNotExist(statErr))
}

// 崩溃残留：没有活跃会话且超过宽限期的 .part 文件必须被清理。
func TestBlobModeReconcileRemovesOrphanStaging(t *testing.T) {
	root := t.TempDir()
	volume, err := storage.NewSharedVolume(filepath.Join(root, "blob"), false)
	require.NoError(t, err)
	assembler := newBlobTestAssembler(t, root, volume, nil, 4, 1<<20)

	orphan := filepath.Join(root, "staging", "phone", "date", "alarm", "file.part")
	require.NoError(t, os.MkdirAll(filepath.Dir(orphan), 0o750))
	require.NoError(t, os.WriteFile(orphan, []byte("half"), 0o644))
	old := time.Now().Add(-2 * time.Hour)
	require.NoError(t, os.Chtimes(orphan, old, old))

	require.NoError(t, assembler.Reconcile())
	_, statErr := os.Stat(orphan)
	require.True(t, os.IsNotExist(statErr))
}

// 暂存区总量到顶后拒绝新的文件登记，并返回可判定的错误。
func TestBlobModeRejectsWhenStagingFull(t *testing.T) {
	root := t.TempDir()
	volume, err := storage.NewSharedVolume(filepath.Join(root, "blob"), false)
	require.NoError(t, err)
	assembler := newBlobTestAssembler(t, root, volume, nil, 4, 4)

	info := AttachmentInfo{Phone: "13800138001", AlarmNo: "A2", AlarmSign: bytes.Repeat([]byte{0x22}, 16)}
	require.NoError(t, assembler.RegisterAlarm(info))
	_, err = assembler.RegisterFile(info, "big.bin", 1, 8)
	require.ErrorIs(t, err, ErrStagingCapacity)
}

// 无进展超时：清理暂存并上报 expired。
func TestBlobModeCleanupStaleReportsExpired(t *testing.T) {
	root := t.TempDir()
	volume, err := storage.NewSharedVolume(filepath.Join(root, "blob"), false)
	require.NoError(t, err)
	sink := newFakeProgressSink()
	assembler := newBlobTestAssembler(t, root, volume, sink, 4, 1<<20)

	info := AttachmentInfo{
		Phone:      "13800138000",
		TerminalID: "T1",
		AlarmNo:    "A1",
		AlarmSign:  bytes.Repeat([]byte{0x11}, 16),
	}
	require.NoError(t, assembler.RegisterAlarm(info))
	state, err := assembler.RegisterFile(info, "clip.bin", 1, 8)
	require.NoError(t, err)
	require.NoError(t, state.writeAt(0, []byte("abc")))
	require.FileExists(t, state.partPath)

	require.Equal(t, 1, assembler.CleanupStale(time.Now().Add(2*time.Hour), time.Hour))
	key := contracts.AttachmentProgressKey{Source: storage.SourceSubia, Phone: info.Phone, AlarmNo: info.AlarmNo, FileName: "clip.bin"}
	progress := sink.waitStatus(t, key, contracts.AttachmentStatusExpired)
	require.Empty(t, progress.StagingPath)
	_, statErr := os.Stat(state.partPath)
	require.True(t, os.IsNotExist(statErr))
}
