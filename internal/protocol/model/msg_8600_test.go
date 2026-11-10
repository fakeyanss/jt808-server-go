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
				ID: 42, Attr: AreaAttrTime | AreaAttrSpeed,
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
			Inflections: []RouteInflection{
				{
					ID: 1, SectionID: 11,
					Latitude: 31000000, Longitude: 121000000,
					Width: 12, Attr: RouteInflectionTime | RouteInflectionSpeed,
					OverlongThreshold: 100, UnderThreshold: 50,
					MaxSpeed: 600, Duration: 3, NightSpeed: 400,
				},
				{
					ID: 2, SectionID: 12,
					Latitude: 31000010, Longitude: 121000010,
					Width: 8,
				},
			},
			Name: "测试路线",
		}},
	}
	decoded := &MsgRegionRouteSet{}
	roundTrip(t, msg, decoded)
	require.Equal(t, msg.Routes[0], decoded.Routes[0])
	require.Len(t, decoded.Routes[0].Inflections, 2)
	require.Equal(t, uint16(400), decoded.Routes[0].Inflections[0].NightSpeed)
}

func TestRegionRouteSetValidation(t *testing.T) {
	// zero items rejected
	_, err := (&MsgRegionRouteSet{Header: genMsgHeader(0x8600), SettingAttr: 1}).Encode()
	require.Error(t, err)

	// trailing bytes rejected
	body := []byte{1, 1}
	body = hex.WriteDoubleWord(body, 1)
	body = hex.WriteWord(body, 0)
	body = hex.WriteDoubleWord(body, 0)
	body = hex.WriteDoubleWord(body, 0)
	body = hex.WriteDoubleWord(body, 0)
	body = hex.WriteWord(body, 0) // empty name
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

// TestRegionRouteSet2013CircleExactLayout 校验 2011/2013 圆形区域按
// JT/T 808-2013 表 57 编码：固定字段后直接跟时间/限速字段，不含夜间
// 最高速度和区域名称；即使调用方填充了这两个字段也不上线。
func TestRegionRouteSet2013CircleExactLayout(t *testing.T) {
	msg := &MsgRegionRouteSet{
		Header:      genMsgHeaderVersion(0x8600, Version2013),
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
			Name:       "不应上线",
		}},
	}
	decoded := &MsgRegionRouteSet{}
	pkt := roundTrip(t, msg, decoded)

	expected := []byte{1, 1}
	expected = hex.WriteDoubleWord(expected, 1)
	expected = hex.WriteWord(expected, uint16(AreaAttrSpeed))
	expected = hex.WriteDoubleWord(expected, 31000000)
	expected = hex.WriteDoubleWord(expected, 121000000)
	expected = hex.WriteDoubleWord(expected, 100)
	expected = hex.WriteWord(expected, 600)
	expected = hex.WriteByte(expected, 5)
	require.Equal(t, expected, pkt.Body)
	require.Equal(t, uint16(0), decoded.Circles[0].NightSpeed)
	require.Empty(t, decoded.Circles[0].Name)
}

// TestRegionRouteSet2013RectangleExactLayout 校验 2011/2013 矩形区域按
// JT/T 808-2013 表 61 编码，不含夜间最高速度与名称。
func TestRegionRouteSet2013RectangleExactLayout(t *testing.T) {
	msg := &MsgRegionRouteSet{
		Header:      genMsgHeaderVersion(0x8602, Version2013),
		SettingAttr: 2,
		Rectangles: []RectangleArea{{
			ID: 7, Attr: AreaAttrSpeed,
			LeftTopLat: 31000000, LeftTopLng: 121000000,
			RightBottomLat: 30000000, RightBottomLng: 122000000,
			MaxSpeed: 1000, Duration: 8,
			NightSpeed: 700, Name: "矩形A",
		}},
	}
	decoded := &MsgRegionRouteSet{}
	pkt := roundTrip(t, msg, decoded)

	expected := []byte{2, 1}
	expected = hex.WriteDoubleWord(expected, 7)
	expected = hex.WriteWord(expected, uint16(AreaAttrSpeed))
	expected = hex.WriteDoubleWord(expected, 31000000)
	expected = hex.WriteDoubleWord(expected, 121000000)
	expected = hex.WriteDoubleWord(expected, 30000000)
	expected = hex.WriteDoubleWord(expected, 122000000)
	expected = hex.WriteWord(expected, 1000)
	expected = hex.WriteByte(expected, 8)
	require.Equal(t, expected, pkt.Body)
	require.Equal(t, uint16(0), decoded.Rectangles[0].NightSpeed)
	require.Empty(t, decoded.Rectangles[0].Name)
}

// TestRegionRouteSet2013PolygonExactLayout 校验 2011/2013 多边形区域按
// JT/T 808-2013 表 63 编码：时间/限速字段在顶点项之前，且不含名称。
func TestRegionRouteSet2013PolygonExactLayout(t *testing.T) {
	msg := &MsgRegionRouteSet{
		Header:      genMsgHeaderVersion(0x8604, Version2013),
		SettingAttr: 4,
		Polygons: []PolygonArea{{
			ID: 9, Attr: AreaAttrTime | AreaAttrSpeed,
			StartTime: "261028000000", EndTime: "261028120000",
			MaxSpeed: 800, Duration: 6, NightSpeed: 500, Name: "不应上线",
			Vertices: []PolygonVertex{
				{Latitude: 31000000, Longitude: 121000000},
				{Latitude: 31000001, Longitude: 121000001},
			},
		}},
	}
	decoded := &MsgRegionRouteSet{}
	pkt := roundTrip(t, msg, decoded)

	expected := []byte{4, 1}
	expected = hex.WriteDoubleWord(expected, 9)
	expected = hex.WriteWord(expected, uint16(AreaAttrTime|AreaAttrSpeed))
	expected = hex.WriteBCD(expected, "261028000000")
	expected = hex.WriteBCD(expected, "261028120000")
	expected = hex.WriteWord(expected, 800)
	expected = hex.WriteByte(expected, 6)
	expected = hex.WriteWord(expected, 2)
	expected = hex.WriteDoubleWord(expected, 31000000)
	expected = hex.WriteDoubleWord(expected, 121000000)
	expected = hex.WriteDoubleWord(expected, 31000001)
	expected = hex.WriteDoubleWord(expected, 121000001)
	require.Equal(t, expected, pkt.Body)
	require.Equal(t, uint16(0), decoded.Polygons[0].NightSpeed)
	require.Empty(t, decoded.Polygons[0].Name)
	require.Len(t, decoded.Polygons[0].Vertices, 2)
}

// TestRegionRouteSet2013RouteExactLayout 校验 2011/2013 路线按
// JT/T 808-2013 表 66/68 编码为拐点项：拐点ID、路段ID、经纬度、
// 路段宽度、路段属性、条件阈值与限速字段，不含名称。
func TestRegionRouteSet2013RouteExactLayout(t *testing.T) {
	msg := &MsgRegionRouteSet{
		Header:      genMsgHeaderVersion(0x8606, Version2013),
		SettingAttr: 1,
		Routes: []Route{{
			ID: 5, Attr: AreaAttrTime,
			StartTime: "261028000000", EndTime: "261028235959",
			Name: "不应上线",
			Inflections: []RouteInflection{
				{
					ID: 11, SectionID: 22,
					Latitude: 31000000, Longitude: 121000000,
					Width: 10, Attr: RouteInflectionTime | RouteInflectionSpeed,
					OverlongThreshold: 100, UnderThreshold: 50,
					MaxSpeed: 600, Duration: 3,
				},
				{
					ID: 12, SectionID: 23,
					Latitude: 31000010, Longitude: 121000010,
					Width: 8,
				},
			},
		}},
	}
	decoded := &MsgRegionRouteSet{}
	pkt := roundTrip(t, msg, decoded)

	expected := []byte{1, 1}
	expected = hex.WriteDoubleWord(expected, 5)
	expected = hex.WriteWord(expected, uint16(AreaAttrTime))
	expected = hex.WriteBCD(expected, "261028000000")
	expected = hex.WriteBCD(expected, "261028235959")
	expected = hex.WriteWord(expected, 2)
	expected = hex.WriteDoubleWord(expected, 11)
	expected = hex.WriteDoubleWord(expected, 22)
	expected = hex.WriteDoubleWord(expected, 31000000)
	expected = hex.WriteDoubleWord(expected, 121000000)
	expected = hex.WriteByte(expected, 10)
	expected = hex.WriteByte(expected, uint8(RouteInflectionTime|RouteInflectionSpeed))
	expected = hex.WriteWord(expected, 100)
	expected = hex.WriteWord(expected, 50)
	expected = hex.WriteWord(expected, 600)
	expected = hex.WriteByte(expected, 3)
	expected = hex.WriteDoubleWord(expected, 12)
	expected = hex.WriteDoubleWord(expected, 23)
	expected = hex.WriteDoubleWord(expected, 31000010)
	expected = hex.WriteDoubleWord(expected, 121000010)
	expected = hex.WriteByte(expected, 8)
	expected = hex.WriteByte(expected, 0)
	require.Equal(t, expected, pkt.Body)
	require.Empty(t, decoded.Routes[0].Name)
	require.Len(t, decoded.Routes[0].Inflections, 2)
	require.Equal(t, msg.Routes[0].Inflections, decoded.Routes[0].Inflections)
}

// TestRegionRouteSetOldVersionIgnoresLegacyNightBit 校验 2011/2013 路径
// 不再用区域属性 bit15 触发任何字段。
func TestRegionRouteSetOldVersionIgnoresLegacyNightBit(t *testing.T) {
	msg := &MsgRegionRouteSet{
		Header:      genMsgHeaderVersion(0x8600, Version2013),
		SettingAttr: 1,
		Circles: []CircleArea{{
			ID: 1, Attr: AreaAttr(1 << 15),
			Latitude: 31000000, Longitude: 121000000, Radius: 100,
			NightSpeed: 500,
		}},
	}
	decoded := &MsgRegionRouteSet{}
	pkt := roundTrip(t, msg, decoded)
	require.Len(t, pkt.Body, 2+18)
	require.Equal(t, uint16(0), decoded.Circles[0].NightSpeed)
}

// TestMsg0608UsesVersionLayout 校验 0x0608 与设置消息共用记录模型时，
// 2011/2013 版本同样使用老版本布局。
func TestMsg0608UsesVersionLayout(t *testing.T) {
	t.Run("2013 circle without name", func(t *testing.T) {
		msg := &Msg0608{
			Header:    genMsgHeaderVersion(0x0608, Version2013),
			QueryType: 1,
			Circles: []CircleArea{{
				ID: 1, Attr: AreaAttrSpeed,
				Latitude: 31000000, Longitude: 121000000, Radius: 100,
				MaxSpeed: 600, Duration: 5, NightSpeed: 500, Name: "不应上线",
			}},
		}
		decoded := &Msg0608{}
		roundTrip(t, msg, decoded)
		require.Equal(t, uint32(1), decoded.Count)
		require.Equal(t, uint16(0), decoded.Circles[0].NightSpeed)
		require.Empty(t, decoded.Circles[0].Name)
	})

	t.Run("2013 route inflections", func(t *testing.T) {
		msg := &Msg0608{
			Header:    genMsgHeaderVersion(0x0608, Version2013),
			QueryType: 4,
			Routes: []Route{{
				ID: 5,
				Inflections: []RouteInflection{{
					ID: 11, SectionID: 22,
					Latitude: 31000000, Longitude: 121000000,
					Width: 10, Attr: RouteInflectionSpeed,
					MaxSpeed: 600, Duration: 3,
				}},
			}},
		}
		decoded := &Msg0608{}
		roundTrip(t, msg, decoded)
		require.Equal(t, uint32(1), decoded.Count)
		require.Len(t, decoded.Routes[0].Inflections, 1)
		require.Equal(t, msg.Routes[0].Inflections, decoded.Routes[0].Inflections)
	})

	t.Run("2019 keeps name", func(t *testing.T) {
		msg := &Msg0608{
			Header:    genMsgHeaderVersion(0x0608, Version2019),
			QueryType: 1,
			Circles: []CircleArea{{
				ID: 1, Attr: AreaAttrSpeed,
				Latitude: 31000000, Longitude: 121000000, Radius: 100,
				MaxSpeed: 600, Duration: 5, NightSpeed: 500, Name: "圆",
			}},
		}
		decoded := &Msg0608{}
		roundTrip(t, msg, decoded)
		require.Equal(t, uint16(500), decoded.Circles[0].NightSpeed)
		require.Equal(t, "圆", decoded.Circles[0].Name)
	})
}

// TestRegionRouteSet2013RejectsTruncated 校验老版本各记录在截断、
// 非法顶点数或条件字段缺失时返回错误而不是越界读取。
func TestRegionRouteSet2013RejectsTruncated(t *testing.T) {
	header := func(msgID uint16) *MsgHeader {
		return genMsgHeaderVersion(msgID, Version2013)
	}
	decode := func(msgID uint16, body []byte) error {
		return (&MsgRegionRouteSet{}).Decode(&PacketData{Header: header(msgID), Body: body})
	}

	// 圆形区域属性限速但缺少速度字段。
	circle := []byte{1, 1}
	circle = hex.WriteDoubleWord(circle, 1)
	circle = hex.WriteWord(circle, uint16(AreaAttrSpeed))
	circle = hex.WriteDoubleWord(circle, 0)
	circle = hex.WriteDoubleWord(circle, 0)
	circle = hex.WriteDoubleWord(circle, 0)
	require.Error(t, decode(0x8600, circle))

	// 多边形区域固定字段后截断。
	polygon := []byte{1, 1}
	polygon = hex.WriteDoubleWord(polygon, 1)
	polygon = hex.WriteWord(polygon, uint16(AreaAttrSpeed))
	polygon = hex.WriteWord(polygon, 800)
	polygon = hex.WriteByte(polygon, 5)
	require.Error(t, decode(0x8604, polygon))

	// 多边形顶点数为 0。
	zeroVertex := []byte{1, 1}
	zeroVertex = hex.WriteDoubleWord(zeroVertex, 1)
	zeroVertex = hex.WriteWord(zeroVertex, 0)
	zeroVertex = hex.WriteWord(zeroVertex, 0)
	require.Error(t, decode(0x8604, zeroVertex))

	// 路线拐点项固定字段截断。
	route := []byte{1, 1}
	route = hex.WriteDoubleWord(route, 1)
	route = hex.WriteWord(route, 0)
	route = hex.WriteWord(route, 1)
	route = append(route, make([]byte, 17)...)
	require.Error(t, decode(0x8606, route))

	// 拐点属性声明的阈值字段缺失。
	timeMissing := []byte{1, 1}
	timeMissing = hex.WriteDoubleWord(timeMissing, 1)
	timeMissing = hex.WriteWord(timeMissing, 0)
	timeMissing = hex.WriteWord(timeMissing, 1)
	timeMissing = hex.WriteDoubleWord(timeMissing, 11)
	timeMissing = hex.WriteDoubleWord(timeMissing, 22)
	timeMissing = hex.WriteDoubleWord(timeMissing, 0)
	timeMissing = hex.WriteDoubleWord(timeMissing, 0)
	timeMissing = hex.WriteByte(timeMissing, 10)
	timeMissing = hex.WriteByte(timeMissing, uint8(RouteInflectionTime))
	require.Error(t, decode(0x8606, timeMissing))

	// 拐点属性声明的限速字段缺失。
	speedMissing := []byte{1, 1}
	speedMissing = hex.WriteDoubleWord(speedMissing, 1)
	speedMissing = hex.WriteWord(speedMissing, 0)
	speedMissing = hex.WriteWord(speedMissing, 1)
	speedMissing = hex.WriteDoubleWord(speedMissing, 11)
	speedMissing = hex.WriteDoubleWord(speedMissing, 22)
	speedMissing = hex.WriteDoubleWord(speedMissing, 0)
	speedMissing = hex.WriteDoubleWord(speedMissing, 0)
	speedMissing = hex.WriteByte(speedMissing, 10)
	speedMissing = hex.WriteByte(speedMissing, uint8(RouteInflectionSpeed))
	speedMissing = hex.WriteWord(speedMissing, 600)
	require.Error(t, decode(0x8606, speedMissing))

	// 路线属性声明含时间但时间字段不完整。
	routeTimeMissing := []byte{1, 1}
	routeTimeMissing = hex.WriteDoubleWord(routeTimeMissing, 1)
	routeTimeMissing = hex.WriteWord(routeTimeMissing, uint16(AreaAttrTime))
	routeTimeMissing = append(routeTimeMissing, make([]byte, 11)...)
	require.Error(t, decode(0x8606, routeTimeMissing))
}

// TestRegionRouteSet2019Layouts 校验 2019 版本名称长度使用 WORD，
// 夜间最高速度随限速位出现。
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

	t.Run("bit15 is gnss flag, not night speed", func(t *testing.T) {
		msg := &MsgRegionRouteSet{
			Header:      genMsgHeaderVersion(0x8600, Version2019),
			SettingAttr: 1,
			Circles: []CircleArea{{
				ID:         1,
				Attr:       AreaAttr(1 << 15),
				Latitude:   31000000,
				Longitude:  121000000,
				Radius:     100,
				NightSpeed: 500,
				Name:       name,
			}},
		}
		decoded := &MsgRegionRouteSet{}
		pkt := roundTrip(t, msg, decoded)
		// 2019 版本 bit15 为 GNSS 采集标志，不触发夜间速度字段
		require.Len(t, pkt.Body, 2+18+2+len(gbk))
		require.Equal(t, uint16(0), decoded.Circles[0].NightSpeed)
	})
}

// TestRegionRouteSet2019PolygonExactLayout 锁定 2019 多边形区域按标准编码：
// 时间/限速字段在顶点之前，夜间最高速度在顶点之后、名称之前。
func TestRegionRouteSet2019PolygonExactLayout(t *testing.T) {
	msg := &MsgRegionRouteSet{
		Header:      genMsgHeaderVersion(0x8604, Version2019),
		SettingAttr: 4,
		Polygons: []PolygonArea{{
			ID: 9, Attr: AreaAttrSpeed,
			MaxSpeed: 800, Duration: 6, NightSpeed: 500,
			Vertices: []PolygonVertex{{Latitude: 31000000, Longitude: 121000000}},
		}},
	}
	decoded := &MsgRegionRouteSet{}
	pkt := roundTrip(t, msg, decoded)

	expected := []byte{4, 1}
	expected = hex.WriteDoubleWord(expected, 9)
	expected = hex.WriteWord(expected, uint16(AreaAttrSpeed))
	expected = hex.WriteWord(expected, 800)
	expected = hex.WriteByte(expected, 6)
	expected = hex.WriteWord(expected, 1)
	expected = hex.WriteDoubleWord(expected, 31000000)
	expected = hex.WriteDoubleWord(expected, 121000000)
	expected = hex.WriteWord(expected, 500)
	expected = hex.WriteWord(expected, 0)
	require.Equal(t, expected, pkt.Body)
	require.Equal(t, uint16(500), decoded.Polygons[0].NightSpeed)
}

// TestRegionRouteSet2019RouteExactLayout 锁定 2019 路线按标准编码为拐点项：
// 限速块内包含夜间最高速度，路线尾部包含 WORD 名称长度。
func TestRegionRouteSet2019RouteExactLayout(t *testing.T) {
	const name = "路线"
	gbk := hex.WriteGBK(nil, name)
	msg := &MsgRegionRouteSet{
		Header:      genMsgHeaderVersion(0x8606, Version2019),
		SettingAttr: 1,
		Routes: []Route{{
			ID: 5, Attr: AreaAttrTime,
			StartTime: "261028000000", EndTime: "261028235959",
			Name: name,
			Inflections: []RouteInflection{{
				ID: 11, SectionID: 22,
				Latitude: 31000000, Longitude: 121000000,
				Width: 10, Attr: RouteInflectionTime | RouteInflectionSpeed,
				OverlongThreshold: 100, UnderThreshold: 50,
				MaxSpeed: 600, Duration: 3, NightSpeed: 400,
			}},
		}},
	}
	decoded := &MsgRegionRouteSet{}
	pkt := roundTrip(t, msg, decoded)

	expected := []byte{1, 1}
	expected = hex.WriteDoubleWord(expected, 5)
	expected = hex.WriteWord(expected, uint16(AreaAttrTime))
	expected = hex.WriteBCD(expected, "261028000000")
	expected = hex.WriteBCD(expected, "261028235959")
	expected = hex.WriteWord(expected, 1)
	expected = hex.WriteDoubleWord(expected, 11)
	expected = hex.WriteDoubleWord(expected, 22)
	expected = hex.WriteDoubleWord(expected, 31000000)
	expected = hex.WriteDoubleWord(expected, 121000000)
	expected = hex.WriteByte(expected, 10)
	expected = hex.WriteByte(expected, uint8(RouteInflectionTime|RouteInflectionSpeed))
	expected = hex.WriteWord(expected, 100)
	expected = hex.WriteWord(expected, 50)
	expected = hex.WriteWord(expected, 600)
	expected = hex.WriteByte(expected, 3)
	expected = hex.WriteWord(expected, 400)
	expected = hex.WriteWord(expected, uint16(len(gbk)))
	expected = append(expected, gbk...)
	require.Equal(t, expected, pkt.Body)
	require.Equal(t, name, decoded.Routes[0].Name)
	require.Equal(t, uint16(400), decoded.Routes[0].Inflections[0].NightSpeed)
}

// TestRegionRouteSet2019RejectsTruncated 校验 2019 多边形/路线在截断、
// 非法顶点数、条件字段缺失或名称越界时返回错误。
func TestRegionRouteSet2019RejectsTruncated(t *testing.T) {
	decode := func(msgID uint16, body []byte) error {
		return (&MsgRegionRouteSet{}).Decode(&PacketData{Header: genMsgHeaderVersion(msgID, Version2019), Body: body})
	}

	// 多边形：固定字段后声明限速但缺少速度/持续时间。
	polygonShort := []byte{1, 1}
	polygonShort = hex.WriteDoubleWord(polygonShort, 1)
	polygonShort = hex.WriteWord(polygonShort, uint16(AreaAttrSpeed))
	require.Error(t, decode(0x8604, polygonShort))

	// 多边形：顶点数为 0。
	polygonZeroVertex := []byte{1, 1}
	polygonZeroVertex = hex.WriteDoubleWord(polygonZeroVertex, 1)
	polygonZeroVertex = hex.WriteWord(polygonZeroVertex, 0)
	polygonZeroVertex = hex.WriteWord(polygonZeroVertex, 0)
	require.Error(t, decode(0x8604, polygonZeroVertex))

	// 多边形：顶点项被截断。
	polygonTruncatedVertex := []byte{1, 1}
	polygonTruncatedVertex = hex.WriteDoubleWord(polygonTruncatedVertex, 1)
	polygonTruncatedVertex = hex.WriteWord(polygonTruncatedVertex, 0)
	polygonTruncatedVertex = hex.WriteWord(polygonTruncatedVertex, 1)
	polygonTruncatedVertex = append(polygonTruncatedVertex, make([]byte, 7)...)
	require.Error(t, decode(0x8604, polygonTruncatedVertex))

	// 多边形：名称长度越界。
	polygonBadName := []byte{1, 1}
	polygonBadName = hex.WriteDoubleWord(polygonBadName, 1)
	polygonBadName = hex.WriteWord(polygonBadName, 0)
	polygonBadName = hex.WriteWord(polygonBadName, 1)
	polygonBadName = hex.WriteDoubleWord(polygonBadName, 0)
	polygonBadName = hex.WriteDoubleWord(polygonBadName, 0)
	polygonBadName = hex.WriteWord(polygonBadName, 100)
	polygonBadName = append(polygonBadName, 'x')
	require.Error(t, decode(0x8604, polygonBadName))

	// 路线：拐点固定字段截断。
	routeShort := []byte{1, 1}
	routeShort = hex.WriteDoubleWord(routeShort, 1)
	routeShort = hex.WriteWord(routeShort, 0)
	routeShort = hex.WriteWord(routeShort, 1)
	routeShort = append(routeShort, make([]byte, 17)...)
	require.Error(t, decode(0x8606, routeShort))

	// 路线：拐点限速块缺少夜间最高速度。
	routeNightMissing := []byte{1, 1}
	routeNightMissing = hex.WriteDoubleWord(routeNightMissing, 1)
	routeNightMissing = hex.WriteWord(routeNightMissing, 0)
	routeNightMissing = hex.WriteWord(routeNightMissing, 1)
	routeNightMissing = hex.WriteDoubleWord(routeNightMissing, 11)
	routeNightMissing = hex.WriteDoubleWord(routeNightMissing, 22)
	routeNightMissing = hex.WriteDoubleWord(routeNightMissing, 0)
	routeNightMissing = hex.WriteDoubleWord(routeNightMissing, 0)
	routeNightMissing = hex.WriteByte(routeNightMissing, 10)
	routeNightMissing = hex.WriteByte(routeNightMissing, uint8(RouteInflectionSpeed))
	routeNightMissing = hex.WriteWord(routeNightMissing, 600)
	routeNightMissing = hex.WriteByte(routeNightMissing, 3)
	require.Error(t, decode(0x8606, routeNightMissing))

	// 路线：拐点阈值字段截断。
	routeThresholdMissing := []byte{1, 1}
	routeThresholdMissing = hex.WriteDoubleWord(routeThresholdMissing, 1)
	routeThresholdMissing = hex.WriteWord(routeThresholdMissing, 0)
	routeThresholdMissing = hex.WriteWord(routeThresholdMissing, 1)
	routeThresholdMissing = hex.WriteDoubleWord(routeThresholdMissing, 11)
	routeThresholdMissing = hex.WriteDoubleWord(routeThresholdMissing, 22)
	routeThresholdMissing = hex.WriteDoubleWord(routeThresholdMissing, 0)
	routeThresholdMissing = hex.WriteDoubleWord(routeThresholdMissing, 0)
	routeThresholdMissing = hex.WriteByte(routeThresholdMissing, 10)
	routeThresholdMissing = hex.WriteByte(routeThresholdMissing, uint8(RouteInflectionTime))
	routeThresholdMissing = hex.WriteWord(routeThresholdMissing, 100)
	require.Error(t, decode(0x8606, routeThresholdMissing))

	// 路线：名称长度越界（最小合法体 2+4+2+2+2，此处少 1 字节名称）。
	routeBadName := []byte{1, 1}
	routeBadName = hex.WriteDoubleWord(routeBadName, 1)
	routeBadName = hex.WriteWord(routeBadName, 0)
	routeBadName = hex.WriteWord(routeBadName, 0)
	routeBadName = hex.WriteWord(routeBadName, 100)
	routeBadName = append(routeBadName, 'x')
	require.Error(t, decode(0x8606, routeBadName))
}

// TestRegionRouteSetRouteNightSpeedByVersion 锁定 2019 路线拐点携带夜间
// 速度，而 2013 路径不存在夜间速度字段。
func TestRegionRouteSetRouteNightSpeedByVersion(t *testing.T) {
	t.Run("2013 route has no night speed", func(t *testing.T) {
		msg := &MsgRegionRouteSet{
			Header:      genMsgHeaderVersion(0x8606, Version2013),
			SettingAttr: 1,
			Routes: []Route{{
				ID: 5,
				Inflections: []RouteInflection{{
					ID: 1, SectionID: 2,
					Attr:     RouteInflectionSpeed | RouteInflectionTime,
					MaxSpeed: 600, Duration: 3, NightSpeed: 400,
					OverlongThreshold: 100, UnderThreshold: 50,
				}},
			}},
		}
		decoded := &MsgRegionRouteSet{}
		pkt := roundTrip(t, msg, decoded)
		require.Len(t, decoded.Routes[0].Inflections, 1)
		require.Equal(t, uint16(0), decoded.Routes[0].Inflections[0].NightSpeed)
		// 2013 限速块只有 3 字节，不包含夜间速度。
		require.Len(t, pkt.Body, 2+4+2+2+18+4+3)
	})

	t.Run("2019 route keeps night speed", func(t *testing.T) {
		msg := &MsgRegionRouteSet{
			Header:      genMsgHeaderVersion(0x8606, Version2019),
			SettingAttr: 1,
			Routes: []Route{{
				ID: 5,
				Inflections: []RouteInflection{{
					ID: 1, SectionID: 2,
					Attr:     RouteInflectionSpeed,
					MaxSpeed: 600, Duration: 3, NightSpeed: 400,
				}},
				Name: "路线",
			}},
		}
		decoded := &MsgRegionRouteSet{}
		roundTrip(t, msg, decoded)
		require.Equal(t, uint16(400), decoded.Routes[0].Inflections[0].NightSpeed)
		require.Equal(t, "路线", decoded.Routes[0].Name)
	})
}

// TestRegionRouteSetRejectsTruncatedBody 校验截断消息体不会触发越界读取。
func TestRegionRouteSetRejectsTruncatedBody(t *testing.T) {
	header := genMsgHeaderVersion(0x8600, Version2019)
	require.Error(t, (&MsgRegionRouteSet{}).Decode(&PacketData{Header: header, Body: []byte{1, 1, 0, 0, 0}}))
}
