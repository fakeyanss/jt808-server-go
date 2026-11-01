package server

import (
	"context"
	"testing"
	"time"

	"github.com/stretchr/testify/require"

	"github.com/fakeyanss/jt808-server-go/internal/client"
	"github.com/fakeyanss/jt808-server-go/internal/protocol"
	"github.com/fakeyanss/jt808-server-go/internal/protocol/model"
	"github.com/fakeyanss/jt808-server-go/internal/repository"
)

// startSimulatorServer brings up a TCP server and a registered client simulator
// for region/route end-to-end tests.
func startSimulatorServer(t *testing.T) (*TCPServer, *client.TCPClient, *model.Device) {
	t.Helper()
	server := NewTCPServer()
	require.NoError(t, server.Listen("127.0.0.1:0"))
	go server.Start()
	t.Cleanup(server.Stop)

	cli := client.NewTCPClient()
	require.NoError(t, cli.Dial(server.listener.Addr().String()))
	t.Cleanup(cli.Stop)
	go cli.Start()

	const phone = "188765433210"
	registration := &model.Msg0100{
		Header:     &model.MsgHeader{MsgID: 0x0100, Attr: &model.MsgBodyAttr{VersionDesc: model.Version2013}, PhoneNumber: phone},
		ProvinceID: 11, CityID: 1101, ManufacturerID: "ABCDE", DeviceMode: "JT808-REGION-SIM",
		DeviceID: "REGION-01", PlateColor: 1, PlateNumber: "京A11111",
	}
	require.NoError(t, cli.SendWithError(registration))

	var device *model.Device
	deadline := time.Now().Add(3 * time.Second)
	for time.Now().Before(deadline) {
		device, _ = repository.Default().Devices.GetByPhone(context.Background(), phone)
		if device != nil && device.Status == model.DeviceStatusOnline {
			break
		}
		time.Sleep(10 * time.Millisecond)
	}
	require.NotNil(t, device)
	require.Equal(t, model.DeviceStatusOnline, device.Status)
	t.Cleanup(func() {
		repository.Default().Devices.DeleteByPhone(context.Background(), phone)
		protocol.NewKeepaliveTimer().Cancel(phone)
	})
	return server, cli, device
}

func TestRegionRouteSetQueryDeleteRoundTrip(t *testing.T) {
	server, _, device := startSimulatorServer(t)
	session, err := repository.Default().Conn.Get(context.Background(), device.SessionID)
	require.NoError(t, err)

	catalog := repository.GetRegionCatalog()

	// --- Set a circle area ---
	setCircle := &model.MsgRegionRouteSet{
		Header:      model.GenMsgHeader(device, 0x8600, session.GetNextSerialNum()),
		SettingAttr: 1,
		Circles: []model.CircleArea{{
			ID: 101, Attr: model.AreaAttrTime | model.AreaAttrSpeed,
			Latitude: 31000000, Longitude: 121000000, Radius: 500,
			StartTime: "261028000000", EndTime: "261028235959",
			MaxSpeed: 600, Duration: 10, Name: "测试圆",
		}},
	}
	server.Send(session.ID, setCircle)
	waitFor(t, 2*time.Second, func() bool {
		resp := catalog.Query(device.Phone, 1, nil)
		return len(resp.Circles) == 1 && resp.Circles[0].ID == 101
	})

	// --- Query circles back through 0x8608/0x0608 ---
	responseCh, cancel, err := repository.GetRegionResponseBroker().Register(device.Phone, 1)
	require.NoError(t, err)
	server.Send(session.ID, &model.Msg8608{Header: model.GenMsgHeader(device, 0x8608, session.GetNextSerialNum()), QueryType: 1})
	select {
	case resp := <-responseCh:
		require.Equal(t, uint32(1), resp.Count)
		require.Len(t, resp.Circles, 1)
		require.Equal(t, uint32(101), resp.Circles[0].ID)
		require.Equal(t, "测试圆", resp.Circles[0].Name)
		require.Equal(t, uint32(500), resp.Circles[0].Radius)
	case <-time.After(2 * time.Second):
		t.Fatal("simulator did not answer circle query")
	}
	cancel()

	// --- Set a rectangle and a route, then query each ---
	setRect := &model.MsgRegionRouteSet{
		Header:      model.GenMsgHeader(device, 0x8602, session.GetNextSerialNum()),
		SettingAttr: 1,
		Rectangles: []model.RectangleArea{{
			ID: 201, Attr: 0, LeftTopLat: 31000000, LeftTopLng: 121000000,
			RightBottomLat: 30000000, RightBottomLng: 122000000, Name: "矩形1",
		}},
	}
	server.Send(session.ID, setRect)
	setRoute := &model.MsgRegionRouteSet{
		Header:      model.GenMsgHeader(device, 0x8606, session.GetNextSerialNum()),
		SettingAttr: 1,
		Routes: []model.Route{{
			ID: 401, Attr: model.AreaAttrTime,
			StartTime: "261028000000", EndTime: "261028120000",
			Segments: []model.RouteSegment{{
				ID: 1, Attr: 0, Length: 1000,
				Vertices: []model.PolygonVertex{
					{Latitude: 31000000, Longitude: 121000000},
					{Latitude: 31000010, Longitude: 121000010},
				},
			}},
			Name: "路线1",
		}},
	}
	server.Send(session.ID, setRoute)

	waitFor(t, 2*time.Second, func() bool {
		r := catalog.Query(device.Phone, 2, nil)
		return len(r.Rectangles) == 1
	})
	respRect := catalog.Query(device.Phone, 2, nil)
	require.Equal(t, uint32(201), respRect.Rectangles[0].ID)

	waitFor(t, 2*time.Second, func() bool {
		return len(catalog.Query(device.Phone, 4, nil).Routes) == 1
	})
	respRoute := catalog.Query(device.Phone, 4, nil)
	require.Equal(t, uint32(401), respRoute.Routes[0].ID)
	require.Len(t, respRoute.Routes[0].Segments, 1)

	// --- Set a polygon, query it back, then delete ---
	setPolygon := &model.MsgRegionRouteSet{
		Header:      model.GenMsgHeader(device, 0x8604, session.GetNextSerialNum()),
		SettingAttr: 1,
		Polygons: []model.PolygonArea{{
			ID: 301, Attr: model.AreaAttrTime | model.AreaAttrSpeed,
			StartTime: "261028000000", EndTime: "261028235959",
			MaxSpeed: 800, Duration: 15,
			Vertices: []model.PolygonVertex{
				{Latitude: 31000000, Longitude: 121000000},
				{Latitude: 31000010, Longitude: 121000010},
				{Latitude: 30999990, Longitude: 121000020},
			},
			Name: "多边形1",
		}},
	}
	server.Send(session.ID, setPolygon)
	waitFor(t, 2*time.Second, func() bool {
		return len(catalog.Query(device.Phone, 3, nil).Polygons) == 1
	})
	respPoly := catalog.Query(device.Phone, 3, nil)
	require.Equal(t, uint32(301), respPoly.Polygons[0].ID)
	require.Equal(t, "多边形1", respPoly.Polygons[0].Name)
	require.Len(t, respPoly.Polygons[0].Vertices, 3)

	server.Send(session.ID, &model.Msg860Delete{Header: model.GenMsgHeader(device, 0x8605, session.GetNextSerialNum()), IDs: []uint32{301}})
	waitFor(t, 2*time.Second, func() bool {
		return len(catalog.Query(device.Phone, 3, nil).Polygons) == 0
	})
	require.Empty(t, catalog.Query(device.Phone, 3, nil).Polygons)

	// --- Delete circle 101, then verify gone ---
	server.Send(session.ID, &model.Msg860Delete{Header: model.GenMsgHeader(device, 0x8601, session.GetNextSerialNum()), IDs: []uint32{101}})
	waitFor(t, 2*time.Second, func() bool {
		return len(catalog.Query(device.Phone, 1, nil).Circles) == 0
	})
	require.Empty(t, catalog.Query(device.Phone, 1, nil).Circles)

	// Delete-all rectangles
	server.Send(session.ID, &model.Msg860Delete{Header: model.GenMsgHeader(device, 0x8603, session.GetNextSerialNum())})
	waitFor(t, 2*time.Second, func() bool {
		return len(catalog.Query(device.Phone, 2, nil).Rectangles) == 0
	})
	require.Empty(t, catalog.Query(device.Phone, 2, nil).Rectangles)
}

func waitFor(t *testing.T, timeout time.Duration, cond func() bool) {
	t.Helper()
	deadline := time.Now().Add(timeout)
	for time.Now().Before(deadline) {
		if cond() {
			return
		}
		time.Sleep(10 * time.Millisecond)
	}
	t.Fatal("condition not met before timeout")
}
