package protocol

import (
	"context"
	"testing"

	"github.com/fakeyanss/jt808-server-go/internal/protocol/model"
	"github.com/fakeyanss/jt808-server-go/internal/storage"
	"github.com/stretchr/testify/require"
)

// encode1078Body 编码消息体并返回可复用的消息头与消息体。
func encode1078Body(t *testing.T, msg model.JT808Msg) (*model.MsgHeader, []byte) {
	t.Helper()
	encoded, err := msg.Encode()
	require.NoError(t, err)
	header := msg.GetHeader()
	bodyLength := int(header.Attr.BodyLength)
	return header, encoded[len(encoded)-bodyLength:]
}

func new1078Header(msgID uint16, phone string, serial uint16) *model.MsgHeader {
	return &model.MsgHeader{
		MsgID:        msgID,
		Attr:         &model.MsgBodyAttr{VersionDesc: model.Version2013},
		PhoneNumber:  phone,
		SerialNumber: serial,
	}
}

// 平台下发类 1078 信令在接收侧按协议回通用应答。
func TestProcess1078CommandsProduceGenericAcknowledgement(t *testing.T) {
	cases := []struct {
		name   string
		msg    model.JT808Msg
		msgID  uint16
		serial uint16
	}{
		{"9101", &model.Msg9101{Header: new1078Header(0x9101, "188765432118", 11), ServerIP: "127.0.0.1", TCPPort: 7100}, 0x9101, 11},
		{"9102", &model.Msg9102{Header: new1078Header(0x9102, "188765432118", 12)}, 0x9102, 12},
		{"9201", &model.Msg9201{Header: new1078Header(0x9201, "188765432118", 13), ServerIP: "127.0.0.1"}, 0x9201, 13},
		{"9202", &model.Msg9202{Header: new1078Header(0x9202, "188765432118", 14)}, 0x9202, 14},
		{"9206", &model.Msg9206{Header: new1078Header(0x9206, "188765432118", 15)}, 0x9206, 15},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			header, body := encode1078Body(t, tc.msg)
			result, err := NewJT808MsgProcessor(nil).Process(context.Background(), &model.PacketData{Header: header, Body: body})
			require.NoError(t, err)
			require.NotNil(t, result)
			ack, ok := result.Outgoing.(*model.Msg0001)
			require.True(t, ok)
			require.Equal(t, tc.serial, ack.AnswerSerialNumber)
			require.Equal(t, tc.msgID, ack.AnswerMessageID)
			require.Equal(t, uint8(model.ResultSuccess), uint8(ack.Result))
		})
	}
}

// 0x1205 / 0x9206 / 0x1206 的媒体元数据要进入持久化仓储抽象。
func TestProcess1078MediaMetadataPersists(t *testing.T) {
	ctx := context.Background()
	const phone = "188765432120"

	listHeader := new1078Header(0x1205, phone, 31)
	list := &model.Msg1205{
		Header:             listHeader,
		AnswerSerialNumber: 8,
		MediaCount:         1,
		Items: []model.DeviceMedia{
			{DeviceMediaQuery: model.DeviceMediaQuery{LogicChannelID: 1, MediaType: 2}, Size: 512},
		},
	}
	_, listBody := encode1078Body(t, list)
	result, err := NewJT808MsgProcessor(nil).Process(ctx, &model.PacketData{Header: listHeader, Body: listBody})
	require.NoError(t, err)
	require.Nil(t, result)

	storedList, err := storage.Default().Media.ResourceList(ctx, phone)
	require.NoError(t, err)
	require.NotNil(t, storedList)
	require.Equal(t, uint32(1), storedList.MediaCount)
	require.Len(t, storedList.Items, 1)
	require.Equal(t, uint32(512), storedList.Items[0].Size)

	taskHeader := new1078Header(0x9206, phone, 32)
	task := &model.Msg9206{
		Header:     taskHeader,
		ServerIP:   "127.0.0.1",
		Port:       21,
		Username:   "user",
		Password:   "pass",
		UploadPath: "/media",
		MediaType:  2,
	}
	_, taskBody := encode1078Body(t, task)
	result, err = NewJT808MsgProcessor(nil).Process(ctx, &model.PacketData{Header: taskHeader, Body: taskBody})
	require.NoError(t, err)
	require.IsType(t, &model.Msg0001{}, result.Outgoing)

	storedTask, err := storage.Default().Media.UploadTask(ctx, phone, 32)
	require.NoError(t, err)
	require.NotNil(t, storedTask)
	require.Equal(t, "127.0.0.1", storedTask.ServerIP)
	require.Equal(t, "/media", storedTask.UploadPath)

	noticeHeader := new1078Header(0x1206, phone, 33)
	notice := &model.Msg1206{Header: noticeHeader, AnswerSerialNumber: 32, Result: 0}
	_, noticeBody := encode1078Body(t, notice)
	_, err = NewJT808MsgProcessor(nil).Process(ctx, &model.PacketData{Header: noticeHeader, Body: noticeBody})
	require.NoError(t, err)

	storedNotice, err := storage.Default().Media.UploadNotice(ctx, phone, 32)
	require.NoError(t, err)
	require.NotNil(t, storedNotice)
	require.Equal(t, uint8(0), storedNotice.Result)
}

func TestProcess1078UplinkSignalingRecordsState(t *testing.T) {
	const phone = "188765432119"
	header := new1078Header(0x9105, phone, 21)
	msg := &model.Msg9105{Header: header, LogicChannelID: 1, PacketLossRate: 5}
	_, body := encode1078Body(t, msg)

	result, err := NewJT808MsgProcessor(nil).Process(context.Background(), &model.PacketData{Header: header, Body: body})
	require.NoError(t, err)
	require.Nil(t, result)

	status := storage.Default().Industry.MediaStatus(phone)
	require.NotNil(t, status)
	require.Equal(t, uint8(1), status.LogicChannelID)
	require.Equal(t, uint8(5), status.PacketLossRate)

	noticeHeader := new1078Header(0x1206, phone, 22)
	notice := &model.Msg1206{Header: noticeHeader, AnswerSerialNumber: 15, Result: 1}
	_, noticeBody := encode1078Body(t, notice)
	_, err = NewJT808MsgProcessor(nil).Process(context.Background(), &model.PacketData{Header: noticeHeader, Body: noticeBody})
	require.NoError(t, err)

	stored := storage.Default().Industry.FileUploadNotice(phone)
	require.NotNil(t, stored)
	require.Equal(t, uint16(15), stored.AnswerSerialNumber)
	require.Equal(t, uint8(1), stored.Result)
}
