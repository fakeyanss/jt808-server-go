package model

import (
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/fakeyanss/jt808-server-go/internal/codec/hex"
)

func TestMsg8600CircleRoundTrip(t *testing.T) {
	set := &MsgRegionRouteSet{
		Header:      genMsgHeader(0x8600),
		SettingAttr: 1,
		Circles: []CircleArea{{
			ID: 1, Attr: AreaAttrTime | AreaAttrSpeed,
			Latitude: 31000000, Longitude: 121000000, Radius: 500,
			StartTime: "261028000000", EndTime: "261028235959",
			MaxSpeed: 600, Duration: 10, Name: "测试圆",
		}},
	}
	decoded := &MsgRegionRouteSet{}
	roundTrip(t, set, decoded)
	require.Equal(t, set.SettingAttr, decoded.SettingAttr)
	require.Equal(t, set.Circles[0].ID, decoded.Circles[0].ID)
	require.Equal(t, set.Circles[0].Name, decoded.Circles[0].Name)
	require.Equal(t, set.Circles[0].StartTime, decoded.Circles[0].StartTime)
}

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

// TestRegionRouteSet2013Layouts 校验 2011/2013 版本保持的既有布局：
// 名称长度 BYTE，夜间最高速度由区域属性 bit15 触发。
func TestRegionRouteSet2013Layouts(t *testing.T) {
	const name = "圆"
	gbk := hex.WriteGBK(nil, name)
	msg := &MsgRegionRouteSet{
		Header:      genMsgHeaderVersion(0x8600, Version2013),
		SettingAttr: 1,
		Circles: []CircleArea{{
			ID:         1,
			Attr:       AreaAttrSpeed | AreaAttrNightSpd,
			Latitude:   31000000,
			Longitude:  121000000,
			Radius:     100,
			MaxSpeed:   600,
			Duration:   5,
			NightSpeed: 500,
			Name:       name,
		}},
	}
	decoded := &MsgRegionRouteSet{}
	pkt := roundTrip(t, msg, decoded)
	// 设置属性(1)+区域数(1)+区域固定字段(18)+速度(3)+夜间速度(2)+名称长度(1)+名称
	require.Len(t, pkt.Body, 2+18+5+1+len(gbk))
	require.Equal(t, uint16(500), decoded.Circles[0].NightSpeed)
	require.Equal(t, name, decoded.Circles[0].Name)
}

// TestRegionRouteSet2019Layouts 校验 2019 版本名称长度使用 WORD，
// 夜间最高速度改由区域属性 bit1（限速）触发。
func TestRegionRouteSet2019Layouts(t *testing.T) {
	const name = "圆"
	gbk := hex.WriteGBK(nil, name)

	t.Run("speed bit carries night speed", func(t *testing.T) {
		msg := &MsgRegionRouteSet{
			Header:      genMsgHeaderVersion(0x8600, Version2019),
			SettingAttr: 1,
			Circles: []CircleArea{{
				ID:         1,
				Attr:       AreaAttrSpeed,
				Latitude:   31000000,
				Longitude:  121000000,
				Radius:     100,
				MaxSpeed:   600,
				Duration:   5,
				NightSpeed: 500,
				Name:       name,
			}},
		}
		decoded := &MsgRegionRouteSet{}
		pkt := roundTrip(t, msg, decoded)
		// 固定字段(18)+速度与超速持续时间(3)+夜间速度(2)+名称长度 WORD(2)+名称
		require.Len(t, pkt.Body, 2+18+5+2+len(gbk))
		require.Equal(t, uint16(500), decoded.Circles[0].NightSpeed)
		require.Equal(t, name, decoded.Circles[0].Name)
	})

	t.Run("night bit is ignored", func(t *testing.T) {
		msg := &MsgRegionRouteSet{
			Header:      genMsgHeaderVersion(0x8600, Version2019),
			SettingAttr: 1,
			Circles: []CircleArea{{
				ID:         1,
				Attr:       AreaAttrNightSpd,
				Latitude:   31000000,
				Longitude:  121000000,
				Radius:     100,
				NightSpeed: 500,
				Name:       name,
			}},
		}
		decoded := &MsgRegionRouteSet{}
		pkt := roundTrip(t, msg, decoded)
		// 2019 版本 bit15 为 GNSS 采集标志，不再触发夜间速度字段
		require.Len(t, pkt.Body, 2+18+2+len(gbk))
		require.Equal(t, uint16(0), decoded.Circles[0].NightSpeed)
	})
}

// TestRegionRouteSetRouteNightSpeedByVersion 校验收路段的夜间最高速度触发位。
func TestRegionRouteSetRouteNightSpeedByVersion(t *testing.T) {
	seg := func(attr uint16) RouteSegment {
		return RouteSegment{
			ID:         1,
			Attr:       attr,
			Length:     100,
			MaxSpeed:   600,
			Duration:   3,
			NightSpeed: 400,
			Vertices: []PolygonVertex{
				{Latitude: 31000000, Longitude: 121000000},
				{Latitude: 31100000, Longitude: 121100000},
			},
		}
	}

	t.Run("2013 bit15", func(t *testing.T) {
		msg := &MsgRegionRouteSet{
			Header:      genMsgHeaderVersion(0x8606, Version2013),
			SettingAttr: 1,
			Routes: []Route{{
				ID:       5,
				Segments: []RouteSegment{seg(uint16(AreaAttrSpeed | AreaAttrNightSpd))},
				Name:     "路线",
			}},
		}
		decoded := &MsgRegionRouteSet{}
		roundTrip(t, msg, decoded)
		require.Equal(t, uint16(400), decoded.Routes[0].Segments[0].NightSpeed)
		require.Equal(t, "路线", decoded.Routes[0].Name)
	})

	t.Run("2019 bit1", func(t *testing.T) {
		msg := &MsgRegionRouteSet{
			Header:      genMsgHeaderVersion(0x8606, Version2019),
			SettingAttr: 1,
			Routes: []Route{{
				ID:       5,
				Segments: []RouteSegment{seg(uint16(AreaAttrSpeed))},
				Name:     "路线",
			}},
		}
		decoded := &MsgRegionRouteSet{}
		roundTrip(t, msg, decoded)
		require.Equal(t, uint16(400), decoded.Routes[0].Segments[0].NightSpeed)
		require.Equal(t, "路线", decoded.Routes[0].Name)
	})
}

// TestRegionRouteSetRejectsTruncatedBody 校验截断消息体不会触发越界读取。
func TestRegionRouteSetRejectsTruncatedBody(t *testing.T) {
	header := genMsgHeaderVersion(0x8600, Version2019)
	require.Error(t, (&MsgRegionRouteSet{}).Decode(&PacketData{Header: header, Body: []byte{1, 1, 0, 0, 0}}))
}
