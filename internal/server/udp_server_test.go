package server

import (
	"bytes"
	"compress/zlib"
	"context"
	"errors"
	"net"
	"strings"
	"testing"
	"time"

	"github.com/fakeyanss/jt808-server-go/internal/client"
	"github.com/fakeyanss/jt808-server-go/internal/protocol"
	"github.com/fakeyanss/jt808-server-go/internal/protocol/model"
	"github.com/fakeyanss/jt808-server-go/internal/storage"
	"github.com/stretchr/testify/require"
)

func TestTCPServerRetransmitsRequestedDownlinkFragments(t *testing.T) {
	server := NewTCPServer()
	require.NoError(t, server.Listen("127.0.0.1:0"))
	go server.Start()
	t.Cleanup(server.Stop)

	cli := client.NewTCPClient()
	require.NoError(t, cli.Dial(server.listener.Addr().String()))
	t.Cleanup(cli.Stop)
	const phone = "188765432109"
	var session *model.Session
	deadline := time.Now().Add(2 * time.Second)
	for time.Now().Before(deadline) {
		session, _ = storage.GetSession(cli.Session.Conn.LocalAddr().String())
		if session != nil {
			break
		}
		time.Sleep(10 * time.Millisecond)
	}
	require.NotNil(t, session)
	device := &model.Device{Phone: phone, VersionDesc: model.Version2013}
	originalSerial := session.GetNextSerialNum()
	params := &model.DeviceParams{ParamCnt: 6}
	for i := 0; i < 6; i++ {
		params.Params = append(params.Params, &model.ParamData{ParamID: 0x0010, ParamValue: strings.Repeat("X", 200)})
	}
	message := &model.Msg8103{Header: model.GenMsgHeader(device, 0x8103, originalSerial), Parameters: params}
	encoded, err := message.Encode()
	require.NoError(t, err)
	total := uint16((len(encoded) - 12 + 1022) / 1023)
	require.Greater(t, total, uint16(1))
	server.Send(session.ID, message)
	frameHandler := protocol.NewJT808FrameHandler(cli.Session.Conn)
	originalFrames := make([][]byte, total)
	for i := uint16(0); i < total; i++ {
		_ = cli.Session.Conn.SetReadDeadline(time.Now().Add(2 * time.Second))
		got, err := frameHandler.Recv(context.Background())
		require.NoError(t, err)
		originalFrames[i] = append([]byte(nil), got...)
	}
	require.NoError(t, cli.RequestFragmentRetransmission(device, originalSerial, []uint16{2, total}))
	for _, index := range []uint16{2, total} {
		_ = cli.Session.Conn.SetReadDeadline(time.Now().Add(2 * time.Second))
		got, err := frameHandler.Recv(context.Background())
		require.NoError(t, err)
		require.Equal(t, originalFrames[index-1], []byte(got), "retransmitted fragment %d must match its original encoded frame", index)
	}
}

func TestUDPSessionConnKeepsDatagramBoundaries(t *testing.T) {
	conn := newUDPSessionConn(&net.UDPConn{}, &net.UDPAddr{}, nil)
	require.True(t, conn.deliver([]byte{0x7e, 0x11}))
	buf := make([]byte, 8)
	n, err := conn.Read(buf)
	require.NoError(t, err)
	require.Equal(t, []byte{0x7e, 0x11}, buf[:n])
	_, err = conn.Read(buf)
	require.True(t, errors.Is(err, protocol.ErrDatagramBoundary))
	_ = conn.Close()
}

func TestUDPServerProcessesJT808Registration(t *testing.T) {
	server := NewUDPServer()
	require.NoError(t, server.Listen("127.0.0.1:0"))
	go server.Start()
	t.Cleanup(server.Stop)

	cli := client.NewTCPClient()
	require.NoError(t, cli.DialTransport(server.listener.LocalAddr().String(), model.UDPProto))
	t.Cleanup(cli.Stop)
	go cli.Start()

	const phone = "188765432101"
	registration := &model.Msg0100{
		Header: &model.MsgHeader{
			MsgID:       0x0100,
			Attr:        &model.MsgBodyAttr{VersionDesc: model.Version2013},
			PhoneNumber: phone,
		},
		ProvinceID:     11,
		CityID:         1101,
		ManufacturerID: "ABCDE",
		DeviceMode:     "JT808-TEST-DEVICE-01",
		DeviceID:       "DEV0001",
		PlateColor:     1,
		PlateNumber:    "京A12345",
	}
	encoded, err := registration.Encode()
	require.NoError(t, err)
	total := uint16((len(encoded) - 12 + 15) / 16)
	require.NoError(t, cli.SendFragmentsWithFault(registration, 16, false, total))
	deadline := time.Now().Add(3 * time.Second)
	for time.Now().Before(deadline) {
		device, err := storage.GetDeviceCache().GetDeviceByPhone(phone)
		if err == nil && device.Status == model.DeviceStatusOnline {
			break
		}
		time.Sleep(10 * time.Millisecond)
	}
	device, err := storage.GetDeviceCache().GetDeviceByPhone(phone)
	require.NoError(t, err)
	require.Equal(t, model.DeviceStatusOnline, device.Status)

	storage.GetDeviceCache().DelDeviceByPhone(phone)
	protocol.NewKeepaliveTimer().Cancel(phone)
}

func TestTCPServerProcessesJT808Registration(t *testing.T) {
	server := NewTCPServer()
	require.NoError(t, server.Listen("127.0.0.1:0"))
	go server.Start()
	t.Cleanup(server.Stop)

	cli := client.NewTCPClient()
	require.NoError(t, cli.DialTransport(server.listener.Addr().String(), model.TCPProto))
	t.Cleanup(cli.Stop)
	go cli.Start()

	const phone = "188765432102"
	registration := &model.Msg0100{
		Header: &model.MsgHeader{
			MsgID:       0x0100,
			Attr:        &model.MsgBodyAttr{VersionDesc: model.Version2013},
			PhoneNumber: phone,
		},
		ProvinceID:     11,
		CityID:         1101,
		ManufacturerID: "ABCDE",
		DeviceMode:     "JT808-TEST-DEVICE-01",
		DeviceID:       "DEV0002",
		PlateColor:     1,
		PlateNumber:    "京A12346",
	}
	encoded, err := registration.Encode()
	require.NoError(t, err)
	total := uint16((len(encoded) - 12 + 15) / 16)
	require.NoError(t, cli.SendFragmentsWithFault(registration, 16, false, total))
	deadline := time.Now().Add(3 * time.Second)
	for time.Now().Before(deadline) {
		device, err := storage.GetDeviceCache().GetDeviceByPhone(phone)
		if err == nil && device.Status == model.DeviceStatusOnline {
			break
		}
		time.Sleep(10 * time.Millisecond)
	}
	device, err := storage.GetDeviceCache().GetDeviceByPhone(phone)
	require.NoError(t, err)
	require.Equal(t, model.DeviceStatusOnline, device.Status)

	session, err := storage.GetSession(device.SessionID)
	require.NoError(t, err)
	paramSerial := session.GetNextSerialNum()
	paramResponse, cancelParam, err := storage.GetParamResponseBroker().Register(phone, paramSerial)
	require.NoError(t, err)
	defer cancelParam()
	server.Send(session.ID, &model.Msg8106{Header: model.GenMsgHeader(device, 0x8106, paramSerial), ParamIDs: []uint32{1, 2}})
	select {
	case response := <-paramResponse:
		require.Equal(t, paramSerial, response.AnswerSerialNumber)
		require.Equal(t, uint8(2), response.AnswerParamCnt)
	case <-time.After(2 * time.Second):
		t.Fatal("client simulator did not answer 0x8106")
	}
	propertyResponse, cancelProperty, err := storage.GetPropertyResponseBroker().Register(phone)
	require.NoError(t, err)
	defer cancelProperty()
	server.Send(session.ID, &model.Msg8107{Header: model.GenMsgHeader(device, 0x8107, session.GetNextSerialNum())})
	select {
	case response := <-propertyResponse:
		require.Equal(t, "FAKEY", response.ManufacturerID)
	case <-time.After(2 * time.Second):
		t.Fatal("client simulator did not answer 0x8107")
	}
	report := &model.Msg0200{Header: model.GenMsgHeader(device, 0x0200, session.GetNextSerialNum()), AlarmSign: 0x01, StatusSign: 0x03, Latitude: 31123456, Longitude: 121123456, Altitude: 12, Speed: 85, Direction: 90, Time: "261028204500"}
	require.NoError(t, cli.SendWithError(report))
	deadline = time.Now().Add(2 * time.Second)
	for time.Now().Before(deadline) {
		if _, err := storage.GetGeoCache().GetGeoLatestByPhone(phone); err == nil {
			break
		}
		time.Sleep(10 * time.Millisecond)
	}
	locationSerial := session.GetNextSerialNum()
	locationResponse, cancelLocation, err := storage.GetLocationResponseBroker().Register(phone, locationSerial)
	require.NoError(t, err)
	defer cancelLocation()
	server.Send(session.ID, &model.Msg8201{Header: model.GenMsgHeader(device, 0x8201, locationSerial)})
	select {
	case response := <-locationResponse:
		require.Equal(t, locationSerial, response.AnswerSerialNumber)
		require.Equal(t, report.Latitude, response.Location.Latitude)
		require.Equal(t, report.Longitude, response.Location.Longitude)
		require.Equal(t, report.AlarmSign, response.Location.AlarmSign)
	case <-time.After(2 * time.Second):
		t.Fatal("client simulator did not answer 0x8201")
	}
	alarms := storage.GetAlarmCache().List(phone)
	require.NotEmpty(t, alarms)
	require.Equal(t, report.Header.SerialNumber, alarms[len(alarms)-1].MessageSerial)
	confirmSerial := session.GetNextSerialNum()
	storage.GetAlarmCache().RegisterConfirmation(phone, confirmSerial, report.Header.SerialNumber, report.AlarmSign)
	server.Send(session.ID, &model.Msg8203{Header: model.GenMsgHeader(device, 0x8203, confirmSerial), AlarmSerial: report.Header.SerialNumber, AlarmType: report.AlarmSign})
	deadline = time.Now().Add(2 * time.Second)
	for time.Now().Before(deadline) {
		alarms = storage.GetAlarmCache().List(phone)
		if len(alarms) > 0 && alarms[len(alarms)-1].Confirmed {
			break
		}
		time.Sleep(10 * time.Millisecond)
	}
	require.True(t, alarms[len(alarms)-1].Confirmed, "terminal 0x0001 acknowledgment should confirm the alarm")
	trackSerial := session.GetNextSerialNum()
	server.Send(session.ID, &model.Msg8202{Header: model.GenMsgHeader(device, 0x8202, trackSerial), TimeInterval: 1, ValidityPeriod: 30})
	deadline = time.Now().Add(2 * time.Second)
	for time.Now().Before(deadline) && storage.GetLocationTrackingCache().Interval(phone, 9*time.Second) != time.Second {
		time.Sleep(10 * time.Millisecond)
	}
	require.Equal(t, time.Second, storage.GetLocationTrackingCache().Interval(phone, 9*time.Second))
	server.Send(session.ID, &model.Msg8202{Header: model.GenMsgHeader(device, 0x8202, session.GetNextSerialNum()), TimeInterval: 0})
	deadline = time.Now().Add(2 * time.Second)
	for time.Now().Before(deadline) && storage.GetLocationTrackingCache().Interval(phone, 9*time.Second) != 9*time.Second {
		time.Sleep(10 * time.Millisecond)
	}
	require.Equal(t, 9*time.Second, storage.GetLocationTrackingCache().Interval(phone, 9*time.Second))
	storage.GetDeviceCache().DelDeviceByPhone(phone)
	protocol.NewKeepaliveTimer().Cancel(phone)
}

func TestTCPClientSimulatorIndustryMessages(t *testing.T) {
	server := NewTCPServer()
	require.NoError(t, server.Listen("127.0.0.1:0"))
	go server.Start()
	t.Cleanup(server.Stop)
	cli := client.NewTCPClient()
	require.NoError(t, cli.Dial(server.listener.Addr().String()))
	t.Cleanup(cli.Stop)
	go cli.Start()
	const phone = "188765433210"
	registration := &model.Msg0100{Header: &model.MsgHeader{MsgID: 0x0100, Attr: &model.MsgBodyAttr{VersionDesc: model.Version2013}, PhoneNumber: phone}, ProvinceID: 11, CityID: 1101, ManufacturerID: "ABCDE", DeviceMode: "JT808-INDUSTRY-SIM", DeviceID: "INDUSTRY-01", PlateColor: 1, PlateNumber: "京A76543"}
	require.NoError(t, cli.SendWithError(registration))
	var device *model.Device
	deadline := time.Now().Add(3 * time.Second)
	for time.Now().Before(deadline) {
		device, _ = storage.GetDeviceCache().GetDeviceByPhone(phone)
		if device != nil && device.Status == model.DeviceStatusOnline {
			break
		}
		time.Sleep(10 * time.Millisecond)
	}
	require.NotNil(t, device)
	require.Equal(t, model.DeviceStatusOnline, device.Status)
	t.Cleanup(func() { storage.GetDeviceCache().DelDeviceByPhone(phone); protocol.NewKeepaliveTimer().Cancel(phone) })

	industry := storage.GetIndustryCache()
	for _, msg := range []model.JT808Msg{
		&model.Msg0702{Header: model.GenMsgHeader(device, 0x0702, 2), State: 1, Time: "261028120000", ReadResult: 0, Name: "模拟司机", QualificationCode: "CERT-01", IssuingAuthority: "交通部门", ValidUntil: "20301231", IdentityNumber: "ID-01"},
		&model.Msg0705{Header: model.GenMsgHeader(device, 0x0705, 3), FirstTime: "1234567890", Items: []model.CANDataItem{{CANID: 0x60000123, Data: [8]byte{1, 2, 3}}}},
		&model.Msg0700{Header: model.GenMsgHeader(device, 0x0700, 4), AnswerSerial: 1, Command: 2, Data: []byte{0xaa}},
		&model.Msg0800{Header: model.GenMsgHeader(device, 0x0800, 5), MultiMediaID: 70, MultiMediaType: 0, MultiMediaContainer: 0, EventID: 1, LogicChannelID: 1},
		&model.Msg0801{Header: model.GenMsgHeader(device, 0x0801, 6), MultiMediaID: 71, MultiMediaType: 0, MultiMediaContainer: 0, EventID: 1, LogicChannelID: 1, GeoAlarmBody: make([]byte, 28), FragmentData: []byte{0xff, 0xd8, 0xff, 0xd9}},
	} {
		require.NoError(t, cli.SendWithError(msg))
	}
	deadline = time.Now().Add(3 * time.Second)
	for time.Now().Before(deadline) {
		if industry.Driver(phone) != nil && industry.CAN(phone) != nil && industry.RecordData(phone) != nil && industry.MediaEvent(phone) != nil && len(industry.MediaUploads(phone)) == 1 {
			break
		}
		time.Sleep(10 * time.Millisecond)
	}
	require.Equal(t, "模拟司机", industry.Driver(phone).Name)
	require.Equal(t, uint32(0x60000123), industry.CAN(phone).Items[0].CANID)
	require.Equal(t, []byte{0xaa}, industry.RecordData(phone).Data)
	require.Equal(t, uint32(71), industry.MediaUploads(phone)[0].MultiMediaID)
	session, err := storage.GetSession(device.SessionID)
	require.NoError(t, err)
	// Exercise media-layer retransmission separately from JT808's 0x8003
	// transport reassembly: the terminal simulator must replay the original
	// encoded media fragments requested by 0x8800.
	media := &model.Msg0801{Header: model.GenMsgHeader(device, 0x0801, 30), MultiMediaID: 72, GeoAlarmBody: make([]byte, 28), FragmentData: make([]byte, 1500)}
	for i := range media.FragmentData {
		media.FragmentData[i] = byte(i)
	}
	require.NoError(t, cli.SendFragments(media, 300))
	deadline = time.Now().Add(3 * time.Second)
	for time.Now().Before(deadline) {
		for _, upload := range industry.MediaUploads(phone) {
			if upload.MultiMediaID == 72 {
				goto mediaUploaded
			}
		}
		time.Sleep(10 * time.Millisecond)
	}
	t.Fatal("simulator upload was not received")
mediaUploaded:
	server.Send(session.ID, &model.Msg8800{Header: model.GenMsgHeader(device, 0x8800, 31), MultiMediaID: 72, RetransmitIDs: []uint16{1, 2, 3, 4, 5, 6}})
	deadline = time.Now().Add(3 * time.Second)
	for time.Now().Before(deadline) {
		count := 0
		for _, upload := range industry.MediaUploads(phone) {
			if upload.MultiMediaID == 72 {
				count++
			}
		}
		if count >= 2 {
			break
		}
		time.Sleep(10 * time.Millisecond)
	}
	countMedia := 0
	for _, upload := range industry.MediaUploads(phone) {
		if upload.MultiMediaID == 72 {
			countMedia++
		}
	}
	require.GreaterOrEqual(t, countMedia, 2, "0x8800 should trigger replay of cached media packet fragments")

	responseCh, cancel, err := storage.GetRegionResponseBroker().Register(phone, 1)
	require.NoError(t, err)
	defer cancel()
	server.Send(session.ID, &model.Msg8608{Header: model.GenMsgHeader(device, 0x8608, session.GetNextSerialNum()), QueryType: 1})
	select {
	case response := <-responseCh:
		require.Equal(t, uint32(0), response.Count)
	case <-time.After(2 * time.Second):
		t.Fatal("simulator did not answer 0x8608")
	}

	server.Send(session.ID, &model.MsgRawBody{Header: model.GenMsgHeader(device, 0x8702, session.GetNextSerialNum())})
	deadline = time.Now().Add(2 * time.Second)
	for time.Now().Before(deadline) {
		if report := industry.Driver(phone); report != nil && report.Name == "模拟驾驶员" {
			break
		}
		time.Sleep(10 * time.Millisecond)
	}
	require.Equal(t, "模拟驾驶员", industry.Driver(phone).Name, "simulator should answer driver query with 0x0702")

	server.Send(session.ID, &model.MsgRecordCommand{Header: model.GenMsgHeader(device, 0x8700, session.GetNextSerialNum()), Command: 3, Data: []byte{0x55}})
	deadline = time.Now().Add(2 * time.Second)
	for time.Now().Before(deadline) {
		if report := industry.RecordData(phone); report != nil && report.Command == 3 {
			break
		}
		time.Sleep(10 * time.Millisecond)
	}
	require.Equal(t, uint8(3), industry.RecordData(phone).Command, "simulator should answer 0x8700 with 0x0700")

	server.Send(session.ID, &model.Msg8801{Header: model.GenMsgHeader(device, 0x8801, session.GetNextSerialNum()), ChannelID: 1, Command: 1, Quality: 1})
	deadline = time.Now().Add(2 * time.Second)
	for time.Now().Before(deadline) {
		if industry.Capture(phone) != nil {
			break
		}
		time.Sleep(10 * time.Millisecond)
	}
	require.NotNil(t, industry.Capture(phone), "simulator should answer 0x8801 with 0x0805")
	server.Send(session.ID, &model.Msg8802{Header: model.GenMsgHeader(device, 0x8802, session.GetNextSerialNum()), MediaType: 0, ChannelID: 1, EventID: 1, StartTime: "000000000000", EndTime: "000000000000"})
	deadline = time.Now().Add(2 * time.Second)
	for time.Now().Before(deadline) {
		if industry.MediaSearch(phone) != nil {
			break
		}
		time.Sleep(10 * time.Millisecond)
	}
	searchResult := industry.MediaSearch(phone)
	require.NotNil(t, searchResult, "simulator should answer 0x8802 with 0x0802")
	require.Len(t, searchResult.Items, 1, "simulator search catalog should return a media result")
	require.Equal(t, uint32(71), searchResult.Items[0].MediaID)
}

func TestSimulatorMediaUploadCommands(t *testing.T) {
	server := NewTCPServer()
	require.NoError(t, server.Listen("127.0.0.1:0"))
	go server.Start()
	t.Cleanup(server.Stop)
	cli := client.NewTCPClient()
	require.NoError(t, cli.Dial(server.listener.Addr().String()))
	t.Cleanup(cli.Stop)
	go cli.Start()
	const phone = "188765433220"
	registration := &model.Msg0100{Header: &model.MsgHeader{MsgID: 0x0100, Attr: &model.MsgBodyAttr{VersionDesc: model.Version2013}, PhoneNumber: phone}, ProvinceID: 11, CityID: 1101, ManufacturerID: "ABCDE", DeviceMode: "JT808-MEDIA-SIM", DeviceID: "MEDIA-01", PlateColor: 1, PlateNumber: "京A88030"}
	require.NoError(t, cli.SendWithError(registration))
	var device *model.Device
	deadline := time.Now().Add(3 * time.Second)
	for time.Now().Before(deadline) {
		device, _ = storage.GetDeviceCache().GetDeviceByPhone(phone)
		if device != nil && device.Status == model.DeviceStatusOnline {
			break
		}
		time.Sleep(10 * time.Millisecond)
	}
	require.NotNil(t, device)
	require.Equal(t, model.DeviceStatusOnline, device.Status)
	t.Cleanup(func() { storage.GetDeviceCache().DelDeviceByPhone(phone); protocol.NewKeepaliveTimer().Cancel(phone) })

	industry := storage.GetIndustryCache()
	session, err := storage.GetSession(device.SessionID)
	require.NoError(t, err)

	// Seed the simulator's stored-media catalog by uploading one image item.
	require.NoError(t, cli.SendWithError(&model.Msg0801{Header: model.GenMsgHeader(device, 0x0801, 6), MultiMediaID: 71, MultiMediaType: 0, MultiMediaContainer: 0, EventID: 1, LogicChannelID: 1, GeoAlarmBody: make([]byte, 28), FragmentData: []byte{0xff, 0xd8, 0xff, 0xd9}}))
	deadline = time.Now().Add(2 * time.Second)
	for time.Now().Before(deadline) {
		if countMediaID(industry.MediaUploads(phone), 71) >= 1 {
			break
		}
		time.Sleep(10 * time.Millisecond)
	}
	require.Equal(t, 1, countMediaID(industry.MediaUploads(phone), 71), "seed media should be uploaded once")

	// 0x8803: upload all stored images on channel 1 event 1 -> simulator replays media 71.
	server.Send(session.ID, &model.Msg8803{Header: model.GenMsgHeader(device, 0x8803, session.GetNextSerialNum()), MediaType: 0, ChannelID: 1, EventID: 1, StartTime: "000000000000", EndTime: "000000000000", DeleteFlag: 0})
	deadline = time.Now().Add(3 * time.Second)
	for time.Now().Before(deadline) {
		if countMediaID(industry.MediaUploads(phone), 71) >= 2 {
			break
		}
		time.Sleep(10 * time.Millisecond)
	}
	require.GreaterOrEqual(t, countMediaID(industry.MediaUploads(phone), 71), 2, "0x8803 should trigger re-upload of matching stored media")

	// 0x8805: upload a single stored media item by id -> simulator replays media 71 again.
	server.Send(session.ID, &model.Msg8805{Header: model.GenMsgHeader(device, 0x8805, session.GetNextSerialNum()), MediaID: 71, DeleteFlag: 0})
	deadline = time.Now().Add(3 * time.Second)
	for time.Now().Before(deadline) {
		if countMediaID(industry.MediaUploads(phone), 71) >= 3 {
			break
		}
		time.Sleep(10 * time.Millisecond)
	}
	require.GreaterOrEqual(t, countMediaID(industry.MediaUploads(phone), 71), 3, "0x8805 should trigger re-upload of the requested media id")

	// 0x8805 with a non-existent media id -> simulator must not upload anything.
	before := len(industry.MediaUploads(phone))
	server.Send(session.ID, &model.Msg8805{Header: model.GenMsgHeader(device, 0x8805, session.GetNextSerialNum()), MediaID: 999999, DeleteFlag: 0})
	time.Sleep(500 * time.Millisecond)
	require.Equal(t, before, len(industry.MediaUploads(phone)), "0x8805 with unknown media id should not trigger any upload")

	// 0x8804 start recording -> simulator emits audio frames (media type 1).
	server.Send(session.ID, &model.Msg8804{Header: model.GenMsgHeader(device, 0x8804, session.GetNextSerialNum()), Command: 1, Duration: 10, Save: 1, SampleRate: 2})
	deadline = time.Now().Add(3 * time.Second)
	for time.Now().Before(deadline) {
		if countMediaType(industry.MediaUploads(phone), 1) >= 3 {
			break
		}
		time.Sleep(10 * time.Millisecond)
	}
	require.GreaterOrEqual(t, countMediaType(industry.MediaUploads(phone), 1), 3, "0x8804 start should upload audio frames")
	require.True(t, industry.IsRecording(phone), "recording flag should be active after start command")

	// 0x8804 stop recording -> simulator clears recording flag.
	server.Send(session.ID, &model.Msg8804{Header: model.GenMsgHeader(device, 0x8804, session.GetNextSerialNum()), Command: 0, Duration: 0, Save: 0, SampleRate: 0})
	deadline = time.Now().Add(1 * time.Second)
	for time.Now().Before(deadline) {
		if !industry.IsRecording(phone) {
			break
		}
		time.Sleep(10 * time.Millisecond)
	}
	require.False(t, industry.IsRecording(phone), "recording flag should be cleared after stop command")
}

// buildGeoAlarmBody builds a 28-byte 0x0200 base body with the given BCD time.
func buildGeoAlarmBody(timeBCD string) []byte {
	body := make([]byte, 28)
	// AlarmSign, StatusSign, Latitude, Longitude, Altitude, Speed, Direction are zero.
	// Time starts at byte 22 (6 BCD bytes).
	for i := 0; i < 6 && i*2 < len(timeBCD); i++ {
		high := timeBCD[i*2] - '0'
		low := byte(0)
		if i*2+1 < len(timeBCD) {
			low = timeBCD[i*2+1] - '0'
		}
		body[22+i] = high<<4 | low
	}
	return body
}

// TestSimulatorMediaResourceListQuery seeds media uploads of varying types,
// channels and times, then verifies 0x9205 queries return correctly filtered
// 0x1205 resource lists backed by the media catalog.
func TestSimulatorMediaResourceListQuery(t *testing.T) {
	server := NewTCPServer()
	require.NoError(t, server.Listen("127.0.0.1:0"))
	go server.Start()
	t.Cleanup(server.Stop)
	cli := client.NewTCPClient()
	require.NoError(t, cli.Dial(server.listener.Addr().String()))
	t.Cleanup(cli.Stop)
	go cli.Start()
	const phone = "188765433229"
	registration := &model.Msg0100{Header: &model.MsgHeader{MsgID: 0x0100, Attr: &model.MsgBodyAttr{VersionDesc: model.Version2013}, PhoneNumber: phone}, ProvinceID: 11, CityID: 1101, ManufacturerID: "ABCDE", DeviceMode: "JT808-MEDIA-SIM", DeviceID: "MEDIA-02", PlateColor: 1, PlateNumber: "京A88031"}
	require.NoError(t, cli.SendWithError(registration))
	var device *model.Device
	deadline := time.Now().Add(3 * time.Second)
	for time.Now().Before(deadline) {
		device, _ = storage.GetDeviceCache().GetDeviceByPhone(phone)
		if device != nil && device.Status == model.DeviceStatusOnline {
			break
		}
		time.Sleep(10 * time.Millisecond)
	}
	require.NotNil(t, device)
	require.Equal(t, model.DeviceStatusOnline, device.Status)
	t.Cleanup(func() { storage.GetDeviceCache().DelDeviceByPhone(phone); protocol.NewKeepaliveTimer().Cancel(phone) })

	industry := storage.GetIndustryCache()
	session, err := storage.GetSession(device.SessionID)
	require.NoError(t, err)

	// Empty catalog query returns zero items.
	querySerial := session.GetNextSerialNum()
	server.Send(session.ID, &model.Msg9205{Header: model.GenMsgHeader(device, 0x9205, querySerial), DeviceMediaQuery: model.DeviceMediaQuery{LogicChannelID: 0, MediaType: 0, StreamType: 0, StorageType: 0}})
	deadline = time.Now().Add(2 * time.Second)
	for time.Now().Before(deadline) {
		if industry.MediaResourceList(phone) != nil {
			break
		}
		time.Sleep(10 * time.Millisecond)
	}
	emptyResult := industry.MediaResourceList(phone)
	require.NotNil(t, emptyResult, "simulator should answer 0x9205 with 0x1205 even for empty catalog")
	require.Equal(t, uint16(querySerial), emptyResult.AnswerSerialNumber, "answer serial must match query serial")
	require.Equal(t, uint32(0), emptyResult.MediaCount, "empty catalog should return zero items")
	require.Len(t, emptyResult.Items, 0)

	// Seed three media items: image on channel 1, audio on channel 2, video on channel 1.
	seedItems := []struct {
		id       uint32
		typ      uint8
		channel  uint8
		timeBCD  string
		dataSize int
	}{
		{101, 0, 1, "240101120000", 100}, // image, channel 1, noon Jan 1
		{102, 1, 2, "240101130000", 200}, // audio, channel 2, 1pm Jan 1
		{103, 2, 1, "240102120000", 300}, // video, channel 1, noon Jan 2
	}
	for _, s := range seedItems {
		require.NoError(t, cli.SendWithError(&model.Msg0801{
			Header:              model.GenMsgHeader(device, 0x0801, uint16(100+s.id)),
			MultiMediaID:        s.id,
			MultiMediaType:      s.typ,
			MultiMediaContainer: 0,
			EventID:             1,
			LogicChannelID:      s.channel,
			GeoAlarmBody:        buildGeoAlarmBody(s.timeBCD),
			FragmentData:        make([]byte, s.dataSize),
		}))
	}
	deadline = time.Now().Add(2 * time.Second)
	for time.Now().Before(deadline) {
		if len(industry.MediaUploads(phone)) >= 3 {
			break
		}
		time.Sleep(10 * time.Millisecond)
	}
	require.Len(t, industry.MediaUploads(phone), 3, "three media items should be uploaded")

	// Query all (mediaType 0) -> all 3 items.
	server.Send(session.ID, &model.Msg9205{Header: model.GenMsgHeader(device, 0x9205, session.GetNextSerialNum()), DeviceMediaQuery: model.DeviceMediaQuery{LogicChannelID: 0, MediaType: 0}})
	deadline = time.Now().Add(2 * time.Second)
	for time.Now().Before(deadline) {
		if r := industry.MediaResourceList(phone); r != nil && r.MediaCount == 3 {
			break
		}
		time.Sleep(10 * time.Millisecond)
	}
	allResult := industry.MediaResourceList(phone)
	require.Equal(t, uint32(3), allResult.MediaCount, "mediaType=0 should return all items")
	require.Len(t, allResult.Items, 3)

	// Filter by channel 1 -> items 101 and 103.
	server.Send(session.ID, &model.Msg9205{Header: model.GenMsgHeader(device, 0x9205, session.GetNextSerialNum()), DeviceMediaQuery: model.DeviceMediaQuery{LogicChannelID: 1, MediaType: 0}})
	deadline = time.Now().Add(2 * time.Second)
	for time.Now().Before(deadline) {
		if r := industry.MediaResourceList(phone); r != nil && r.MediaCount == 2 {
			break
		}
		time.Sleep(10 * time.Millisecond)
	}
	chResult := industry.MediaResourceList(phone)
	require.Equal(t, uint32(2), chResult.MediaCount, "channel 1 filter should return 2 items")
	for _, item := range chResult.Items {
		require.Equal(t, uint8(1), item.LogicChannelID)
	}

	// Filter by media type 1 (audio) -> only item 102.
	server.Send(session.ID, &model.Msg9205{Header: model.GenMsgHeader(device, 0x9205, session.GetNextSerialNum()), DeviceMediaQuery: model.DeviceMediaQuery{LogicChannelID: 0, MediaType: 1}})
	deadline = time.Now().Add(2 * time.Second)
	for time.Now().Before(deadline) {
		if r := industry.MediaResourceList(phone); r != nil && r.MediaCount == 1 {
			break
		}
		time.Sleep(10 * time.Millisecond)
	}
	audioResult := industry.MediaResourceList(phone)
	require.Equal(t, uint32(1), audioResult.MediaCount, "mediaType=1 (audio) should return 1 item")
	require.Equal(t, uint8(1), audioResult.Items[0].MediaType)
	require.Equal(t, uint32(200), audioResult.Items[0].Size, "item size should reflect fragment data length")

	// Filter by time range Jan 1 00:00 to Jan 1 23:59 -> items 101 and 102.
	start := time.Date(2024, 1, 1, 0, 0, 0, 0, time.Local)
	end := time.Date(2024, 1, 1, 23, 59, 59, 0, time.Local)
	server.Send(session.ID, &model.Msg9205{Header: model.GenMsgHeader(device, 0x9205, session.GetNextSerialNum()), DeviceMediaQuery: model.DeviceMediaQuery{LogicChannelID: 0, MediaType: 0, StartTime: &start, EndTime: &end}})
	deadline = time.Now().Add(2 * time.Second)
	for time.Now().Before(deadline) {
		if r := industry.MediaResourceList(phone); r != nil && r.MediaCount == 2 {
			break
		}
		time.Sleep(10 * time.Millisecond)
	}
	timeResult := industry.MediaResourceList(phone)
	require.Equal(t, uint32(2), timeResult.MediaCount, "Jan 1 time range should return 2 items")
}

// TestSimulatorTextAndEventMessages exercises 0x8300 text issue, 0x8301 event
// set/delete/append semantics, and 0x0301 event report storage.
func TestSimulatorTextAndEventMessages(t *testing.T) {
	server := NewTCPServer()
	require.NoError(t, server.Listen("127.0.0.1:0"))
	go server.Start()
	t.Cleanup(server.Stop)
	cli := client.NewTCPClient()
	require.NoError(t, cli.Dial(server.listener.Addr().String()))
	t.Cleanup(cli.Stop)
	go cli.Start()
	const phone = "188765433228"
	registration := &model.Msg0100{Header: &model.MsgHeader{MsgID: 0x0100, Attr: &model.MsgBodyAttr{VersionDesc: model.Version2013}, PhoneNumber: phone}, ProvinceID: 11, CityID: 1101, ManufacturerID: "ABCDE", DeviceMode: "JT808-EVENT-SIM", DeviceID: "EVENT-01", PlateColor: 1, PlateNumber: "京A88032"}
	require.NoError(t, cli.SendWithError(registration))
	var device *model.Device
	deadline := time.Now().Add(3 * time.Second)
	for time.Now().Before(deadline) {
		device, _ = storage.GetDeviceCache().GetDeviceByPhone(phone)
		if device != nil && device.Status == model.DeviceStatusOnline {
			break
		}
		time.Sleep(10 * time.Millisecond)
	}
	require.NotNil(t, device)
	require.Equal(t, model.DeviceStatusOnline, device.Status)
	t.Cleanup(func() { storage.GetDeviceCache().DelDeviceByPhone(phone); protocol.NewKeepaliveTimer().Cancel(phone) })

	industry := storage.GetIndustryCache()
	session, err := storage.GetSession(device.SessionID)
	require.NoError(t, err)

	// 0x8300 text issue -> terminal acknowledges with 0x0001 (no observable side effect).
	server.Send(session.ID, &model.Msg8300{Header: model.GenMsgHeader(device, 0x8300, session.GetNextSerialNum()), Flag: 1, TextType: 1, Text: "hello终端"})
	time.Sleep(300 * time.Millisecond)

	// 0x8301 set type 1 (replace) with two events.
	server.Send(session.ID, &model.Msg8301{
		Header:  model.GenMsgHeader(device, 0x8301, session.GetNextSerialNum()),
		SetType: 1,
		Items: []model.EventItem{
			{ID: 1, Content: "紧急报警"},
			{ID: 2, Content: "疲劳驾驶"},
		},
	})
	deadline = time.Now().Add(2 * time.Second)
	for time.Now().Before(deadline) {
		if len(industry.Events(phone)) == 2 {
			break
		}
		time.Sleep(10 * time.Millisecond)
	}
	events := industry.Events(phone)
	require.Len(t, events, 2, "set type 1 should store two events")
	eventMap := map[uint8]string{}
	for _, e := range events {
		eventMap[e.ID] = e.Content
	}
	require.Equal(t, "紧急报警", eventMap[1])
	require.Equal(t, "疲劳驾驶", eventMap[2])

	// 0x8301 set type 2 (append) adds a third event.
	server.Send(session.ID, &model.Msg8301{
		Header:  model.GenMsgHeader(device, 0x8301, session.GetNextSerialNum()),
		SetType: 2,
		Items:   []model.EventItem{{ID: 3, Content: "超速"}},
	})
	deadline = time.Now().Add(2 * time.Second)
	for time.Now().Before(deadline) {
		if len(industry.Events(phone)) == 3 {
			break
		}
		time.Sleep(10 * time.Millisecond)
	}
	require.Len(t, industry.Events(phone), 3, "append should add one event")

	// 0x8301 set type 0 (delete all) clears the catalog.
	server.Send(session.ID, &model.Msg8301{Header: model.GenMsgHeader(device, 0x8301, session.GetNextSerialNum()), SetType: 0})
	deadline = time.Now().Add(2 * time.Second)
	for time.Now().Before(deadline) {
		if len(industry.Events(phone)) == 0 {
			break
		}
		time.Sleep(10 * time.Millisecond)
	}
	require.Len(t, industry.Events(phone), 0, "delete all should clear events")

	// 0x0301 event report from terminal -> platform stores it.
	require.NoError(t, cli.SendWithError(&model.Msg0301{Header: model.GenMsgHeader(device, 0x0301, 5), EventID: 7}))
	deadline = time.Now().Add(2 * time.Second)
	for time.Now().Before(deadline) {
		if industry.EventReport(phone) != nil {
			break
		}
		time.Sleep(10 * time.Millisecond)
	}
	report := industry.EventReport(phone)
	require.NotNil(t, report, "platform should store 0x0301 event report")
	require.Equal(t, uint8(7), report.EventID)
}

// TestSimulatorVehicleControl verifies 0x8500 commands store control flags and
// the 0x0500 response echoes them back to the platform.
func TestSimulatorVehicleControl(t *testing.T) {
	server := NewTCPServer()
	require.NoError(t, server.Listen("127.0.0.1:0"))
	go server.Start()
	t.Cleanup(server.Stop)
	cli := client.NewTCPClient()
	require.NoError(t, cli.Dial(server.listener.Addr().String()))
	t.Cleanup(cli.Stop)
	go cli.Start()
	const phone = "188765433227"
	registration := &model.Msg0100{Header: &model.MsgHeader{MsgID: 0x0100, Attr: &model.MsgBodyAttr{VersionDesc: model.Version2013}, PhoneNumber: phone}, ProvinceID: 11, CityID: 1101, ManufacturerID: "ABCDE", DeviceMode: "JT808-VEH-SIM", DeviceID: "VEH-01", PlateColor: 1, PlateNumber: "京A88033"}
	require.NoError(t, cli.SendWithError(registration))
	var device *model.Device
	deadline := time.Now().Add(3 * time.Second)
	for time.Now().Before(deadline) {
		device, _ = storage.GetDeviceCache().GetDeviceByPhone(phone)
		if device != nil && device.Status == model.DeviceStatusOnline {
			break
		}
		time.Sleep(10 * time.Millisecond)
	}
	require.NotNil(t, device)
	require.Equal(t, model.DeviceStatusOnline, device.Status)
	t.Cleanup(func() { storage.GetDeviceCache().DelDeviceByPhone(phone); protocol.NewKeepaliveTimer().Cancel(phone) })

	industry := storage.GetIndustryCache()
	session, err := storage.GetSession(device.SessionID)
	require.NoError(t, err)

	// 0x8500 lock door (bit0=1) -> terminal stores flags and echoes via 0x0500.
	const lockFlags uint16 = 0x0001
	server.Send(session.ID, &model.Msg8500{Header: model.GenMsgHeader(device, 0x8500, session.GetNextSerialNum()), ControlFlags: lockFlags})
	deadline = time.Now().Add(2 * time.Second)
	for time.Now().Before(deadline) {
		if industry.VehicleControl(phone) == lockFlags {
			break
		}
		time.Sleep(10 * time.Millisecond)
	}
	require.Equal(t, lockFlags, industry.VehicleControl(phone), "0x8500 should store control flags")

	// 0x8500 unlock door (bit0=0) -> flags update.
	const unlockFlags uint16 = 0x0000
	server.Send(session.ID, &model.Msg8500{Header: model.GenMsgHeader(device, 0x8500, session.GetNextSerialNum()), ControlFlags: unlockFlags})
	deadline = time.Now().Add(2 * time.Second)
	for time.Now().Before(deadline) {
		if industry.VehicleControl(phone) == unlockFlags {
			break
		}
		time.Sleep(10 * time.Millisecond)
	}
	require.Equal(t, unlockFlags, industry.VehicleControl(phone), "second 0x8500 should update control flags")
}

// TestSimulatorQuestionMenuInfoService verifies 0x8302/0x0302 question flow,
// 0x8303/0x0303 info menu flow, and 0x8304 info service are decoded and cached.
func TestSimulatorQuestionMenuInfoService(t *testing.T) {
	server := NewTCPServer()
	require.NoError(t, server.Listen("127.0.0.1:0"))
	go server.Start()
	t.Cleanup(server.Stop)
	cli := client.NewTCPClient()
	require.NoError(t, cli.Dial(server.listener.Addr().String()))
	t.Cleanup(cli.Stop)
	go cli.Start()
	const phone = "188765433225"
	registration := &model.Msg0100{Header: &model.MsgHeader{MsgID: 0x0100, Attr: &model.MsgBodyAttr{VersionDesc: model.Version2013}, PhoneNumber: phone}, ProvinceID: 11, CityID: 1101, ManufacturerID: "ABCDE", DeviceMode: "JT808-Q-SIM", DeviceID: "Q-01", PlateColor: 1, PlateNumber: "京A88035"}
	require.NoError(t, cli.SendWithError(registration))
	var device *model.Device
	deadline := time.Now().Add(3 * time.Second)
	for time.Now().Before(deadline) {
		device, _ = storage.GetDeviceCache().GetDeviceByPhone(phone)
		if device != nil && device.Status == model.DeviceStatusOnline {
			break
		}
		time.Sleep(10 * time.Millisecond)
	}
	require.NotNil(t, device)
	require.Equal(t, model.DeviceStatusOnline, device.Status)
	t.Cleanup(func() { storage.GetDeviceCache().DelDeviceByPhone(phone); protocol.NewKeepaliveTimer().Cancel(phone) })

	industry := storage.GetIndustryCache()
	session, err := storage.GetSession(device.SessionID)
	require.NoError(t, err)

	// 0x8302 question issue with two candidates.
	server.Send(session.ID, &model.Msg8302{
		Header:   model.GenMsgHeader(device, 0x8302, session.GetNextSerialNum()),
		Flag:     1,
		Question: "是否继续运输",
		Candidates: []model.QuestionCandidate{
			{ID: 101, Content: "继续"},
			{ID: 102, Content: "停止"},
		},
	})
	deadline = time.Now().Add(2 * time.Second)
	for time.Now().Before(deadline) {
		if q := industry.Question(phone); q != nil {
			break
		}
		time.Sleep(10 * time.Millisecond)
	}
	q := industry.Question(phone)
	require.NotNil(t, q, "0x8302 should store the question")
	require.Equal(t, "是否继续运输", q.Question)
	require.Len(t, q.Candidates, 2)
	candMap := map[uint32]string{}
	for _, c := range q.Candidates {
		candMap[c.ID] = c.Content
	}
	require.Equal(t, "继续", candMap[101])
	require.Equal(t, "停止", candMap[102])

	// 0x0302 question response from terminal.
	require.NoError(t, cli.SendWithError(&model.Msg0302{Header: model.GenMsgHeader(device, 0x0302, 6), AnswerSerial: session.GetNextSerialNum() - 1, AnswerID: 101}))
	deadline = time.Now().Add(2 * time.Second)
	for time.Now().Before(deadline) {
		if industry.QuestionResponse(phone) != nil {
			break
		}
		time.Sleep(10 * time.Millisecond)
	}
	resp := industry.QuestionResponse(phone)
	require.NotNil(t, resp, "0x0302 should be stored")
	require.Equal(t, uint32(101), resp.AnswerID)

	// 0x8303 set type 1 (replace) with two menu items.
	server.Send(session.ID, &model.Msg8303{
		Header:  model.GenMsgHeader(device, 0x8303, session.GetNextSerialNum()),
		SetType: 1,
		Items: []model.InfoMenuItem{
			{ID: 1, Content: "路况播报"},
			{ID: 2, Content: "天气信息"},
		},
	})
	deadline = time.Now().Add(2 * time.Second)
	for time.Now().Before(deadline) {
		if len(industry.InfoMenu(phone)) == 2 {
			break
		}
		time.Sleep(10 * time.Millisecond)
	}
	menus := industry.InfoMenu(phone)
	require.Len(t, menus, 2, "set type 1 should store two menu items")
	menuMap := map[uint32]string{}
	for _, m := range menus {
		menuMap[m.ID] = m.Content
	}
	require.Equal(t, "路况播报", menuMap[1])
	require.Equal(t, "天气信息", menuMap[2])

	// 0x8303 set type 0 (delete all) clears the menu.
	server.Send(session.ID, &model.Msg8303{Header: model.GenMsgHeader(device, 0x8303, session.GetNextSerialNum()), SetType: 0})
	deadline = time.Now().Add(2 * time.Second)
	for time.Now().Before(deadline) {
		if len(industry.InfoMenu(phone)) == 0 {
			break
		}
		time.Sleep(10 * time.Millisecond)
	}
	require.Len(t, industry.InfoMenu(phone), 0, "delete all should clear menu")

	// 0x0303 info on-demand from terminal.
	require.NoError(t, cli.SendWithError(&model.Msg0303{Header: model.GenMsgHeader(device, 0x0303, 7), Action: 1, MenuID: 1}))
	deadline = time.Now().Add(2 * time.Second)
	for time.Now().Before(deadline) {
		if industry.MenuAction(phone) != nil {
			break
		}
		time.Sleep(10 * time.Millisecond)
	}
	action := industry.MenuAction(phone)
	require.NotNil(t, action, "0x0303 should be stored")
	require.Equal(t, uint8(1), action.Action)
	require.Equal(t, uint32(1), action.MenuID)

	// 0x8304 info service.
	server.Send(session.ID, &model.Msg8304{Header: model.GenMsgHeader(device, 0x8304, session.GetNextSerialNum()), InfoType: 2, Content: "前方服务区2公里"})
	deadline = time.Now().Add(2 * time.Second)
	for time.Now().Before(deadline) {
		if industry.InfoService(phone) != nil {
			break
		}
		time.Sleep(10 * time.Millisecond)
	}
	info := industry.InfoService(phone)
	require.NotNil(t, info, "0x8304 should be stored")
	require.Equal(t, uint8(2), info.InfoType)
	require.Equal(t, "前方服务区2公里", info.Content)
}

// TestSimulatorPhonebookAndUpgrade verifies 0x8400 call callback, 0x8401
// phonebook setting, 0x8108 terminal upgrade, and 0x0108 upgrade result are
// decoded and cached.
func TestSimulatorPhonebookAndUpgrade(t *testing.T) {
	server := NewTCPServer()
	require.NoError(t, server.Listen("127.0.0.1:0"))
	go server.Start()
	t.Cleanup(server.Stop)
	cli := client.NewTCPClient()
	require.NoError(t, cli.Dial(server.listener.Addr().String()))
	t.Cleanup(cli.Stop)
	go cli.Start()
	const phone = "188765433224"
	registration := &model.Msg0100{Header: &model.MsgHeader{MsgID: 0x0100, Attr: &model.MsgBodyAttr{VersionDesc: model.Version2013}, PhoneNumber: phone}, ProvinceID: 11, CityID: 1101, ManufacturerID: "ABCDE", DeviceMode: "JT808-PB-SIM", DeviceID: "PB-01", PlateColor: 1, PlateNumber: "京A88036"}
	require.NoError(t, cli.SendWithError(registration))
	var device *model.Device
	deadline := time.Now().Add(3 * time.Second)
	for time.Now().Before(deadline) {
		device, _ = storage.GetDeviceCache().GetDeviceByPhone(phone)
		if device != nil && device.Status == model.DeviceStatusOnline {
			break
		}
		time.Sleep(10 * time.Millisecond)
	}
	require.NotNil(t, device)
	require.Equal(t, model.DeviceStatusOnline, device.Status)
	t.Cleanup(func() { storage.GetDeviceCache().DelDeviceByPhone(phone); protocol.NewKeepaliveTimer().Cancel(phone) })

	industry := storage.GetIndustryCache()
	session, err := storage.GetSession(device.SessionID)
	require.NoError(t, err)

	// 0x8400 call callback.
	server.Send(session.ID, &model.Msg8400{Header: model.GenMsgHeader(device, 0x8400, session.GetNextSerialNum()), Flag: 0, Phone: "13800138000"})
	deadline = time.Now().Add(2 * time.Second)
	for time.Now().Before(deadline) {
		if industry.CallBack(phone) != nil {
			break
		}
		time.Sleep(10 * time.Millisecond)
	}
	cb := industry.CallBack(phone)
	require.NotNil(t, cb, "0x8400 should be stored")
	require.Equal(t, uint8(0), cb.Flag)
	require.Equal(t, "13800138000", cb.Phone)

	// 0x8401 set type 1 (replace) with two contacts.
	server.Send(session.ID, &model.Msg8401{
		Header:  model.GenMsgHeader(device, 0x8401, session.GetNextSerialNum()),
		SetType: 1,
		Items: []model.PhonebookContact{
			{Flag: 3, Phone: "13900139000", Name: "调度"},
			{Flag: 1, Phone: "13700137000", Name: "家人"},
		},
	})
	deadline = time.Now().Add(2 * time.Second)
	for time.Now().Before(deadline) {
		if len(industry.Phonebook(phone)) == 2 {
			break
		}
		time.Sleep(10 * time.Millisecond)
	}
	contacts := industry.Phonebook(phone)
	require.Len(t, contacts, 2, "set type 1 should store two contacts")
	contactMap := map[string]string{}
	for _, c := range contacts {
		contactMap[c.Phone] = c.Name
	}
	require.Equal(t, "调度", contactMap["13900139000"])
	require.Equal(t, "家人", contactMap["13700137000"])

	// 0x8401 set type 0 (delete all) clears the phonebook.
	server.Send(session.ID, &model.Msg8401{Header: model.GenMsgHeader(device, 0x8401, session.GetNextSerialNum()), SetType: 0})
	deadline = time.Now().Add(2 * time.Second)
	for time.Now().Before(deadline) {
		if len(industry.Phonebook(phone)) == 0 {
			break
		}
		time.Sleep(10 * time.Millisecond)
	}
	require.Len(t, industry.Phonebook(phone), 0, "delete all should clear phonebook")

	// 0x8108 terminal upgrade command.
	upgradeData := []byte{0x01, 0x02, 0x03, 0x04}
	server.Send(session.ID, &model.Msg8108{
		Header:      model.GenMsgHeader(device, 0x8108, session.GetNextSerialNum()),
		UpgradeType: 0,
		MakerID:     "JT808",
		Version:     "v1.2.3",
		Data:        upgradeData,
	})
	deadline = time.Now().Add(2 * time.Second)
	for time.Now().Before(deadline) {
		if industry.Upgrade(phone) != nil {
			break
		}
		time.Sleep(10 * time.Millisecond)
	}
	up := industry.Upgrade(phone)
	require.NotNil(t, up, "0x8108 should be stored")
	require.Equal(t, uint8(0), up.UpgradeType)
	require.Equal(t, "JT808", up.MakerID)
	require.Equal(t, "v1.2.3", up.Version)
	require.Equal(t, upgradeData, up.Data)

	// 0x0108 upgrade result notification from terminal.
	require.NoError(t, cli.SendWithError(&model.Msg0108{Header: model.GenMsgHeader(device, 0x0108, 8), UpgradeType: 0, Result: 0}))
	deadline = time.Now().Add(2 * time.Second)
	for time.Now().Before(deadline) {
		if industry.UpgradeResult(phone) != nil {
			break
		}
		time.Sleep(10 * time.Millisecond)
	}
	result := industry.UpgradeResult(phone)
	require.NotNil(t, result, "0x0108 should be stored")
	require.Equal(t, uint8(0), result.UpgradeType)
	require.Equal(t, uint8(0), result.Result)
}

// TestSimulatorTransparentAndCompressed verifies 0x8900 downlink transparent,
// 0x0900 uplink transparent, and 0x0901 compressed report are decoded and
// cached (with zlib decompression for 0x0901).
func TestSimulatorTransparentAndCompressed(t *testing.T) {
	server := NewTCPServer()
	require.NoError(t, server.Listen("127.0.0.1:0"))
	go server.Start()
	t.Cleanup(server.Stop)
	cli := client.NewTCPClient()
	require.NoError(t, cli.Dial(server.listener.Addr().String()))
	t.Cleanup(cli.Stop)
	go cli.Start()
	const phone = "188765433223"
	registration := &model.Msg0100{Header: &model.MsgHeader{MsgID: 0x0100, Attr: &model.MsgBodyAttr{VersionDesc: model.Version2013}, PhoneNumber: phone}, ProvinceID: 11, CityID: 1101, ManufacturerID: "ABCDE", DeviceMode: "JT808-TT-SIM", DeviceID: "TT-01", PlateColor: 1, PlateNumber: "京A88037"}
	require.NoError(t, cli.SendWithError(registration))
	var device *model.Device
	deadline := time.Now().Add(3 * time.Second)
	for time.Now().Before(deadline) {
		device, _ = storage.GetDeviceCache().GetDeviceByPhone(phone)
		if device != nil && device.Status == model.DeviceStatusOnline {
			break
		}
		time.Sleep(10 * time.Millisecond)
	}
	require.NotNil(t, device)
	require.Equal(t, model.DeviceStatusOnline, device.Status)
	t.Cleanup(func() { storage.GetDeviceCache().DelDeviceByPhone(phone); protocol.NewKeepaliveTimer().Cancel(phone) })

	industry := storage.GetIndustryCache()
	session, err := storage.GetSession(device.SessionID)
	require.NoError(t, err)

	// 0x8900 downlink transparent transmission.
	payload := []byte{0xAA, 0xBB, 0xCC, 0xDD}
	server.Send(session.ID, &model.Msg8900{Header: model.GenMsgHeader(device, 0x8900, session.GetNextSerialNum()), Type: 0x0001, Data: payload})
	deadline = time.Now().Add(2 * time.Second)
	for time.Now().Before(deadline) {
		if industry.TransparentDown(phone) != nil {
			break
		}
		time.Sleep(10 * time.Millisecond)
	}
	down := industry.TransparentDown(phone)
	require.NotNil(t, down, "0x8900 should be stored")
	require.Equal(t, uint32(0x0001), down.Type)
	require.Equal(t, payload, down.Data)

	// 0x0900 uplink transparent transmission from terminal.
	require.NoError(t, cli.SendWithError(&model.Msg0900{Header: model.GenMsgHeader(device, 0x0900, 9), Type: 0x0002, Data: []byte{0x11, 0x22}}))
	deadline = time.Now().Add(2 * time.Second)
	for time.Now().Before(deadline) {
		if industry.TransparentUp(phone) != nil {
			break
		}
		time.Sleep(10 * time.Millisecond)
	}
	up := industry.TransparentUp(phone)
	require.NotNil(t, up, "0x0900 should be stored")
	require.Equal(t, uint32(0x0002), up.Type)
	require.Equal(t, []byte{0x11, 0x22}, up.Data)

	// 0x0901 compressed data report from terminal.
	raw := []byte("compressed payload for testing")
	var buf bytes.Buffer
	zw := zlib.NewWriter(&buf)
	_, err = zw.Write(raw)
	require.NoError(t, err)
	require.NoError(t, zw.Close())
	compressed := buf.Bytes()
	require.NoError(t, cli.SendWithError(&model.Msg0901{Header: model.GenMsgHeader(device, 0x0901, 10), Data: compressed}))
	deadline = time.Now().Add(2 * time.Second)
	for time.Now().Before(deadline) {
		if industry.Compressed(phone) != nil {
			break
		}
		time.Sleep(10 * time.Millisecond)
	}
	cmp := industry.Compressed(phone)
	require.NotNil(t, cmp, "0x0901 should be stored")
	require.Equal(t, compressed, cmp.Data)
	require.Equal(t, raw, cmp.Decompressed, "0x0901 should decompress to original payload")
}

// TestSimulatorWaybillAndBatchLocation verifies 0x0701 electronic waybill and
// 0x0704 batch positioning uploads are decoded and cached.
func TestSimulatorWaybillAndBatchLocation(t *testing.T) {
	server := NewTCPServer()
	require.NoError(t, server.Listen("127.0.0.1:0"))
	go server.Start()
	t.Cleanup(server.Stop)
	cli := client.NewTCPClient()
	require.NoError(t, cli.Dial(server.listener.Addr().String()))
	t.Cleanup(cli.Stop)
	go cli.Start()
	const phone = "188765433226"
	registration := &model.Msg0100{Header: &model.MsgHeader{MsgID: 0x0100, Attr: &model.MsgBodyAttr{VersionDesc: model.Version2013}, PhoneNumber: phone}, ProvinceID: 11, CityID: 1101, ManufacturerID: "ABCDE", DeviceMode: "JT808-WB-SIM", DeviceID: "WB-01", PlateColor: 1, PlateNumber: "京A88034"}
	require.NoError(t, cli.SendWithError(registration))
	var device *model.Device
	deadline := time.Now().Add(3 * time.Second)
	for time.Now().Before(deadline) {
		device, _ = storage.GetDeviceCache().GetDeviceByPhone(phone)
		if device != nil && device.Status == model.DeviceStatusOnline {
			break
		}
		time.Sleep(10 * time.Millisecond)
	}
	require.NotNil(t, device)
	require.Equal(t, model.DeviceStatusOnline, device.Status)
	t.Cleanup(func() { storage.GetDeviceCache().DelDeviceByPhone(phone); protocol.NewKeepaliveTimer().Cancel(phone) })

	industry := storage.GetIndustryCache()

	// 0x0701 electronic waybill upload.
	require.NoError(t, cli.SendWithError(&model.Msg0701{Header: model.GenMsgHeader(device, 0x0701, 3), Content: "运单编号:WB20240101001;发货方:北京A公司;收货方:上海B公司"}))
	deadline = time.Now().Add(2 * time.Second)
	for time.Now().Before(deadline) {
		if industry.Waybill(phone) != nil {
			break
		}
		time.Sleep(10 * time.Millisecond)
	}
	waybill := industry.Waybill(phone)
	require.NotNil(t, waybill, "platform should store 0x0701 waybill")
	require.Contains(t, waybill.Content, "WB20240101001")

	// 0x0704 batch positioning with two 28-byte location items.
	loc1Raw := buildGeoAlarmBody("240101120000")
	loc2Raw := buildGeoAlarmBody("240101120100")
	batch := &model.Msg0704{
		Header:    model.GenMsgHeader(device, 0x0704, 4),
		ItemCount: 2,
		DataType:  0,
		Items: []model.BatchLocationItem{
			{Length: uint16(len(loc1Raw)), Body: loc1Raw},
			{Length: uint16(len(loc2Raw)), Body: loc2Raw},
		},
	}
	require.NoError(t, cli.SendWithError(batch))
	deadline = time.Now().Add(2 * time.Second)
	for time.Now().Before(deadline) {
		if bl := industry.BatchLocation(phone); bl != nil && bl.ItemCount == 2 {
			break
		}
		time.Sleep(10 * time.Millisecond)
	}
	batchLoc := industry.BatchLocation(phone)
	require.NotNil(t, batchLoc, "platform should store 0x0704 batch location")
	require.Equal(t, uint16(2), batchLoc.ItemCount)
	require.Equal(t, uint8(0), batchLoc.DataType)
	require.Len(t, batchLoc.Items, 2)
	require.Equal(t, "240101120000", batchLoc.Items[0].Loc.Time)
	require.Equal(t, "240101120100", batchLoc.Items[1].Loc.Time)
}

// TestSimulatorMediaRetransmissionRecoversDroppedPacket sends a fragmented
// 0x0801 with one fragment dropped and verifies the server detects the gap,
// requests retransmission via 0x8800, and reassembles the media exactly once.
func TestSimulatorMediaRetransmissionRecoversDroppedPacket(t *testing.T) {
	server := NewTCPServer()
	require.NoError(t, server.Listen("127.0.0.1:0"))
	go server.Start()
	t.Cleanup(server.Stop)
	cli := client.NewTCPClient()
	require.NoError(t, cli.Dial(server.listener.Addr().String()))
	t.Cleanup(cli.Stop)
	go cli.Start()
	const phone = "188765433240"
	registration := &model.Msg0100{Header: &model.MsgHeader{MsgID: 0x0100, Attr: &model.MsgBodyAttr{VersionDesc: model.Version2013}, PhoneNumber: phone}, ProvinceID: 11, CityID: 1101, ManufacturerID: "ABCDE", DeviceMode: "JT808-MEDIA-SIM", DeviceID: "MEDIA-02", PlateColor: 1, PlateNumber: "京A88000"}
	require.NoError(t, cli.SendWithError(registration))
	var device *model.Device
	deadline := time.Now().Add(3 * time.Second)
	for time.Now().Before(deadline) {
		device, _ = storage.GetDeviceCache().GetDeviceByPhone(phone)
		if device != nil && device.Status == model.DeviceStatusOnline {
			break
		}
		time.Sleep(10 * time.Millisecond)
	}
	require.NotNil(t, device)
	require.Equal(t, model.DeviceStatusOnline, device.Status)
	t.Cleanup(func() { storage.GetDeviceCache().DelDeviceByPhone(phone); protocol.NewKeepaliveTimer().Cancel(phone) })

	industry := storage.GetIndustryCache()

	// Build a 1500-byte media payload split into 5 fragments of 300 bytes.
	const mediaID = uint32(8801)
	original := make([]byte, 1500)
	for i := range original {
		original[i] = byte(i % 251)
	}
	media := &model.Msg0801{
		Header:       model.GenMsgHeader(device, 0x0801, 40),
		MultiMediaID: mediaID,
		GeoAlarmBody: make([]byte, 28),
		FragmentData: original,
	}
	// Drop fragment 3 (1-based); the server must request it via 0x8800.
	require.NoError(t, cli.SendFragmentsWithFault(media, 300, false, 3))

	// The media should be reassembled exactly once after retransmission.
	deadline = time.Now().Add(5 * time.Second)
	var matched *model.Msg0801
	for time.Now().Before(deadline) {
		for i := range industry.MediaUploads(phone) {
			u := industry.MediaUploads(phone)[i]
			if u.MultiMediaID == mediaID && len(u.FragmentData) == len(original) {
				matched = &u
				break
			}
		}
		if matched != nil {
			break
		}
		time.Sleep(10 * time.Millisecond)
	}
	require.NotNil(t, matched, "dropped media fragment should be recovered via 0x8800 retransmission")
	require.Equal(t, original, matched.FragmentData, "reassembled media must match the original payload exactly")

	// The media record must appear exactly once (no duplicate reassembly).
	count := 0
	for _, u := range industry.MediaUploads(phone) {
		if u.MultiMediaID == mediaID && len(u.FragmentData) == len(original) {
			count++
		}
	}
	require.Equal(t, 1, count, "media should be reassembled exactly once after retransmission")
}

func countMediaID(uploads []model.Msg0801, id uint32) int {
	count := 0
	for _, u := range uploads {
		if u.MultiMediaID == id {
			count++
		}
	}
	return count
}

func countMediaType(uploads []model.Msg0801, mediaType uint8) int {
	count := 0
	for _, u := range uploads {
		if u.MultiMediaType == mediaType {
			count++
		}
	}
	return count
}

// TestSimulatorRecorderCommandTypedResponses sends supported GB/T 19056
// commands and verifies the 0x0700 carries a typed response data block.
func TestSimulatorRecorderCommandTypedResponses(t *testing.T) {
	server := NewTCPServer()
	require.NoError(t, server.Listen("127.0.0.1:0"))
	go server.Start()
	t.Cleanup(server.Stop)
	cli := client.NewTCPClient()
	require.NoError(t, cli.Dial(server.listener.Addr().String()))
	t.Cleanup(cli.Stop)
	go cli.Start()
	const phone = "188765433230"
	registration := &model.Msg0100{Header: &model.MsgHeader{MsgID: 0x0100, Attr: &model.MsgBodyAttr{VersionDesc: model.Version2013}, PhoneNumber: phone}, ProvinceID: 11, CityID: 1101, ManufacturerID: "ABCDE", DeviceMode: "JT808-RECORDER-SIM", DeviceID: "RECORDER-01", PlateColor: 1, PlateNumber: "京A87000"}
	require.NoError(t, cli.SendWithError(registration))
	var device *model.Device
	deadline := time.Now().Add(3 * time.Second)
	for time.Now().Before(deadline) {
		device, _ = storage.GetDeviceCache().GetDeviceByPhone(phone)
		if device != nil && device.Status == model.DeviceStatusOnline {
			break
		}
		time.Sleep(10 * time.Millisecond)
	}
	require.NotNil(t, device)
	require.Equal(t, model.DeviceStatusOnline, device.Status)
	t.Cleanup(func() { storage.GetDeviceCache().DelDeviceByPhone(phone); protocol.NewKeepaliveTimer().Cancel(phone) })

	industry := storage.GetIndustryCache()
	session, err := storage.GetSession(device.SessionID)
	require.NoError(t, err)

	// 0x31 collect info -> response should be a typed RecorderInfoResponse.
	infoReq := &model.RecorderInfoRequest{
		StandardVersion: "21", RevisionNo: 0,
		CommTime: "261030120000", CommDeviceName: "PLATFORM-01",
	}
	server.Send(session.ID, &model.MsgRecordCommand{
		Header:    model.GenMsgHeader(device, 0x8700, session.GetNextSerialNum()),
		Command:   model.RecorderCmdCollectInfo,
		DataBlock: infoReq,
	})
	deadline = time.Now().Add(3 * time.Second)
	for time.Now().Before(deadline) {
		if r := industry.RecordData(phone); r != nil && r.Command == model.RecorderCmdCollectInfo {
			if _, ok := r.DataBlock.(*model.RecorderInfoResponse); ok {
				break
			}
		}
		time.Sleep(10 * time.Millisecond)
	}
	resp := industry.RecordData(phone)
	require.NotNil(t, resp)
	require.Equal(t, model.RecorderCmdCollectInfo, resp.Command)
	infoResp, ok := resp.DataBlock.(*model.RecorderInfoResponse)
	require.True(t, ok, "0x31 response should carry a typed RecorderInfoResponse")
	require.Equal(t, "GBT19056", infoResp.Magic)
	require.Equal(t, "21", infoResp.StandardVersion)
	require.Equal(t, "京A12345", infoResp.PlateNo)
	require.Equal(t, "LSVNV2182E2100001", infoResp.VIN)

	// 0x58 digest test -> response should be a typed RecorderDigestTestResponse.
	server.Send(session.ID, &model.MsgRecordCommand{
		Header:    model.GenMsgHeader(device, 0x8700, session.GetNextSerialNum()),
		Command:   model.RecorderCmdDigestTest,
		DataBlock: &model.RecorderDigestTestRequest{CommTime: "261030120000"},
	})
	deadline = time.Now().Add(3 * time.Second)
	for time.Now().Before(deadline) {
		if r := industry.RecordData(phone); r != nil && r.Command == model.RecorderCmdDigestTest {
			if _, ok := r.DataBlock.(*model.RecorderDigestTestResponse); ok {
				break
			}
		}
		time.Sleep(10 * time.Millisecond)
	}
	resp = industry.RecordData(phone)
	require.Equal(t, model.RecorderCmdDigestTest, resp.Command)
	digestResp, ok := resp.DataBlock.(*model.RecorderDigestTestResponse)
	require.True(t, ok, "0x58 response should carry a typed RecorderDigestTestResponse")
	require.Len(t, digestResp.RecorderID, 35)
	require.Len(t, digestResp.Digest, 32)

	// 0x33 collect file -> response should be a typed RecorderFileResponse.
	server.Send(session.ID, &model.MsgRecordCommand{
		Header:  model.GenMsgHeader(device, 0x8700, session.GetNextSerialNum()),
		Command: model.RecorderCmdCollectFile,
		DataBlock: &model.RecorderFileRequest{Items: []model.RecorderFileRequestItem{
			{Category: model.FileCategoryLog, StartTime: "261030000000", EndTime: "261030235959"},
		}},
	})
	deadline = time.Now().Add(3 * time.Second)
	for time.Now().Before(deadline) {
		if r := industry.RecordData(phone); r != nil && r.Command == model.RecorderCmdCollectFile {
			if _, ok := r.DataBlock.(*model.RecorderFileResponse); ok {
				break
			}
		}
		time.Sleep(10 * time.Millisecond)
	}
	resp = industry.RecordData(phone)
	require.Equal(t, model.RecorderCmdCollectFile, resp.Command)
	fileResp, ok := resp.DataBlock.(*model.RecorderFileResponse)
	require.True(t, ok, "0x33 response should carry a typed RecorderFileResponse")
	require.Len(t, fileResp.Items, 1)
	require.Equal(t, model.FileCategoryLog, fileResp.Items[0].Category)
	require.Equal(t, uint32(4), fileResp.Items[0].FileLength)
	require.Equal(t, []byte{0xAA, 0xBB, 0xCC, 0xDD}, fileResp.Items[0].FileData)
}

// TestSimulatorRecorderParamSettingRoundTrip sends 0x8701 (parameter setting)
// and verifies the terminal acknowledges it with a 0x0001. A short ack
// timeout makes the connection close quickly if the 0x0001 is never received,
// so a still-online device proves the round-trip succeeded.
func TestSimulatorRecorderParamSettingRoundTrip(t *testing.T) {
	server := NewTCPServer()
	require.NoError(t, server.Listen("127.0.0.1:0"))
	go server.Start()
	t.Cleanup(server.Stop)
	cli := client.NewTCPClient()
	require.NoError(t, cli.Dial(server.listener.Addr().String()))
	t.Cleanup(cli.Stop)
	go cli.Start()
	const phone = "188765433231"
	registration := &model.Msg0100{Header: &model.MsgHeader{MsgID: 0x0100, Attr: &model.MsgBodyAttr{VersionDesc: model.Version2013}, PhoneNumber: phone}, ProvinceID: 11, CityID: 1101, ManufacturerID: "ABCDE", DeviceMode: "JT808-RECORDER-SIM", DeviceID: "RECORDER-02", PlateColor: 1, PlateNumber: "京A87010"}
	require.NoError(t, cli.SendWithError(registration))
	var device *model.Device
	deadline := time.Now().Add(3 * time.Second)
	for time.Now().Before(deadline) {
		device, _ = storage.GetDeviceCache().GetDeviceByPhone(phone)
		if device != nil && device.Status == model.DeviceStatusOnline {
			break
		}
		time.Sleep(10 * time.Millisecond)
	}
	require.NotNil(t, device)
	require.Equal(t, model.DeviceStatusOnline, device.Status)
	t.Cleanup(func() { storage.GetDeviceCache().DelDeviceByPhone(phone); protocol.NewKeepaliveTimer().Cancel(phone) })

	session, err := storage.GetSession(device.SessionID)
	require.NoError(t, err)
	// Tighten the reliable-send window so a missing 0x0001 closes the
	// connection within a few hundred milliseconds.
	session.AckTimeout = 50 * time.Millisecond
	session.MaxRetries = 1

	server.Send(session.ID, &model.MsgRecordCommand{
		Header:  model.GenMsgHeader(device, 0x8701, session.GetNextSerialNum()),
		Command: model.RecorderCmdParamSet,
		DataBlock: &model.RecorderParamSetting{
			ParamFlags:    0xFF,
			RecorderID:    make([]byte, 35),
			PlateNo:       "京A87010",
			PlateCategory: "小型汽车",
			VIN:           "LSVNV2182E2100001",
			SerialNo:      "000000000001",
			PulseCoeff:    100,
			InstallTime:   "230101000000",
			Salt:          make([]byte, 16),
		},
	})

	// Wait well beyond the retransmission window; if the 0x0001 ack was
	// processed the session stays open, otherwise it gets closed.
	time.Sleep(300 * time.Millisecond)
	device, _ = storage.GetDeviceCache().GetDeviceByPhone(phone)
	require.NotNil(t, device, "device should still be cached after 0x8701 round-trip")
	require.Equal(t, model.DeviceStatusOnline, device.Status, "0x8701 should be acknowledged by 0x0001 and keep the device online")
}

func TestClientFaultFramesDoNotPreventFollowingRegistration(t *testing.T) {
	for _, transport := range []model.TransportProtocol{model.TCPProto, model.UDPProto} {
		for _, fault := range []string{"bad-checksum", "bad-escape", "oversize"} {
			t.Run(string(transport)+"/"+fault, func(t *testing.T) {
				var addr string
				if transport == model.TCPProto {
					server := NewTCPServer()
					require.NoError(t, server.Listen("127.0.0.1:0"))
					addr = server.listener.Addr().String()
					go server.Start()
					t.Cleanup(server.Stop)
				} else {
					server := NewUDPServer()
					require.NoError(t, server.Listen("127.0.0.1:0"))
					addr = server.listener.LocalAddr().String()
					go server.Start()
					t.Cleanup(server.Stop)
				}

				cli := client.NewTCPClient()
				require.NoError(t, cli.DialTransport(addr, transport))
				t.Cleanup(cli.Stop)
				go cli.Start()

				phone := map[bool]string{true: "188765433000", false: "188765433001"}[transport == model.TCPProto]
				registration := &model.Msg0100{
					Header:     &model.MsgHeader{MsgID: 0x0100, Attr: &model.MsgBodyAttr{VersionDesc: model.Version2013}, PhoneNumber: phone},
					ProvinceID: 11, CityID: 1101, ManufacturerID: "ABCDE", DeviceMode: "JT808-TEST-DEVICE-01", DeviceID: "FAULT-TEST", PlateColor: 1, PlateNumber: "京A12345",
				}
				require.NoError(t, cli.SendMalformedFrame(registration, fault))
				deadline := time.Now().Add(6 * time.Second)
				for time.Now().Before(deadline) {
					device, err := storage.GetDeviceCache().GetDeviceByPhone(phone)
					if err == nil && device.Status == model.DeviceStatusOnline {
						break
					}
					time.Sleep(10 * time.Millisecond)
				}
				device, err := storage.GetDeviceCache().GetDeviceByPhone(phone)
				require.NoError(t, err, "valid registration after %s was not processed", fault)
				require.Equal(t, model.DeviceStatusOnline, device.Status)
				t.Cleanup(func() {
					storage.GetDeviceCache().DelDeviceByPhone(phone)
					protocol.NewKeepaliveTimer().Cancel(phone)
				})
			})
		}
	}
}

// bareTerminal is a raw TCP peer driving the JT808 wire protocol itself, so
// tests can send uplink messages the high-level client never generates
// (e.g. a terminal-side 0x8204/0x8105/0x8500).
type bareTerminal struct {
	conn  net.Conn
	codec *protocol.JT808PacketCodec
	fh    *protocol.JT808FrameHandler
}

func uplinkHeader(msgID uint16, phone string, serial uint16) *model.MsgHeader {
	return &model.MsgHeader{
		MsgID:        msgID,
		Attr:         &model.MsgBodyAttr{VersionDesc: model.Version2013},
		PhoneNumber:  phone,
		SerialNumber: serial,
	}
}

func newBareTerminal(t *testing.T, addr string) *bareTerminal {
	t.Helper()
	conn, err := net.Dial("tcp", addr)
	require.NoError(t, err)
	t.Cleanup(func() { _ = conn.Close() })
	return &bareTerminal{
		conn:  conn,
		codec: protocol.NewJT808PacketCodec(),
		fh:    protocol.NewJT808FrameHandler(conn),
	}
}

// dialBareTerminal opens a raw connection and completes 0x0100/0x0102
// registration, leaving an online device the test fully controls on the wire.
func dialBareTerminal(t *testing.T, addr, phone, plate, deviceID string) *bareTerminal {
	t.Helper()
	b := newBareTerminal(t, addr)
	t.Cleanup(func() {
		storage.GetDeviceCache().DelDeviceByPhone(phone)
		protocol.NewKeepaliveTimer().Cancel(phone)
	})
	b.send(t, &model.Msg0100{
		Header:         uplinkHeader(0x0100, phone, 1),
		ProvinceID:     11,
		CityID:         1101,
		ManufacturerID: "ABCDE",
		DeviceMode:     "JT808-BARE-SIM",
		DeviceID:       deviceID,
		PlateColor:     1,
		PlateNumber:    plate,
	})
	regReply := &model.Msg8100{}
	b.recvInto(t, 3*time.Second, 0x8100, regReply)
	require.Equal(t, model.ResSuccess, regReply.Result, "registration should succeed")

	b.send(t, &model.Msg0102{Header: uplinkHeader(0x0102, phone, 2), AuthCode: regReply.AuthCode})
	authReply := &model.Msg8001{}
	b.recvInto(t, 3*time.Second, 0x8001, authReply)
	require.Equal(t, model.ResultSuccess, authReply.Result)
	require.Equal(t, uint16(0x0102), authReply.AnswerMessageID)

	deadline := time.Now().Add(3 * time.Second)
	for time.Now().Before(deadline) {
		d, err := storage.GetDeviceCache().GetDeviceByPhone(phone)
		if err == nil && d.Status == model.DeviceStatusOnline {
			return b
		}
		time.Sleep(5 * time.Millisecond)
	}
	t.Fatalf("device %s did not become online", phone)
	return nil
}

func (b *bareTerminal) send(t *testing.T, msg model.JT808Msg) {
	t.Helper()
	frame, err := b.codec.Encode(msg)
	require.NoError(t, err)
	_, err = b.conn.Write(frame)
	require.NoError(t, err)
}

// recvInto skips unrelated frames and decodes the first frame carrying
// wantMsgID into msg.
func (b *bareTerminal) recvInto(t *testing.T, timeout time.Duration, wantMsgID uint16, msg model.JT808Msg) {
	t.Helper()
	deadline := time.Now().Add(timeout)
	for {
		if remain := time.Until(deadline); remain <= 0 {
			t.Fatalf("timeout waiting for msg 0x%04x", wantMsgID)
		}
		require.NoError(t, b.conn.SetReadDeadline(deadline))
		frame, err := b.fh.Recv(context.Background())
		require.NoError(t, err)
		pkt, err := b.codec.Decode(frame)
		require.NoError(t, err)
		if pkt.Header.MsgID != wantMsgID {
			continue
		}
		require.NoError(t, msg.Decode(pkt))
		return
	}
}

func waitDeviceAbsent(t *testing.T, phone string) {
	t.Helper()
	deadline := time.Now().Add(3 * time.Second)
	for time.Now().Before(deadline) {
		if _, err := storage.GetDeviceCache().GetDeviceByPhone(phone); err != nil {
			return
		}
		time.Sleep(5 * time.Millisecond)
	}
	t.Fatalf("device %s was not removed from cache", phone)
}

func TestTCPServerHeartbeatRefreshesDeviceComTime(t *testing.T) {
	server := NewTCPServer()
	require.NoError(t, server.Listen("127.0.0.1:0"))
	go server.Start()
	t.Cleanup(server.Stop)

	const phone = "188765433221"
	b := dialBareTerminal(t, server.listener.Addr().String(), phone, "京B00021", "BARE-HB-01")

	cache := storage.GetDeviceCache()
	device, err := cache.GetDeviceByPhone(phone)
	require.NoError(t, err)
	// Backdate the last communication time beyond the keepalive window.
	stale := time.Now().Add(-90 * time.Second)
	device.LastestComTime = stale
	cache.CacheDevice(device)

	b.send(t, &model.Msg0002{Header: uplinkHeader(0x0002, phone, 10)})
	ack := &model.Msg8001{}
	b.recvInto(t, 3*time.Second, 0x8001, ack)
	require.Equal(t, uint16(0x0002), ack.AnswerMessageID)
	require.Equal(t, model.ResultSuccess, ack.Result)

	refreshed, err := cache.GetDeviceByPhone(phone)
	require.NoError(t, err)
	require.True(t, refreshed.LastestComTime.After(stale.Add(80*time.Second)), "heartbeat must refresh LastestComTime")
	require.WithinDuration(t, time.Now(), refreshed.LastestComTime, 5*time.Second)
}

func TestTCPServerHeartbeatFromUnknownDeviceClosesConn(t *testing.T) {
	server := NewTCPServer()
	require.NoError(t, server.Listen("127.0.0.1:0"))
	go server.Start()
	t.Cleanup(server.Stop)

	b := newBareTerminal(t, server.listener.Addr().String())
	b.send(t, &model.Msg0002{Header: uplinkHeader(0x0002, "188765439999", 1)})
	require.NoError(t, b.conn.SetReadDeadline(time.Now().Add(3*time.Second)))
	_, err := b.fh.Recv(context.Background())
	require.Error(t, err, "unknown-device heartbeat must close the connection")
}

func TestTCPServerLogoutClearsCacheAndKeepsConn(t *testing.T) {
	server := NewTCPServer()
	require.NoError(t, server.Listen("127.0.0.1:0"))
	go server.Start()
	t.Cleanup(server.Stop)

	const phone = "188765433222"
	b := dialBareTerminal(t, server.listener.Addr().String(), phone, "京B00022", "BARE-LO-01")

	b.send(t, &model.Msg0003{Header: uplinkHeader(0x0003, phone, 20)})
	ack := &model.Msg8001{}
	b.recvInto(t, 3*time.Second, 0x8001, ack)
	require.Equal(t, uint16(0x0003), ack.AnswerMessageID)
	require.Equal(t, model.ResultSuccess, ack.Result)

	waitDeviceAbsent(t, phone)

	// The server keeps the connection open after logout, waiting for the
	// terminal to close it to avoid TCP TIMEWAIT on the server side.
	require.NoError(t, b.conn.SetReadDeadline(time.Now().Add(300*time.Millisecond)))
	_, err := b.fh.Recv(context.Background())
	require.Error(t, err)
	var netErr net.Error
	require.True(t, errors.As(err, &netErr) && netErr.Timeout(), "expected read timeout on kept-open conn, got %v", err)
}

func TestTCPServerAnswersServerTimeQuery(t *testing.T) {
	server := NewTCPServer()
	require.NoError(t, server.Listen("127.0.0.1:0"))
	go server.Start()
	t.Cleanup(server.Stop)

	const phone = "188765433232"
	b := dialBareTerminal(t, server.listener.Addr().String(), phone, "京B00032", "BARE-TIME-1")

	b.send(t, &model.Msg0004{Header: uplinkHeader(0x0004, phone, 30)})
	reply := &model.Msg8004{}
	b.recvInto(t, 3*time.Second, 0x8004, reply)
	require.NotNil(t, reply.ServerTime)
	// BCD time carries local wall-clock fields but hex.ParseTime labels it
	// UTC, so rebuild the wall clock in the local location before comparing.
	srv := reply.ServerTime
	srvWall := time.Date(srv.Year(), srv.Month(), srv.Day(), srv.Hour(), srv.Minute(), srv.Second(), 0, time.Local)
	require.WithinDuration(t, time.Now(), srvWall, 2*time.Second)
}

func TestTCPServerAnswersTerminalUplink8204(t *testing.T) {
	server := NewTCPServer()
	require.NoError(t, server.Listen("127.0.0.1:0"))
	go server.Start()
	t.Cleanup(server.Stop)

	const phone = "188765433233"
	b := dialBareTerminal(t, server.listener.Addr().String(), phone, "京B00033", "BARE-LINK-1")

	b.send(t, &model.Msg8204{Header: uplinkHeader(0x8204, phone, 40)})
	ack := &model.Msg0001{}
	b.recvInto(t, 3*time.Second, 0x0001, ack)
	require.Equal(t, uint16(0x8204), ack.AnswerMessageID)
	require.Equal(t, uint8(0), ack.Result)
}

func TestTCPServerAnswersTerminalUplink8105(t *testing.T) {
	server := NewTCPServer()
	require.NoError(t, server.Listen("127.0.0.1:0"))
	go server.Start()
	t.Cleanup(server.Stop)

	const phone = "188765433234"
	b := dialBareTerminal(t, server.listener.Addr().String(), phone, "京B00034", "BARE-CTRL-1")

	for i, msg := range []*model.Msg8105{
		{Header: uplinkHeader(0x8105, phone, 50), CommandWord: 0x01},
		{Header: uplinkHeader(0x8105, phone, 51), CommandWord: 0x02, CommandParam: "12345"},
	} {
		b.send(t, msg)
		ack := &model.Msg0001{}
		b.recvInto(t, 3*time.Second, 0x0001, ack)
		require.Equal(t, uint16(0x8105), ack.AnswerMessageID, "8105 case %d", i)
		require.Equal(t, uint8(0), ack.Result)
	}
}

func TestTCPServerTerminalUplink8500Echoes0500(t *testing.T) {
	server := NewTCPServer()
	require.NoError(t, server.Listen("127.0.0.1:0"))
	go server.Start()
	t.Cleanup(server.Stop)

	const phone = "188765433235"
	b := dialBareTerminal(t, server.listener.Addr().String(), phone, "京B00035", "BARE-VCTL-1")

	b.send(t, &model.Msg8500{Header: uplinkHeader(0x8500, phone, 60), ControlFlags: 0x0007})
	reply := &model.Msg0500{}
	b.recvInto(t, 3*time.Second, 0x0500, reply)
	require.Equal(t, uint16(0x0007), reply.ControlFlags)
	require.Equal(t, uint16(0x0007), storage.GetIndustryCache().VehicleControl(phone))
}

func TestTCPServerRejectsDuplicateRegistration(t *testing.T) {
	server := NewTCPServer()
	require.NoError(t, server.Listen("127.0.0.1:0"))
	go server.Start()
	t.Cleanup(server.Stop)

	const phoneA = "188765433236"
	const phoneB = "188765433237"
	const plateA = "京B00036"
	const plateC = "京B00037"
	dialBareTerminal(t, server.listener.Addr().String(), phoneA, plateA, "BARE-DUP-A")
	cache := storage.GetDeviceCache()

	// Same plate, different phone on a new conn -> car already registered.
	b2 := newBareTerminal(t, server.listener.Addr().String())
	b2.send(t, &model.Msg0100{
		Header:         uplinkHeader(0x0100, phoneB, 1),
		ProvinceID:     11,
		CityID:         1101,
		ManufacturerID: "ABCDE",
		DeviceMode:     "JT808-BARE-SIM",
		DeviceID:       "BARE-DUP-B",
		PlateColor:     1,
		PlateNumber:    plateA,
	})
	carDup := &model.Msg8100{}
	b2.recvInto(t, 3*time.Second, 0x8100, carDup)
	require.Equal(t, model.ResCarAlreadyRegister, carDup.Result)
	_, err := cache.GetDeviceByPhone(phoneB)
	require.ErrorIs(t, err, storage.ErrDeviceNotFound)

	// Same phone, different plate on a new conn -> device already registered.
	b3 := newBareTerminal(t, server.listener.Addr().String())
	b3.send(t, &model.Msg0100{
		Header:         uplinkHeader(0x0100, phoneA, 1),
		ProvinceID:     11,
		CityID:         1101,
		ManufacturerID: "ABCDE",
		DeviceMode:     "JT808-BARE-SIM",
		DeviceID:       "BARE-DUP-C",
		PlateColor:     1,
		PlateNumber:    plateC,
	})
	devDup := &model.Msg8100{}
	b3.recvInto(t, 3*time.Second, 0x8100, devDup)
	require.Equal(t, model.ResDeviceAlreadyRegister, devDup.Result)
	require.False(t, cache.HasPlate(plateC), "rejected registration must not cache its plate")

	original, err := cache.GetDeviceByPhone(phoneA)
	require.NoError(t, err)
	require.Equal(t, model.DeviceStatusOnline, original.Status, "original device must stay untouched")
}

func TestTCPServerRejectsWrongAuthCode(t *testing.T) {
	server := NewTCPServer()
	require.NoError(t, server.Listen("127.0.0.1:0"))
	go server.Start()
	t.Cleanup(server.Stop)

	const phone = "188765433238"
	b := newBareTerminal(t, server.listener.Addr().String())
	t.Cleanup(func() {
		storage.GetDeviceCache().DelDeviceByPhone(phone)
		protocol.NewKeepaliveTimer().Cancel(phone)
	})
	b.send(t, &model.Msg0100{
		Header:         uplinkHeader(0x0100, phone, 1),
		ProvinceID:     11,
		CityID:         1101,
		ManufacturerID: "ABCDE",
		DeviceMode:     "JT808-BARE-SIM",
		DeviceID:       "BARE-AUTH-1",
		PlateColor:     1,
		PlateNumber:    "京B00038",
	})
	regReply := &model.Msg8100{}
	b.recvInto(t, 3*time.Second, 0x8100, regReply)
	require.Equal(t, model.ResSuccess, regReply.Result)

	b.send(t, &model.Msg0102{Header: uplinkHeader(0x0102, phone, 2), AuthCode: regReply.AuthCode + "X"})
	ack := &model.Msg8001{}
	b.recvInto(t, 3*time.Second, 0x8001, ack)
	require.Equal(t, uint16(0x0102), ack.AnswerMessageID)
	require.Equal(t, model.ResultFail, ack.Result)

	waitDeviceAbsent(t, phone)

	// Connection is intentionally kept open after a failed authentication.
	require.NoError(t, b.conn.SetReadDeadline(time.Now().Add(300*time.Millisecond)))
	_, err := b.fh.Recv(context.Background())
	require.Error(t, err)
	var netErr net.Error
	require.True(t, errors.As(err, &netErr) && netErr.Timeout(), "expected read timeout on kept-open conn, got %v", err)
}

func TestTCPServerMergesTerminalParamsAndAnswers8104(t *testing.T) {
	server := NewTCPServer()
	require.NoError(t, server.Listen("127.0.0.1:0"))
	go server.Start()
	t.Cleanup(server.Stop)

	const phone = "188765433239"
	b := dialBareTerminal(t, server.listener.Addr().String(), phone, "京B00039", "BARE-PARAM-1")

	b.send(t, &model.Msg8103{
		Header:     uplinkHeader(0x8103, phone, 3),
		Parameters: &model.DeviceParams{ParamCnt: 1, Params: []*model.ParamData{{ParamID: 0x0001, ParamValue: uint32(111)}}},
	})
	ack := &model.Msg0001{}
	b.recvInto(t, 3*time.Second, 0x0001, ack)
	require.Equal(t, uint16(0x8103), ack.AnswerMessageID)

	b.send(t, &model.Msg8103{
		Header: uplinkHeader(0x8103, phone, 4),
		Parameters: &model.DeviceParams{ParamCnt: 2, Params: []*model.ParamData{
			{ParamID: 0x0001, ParamValue: uint32(222)},
			{ParamID: 0x0002, ParamValue: uint32(333)},
		}},
	})
	b.recvInto(t, 3*time.Second, 0x0001, &model.Msg0001{})

	// Terminal-side 0x8104 query: the shared processor answers with the merged
	// parameter set carried in a platform-direction 0x0104.
	b.send(t, &model.Msg8104{Header: uplinkHeader(0x8104, phone, 5)})
	reply := &model.Msg0104{}
	b.recvInto(t, 3*time.Second, 0x0104, reply)
	require.Equal(t, uint16(5), reply.AnswerSerialNumber)
	require.Equal(t, uint8(2), reply.AnswerParamCnt)
	require.Len(t, reply.Parameters.Params, 2)
	require.Equal(t, uint32(0x0001), reply.Parameters.Params[0].ParamID)
	require.Equal(t, uint32(222), reply.Parameters.Params[0].ParamValue)
	require.Equal(t, uint32(0x0002), reply.Parameters.Params[1].ParamID)
	require.Equal(t, uint32(333), reply.Parameters.Params[1].ParamValue)

	cached, err := storage.GetDeviceParamsCache().GetDeviceParamsByPhone(phone)
	require.NoError(t, err)
	require.Len(t, cached.Params, 2)
	require.Equal(t, uint32(222), cached.Params[0].ParamValue)
}

// TestSimulatorRecorderPositionTestCommands covers GB/T 19056 0x56/0x57:
// the simulator answers with a typed 0xD6 position-test data block.
func TestSimulatorRecorderPositionTestCommands(t *testing.T) {
	server := NewTCPServer()
	require.NoError(t, server.Listen("127.0.0.1:0"))
	go server.Start()
	t.Cleanup(server.Stop)
	cli := client.NewTCPClient()
	require.NoError(t, cli.Dial(server.listener.Addr().String()))
	t.Cleanup(cli.Stop)
	go cli.Start()
	const phone = "188765433241"
	registration := &model.Msg0100{Header: &model.MsgHeader{MsgID: 0x0100, Attr: &model.MsgBodyAttr{VersionDesc: model.Version2013}, PhoneNumber: phone}, ProvinceID: 11, CityID: 1101, ManufacturerID: "ABCDE", DeviceMode: "JT808-RECORDER-SIM", DeviceID: "RECORDER-03", PlateColor: 1, PlateNumber: "京A87020"}
	require.NoError(t, cli.SendWithError(registration))
	var device *model.Device
	deadline := time.Now().Add(3 * time.Second)
	for time.Now().Before(deadline) {
		device, _ = storage.GetDeviceCache().GetDeviceByPhone(phone)
		if device != nil && device.Status == model.DeviceStatusOnline {
			break
		}
		time.Sleep(10 * time.Millisecond)
	}
	require.NotNil(t, device)
	require.Equal(t, model.DeviceStatusOnline, device.Status)
	t.Cleanup(func() { storage.GetDeviceCache().DelDeviceByPhone(phone); protocol.NewKeepaliveTimer().Cancel(phone) })

	industry := storage.GetIndustryCache()
	session, err := storage.GetSession(device.SessionID)
	require.NoError(t, err)

	cases := []struct {
		name  string
		cmd   uint8
		block any
	}{
		{"on", model.RecorderCmdPositionTestOn, &model.RecorderPositionTestRequest{Duration: 60}},
		{"off", model.RecorderCmdPositionTestOff, nil},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			server.Send(session.ID, &model.MsgRecordCommand{
				Header:    model.GenMsgHeader(device, 0x8700, session.GetNextSerialNum()),
				Command:   tc.cmd,
				DataBlock: tc.block,
			})
			deadline := time.Now().Add(3 * time.Second)
			for time.Now().Before(deadline) {
				if r := industry.RecordData(phone); r != nil && r.Command == tc.cmd {
					if _, ok := r.DataBlock.(*model.RecorderPositionTestResponse); ok {
						break
					}
				}
				time.Sleep(10 * time.Millisecond)
			}
			resp := industry.RecordData(phone)
			require.NotNil(t, resp)
			require.Equal(t, tc.cmd, resp.Command)
			pos, ok := resp.DataBlock.(*model.RecorderPositionTestResponse)
			require.True(t, ok, "0x%02x response should carry a typed RecorderPositionTestResponse", tc.cmd)
			require.True(t, strings.HasPrefix(pos.GGAData, "$GPGGA"), "unexpected GGA data %q", pos.GGAData)
			require.True(t, strings.HasPrefix(pos.RMCData, "$GPRMC"), "unexpected RMC data %q", pos.RMCData)
		})
	}
}
