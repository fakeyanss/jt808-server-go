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
	"github.com/fakeyanss/jt808-server-go/internal/repository"
	"github.com/fakeyanss/jt808-server-go/internal/repository/memory"
	"github.com/fakeyanss/jt808-server-go/internal/storage"
)

// 对象存储模式：切片按顺序提交，水位与段列表落共享库，文件收完才 marked committed。
func TestBlobModeStagesSegmentsAndPersistsProgress(t *testing.T) {
	root := t.TempDir()
	volume, err := storage.NewSharedVolume(filepath.Join(root, "blob"), false)
	require.NoError(t, err)
	files := memory.NewAttachmentFileRepository()
	staging := filepath.Join(root, "staging")
	assembler := NewAssembler(Config{
		Dir:            filepath.Join(root, "alarms"),
		MaxFileSize:    1 << 20,
		MaxConcurrent:  4,
		SessionTimeout: time.Hour,
		Storage: &StorageConfig{
			Blob:            volume,
			Files:           files,
			Source:          repository.AttachmentSourceSubia,
			SegmentSize:     4,
			StagingDir:      staging,
			MaxStagingBytes: 1 << 20,
		},
	})
	info := AttachmentInfo{
		Phone:      "13800138000",
		TerminalID: "T1",
		AlarmNo:    "A1",
		AlarmSign:  bytes.Repeat([]byte{0x11}, 16),
	}
	require.NoError(t, assembler.RegisterAlarm(info))
	state, err := assembler.RegisterFile(info, "clip.bin", 1, 8)
	require.NoError(t, err)

	require.NoError(t, state.writeAt(0, []byte("abcd")))
	record, err := files.Get(context.Background(), repository.AttachmentFileKey{
		Source: repository.AttachmentSourceSubia, Phone: info.Phone, AlarmNo: info.AlarmNo, FileName: "clip.bin",
	})
	require.NoError(t, err)
	require.Equal(t, repository.AttachmentStatusReceiving, record.Status)
	require.EqualValues(t, 4, record.Watermark)
	require.Len(t, record.Segments, 1)
	require.Equal(t, storage.AttachmentSegment(repository.AttachmentSourceSubia, info.Phone, info.AlarmNo, "clip.bin", 0), record.Segments[0].Key)
	info0, err := volume.Stat(context.Background(), record.Segments[0].Key)
	require.NoError(t, err)
	require.Equal(t, contracts.BlobStatePending, info0.State)
	require.Equal(t, 1, assembler.ActiveReceiving())

	// 第二个切片补齐后文件完成：所有对象转 committed，暂存文件删除。
	require.NoError(t, state.writeAt(4, []byte("efgh")))
	record, err = files.Get(context.Background(), repository.AttachmentFileKey{
		Source: repository.AttachmentSourceSubia, Phone: info.Phone, AlarmNo: info.AlarmNo, FileName: "clip.bin",
	})
	require.NoError(t, err)
	require.Equal(t, repository.AttachmentStatusCompleted, record.Status)
	require.Len(t, record.Segments, 2)
	require.EqualValues(t, 8, record.Watermark)
	for _, segment := range record.Segments {
		stored, statErr := volume.Stat(context.Background(), segment.Key)
		require.NoError(t, statErr)
		require.Equal(t, contracts.BlobStateCommitted, stored.State)
	}
	_, statErr := os.Stat(state.partPath)
	require.True(t, os.IsNotExist(statErr))
	require.Equal(t, 0, assembler.ActiveReceiving())
}

// 崩溃残留：没有元数据引用且超过宽限期的 .part 文件必须被清理。
func TestBlobModeReconcileRemovesOrphanStaging(t *testing.T) {
	root := t.TempDir()
	volume, err := storage.NewSharedVolume(filepath.Join(root, "blob"), false)
	require.NoError(t, err)
	staging := filepath.Join(root, "staging")
	assembler := NewAssembler(Config{
		Dir:            filepath.Join(root, "alarms"),
		MaxFileSize:    1 << 20,
		MaxConcurrent:  4,
		SessionTimeout: time.Minute,
		Storage: &StorageConfig{
			Blob:            volume,
			Files:           memory.NewAttachmentFileRepository(),
			SegmentSize:     4,
			StagingDir:      staging,
			MaxStagingBytes: 1 << 20,
		},
	})
	orphan := filepath.Join(staging, "phone", "date", "alarm", "file.part")
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
	assembler := NewAssembler(Config{
		Dir:           filepath.Join(root, "alarms"),
		MaxFileSize:   1 << 20,
		MaxConcurrent: 4,
		Storage: &StorageConfig{
			Blob:            volume,
			Files:           memory.NewAttachmentFileRepository(),
			SegmentSize:     4,
			StagingDir:      filepath.Join(root, "staging"),
			MaxStagingBytes: 4,
		},
	})
	info := AttachmentInfo{Phone: "13800138001", AlarmNo: "A2", AlarmSign: bytes.Repeat([]byte{0x22}, 16)}
	require.NoError(t, assembler.RegisterAlarm(info))
	_, err = assembler.RegisterFile(info, "big.bin", 1, 8)
	require.ErrorIs(t, err, ErrStagingCapacity)
}
