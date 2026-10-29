package model

import (
	"testing"

	"github.com/stretchr/testify/require"
)

func TestCircleAreaRoundTrip(t *testing.T) {
	msg := &MsgRegionRouteSet{
		Header:      genMsgHeader(0x8600),
		SettingAttr: 1,
		Circles: []CircleArea{
			{
				ID: 42, Attr: AreaAttrTime | AreaAttrSpeed | AreaAttrNightSpd,
				Latitude: 31234567, Longitude: 121234567, Radius: 1000,
				StartTime: "261028000000", EndTime: "261028235959",
				MaxSpeed: 800, Duration: 5, NightSpeed: 600, Name: "测试圆形区域",
			},
			{ID: 43, Attr: 0, Latitude: 30000000, Longitude: 120000000, Radius: 500, Name: "无属性圆"},
		},
	}
	decoded := &MsgRegionRouteSet{}
	roundTrip(t, msg, decoded)
	require.Equal(t, msg.SettingAttr, decoded.SettingAttr)
	require.Len(t, decoded.Circles, 2)
	require.Equal(t, msg.Circles[0], decoded.Circles[0])
	require.Equal(t, msg.Circles[1], decoded.Circles[1])
}

func TestRectangleAreaRoundTrip(t *testing.T) {
	msg := &MsgRegionRouteSet{
		Header:      genMsgHeader(0x8602),
		SettingAttr: 2,
		Rectangles: []RectangleArea{{
			ID: 7, Attr: AreaAttrSpeed,
			LeftTopLat: 31000000, LeftTopLng: 121000000,
			RightBottomLat: 30000000, RightBottomLng: 122000000,
			MaxSpeed: 1000, Duration: 8, Name: "矩形A",
		}},
	}
	decoded := &MsgRegionRouteSet{}
	roundTrip(t, msg, decoded)
	require.Equal(t, msg.Rectangles[0], decoded.Rectangles[0])
}

func TestPolygonAreaRoundTrip(t *testing.T) {
	msg := &MsgRegionRouteSet{
		Header:      genMsgHeader(0x8604),
		SettingAttr: 4,
		Polygons: []PolygonArea{{
			ID: 9, Attr: AreaAttrTime,
			Vertices: []PolygonVertex{
				{Latitude: 31000000, Longitude: 121000000},
				{Latitude: 31000001, Longitude: 121000001},
				{Latitude: 31000002, Longitude: 121000002},
			},
			StartTime: "261028000000", EndTime: "261028120000",
			Name: "三角区",
		}},
	}
	decoded := &MsgRegionRouteSet{}
	roundTrip(t, msg, decoded)
	require.Equal(t, msg.Polygons[0], decoded.Polygons[0])
	require.Len(t, decoded.Polygons[0].Vertices, 3)
}

func TestRouteRoundTrip(t *testing.T) {
	msg := &MsgRegionRouteSet{
		Header:      genMsgHeader(0x8606),
		SettingAttr: 1,
		Routes: []Route{{
			ID: 5, Attr: AreaAttrTime | AreaAttrSpeed,
			StartTime: "261028000000", EndTime: "261028235959",
			Segments: []RouteSegment{
				{
					ID: 1, Attr: uint16(AreaAttrSpeed), Length: 1500,
					MaxSpeed: 600, Duration: 3,
					Vertices: []PolygonVertex{
						{Latitude: 31000000, Longitude: 121000000},
						{Latitude: 31000010, Longitude: 121000010},
					},
				},
				{
					ID: 2, Attr: 0, Length: 800,
					Vertices: []PolygonVertex{
						{Latitude: 31000010, Longitude: 121000010},
						{Latitude: 31000020, Longitude: 121000020},
						{Latitude: 31000030, Longitude: 121000030},
					},
				},
			},
			Name: "测试路线",
		}},
	}
	decoded := &MsgRegionRouteSet{}
	roundTrip(t, msg, decoded)
	require.Equal(t, msg.Routes[0], decoded.Routes[0])
	require.Len(t, decoded.Routes[0].Segments, 2)
	require.Len(t, decoded.Routes[0].Segments[1].Vertices, 3)
}

func TestRegionRouteSetValidation(t *testing.T) {
	// zero items rejected
	_, err := (&MsgRegionRouteSet{Header: genMsgHeader(0x8600), SettingAttr: 1}).Encode()
	require.Error(t, err)

	// trailing bytes rejected
	body := []byte{1, 1, 0x00, 0x00, 0x00, 0x01, 0x00, 0x00, 0x00, 0x00, 0x00, 0x00, 0x00, 0x00, 0x00, 0x00, 0x00, 0x00, 0x00, 0x00, 0x00, 0x64, 0x00, 0x01, 0x61, 0x41} // a minimal circle "A"
	// Append an extra byte
	body = append(body, 0xff)
	err = (&MsgRegionRouteSet{}).Decode(&PacketData{Header: genMsgHeader(0x8600), Body: body})
	require.Error(t, err)

	// invalid setting attr > 7 rejected
	err = (&MsgRegionRouteSet{}).Decode(&PacketData{Header: genMsgHeader(0x8600), Body: []byte{8, 1}})
	require.Error(t, err)

	// zero count rejected
	err = (&MsgRegionRouteSet{}).Decode(&PacketData{Header: genMsgHeader(0x8600), Body: []byte{1, 0}})
	require.Error(t, err)
}

func TestMsg0608RoundTripWithRecords(t *testing.T) {
	resp := &Msg0608{
		Header:    genMsgHeader(0x0608),
		QueryType: 1,
		Circles: []CircleArea{
			{ID: 1, Attr: 0, Latitude: 31000000, Longitude: 121000000, Radius: 500, Name: "圆1"},
			{ID: 2, Attr: AreaAttrSpeed, Latitude: 32000000, Longitude: 122000000, Radius: 300, MaxSpeed: 500, Duration: 5, Name: "圆2"},
		},
	}
	decoded := &Msg0608{}
	roundTrip(t, resp, decoded)
	require.Equal(t, resp.QueryType, decoded.QueryType)
	require.Equal(t, uint32(2), decoded.Count)
	require.Equal(t, resp.Circles, decoded.Circles)
}
