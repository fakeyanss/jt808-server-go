package repository

import (
	"context"
	"testing"
	"time"

	"github.com/fakeyanss/jt808-server-go/internal/protocol/model"
	"github.com/stretchr/testify/require"
)

func TestMediaMetadataRepositoryMemoryRoundTrip(t *testing.T) {
	ctx := context.Background()
	const phone = "188765433001"
	repo := NewMediaMetadataRepository()

	start := time.Date(2026, 11, 8, 9, 0, 0, 0, time.Local)
	items := []model.DeviceMedia{
		{
			DeviceMediaQuery: model.DeviceMediaQuery{
				LogicChannelID: 1,
				StartTime:      &start,
				MediaType:      2,
			},
			Size: 1024,
		},
	}
	list := &model.Msg1205{Header: &model.MsgHeader{PhoneNumber: phone}, AnswerSerialNumber: 9, MediaCount: 1, Items: items}
	require.NoError(t, repo.ReplaceResourceList(ctx, phone, list))

	stored, err := repo.ResourceList(ctx, phone)
	require.NoError(t, err)
	require.NotNil(t, stored)
	require.Equal(t, uint32(1), stored.MediaCount)
	require.Len(t, stored.Items, 1)
	require.Equal(t, uint32(1024), stored.Items[0].Size)

	// 返回快照：修改读取结果不得影响仓储内容。
	stored.Items[0].Size = 2048
	again, err := repo.ResourceList(ctx, phone)
	require.NoError(t, err)
	require.Equal(t, uint32(1024), again.Items[0].Size)

	task := &model.Msg9206{
		Header:         &model.MsgHeader{PhoneNumber: phone, SerialNumber: 21},
		ServerIP:       "10.0.0.1",
		Port:           21,
		Username:       "user",
		Password:       "pass",
		UploadPath:     "/media",
		LogicChannelID: 1,
		AlarmSign:      0x0000000100000002,
		MediaType:      2,
	}
	require.NoError(t, repo.SaveUploadTask(ctx, phone, task))
	savedTask, err := repo.UploadTask(ctx, phone, 21)
	require.NoError(t, err)
	require.NotNil(t, savedTask)
	require.Equal(t, "10.0.0.1", savedTask.ServerIP)
	require.Equal(t, uint64(0x0000000100000002), savedTask.AlarmSign)

	notice := &model.Msg1206{Header: &model.MsgHeader{PhoneNumber: phone}, AnswerSerialNumber: 21, Result: 0}
	require.NoError(t, repo.SaveUploadNotice(ctx, phone, notice))
	savedNotice, err := repo.UploadNotice(ctx, phone, 21)
	require.NoError(t, err)
	require.NotNil(t, savedNotice)
	require.Equal(t, uint8(0), savedNotice.Result)

	missing, err := repo.ResourceList(ctx, "188765433002")
	require.NoError(t, err)
	require.Nil(t, missing)
	require.Error(t, repo.ReplaceResourceList(ctx, phone, nil))
}
