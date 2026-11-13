package sqlite

import (
	"context"
	"path/filepath"
	"testing"
	"time"

	"github.com/stretchr/testify/require"

	"github.com/fakeyanss/jt808-server-go/internal/protocol/model"
)

// SQLite 写入媒体元数据后关闭重开同一文件，资源列表、上传指令与完成通知仍在。
func TestSQLitePersistsMediaMetadataAcrossReopen(t *testing.T) {
	ctx := context.Background()
	const phone = "188765466001"
	dbPath := filepath.Join(t.TempDir(), "jt808.db")
	migrateSQLite(t, dbPath)

	first, err := Open(dbPath, 0)
	require.NoError(t, err)

	start := time.Date(2026, 11, 8, 9, 0, 0, 0, time.Local)
	end := time.Date(2026, 11, 8, 10, 0, 0, 0, time.Local)
	list := &model.Msg1205{
		Header:             &model.MsgHeader{PhoneNumber: phone},
		AnswerSerialNumber: 3,
		MediaCount:         2,
		Items: []model.DeviceMedia{
			{DeviceMediaQuery: model.DeviceMediaQuery{LogicChannelID: 1, StartTime: &start, EndTime: &end, MediaType: 2}, Size: 4096},
			{DeviceMediaQuery: model.DeviceMediaQuery{LogicChannelID: 2, StartTime: &start, MediaType: 1}, Size: 2048},
		},
	}
	require.NoError(t, first.MediaMetadataRepository().ReplaceResourceList(ctx, phone, list))

	task := &model.Msg9206{
		Header:         &model.MsgHeader{PhoneNumber: phone, SerialNumber: 17},
		ServerIP:       "ftp.example.com",
		Port:           2121,
		Username:       "user",
		Password:       "secret",
		UploadPath:     "/upload/20261108",
		LogicChannelID: 2,
		StartTime:      &start,
		EndTime:        &end,
		AlarmSign:      0x0000000100000002,
		MediaType:      2,
		StreamType:     1,
		StorageType:    1,
		TaskCondition:  0x07,
	}
	require.NoError(t, first.MediaMetadataRepository().SaveUploadTask(ctx, phone, task))
	notice := &model.Msg1206{
		Header:             &model.MsgHeader{PhoneNumber: phone},
		AnswerSerialNumber: 17,
		Result:             1,
	}
	require.NoError(t, first.MediaMetadataRepository().SaveUploadNotice(ctx, phone, notice))

	require.NoError(t, first.Close())

	second, err := Open(dbPath, 0)
	require.NoError(t, err)
	t.Cleanup(func() { _ = second.Close() })
	media := second.MediaMetadataRepository()

	reloadedList, err := media.ResourceList(ctx, phone)
	require.NoError(t, err)
	require.NotNil(t, reloadedList)
	require.Equal(t, uint32(2), reloadedList.MediaCount)
	require.Len(t, reloadedList.Items, 2)
	require.Equal(t, uint8(1), reloadedList.Items[0].LogicChannelID)
	require.Equal(t, uint32(4096), reloadedList.Items[0].Size)
	require.Equal(t, uint32(2048), reloadedList.Items[1].Size)

	reloadedTask, err := media.UploadTask(ctx, phone, 17)
	require.NoError(t, err)
	require.NotNil(t, reloadedTask)
	require.Equal(t, "ftp.example.com", reloadedTask.ServerIP)
	require.Equal(t, uint16(2121), reloadedTask.Port)
	require.Equal(t, "user", reloadedTask.Username)
	require.Equal(t, "secret", reloadedTask.Password)
	require.Equal(t, "/upload/20261108", reloadedTask.UploadPath)
	require.Equal(t, uint64(0x0000000100000002), reloadedTask.AlarmSign)
	require.Equal(t, uint8(0x07), reloadedTask.TaskCondition)
	require.NotNil(t, reloadedTask.StartTime)
	require.Equal(t, start.Unix(), reloadedTask.StartTime.Unix())

	reloadedNotice, err := media.UploadNotice(ctx, phone, 17)
	require.NoError(t, err)
	require.NotNil(t, reloadedNotice)
	require.Equal(t, uint8(1), reloadedNotice.Result)

	// 未写入的流水号返回空，不报错。
	missingTask, err := media.UploadTask(ctx, phone, 99)
	require.NoError(t, err)
	require.Nil(t, missingTask)
	missingList, err := media.ResourceList(ctx, "188765466002")
	require.NoError(t, err)
	require.Nil(t, missingList)
}
