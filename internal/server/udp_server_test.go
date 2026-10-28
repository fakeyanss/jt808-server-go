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
	report := &model.Msg0200{Header: model.GenMsgHeader(device, 0x0200, session.GetNextSerialNum()), AlarmSign: 0x20, StatusSign: 0x03, Latitude: 31123456, Longitude: 121123456, Altitude: 12, Speed: 85, Direction: 90, Time: "261028204500"}
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
