package protocol

import (
	"context"
	"encoding/binary"
	"encoding/json"
	"fmt"
	"net"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	actionpkg "github.com/fakeyanss/jt808-server-go/internal/action"
	"github.com/fakeyanss/jt808-server-go/internal/codec/hex"
	"github.com/fakeyanss/jt808-server-go/internal/protocol/model"
	"github.com/fakeyanss/jt808-server-go/internal/repository/database"
	"github.com/fakeyanss/jt808-server-go/internal/runtime"
	"github.com/stretchr/testify/require"
)

func TestTerminalControlProducesGenericAcknowledgement(t *testing.T) {
	header := &model.MsgHeader{MsgID: 0x8105, SerialNumber: 17, PhoneNumber: "188765432109", Attr: &model.MsgBodyAttr{}}
	result, err := NewJT808MsgProcessor(nil).Process(context.Background(), &model.PacketData{Header: header, Body: []byte{2, 'h', 'o', 's', 't'}})
	require.NoError(t, err)
	require.IsType(t, &model.Msg0001{}, result.Outgoing)
	ack := result.Outgoing.(*model.Msg0001)
	require.Equal(t, uint16(17), ack.AnswerSerialNumber)
	require.Equal(t, uint16(0x8105), ack.AnswerMessageID)
	require.Equal(t, uint8(0), ack.Result)
}

func TestRegionRouteDeletionProducesGenericAcknowledgement(t *testing.T) {
	for _, messageID := range []uint16{0x8601, 0x8603, 0x8605, 0x8607} {
		t.Run(fmt.Sprintf("%04x", messageID), func(t *testing.T) {
			header := &model.MsgHeader{MsgID: messageID, SerialNumber: 18, PhoneNumber: "188765432109", Attr: &model.MsgBodyAttr{}}
			result, err := NewJT808MsgProcessor(nil).Process(context.Background(), &model.PacketData{Header: header, Body: []byte{0}})
			require.NoError(t, err)
			require.IsType(t, &model.Msg0001{}, result.Outgoing)
			require.Equal(t, messageID, result.Outgoing.(*model.Msg0001).AnswerMessageID)
		})
	}
}

// 0x0802 是 0x8802 检索命令的应答，必须按 (phone, answerSerial) 落库并可查询。
func TestProcessMsg0802PersistsSearchResults(t *testing.T) {
	const phone = "188765432802"
	ctx := context.Background()
	msg := &model.Msg0802{
		Header:       &model.MsgHeader{MsgID: 0x0802, PhoneNumber: phone, Attr: &model.MsgBodyAttr{}},
		AnswerSerial: 21,
		Items: []model.MediaSearchItem{{
			MediaID: 9001, MediaType: 0, ChannelID: 1, EventID: 2,
			Location: model.Msg0200{AlarmSign: 4, Latitude: 31000000, Time: "261028103000"},
		}},
	}
	require.NoError(t, NewJT808MsgProcessor(nil).processMsg0802(ctx, &model.ProcessData{Incoming: msg}))

	got, err := database.Default().MediaSearches.List(ctx, phone, 10)
	require.NoError(t, err)
	require.Len(t, got, 1)
	require.Equal(t, uint16(21), got[0].AnswerSerial)
	require.Equal(t, uint32(9001), got[0].MediaID)
	require.Equal(t, uint32(31000000), got[0].Latitude)
	require.True(t, got[0].LocationTime.Equal(time.Date(2026, 10, 28, 10, 30, 0, 0, hex.ProtocolTimeZone)))

	// 终端重传同一条应答但结果变空时，旧结果必须被覆盖清空。
	msg.Items = nil
	require.NoError(t, NewJT808MsgProcessor(nil).processMsg0802(ctx, &model.ProcessData{Incoming: msg}))
	got, err = database.Default().MediaSearches.List(ctx, phone, 10)
	require.NoError(t, err)
	require.Empty(t, got)
}

func TestParseMediaSearchLocationTime(t *testing.T) {
	require.True(t, parseMediaSearchLocationTime("261028103000").Equal(
		time.Date(2026, 10, 28, 10, 30, 0, 0, hex.ProtocolTimeZone)))
	require.True(t, parseMediaSearchLocationTime("26102810300").IsZero())
	require.True(t, parseMediaSearchLocationTime("261340103000").IsZero())
}

func TestProcessMsg0107PersistsAndDeliversTerminalProperties(t *testing.T) {
	const phone = "188765432107"
	ctx := context.Background()

	waiter, cancel, err := runtime.Default().PropertyResponses.Register(phone)
	require.NoError(t, err)
	defer cancel()

	msg := &model.Msg0107{
		Header: &model.MsgHeader{
			MsgID:       0x0107,
			Attr:        &model.MsgBodyAttr{},
			PhoneNumber: phone,
		},
		ManufacturerID:  "ACME",
		TerminalModel:   "T-2019",
		TerminalID:      "TERM-107",
		FirmwareVersion: "FW-1.0",
	}
	require.NoError(t, NewJT808MsgProcessor(nil).processMsg0107(ctx, &model.ProcessData{Incoming: msg}))

	select {
	case got := <-waiter:
		require.Same(t, msg, got)
	default:
		t.Fatal("property response was not delivered")
	}

	props, err := database.Default().Properties.Get(ctx, phone)
	require.NoError(t, err)
	require.Equal(t, "ACME", props.ManufacturerID)
	require.Equal(t, "T-2019", props.TerminalModel)
	require.Equal(t, "FW-1.0", props.FirmwareVersion)
	require.NoError(t, database.Default().Properties.Delete(ctx, phone))
}

// 心跳只刷新进程内最近通信时间，不再写库：运行时状态不持久化。
func TestHeartbeatDoesNotPersistRuntime(t *testing.T) {
	const phone = "188765499004"
	ctx := context.Background()
	devices := database.Default().Devices
	require.NoError(t, devices.Upsert(ctx, &model.Device{
		Phone:     phone,
		Keepalive: time.Minute,
	}))
	t.Cleanup(func() {
		runtime.Default().Heartbeat.Remove(ctx, phone)
		_ = devices.DeleteByPhone(ctx, phone)
	})

	msg := &model.Msg0002{Header: &model.MsgHeader{MsgID: 0x0002, PhoneNumber: phone, Attr: &model.MsgBodyAttr{}}}
	require.NoError(t, NewJT808MsgProcessor(nil).processMsg0002(ctx, &model.ProcessData{Incoming: msg}))

	stored, err := devices.GetByPhone(ctx, phone)
	require.NoError(t, err)
	require.True(t, stored.LastestComTime.IsZero(), "heartbeat must not persist last com time")
	require.Equal(t, model.DeviceStatusOffline, stored.Status, "heartbeat must not persist status")

	last, ok := runtime.Default().Heartbeat.Last(ctx, phone)
	require.True(t, ok)
	require.WithinDuration(t, time.Now(), last, 5*time.Second)
}

// 应答必须按流水号精确配对：先回第二条不应清掉第一条的等待项。
func TestProcessAcknowledgesResponseBySerial(t *testing.T) {
	const phone = "188765432111"
	left, _ := net.Pipe()
	defer left.Close()
	session := &model.Session{ID: "ack-serial", Conn: left, AckTimeout: time.Hour, MaxRetries: 1}
	defer session.ClosePending()

	session.TrackReliable(0x8103, 31, phone, []byte("cmd-1"))
	session.TrackReliable(0x8103, 32, phone, []byte("cmd-2"))

	ctx := context.WithValue(context.Background(), model.SessionCtxKey{}, session)
	// 应答第二条命令。
	header := &model.MsgHeader{MsgID: 0x0001, PhoneNumber: phone, Attr: &model.MsgBodyAttr{}}
	_, err := NewJT808MsgProcessor(nil).Process(ctx, &model.PacketData{Header: header, Body: []byte{0, 32, 0x81, 0x03, 0}})
	require.NoError(t, err)
	require.Equal(t, 1, session.PendingCount(), "only the answered serial may be cleared")
	require.True(t, session.Acknowledge(0x8103, 31), "the first command must still be pending")
	require.Equal(t, 0, session.PendingCount())

	// 重复应答不清除任何等待项。
	_, err = NewJT808MsgProcessor(nil).Process(ctx, &model.PacketData{Header: header, Body: []byte{0, 32, 0x81, 0x03, 0}})
	require.NoError(t, err)
	require.Equal(t, 0, session.PendingCount())
}

// 错会话的应答不得清除另一个会话的等待项。
func TestProcessIgnoresResponseFromOtherSession(t *testing.T) {
	const phone = "188765432112"
	ownerConn, _ := net.Pipe()
	defer ownerConn.Close()
	owner := &model.Session{ID: "owner", Conn: ownerConn, AckTimeout: time.Hour, MaxRetries: 1}
	defer owner.ClosePending()
	owner.TrackReliable(0x8103, 41, phone, []byte("cmd"))

	otherConn, _ := net.Pipe()
	defer otherConn.Close()
	other := &model.Session{ID: "other", Conn: otherConn, AckTimeout: time.Hour, MaxRetries: 1}
	defer other.ClosePending()

	ctx := context.WithValue(context.Background(), model.SessionCtxKey{}, other)
	header := &model.MsgHeader{MsgID: 0x0001, PhoneNumber: phone, Attr: &model.MsgBodyAttr{}}
	_, err := NewJT808MsgProcessor(nil).Process(ctx, &model.PacketData{Header: header, Body: []byte{0, 41, 0x81, 0x03, 0}})
	require.NoError(t, err)
	require.Equal(t, 1, owner.PendingCount(), "response from another session must not clear pending")
	require.Equal(t, 0, other.PendingCount())
}

func TestSimulatorParameterTemplateDecodes(t *testing.T) {
	header := &model.MsgHeader{MsgID: 0x8104, SerialNumber: 20, PhoneNumber: "188765432100", Attr: &model.MsgBodyAttr{}}
	result, err := NewJT808MsgProcessor(nil).Process(context.Background(), &model.PacketData{Header: header})
	require.NoError(t, err)
	reply, ok := result.Outgoing.(*model.Msg0104)
	require.True(t, ok)
	require.Equal(t, uint8(39), reply.Parameters.ParamCnt)
	require.Len(t, reply.Parameters.Params, 39)
	last := reply.Parameters.Params[38]
	require.Equal(t, uint32(0x0076), last.ParamID)
	require.Equal(t, uint8(15), last.ParamLen)
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
	database.Default().Devices.Upsert(context.Background(), &model.Device{Phone: phone, Keepalive: time.Minute})
	t.Cleanup(func() {
		database.Default().Devices.DeleteByPhone(context.Background(), phone)
		database.Default().Geo.Delete(context.Background(), phone)
	})

	processor := NewJT808MsgProcessor(nil)
	header := &model.MsgHeader{MsgID: 0x0200, SerialNumber: 32, PhoneNumber: phone, Attr: &model.MsgBodyAttr{}}
	result, err := processor.Process(context.Background(), &model.PacketData{Header: header, Body: locationReportBody(0)})
	require.NoError(t, err)
	require.IsType(t, &model.Msg8001{}, result.Outgoing)

	geo, err := database.Default().Geo.Latest(context.Background(), phone)
	require.NoError(t, err)
	require.Equal(t, phone, geo.Phone)
}

func TestSimulatorRepliesToRegionDriverRecorderAndMultimediaCommands(t *testing.T) {
	tests := []struct {
		id       uint16
		body     []byte
		response any
	}{
		{0x8608, []byte{1, 0, 0, 0, 0}, &model.Msg0608{}},
		{0x8702, nil, &model.Msg0702{}},
		{0x8700, []byte{3, 0xaa}, &model.Msg0700{}},
		{0x8701, []byte{3, 0xaa}, &model.Msg0001{}},
		{0x8801, []byte{1, 0, 1, 0, 0, 0, 1, 1, 0, 0, 0, 0}, &model.Msg0805{}},
		{0x8802, make([]byte, 15), &model.Msg0802{}},
		{0x8803, []byte{1, 1, 1, 0x26, 0x10, 0x28, 0, 0, 0, 0x26, 0x10, 0x28, 0x23, 0x59, 0x59, 1}, &model.Msg0001{}},
		{0x8804, []byte{1, 0, 30, 1, 2}, &model.Msg0001{}},
		{0x8805, []byte{0, 0, 0, 1, 1}, &model.Msg0001{}},
	}
	for _, tt := range tests {
		t.Run(fmt.Sprintf("%04x", tt.id), func(t *testing.T) {
			header := &model.MsgHeader{MsgID: tt.id, SerialNumber: 19, PhoneNumber: "188765432109", Attr: &model.MsgBodyAttr{}}
			result, err := NewJT808MsgProcessor(nil).Process(context.Background(), &model.PacketData{Header: header, Body: tt.body})
			require.NoError(t, err)
			require.IsType(t, tt.response, result.Outgoing)
			if ack, ok := result.Outgoing.(*model.Msg0001); ok {
				require.Equal(t, tt.id, ack.AnswerMessageID)
			}
			if capture, ok := result.Outgoing.(*model.Msg0805); ok {
				require.Equal(t, uint16(19), capture.AnswerSerial)
				require.Equal(t, uint8(0), capture.Result)
			}
		})
	}
}

func buildTest0200AlarmIDNumber() []byte {
	data := hex.WriteString(nil, "ABC1234")
	data = hex.WriteBCD(data, "210101120000")
	data = hex.WriteByte(data, 5)
	data = hex.WriteByte(data, 2)
	return hex.WriteByte(data, 0xA0)
}

func buildTest0200ADAS() []byte {
	data := hex.WriteDoubleWord(nil, 0x01020304)
	data = hex.WriteByte(data, 0x01)
	data = hex.WriteByte(data, 0x01)
	data = hex.WriteByte(data, 0x01)
	data = hex.WriteByte(data, 60)
	data = hex.WriteByte(data, 20)
	data = hex.WriteByte(data, 0x01)
	data = hex.WriteByte(data, 0x01)
	data = hex.WriteByte(data, 80)
	data = hex.WriteByte(data, 70)
	data = hex.WriteWord(data, 100)
	data = hex.WriteDoubleWord(data, 0x07D5A8C0)
	data = hex.WriteDoubleWord(data, 0x0E5A9FA0)
	data = hex.WriteBCD(data, "201231235959")
	data = hex.WriteWord(data, 0x0003)
	return append(data, buildTest0200AlarmIDNumber()...)
}

func buildTest0200DSM() []byte {
	data := hex.WriteDoubleWord(nil, 0x11223344)
	data = hex.WriteByte(data, 0x02)
	data = hex.WriteByte(data, 0x02)
	data = hex.WriteByte(data, 0x02)
	data = hex.WriteByte(data, 5)
	data = hex.WriteBytes(data, []byte{0x11, 0x22, 0x33, 0x44})
	data = hex.WriteByte(data, 66)
	data = hex.WriteWord(data, 200)
	data = hex.WriteDoubleWord(data, 0x07D5A8C1)
	data = hex.WriteDoubleWord(data, 0x0E5A9FA1)
	data = hex.WriteBCD(data, "210101120000")
	data = hex.WriteWord(data, 0x0011)
	return append(data, buildTest0200AlarmIDNumber()...)
}

func buildTest0200Body(alarmSign uint32, extItems ...model.LocationExtItem) []byte {
	data := hex.WriteDoubleWord(nil, alarmSign)
	data = hex.WriteDoubleWord(data, 0)
	data = hex.WriteDoubleWord(data, 0x07D5A8C0)
	data = hex.WriteDoubleWord(data, 0x0E5A9FA0)
	data = hex.WriteWord(data, 100)
	data = hex.WriteWord(data, 70)
	data = hex.WriteWord(data, 90)
	data = hex.WriteBCD(data, "201231235959")
	for _, item := range extItems {
		data = hex.WriteByte(data, item.ID)
		data = hex.WriteByte(data, uint8(len(item.Data)))
		data = hex.WriteBytes(data, item.Data)
	}
	return data
}

// 0x0200 报警事件经 OnAlarm HTTP 回调后，JSON 中应保留结构化扩展项，
// 且 adas 与 dsm 可通过 extType 区分。
func TestOnAlarmHTTPCallbackIncludesExtItems(t *testing.T) {
	received := make(chan map[string]any, 4)
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		var body map[string]any
		if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
			w.WriteHeader(http.StatusBadRequest)
			return
		}
		received <- body
		w.WriteHeader(http.StatusOK)
	}))
	defer server.Close()

	const phone = "188765432120"
	ctx := context.Background()
	devices := database.Default().Devices
	require.NoError(t, devices.Upsert(ctx, &model.Device{Phone: phone, Keepalive: time.Minute}))
	t.Cleanup(func() { _ = devices.DeleteByPhone(ctx, phone) })

	adas := buildTest0200ADAS()
	dsm := buildTest0200DSM()
	body := buildTest0200Body(0x00000001,
		model.LocationExtItem{ID: 0x64, Data: adas},
		model.LocationExtItem{ID: 0x65, Data: dsm},
	)
	header := &model.MsgHeader{
		MsgID:        0x0200,
		PhoneNumber:  phone,
		SerialNumber: 101,
		Attr:         &model.MsgBodyAttr{VersionDesc: model.Version2013},
	}

	callback := actionpkg.NewHTTPCallback(server.URL, time.Second)
	_, err := NewJT808MsgProcessor(callback).Process(ctx, &model.PacketData{Header: header, Body: body})
	require.NoError(t, err)

	timeout := time.After(2 * time.Second)
	for {
		select {
		case event := <-received:
			if event["event"] != "alarm" {
				continue
			}
			payload, ok := event["payload"].(map[string]any)
			require.True(t, ok)
			items, ok := payload["extItems"].([]any)
			require.True(t, ok)
			require.Len(t, items, 2)

			adasJSON, ok := items[0].(map[string]any)
			require.True(t, ok)
			require.Equal(t, "adas", adasJSON["extType"])
			require.Equal(t, float64(0x64), adasJSON["extId"])
			require.Equal(t, "ABC1234", adasJSON["alarmIdNumber"].(map[string]any)["deviceId"])

			dsmJSON, ok := items[1].(map[string]any)
			require.True(t, ok)
			require.Equal(t, "dsm", dsmJSON["extType"])
			require.Equal(t, float64(0x65), dsmJSON["extId"])
			require.Equal(t, float64(2), dsmJSON["alarmOrEventType"])
			return
		case <-timeout:
			t.Fatal("alarm callback was not delivered")
		}
	}
}
