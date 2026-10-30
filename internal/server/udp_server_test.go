package server

import (
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
