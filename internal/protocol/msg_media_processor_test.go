package protocol

import (
	"context"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/stretchr/testify/require"

	"github.com/fakeyanss/jt808-server-go/internal/codec/hex"
	"github.com/fakeyanss/jt808-server-go/internal/mediaupload"
	"github.com/fakeyanss/jt808-server-go/internal/protocol/model"
	"github.com/fakeyanss/jt808-server-go/internal/storage"
)

// mediaGeoBody 构造 0x0801 使用的 28 字节位置体。
func mediaGeoBody(alarmSign uint32, bcdTime string) []byte {
	body := hex.WriteDoubleWord(nil, alarmSign)
	body = hex.WriteDoubleWord(body, 0x00000001)
	body = hex.WriteDoubleWord(body, 30000000)
	body = hex.WriteDoubleWord(body, 120000000)
	body = hex.WriteWord(body, 100)
	body = hex.WriteWord(body, 600)
	body = hex.WriteWord(body, 90)
	return hex.WriteBCD(body, bcdTime)
}

// encode0801Body 构造非分包的 0x0801 消息体。
func encode0801Body(mediaID uint32, channel uint8, alarmSign uint32, data []byte) []byte {
	body := hex.WriteDoubleWord(nil, mediaID)
	body = append(body, 0, 0, 1, channel) // 多媒体类型、格式、事件项、逻辑通道
	body = hex.WriteBytes(body, mediaGeoBody(alarmSign, "261112110000"))
	return hex.WriteBytes(body, data)
}

func TestProcess0801PersistsMediaFile(t *testing.T) {
	ctx := context.Background()
	const phone = "188765432109"
	stores := storage.NewMemory()
	mediaStore, err := mediaupload.NewStore(t.TempDir())
	require.NoError(t, err)
	processor := NewJT808MsgProcessor(nil, WithProcessorDeps(ProcessorDeps{Storage: stores, MediaUpload: mediaStore}))

	header := &model.MsgHeader{MsgID: 0x0801, PhoneNumber: phone, SerialNumber: 21, Attr: &model.MsgBodyAttr{}}
	data := []byte("media-payload")
	result, err := processor.Process(ctx, &model.PacketData{Header: header, Body: encode0801Body(7001, 3, 0x00000008, data)})
	require.NoError(t, err)
	require.IsType(t, &model.Msg8800{}, result.Outgoing)

	file, err := stores.MediaFiles.Get(ctx, phone, 7001)
	require.NoError(t, err)
	require.Equal(t, uint8(3), file.ChannelID)
	require.Equal(t, uint32(0x00000008), file.AlarmSign)
	require.Equal(t, int64(len(data)), file.FileSize)
	require.Equal(t, "188765432109/20261112/7001.jpg", filepath.ToSlash(file.RelativePath))

	content, err := os.ReadFile(filepath.Join(mediaStore.Root(), file.RelativePath))
	require.NoError(t, err)
	require.Equal(t, data, content)

	// 行业内存快照保持原有行为。
	require.NotEmpty(t, stores.Industry.MediaUploads(phone))
}

func TestProcess0801WithoutUploadStoreKeepsIndustrySnapshot(t *testing.T) {
	ctx := context.Background()
	const phone = "188765432110"
	stores := storage.NewMemory()
	processor := NewJT808MsgProcessor(nil, WithProcessorDeps(ProcessorDeps{Storage: stores}))

	header := &model.MsgHeader{MsgID: 0x0801, PhoneNumber: phone, SerialNumber: 22, Attr: &model.MsgBodyAttr{}}
	_, err := processor.Process(ctx, &model.PacketData{Header: header, Body: encode0801Body(7002, 1, 0, []byte("x"))})
	require.NoError(t, err)
	require.NotEmpty(t, stores.Industry.MediaUploads(phone))

	_, err = stores.MediaFiles.Get(ctx, phone, 7002)
	require.Error(t, err)
}

// 0x0805 只返回 mediaId 列表，文件由后续 0x0801 补齐；查询时按到达状态展示。
func TestProcess0805LinksMediaIDsWithUploadedFiles(t *testing.T) {
	ctx := context.Background()
	const phone = "188765432111"
	stores := storage.NewMemory()
	mediaStore, err := mediaupload.NewStore(t.TempDir())
	require.NoError(t, err)
	processor := NewJT808MsgProcessor(nil, WithProcessorDeps(ProcessorDeps{Storage: stores, MediaUpload: mediaStore}))

	uploadHeader := &model.MsgHeader{MsgID: 0x0801, PhoneNumber: phone, SerialNumber: 23, Attr: &model.MsgBodyAttr{}}
	_, err = processor.Process(ctx, &model.PacketData{Header: uploadHeader, Body: encode0801Body(7101, 1, 0, []byte("img-7101"))})
	require.NoError(t, err)

	body := hex.WriteWord(nil, 31)
	body = append(body, 0)
	body = hex.WriteWord(body, 2)
	body = hex.WriteDoubleWord(body, 7101)
	body = hex.WriteDoubleWord(body, 7102)
	captureHeader := &model.MsgHeader{MsgID: 0x0805, PhoneNumber: phone, SerialNumber: 24, Attr: &model.MsgBodyAttr{}}
	result, err := processor.Process(ctx, &model.PacketData{Header: captureHeader, Body: body})
	require.NoError(t, err)
	require.IsType(t, &model.Msg8001{}, result.Outgoing)

	captures, err := stores.MediaCaptures.ListCaptures(ctx, phone, 10)
	require.NoError(t, err)
	require.Len(t, captures, 1)
	require.Equal(t, uint16(31), captures[0].AnswerSerial)
	require.Len(t, captures[0].Items, 2)
	require.True(t, captures[0].Items[0].Arrived)
	require.Equal(t, int64(len("img-7101")), captures[0].Items[0].FileSize)
	require.False(t, captures[0].Items[1].Arrived)
}

// 解码后的位置时间应按本地时区解释，避免 API 时间过滤出现 8 小时偏差。
func TestProcess0801LocationTimeUsesLocalTimezone(t *testing.T) {
	ctx := context.Background()
	const phone = "188765432112"
	stores := storage.NewMemory()
	mediaStore, err := mediaupload.NewStore(t.TempDir())
	require.NoError(t, err)
	processor := NewJT808MsgProcessor(nil, WithProcessorDeps(ProcessorDeps{Storage: stores, MediaUpload: mediaStore}))

	header := &model.MsgHeader{MsgID: 0x0801, PhoneNumber: phone, SerialNumber: 25, Attr: &model.MsgBodyAttr{}}
	_, err = processor.Process(ctx, &model.PacketData{Header: header, Body: encode0801Body(7201, 1, 0, []byte("x"))})
	require.NoError(t, err)

	file, err := stores.MediaFiles.Get(ctx, phone, 7201)
	require.NoError(t, err)
	require.True(t, time.Date(2026, 11, 12, 11, 0, 0, 0, hex.ProtocolTimeZone).Equal(file.LocationTime))
}
