package protocol_test

import (
	"context"
	"encoding/binary"
	"testing"
	"time"

	"github.com/stretchr/testify/require"

	"github.com/fakeyanss/jt808-server-go/internal/business"
	"github.com/fakeyanss/jt808-server-go/internal/domain"
	"github.com/fakeyanss/jt808-server-go/internal/protocol"
	"github.com/fakeyanss/jt808-server-go/internal/protocol/model"
	"github.com/fakeyanss/jt808-server-go/internal/repository/database"
)

// newBusinessProcessor 构造单机形态的处理器：协议内核 + 注入的管控业务。
func newBusinessProcessor() *protocol.JT808MsgProcessor {
	repos := database.Default().Repositories
	return protocol.NewJT808MsgProcessor(nil, protocol.WithProcessorDeps(protocol.ProcessorDeps{
		Repos:    repos,
		Business: business.New(business.Options{Repositories: repos}),
	}))
}

func locationReportBody(alarmSign uint32) []byte {
	body := make([]byte, 28)
	binary.BigEndian.PutUint32(body[0:4], alarmSign)
	binary.BigEndian.PutUint32(body[8:12], 39543265)   // 纬度
	binary.BigEndian.PutUint32(body[12:16], 116235432) // 经度
	copy(body[22:28], []byte{0x26, 0x11, 0x01, 0x12, 0x00, 0x00})
	return body
}

// 位置上报写入定位缓存。
func TestLocationReportWritesGeo(t *testing.T) {
	const phone = "188765499002"
	database.Default().Devices.Upsert(context.Background(), &domain.Device{Phone: phone, Keepalive: time.Minute})
	t.Cleanup(func() {
		database.Default().Devices.DeleteByPhone(context.Background(), phone)
		database.Default().Geo.Delete(context.Background(), phone)
	})

	processor := newBusinessProcessor()
	header := &model.MsgHeader{MsgID: 0x0200, SerialNumber: 32, PhoneNumber: phone, Attr: &model.MsgBodyAttr{}}
	result, err := processor.Process(context.Background(), &model.PacketData{Header: header, Body: locationReportBody(0)})
	require.NoError(t, err)
	require.IsType(t, &model.Msg8001{}, result.Outgoing)

	geo, err := database.Default().Geo.Latest(context.Background(), phone)
	require.NoError(t, err)
	require.Equal(t, phone, geo.Phone)
}

// 旧位置不能把新在线态改成休眠：只有不早于最新定位的 ACC 关上报才推导 Sleeping。
func TestLocationReportStaleAccOffDoesNotRegressStatus(t *testing.T) {
	const phone = "188765499003"
	ctx := context.Background()
	devices := database.Default().Devices
	require.NoError(t, devices.Upsert(ctx, &domain.Device{
		Phone: phone, Keepalive: time.Minute, Status: domain.DeviceStatusOnline,
	}))
	t.Cleanup(func() {
		_ = devices.DeleteByPhone(ctx, phone)
		_ = database.Default().Geo.Delete(ctx, phone)
	})

	processor := newBusinessProcessor()
	send := func(statusSign uint32, when [6]byte) {
		body := locationReportBody(0)
		binary.BigEndian.PutUint32(body[4:8], statusSign)
		copy(body[22:28], when[:])
		header := &model.MsgHeader{MsgID: 0x0200, SerialNumber: 32, PhoneNumber: phone, Attr: &model.MsgBodyAttr{}}
		_, err := processor.Process(ctx, &model.PacketData{Header: header, Body: body})
		require.NoError(t, err)
	}

	// ACC 开，位置时间 12:00。
	send(1, [6]byte{0x26, 0x11, 0x01, 0x12, 0x00, 0x00})
	// 晚到的旧位置 11:00，ACC 关；不得把在线态改成休眠。
	send(0, [6]byte{0x26, 0x11, 0x01, 0x11, 0x00, 0x00})
	device, err := devices.GetByPhone(ctx, phone)
	require.NoError(t, err)
	require.Equal(t, domain.DeviceStatusOnline, device.Status)

	// 更新的位置 13:00，ACC 关；允许推导休眠。
	send(0, [6]byte{0x26, 0x11, 0x01, 0x13, 0x00, 0x00})
	device, err = devices.GetByPhone(ctx, phone)
	require.NoError(t, err)
	require.Equal(t, domain.DeviceStatusSleeping, device.Status)
}
