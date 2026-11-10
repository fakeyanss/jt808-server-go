package model

import (
	"github.com/pkg/errors"

	"github.com/fakeyanss/jt808-server-go/internal/codec/hex"
)

// RegionRouteType discriminates the four area/route families that share the
// same message envelope under message IDs 0x8600/0x8602/0x8604/0x8606.
type RegionRouteType uint8

const (
	RegionRouteCircle  RegionRouteType = 1
	RegionRouteRect    RegionRouteType = 2
	RegionRoutePolygon RegionRouteType = 3
	RegionRouteRoute   RegionRouteType = 4
)

// AreaSettingAttr is the byte at the start of a set message body.
// bit0=update, bit1=append, bit2=modify.
type AreaSettingAttr uint8

// AreaAttr 是区域/路线的 16 位属性字段。
// bit0=时间，bit1=限速，bit2=开门；bit15 在 2019 中表示 GNSS 采集标志，
// 与夜间最高速度无关。
type AreaAttr uint16

const (
	AreaAttrTime  AreaAttr = 1 << 0
	AreaAttrSpeed AreaAttr = 1 << 1
	AreaAttrDoor  AreaAttr = 1 << 2
)

func (a AreaAttr) HasTime() bool  { return a&AreaAttrTime != 0 }
func (a AreaAttr) HasSpeed() bool { return a&AreaAttrSpeed != 0 }
func (a AreaAttr) HasDoor() bool  { return a&AreaAttrDoor != 0 }

// CircleArea is one item of a 0x8600 set circular area message.
type CircleArea struct {
	ID         uint32   `json:"id"`
	Attr       AreaAttr `json:"attr"`
	Latitude   uint32   `json:"latitude"`
	Longitude  uint32   `json:"longitude"`
	Radius     uint32   `json:"radius"`
	StartTime  string   `json:"startTime,omitempty"`
	EndTime    string   `json:"endTime,omitempty"`
	MaxSpeed   uint16   `json:"maxSpeed,omitempty"`
	Duration   uint8    `json:"duration,omitempty"`
	NightSpeed uint16   `json:"nightSpeed,omitempty"`
	Name       string   `json:"name"`
}

// RectangleArea is one item of a 0x8602 set rectangular area message.
type RectangleArea struct {
	ID             uint32   `json:"id"`
	Attr           AreaAttr `json:"attr"`
	LeftTopLat     uint32   `json:"leftTopLat"`
	LeftTopLng     uint32   `json:"leftTopLng"`
	RightBottomLat uint32   `json:"rightBottomLat"`
	RightBottomLng uint32   `json:"rightBottomLng"`
	StartTime      string   `json:"startTime,omitempty"`
	EndTime        string   `json:"endTime,omitempty"`
	MaxSpeed       uint16   `json:"maxSpeed,omitempty"`
	Duration       uint8    `json:"duration,omitempty"`
	NightSpeed     uint16   `json:"nightSpeed,omitempty"`
	Name           string   `json:"name"`
}

// PolygonVertex is a single vertex of a polygon area.
type PolygonVertex struct {
	Latitude  uint32 `json:"latitude"`
	Longitude uint32 `json:"longitude"`
}

// PolygonArea is one item of a 0x8604 set polygonal area message.
type PolygonArea struct {
	ID         uint32          `json:"id"`
	Attr       AreaAttr        `json:"attr"`
	Vertices   []PolygonVertex `json:"vertices"`
	StartTime  string          `json:"startTime,omitempty"`
	EndTime    string          `json:"endTime,omitempty"`
	MaxSpeed   uint16          `json:"maxSpeed,omitempty"`
	Duration   uint8           `json:"duration,omitempty"`
	NightSpeed uint16          `json:"nightSpeed,omitempty"`
	Name       string          `json:"name"`
}

// RouteSegment 是 2019 版本路线报文中“路段 + 顶点列表”的容器。
// 非 2019 版本不使用该结构，改用 RouteInflection。
type RouteSegment struct {
	ID         uint32          `json:"id"`
	Attr       uint16          `json:"attr"`
	Length     uint16          `json:"length"`
	MaxSpeed   uint16          `json:"maxSpeed,omitempty"`
	Duration   uint8           `json:"duration,omitempty"`
	NightSpeed uint16          `json:"nightSpeed,omitempty"`
	Vertices   []PolygonVertex `json:"vertices"`
}

// RouteInflectionAttr 是 2011/2013 路线拐点项的路段属性字节。
// bit0=行驶时间（超长/不足阈值），bit1=限速。
type RouteInflectionAttr uint8

const (
	RouteInflectionTime  RouteInflectionAttr = 1 << 0
	RouteInflectionSpeed RouteInflectionAttr = 1 << 1
)

// RouteInflection 是 2011/2013 路线报文中的拐点项（JT/T 808-2013 表 68）。
type RouteInflection struct {
	ID                uint32              `json:"id"`
	SectionID         uint32              `json:"sectionId"`
	Latitude          uint32              `json:"latitude"`
	Longitude         uint32              `json:"longitude"`
	Width             uint8               `json:"width"`
	Attr              RouteInflectionAttr `json:"attr"`
	OverlongThreshold uint16              `json:"overlongThreshold,omitempty"`
	UnderThreshold    uint16              `json:"underThreshold,omitempty"`
	MaxSpeed          uint16              `json:"maxSpeed,omitempty"`
	Duration          uint8               `json:"duration,omitempty"`
}

// Route is one item of a 0x8606 set route message.
// 2019 版本使用 Segments；2011/2013 版本使用标准定义的拐点项 Inflections。
type Route struct {
	ID          uint32            `json:"id"`
	Attr        AreaAttr          `json:"attr"`
	StartTime   string            `json:"startTime,omitempty"`
	EndTime     string            `json:"endTime,omitempty"`
	Segments    []RouteSegment    `json:"segments,omitempty"`
	Inflections []RouteInflection `json:"inflections,omitempty"`
	Name        string            `json:"name"`
}

// regionRouteTypeFromMsgID maps a platform message ID to its area/route family.
func regionRouteTypeFromMsgID(msgID uint16) (RegionRouteType, bool) {
	switch msgID {
	case 0x8600:
		return RegionRouteCircle, true
	case 0x8602:
		return RegionRouteRect, true
	case 0x8604:
		return RegionRoutePolygon, true
	case 0x8606:
		return RegionRouteRoute, true
	default:
		return 0, false
	}
}

func regionRouteTypeFromQueryType(qt uint8) (RegionRouteType, bool) {
	switch qt {
	case 1:
		return RegionRouteCircle, true
	case 2:
		return RegionRouteRect, true
	case 3:
		return RegionRoutePolygon, true
	case 4:
		return RegionRouteRoute, true
	default:
		return 0, false
	}
}

// RegionRouteDeleteType maps a delete message ID (0x8601/0x8603/0x8605/0x8607)
// to its area/route family.
func RegionRouteDeleteType(msgID uint16) (RegionRouteType, bool) {
	switch msgID {
	case 0x8601:
		return RegionRouteCircle, true
	case 0x8603:
		return RegionRouteRect, true
	case 0x8605:
		return RegionRoutePolygon, true
	case 0x8607:
		return RegionRouteRoute, true
	default:
		return 0, false
	}
}

// areaAttrFieldsLength 返回时间/速度条件字段占用的字节数。
// 2019 版本在最高速度与超速持续时间之后追加夜间最高速度；2011/2013
// 版本没有夜间最高速度字段（JT/T 808-2013 表 57/61/63）。
func areaAttrFieldsLength(attr AreaAttr, is2019 bool) int {
	n := 0
	if attr.HasTime() {
		n += 12
	}
	if attr.HasSpeed() {
		n += 3
		if is2019 {
			n += 2
		}
	}
	return n
}

// encodeAreaAttrFields writes the conditional time/speed fields that are shared
// by circle, rectangle and polygon items. 夜间最高速度仅存在于 2019 版本。
func encodeAreaAttrFields(pkt []byte, attr AreaAttr, start, end string, maxSpeed uint16, duration uint8, nightSpeed uint16, is2019 bool) []byte {
	if attr.HasTime() {
		pkt = hex.WriteBCD(pkt, start)
		pkt = hex.WriteBCD(pkt, end)
	}
	if attr.HasSpeed() {
		pkt = hex.WriteWord(pkt, maxSpeed)
		pkt = hex.WriteByte(pkt, duration)
		if is2019 {
			pkt = hex.WriteWord(pkt, nightSpeed)
		}
	}
	return pkt
}

func decodeAreaAttrFields(pkt []byte, idx *int, attr AreaAttr, is2019 bool) (start, end string, maxSpeed uint16, duration uint8, nightSpeed uint16) {
	if attr.HasTime() {
		start = hex.ReadBCD(pkt, idx, 6)
		end = hex.ReadBCD(pkt, idx, 6)
	}
	if attr.HasSpeed() {
		maxSpeed = hex.ReadWord(pkt, idx)
		duration = hex.ReadByte(pkt, idx)
		if is2019 {
			nightSpeed = hex.ReadWord(pkt, idx)
		}
	}
	return
}

// encodeName 写入区域/路线名称，仅 2019 版本使用，长度字段为 WORD。
func encodeName(pkt []byte, name string) []byte {
	gbk := hex.WriteGBK(nil, name)
	pkt = hex.WriteWord(pkt, uint16(len(gbk)))
	pkt = append(pkt, gbk...)
	return pkt
}

func decodeName(pkt []byte, idx *int) (string, error) {
	if *idx+2 > len(pkt) {
		return "", errors.New("name length missing")
	}
	n := int(hex.ReadWord(pkt, idx))
	if *idx+n > len(pkt) {
		return "", errors.New("name bytes exceed payload")
	}
	if n == 0 {
		return "", nil
	}
	return hex.ReadGBK(pkt, idx, n), nil
}

func encodeVertices(pkt []byte, verts []PolygonVertex) []byte {
	pkt = hex.WriteWord(pkt, uint16(len(verts)))
	for _, v := range verts {
		pkt = hex.WriteDoubleWord(pkt, v.Latitude)
		pkt = hex.WriteDoubleWord(pkt, v.Longitude)
	}
	return pkt
}

func decodeVertices(pkt []byte, idx *int) ([]PolygonVertex, error) {
	if *idx+2 > len(pkt) {
		return nil, errors.New("vertex count missing")
	}
	cnt := int(hex.ReadWord(pkt, idx))
	if cnt == 0 {
		return nil, errors.New("vertex count must be nonzero")
	}
	if *idx+cnt*8 > len(pkt) {
		return nil, errors.New("vertex items exceed payload")
	}
	verts := make([]PolygonVertex, 0, cnt)
	for i := 0; i < cnt; i++ {
		verts = append(verts, PolygonVertex{
			Latitude:  hex.ReadDoubleWord(pkt, idx),
			Longitude: hex.ReadDoubleWord(pkt, idx),
		})
	}
	return verts, nil
}

// ---- Circle ----

func (c *CircleArea) encode(is2019 bool) []byte {
	pkt := hex.WriteDoubleWord(nil, c.ID)
	pkt = hex.WriteWord(pkt, uint16(c.Attr))
	pkt = hex.WriteDoubleWord(pkt, c.Latitude)
	pkt = hex.WriteDoubleWord(pkt, c.Longitude)
	pkt = hex.WriteDoubleWord(pkt, c.Radius)
	pkt = encodeAreaAttrFields(pkt, c.Attr, c.StartTime, c.EndTime, c.MaxSpeed, c.Duration, c.NightSpeed, is2019)
	if is2019 {
		pkt = encodeName(pkt, c.Name)
	}
	return pkt
}

func decodeCircle(pkt []byte, idx *int, is2019 bool) (*CircleArea, error) {
	const fixedLen = 4 + 2 + 4 + 4 + 4
	if len(pkt)-*idx < fixedLen {
		return nil, errors.New("circle area too short")
	}
	c := &CircleArea{}
	c.ID = hex.ReadDoubleWord(pkt, idx)
	c.Attr = AreaAttr(hex.ReadWord(pkt, idx))
	c.Latitude = hex.ReadDoubleWord(pkt, idx)
	c.Longitude = hex.ReadDoubleWord(pkt, idx)
	c.Radius = hex.ReadDoubleWord(pkt, idx)
	if len(pkt)-*idx < areaAttrFieldsLength(c.Attr, is2019) {
		return nil, errors.New("circle area attr fields exceed payload")
	}
	c.StartTime, c.EndTime, c.MaxSpeed, c.Duration, c.NightSpeed = decodeAreaAttrFields(pkt, idx, c.Attr, is2019)
	if is2019 {
		name, err := decodeName(pkt, idx)
		if err != nil {
			return nil, err
		}
		c.Name = name
	}
	return c, nil
}

// ---- Rectangle ----

func (r *RectangleArea) encode(is2019 bool) []byte {
	pkt := hex.WriteDoubleWord(nil, r.ID)
	pkt = hex.WriteWord(pkt, uint16(r.Attr))
	pkt = hex.WriteDoubleWord(pkt, r.LeftTopLat)
	pkt = hex.WriteDoubleWord(pkt, r.LeftTopLng)
	pkt = hex.WriteDoubleWord(pkt, r.RightBottomLat)
	pkt = hex.WriteDoubleWord(pkt, r.RightBottomLng)
	pkt = encodeAreaAttrFields(pkt, r.Attr, r.StartTime, r.EndTime, r.MaxSpeed, r.Duration, r.NightSpeed, is2019)
	if is2019 {
		pkt = encodeName(pkt, r.Name)
	}
	return pkt
}

func decodeRectangle(pkt []byte, idx *int, is2019 bool) (*RectangleArea, error) {
	const fixedLen = 4 + 2 + 4 + 4 + 4 + 4
	if len(pkt)-*idx < fixedLen {
		return nil, errors.New("rectangle area too short")
	}
	r := &RectangleArea{}
	r.ID = hex.ReadDoubleWord(pkt, idx)
	r.Attr = AreaAttr(hex.ReadWord(pkt, idx))
	r.LeftTopLat = hex.ReadDoubleWord(pkt, idx)
	r.LeftTopLng = hex.ReadDoubleWord(pkt, idx)
	r.RightBottomLat = hex.ReadDoubleWord(pkt, idx)
	r.RightBottomLng = hex.ReadDoubleWord(pkt, idx)
	if len(pkt)-*idx < areaAttrFieldsLength(r.Attr, is2019) {
		return nil, errors.New("rectangle area attr fields exceed payload")
	}
	r.StartTime, r.EndTime, r.MaxSpeed, r.Duration, r.NightSpeed = decodeAreaAttrFields(pkt, idx, r.Attr, is2019)
	if is2019 {
		name, err := decodeName(pkt, idx)
		if err != nil {
			return nil, err
		}
		r.Name = name
	}
	return r, nil
}

// ---- Polygon ----

// encode 按版本写入多边形区域项：
//   - 2019：ID、属性、顶点项、时间/速度/夜间速度、名称；
//   - 2011/2013：ID、属性、时间/速度、顶点项（JT/T 808-2013 表 63）。
func (p *PolygonArea) encode(is2019 bool) []byte {
	pkt := hex.WriteDoubleWord(nil, p.ID)
	pkt = hex.WriteWord(pkt, uint16(p.Attr))
	if is2019 {
		pkt = encodeVertices(pkt, p.Vertices)
		pkt = encodeAreaAttrFields(pkt, p.Attr, p.StartTime, p.EndTime, p.MaxSpeed, p.Duration, p.NightSpeed, true)
		return encodeName(pkt, p.Name)
	}
	pkt = encodeAreaAttrFields(pkt, p.Attr, p.StartTime, p.EndTime, p.MaxSpeed, p.Duration, p.NightSpeed, false)
	return encodeVertices(pkt, p.Vertices)
}

func decodePolygon(pkt []byte, idx *int, is2019 bool) (*PolygonArea, error) {
	if len(pkt)-*idx < 4+2 {
		return nil, errors.New("polygon area too short")
	}
	p := &PolygonArea{}
	p.ID = hex.ReadDoubleWord(pkt, idx)
	p.Attr = AreaAttr(hex.ReadWord(pkt, idx))
	if is2019 {
		verts, err := decodeVertices(pkt, idx)
		if err != nil {
			return nil, err
		}
		p.Vertices = verts
		if len(pkt)-*idx < areaAttrFieldsLength(p.Attr, true) {
			return nil, errors.New("polygon area attr fields exceed payload")
		}
		p.StartTime, p.EndTime, p.MaxSpeed, p.Duration, p.NightSpeed = decodeAreaAttrFields(pkt, idx, p.Attr, true)
		name, err := decodeName(pkt, idx)
		if err != nil {
			return nil, err
		}
		p.Name = name
		return p, nil
	}
	if len(pkt)-*idx < areaAttrFieldsLength(p.Attr, false) {
		return nil, errors.New("polygon area attr fields exceed payload")
	}
	p.StartTime, p.EndTime, p.MaxSpeed, p.Duration, p.NightSpeed = decodeAreaAttrFields(pkt, idx, p.Attr, false)
	verts, err := decodeVertices(pkt, idx)
	if err != nil {
		return nil, err
	}
	p.Vertices = verts
	return p, nil
}

// ---- Route ----

func (r *Route) encode(is2019 bool) []byte {
	pkt := hex.WriteDoubleWord(nil, r.ID)
	pkt = hex.WriteWord(pkt, uint16(r.Attr))
	if r.Attr.HasTime() {
		pkt = hex.WriteBCD(pkt, r.StartTime)
		pkt = hex.WriteBCD(pkt, r.EndTime)
	}
	if is2019 {
		pkt = r.encodeSegments2019(pkt)
		return encodeName(pkt, r.Name)
	}
	return r.encodeInflections(pkt)
}

// encodeSegments2019 保持 2019 路径的既有“路段 + 顶点列表”布局不变。
func (r *Route) encodeSegments2019(pkt []byte) []byte {
	pkt = hex.WriteWord(pkt, uint16(len(r.Segments)))
	for _, s := range r.Segments {
		pkt = hex.WriteDoubleWord(pkt, s.ID)
		pkt = hex.WriteWord(pkt, s.Attr)
		pkt = hex.WriteWord(pkt, s.Length)
		segAttr := AreaAttr(s.Attr)
		if segAttr.HasSpeed() {
			pkt = hex.WriteWord(pkt, s.MaxSpeed)
			pkt = hex.WriteByte(pkt, s.Duration)
			pkt = hex.WriteWord(pkt, s.NightSpeed)
		}
		pkt = encodeVertices(pkt, s.Vertices)
	}
	return pkt
}

// encodeInflections 按 JT/T 808-2013 表 66/68 写入路线的拐点项。
func (r *Route) encodeInflections(pkt []byte) []byte {
	pkt = hex.WriteWord(pkt, uint16(len(r.Inflections)))
	for _, inf := range r.Inflections {
		pkt = hex.WriteDoubleWord(pkt, inf.ID)
		pkt = hex.WriteDoubleWord(pkt, inf.SectionID)
		pkt = hex.WriteDoubleWord(pkt, inf.Latitude)
		pkt = hex.WriteDoubleWord(pkt, inf.Longitude)
		pkt = hex.WriteByte(pkt, inf.Width)
		pkt = hex.WriteByte(pkt, uint8(inf.Attr))
		if inf.Attr&RouteInflectionTime != 0 {
			pkt = hex.WriteWord(pkt, inf.OverlongThreshold)
			pkt = hex.WriteWord(pkt, inf.UnderThreshold)
		}
		if inf.Attr&RouteInflectionSpeed != 0 {
			pkt = hex.WriteWord(pkt, inf.MaxSpeed)
			pkt = hex.WriteByte(pkt, inf.Duration)
		}
	}
	return pkt
}

func decodeRoute(pkt []byte, idx *int, is2019 bool) (*Route, error) {
	if len(pkt)-*idx < 4+2 {
		return nil, errors.New("route too short")
	}
	r := &Route{}
	r.ID = hex.ReadDoubleWord(pkt, idx)
	r.Attr = AreaAttr(hex.ReadWord(pkt, idx))
	if r.Attr.HasTime() {
		if len(pkt)-*idx < 12 {
			return nil, errors.New("route time fields exceed payload")
		}
		r.StartTime = hex.ReadBCD(pkt, idx, 6)
		r.EndTime = hex.ReadBCD(pkt, idx, 6)
	}
	if is2019 {
		if err := r.decodeSegments2019(pkt, idx); err != nil {
			return nil, err
		}
		name, err := decodeName(pkt, idx)
		if err != nil {
			return nil, err
		}
		r.Name = name
		return r, nil
	}
	if err := r.decodeInflections(pkt, idx); err != nil {
		return nil, err
	}
	return r, nil
}

// decodeSegments2019 保持 2019 路径的既有“路段 + 顶点列表”解析不变。
func (r *Route) decodeSegments2019(pkt []byte, idx *int) error {
	if len(pkt)-*idx < 2 {
		return errors.New("route segment count missing")
	}
	segCnt := int(hex.ReadWord(pkt, idx))
	r.Segments = make([]RouteSegment, 0, segCnt)
	for i := 0; i < segCnt; i++ {
		if len(pkt)-*idx < 4+2+2 {
			return errors.New("route segment too short")
		}
		s := RouteSegment{}
		s.ID = hex.ReadDoubleWord(pkt, idx)
		s.Attr = hex.ReadWord(pkt, idx)
		s.Length = hex.ReadWord(pkt, idx)
		if AreaAttr(s.Attr).HasSpeed() {
			if len(pkt)-*idx < 5 {
				return errors.New("route segment speed fields exceed payload")
			}
			s.MaxSpeed = hex.ReadWord(pkt, idx)
			s.Duration = hex.ReadByte(pkt, idx)
			s.NightSpeed = hex.ReadWord(pkt, idx)
		}
		verts, err := decodeVertices(pkt, idx)
		if err != nil {
			return err
		}
		s.Vertices = verts
		r.Segments = append(r.Segments, s)
	}
	return nil
}

// decodeInflections 按 JT/T 808-2013 表 66/68 解析路线的拐点项。
func (r *Route) decodeInflections(pkt []byte, idx *int) error {
	if len(pkt)-*idx < 2 {
		return errors.New("route inflection count missing")
	}
	cnt := int(hex.ReadWord(pkt, idx))
	r.Inflections = make([]RouteInflection, 0, cnt)
	for i := 0; i < cnt; i++ {
		const fixedLen = 4 + 4 + 4 + 4 + 1 + 1
		if len(pkt)-*idx < fixedLen {
			return errors.New("route inflection too short")
		}
		inf := RouteInflection{}
		inf.ID = hex.ReadDoubleWord(pkt, idx)
		inf.SectionID = hex.ReadDoubleWord(pkt, idx)
		inf.Latitude = hex.ReadDoubleWord(pkt, idx)
		inf.Longitude = hex.ReadDoubleWord(pkt, idx)
		inf.Width = hex.ReadByte(pkt, idx)
		inf.Attr = RouteInflectionAttr(hex.ReadByte(pkt, idx))
		if inf.Attr&RouteInflectionTime != 0 {
			if len(pkt)-*idx < 4 {
				return errors.New("route inflection thresholds exceed payload")
			}
			inf.OverlongThreshold = hex.ReadWord(pkt, idx)
			inf.UnderThreshold = hex.ReadWord(pkt, idx)
		}
		if inf.Attr&RouteInflectionSpeed != 0 {
			if len(pkt)-*idx < 3 {
				return errors.New("route inflection speed fields exceed payload")
			}
			inf.MaxSpeed = hex.ReadWord(pkt, idx)
			inf.Duration = hex.ReadByte(pkt, idx)
		}
		r.Inflections = append(r.Inflections, inf)
	}
	return nil
}
