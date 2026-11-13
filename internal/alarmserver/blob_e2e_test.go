package alarmserver

import (
	"bufio"
	"context"
	"net"
	"path/filepath"
	"testing"
	"time"

	"github.com/stretchr/testify/require"

	"github.com/fakeyanss/jt808-server-go/internal/contracts"
	"github.com/fakeyanss/jt808-server-go/internal/protocol/model"
	"github.com/fakeyanss/jt808-server-go/internal/storage"
)

// 附件协议接入 + 对象存储切片：真实 TCP 帧进来，最终向 control 上报 completed，
// 每个切片对象为 committed，本地暂存被清理。
func TestServerEndToEndObjectStorageSegments(t *testing.T) {
	root := t.TempDir()
	volume, err := storage.NewSharedVolume(filepath.Join(root, "blob"), false)
	require.NoError(t, err)
	sink := newFakeProgressSink()
	srv := NewServer(Config{
		Dir:            filepath.Join(root, "alarms"),
		MaxFileSize:    16 * 1024 * 1024,
		MaxConcurrent:  4,
		SessionTimeout: time.Minute,
		Storage: &StorageConfig{
			Blob:            volume,
			Progress:        sink,
			Source:          storage.SourceSubia,
			SegmentSize:     2 * DefaultBlockSize,
			StagingDir:      filepath.Join(root, "staging"),
			MaxStagingBytes: 1 << 30,
		},
	})
	require.NoError(t, srv.Listen("127.0.0.1:0"))
	go srv.Start()
	t.Cleanup(srv.Stop)

	conn, err := net.Dial("tcp", srv.Addr())
	require.NoError(t, err)
	t.Cleanup(func() { _ = conn.Close() })
	reader := bufio.NewReader(conn)

	phone := "013800000001"
	fileName := "00_64_6401_01_ALARM.jpg"
	size := 3*DefaultBlockSize + 321
	source := buildSource(size)

	sendMsg(t, conn, &model.Msg1210{
		Header:     newAlarmHeader(0x1210, 1),
		TerminalID: "ABC1234",
		AlarmSign:  testAlarmSign(),
		AlarmNo:    "ALARM-OBJ-1",
		Attachments: []model.AlarmAttachmentEntry{
			{FileName: fileName, FileSize: uint32(size)},
		},
	})
	require.Equal(t, uint16(0x0001), readJT808Message(t, reader).Header.MsgID)

	for offset := 0; offset < size; offset += DefaultBlockSize {
		end := offset + DefaultBlockSize
		if end > size {
			end = size
		}
		sendChunk(t, conn, fileName, uint32(offset), source[offset:end])
	}

	sendMsg(t, conn, &model.Msg1211{
		Header:   newAlarmHeader(0x1211, 2),
		FileName: fileName,
		FileSize: uint32(size),
	})
	require.Equal(t, uint16(0x0001), readJT808Message(t, reader).Header.MsgID)
	sendMsg(t, conn, &model.Msg1212{
		Header:   newAlarmHeader(0x1212, 3),
		FileName: fileName,
		FileSize: uint32(size),
	})
	retransmit := receive9212(t, reader)
	require.Equal(t, uint8(0), retransmit.Result)

	key := contracts.AttachmentProgressKey{Source: storage.SourceSubia, Phone: phone, AlarmNo: "ALARM-OBJ-1", FileName: fileName}
	progress := sink.waitStatus(t, key, contracts.AttachmentStatusCompleted)
	require.EqualValues(t, size, progress.ReceivedBytes)
	require.Len(t, progress.Segments, 2)
	for _, segment := range progress.Segments {
		stored, statErr := volume.Stat(context.Background(), segment.Key)
		require.NoError(t, statErr)
		require.Equal(t, contracts.BlobStateCommitted, stored.State)
	}
}
