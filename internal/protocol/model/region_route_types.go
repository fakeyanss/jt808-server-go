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

// AreaAttr is the 16-bit area/route attribute field.
// bit0=time, bit1=speed-limit, bit2=open-door, bit15=night-speed (2019).
type AreaAttr uint16

const (
	AreaAttrTime     AreaAttr = 1 << 0
	AreaAttrSpeed    AreaAttr = 1 << 1
	AreaAttrDoor     AreaAttr = 1 << 2
	AreaAttrNightSpd AreaAttr = 1 << 15
)

func (a AreaAttr) HasTime() bool       { return a&AreaAttrTime != 0 }
func (a AreaAttr) HasSpeed() bool      { return a&AreaAttrSpeed != 0 }
func (a AreaAttr) HasDoor() bool       { return a&AreaAttrDoor != 0 }
func (a AreaAttr) HasNightSpeed() bool { return a&AreaAttrNightSpd != 0 }

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

// PolygonVertex is a single vertex of a polygon area or a route segment.
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

// RouteSegment is one segment inside a 0x8606 route.
type RouteSegment struct {
	ID         uint32          `json:"id"`
	Attr       uint16          `json:"attr"`
	Length     uint16          `json:"length"`
	MaxSpeed   uint16          `json:"maxSpeed,omitempty"`
	Duration   uint8           `json:"duration,omitempty"`
	NightSpeed uint16          `json:"nightSpeed,omitempty"`
	Vertices   []PolygonVertex `json:"vertices"`
}

// Route is one item of a 0x8606 set route message.
type Route struct {
	ID        uint32         `json:"id"`
	Attr      AreaAttr       `json:"attr"`
	StartTime string         `json:"startTime,omitempty"`
	EndTime   string         `json:"endTime,omitempty"`
	Segments  []RouteSegment `json:"segments"`
	Name      string         `json:"name"`
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

// areaAttrFieldsLength 返回时间/速度/夜间速度条件字段占用的字节数。
// 2019 版本的夜间最高速度由 bit1（限速）触发，且紧跟在最高速度与超速持续时间之后；
// 2011/2013 版本保持仓库既有行为，由 bit15 触发。
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
	if !is2019 && attr.HasNightSpeed() {
		n += 2
	}
	return n
}

// encodeAreaAttrFields writes the conditional time/speed/night-speed fields that
// are shared by circle, rectangle and polygon items.
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
	if !is2019 && attr.HasNightSpeed() {
		pkt = hex.WriteWord(pkt, nightSpeed)
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
	if !is2019 && attr.HasNightSpeed() {
		nightSpeed = hex.ReadWord(pkt, idx)
	}
	return
}

// encodeName 写入名称。2019 版本名称长度字段为 WORD，2011/2013 版本保持
// 仓库既有的 BYTE 长度。
func encodeName(pkt []byte, name string, is2019 bool) []byte {
	gbk := hex.WriteGBK(nil, name)
	if is2019 {
		pkt = hex.WriteWord(pkt, uint16(len(gbk)))
	} else {
		pkt = hex.WriteByte(pkt, uint8(len(gbk)))
	}
	pkt = append(pkt, gbk...)
	return pkt
}

func decodeName(pkt []byte, idx *int, is2019 bool) (string, error) {
	var n int
	if is2019 {
		if *idx+2 > len(pkt) {
			return "", errors.New("name length missing")
		}
		n = int(hex.ReadWord(pkt, idx))
	} else {
		if *idx >= len(pkt) {
			return "", errors.New("name length missing")
		}
		n = int(hex.ReadByte(pkt, idx))
	}
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
	pkt = encodeName(pkt, c.Name, is2019)
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
	name, err := decodeName(pkt, idx, is2019)
	if err != nil {
		return nil, err
	}
	c.Name = name
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
	pkt = encodeName(pkt, r.Name, is2019)
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
	name, err := decodeName(pkt, idx, is2019)
	if err != nil {
		return nil, err
	}
	r.Name = name
	return r, nil
}

// ---- Polygon ----

func (p *PolygonArea) encode(is2019 bool) []byte {
	pkt := hex.WriteDoubleWord(nil, p.ID)
	pkt = hex.WriteWord(pkt, uint16(p.Attr))
	pkt = encodeVertices(pkt, p.Vertices)
	pkt = encodeAreaAttrFields(pkt, p.Attr, p.StartTime, p.EndTime, p.MaxSpeed, p.Duration, p.NightSpeed, is2019)
	pkt = encodeName(pkt, p.Name, is2019)
	return pkt
}

func decodePolygon(pkt []byte, idx *int, is2019 bool) (*PolygonArea, error) {
	if len(pkt)-*idx < 4+2 {
		return nil, errors.New("polygon area too short")
	}
	p := &PolygonArea{}
	p.ID = hex.ReadDoubleWord(pkt, idx)
	p.Attr = AreaAttr(hex.ReadWord(pkt, idx))
	verts, err := decodeVertices(pkt, idx)
	if err != nil {
		return nil, err
	}
	p.Vertices = verts
	if len(pkt)-*idx < areaAttrFieldsLength(p.Attr, is2019) {
		return nil, errors.New("polygon area attr fields exceed payload")
	}
	p.StartTime, p.EndTime, p.MaxSpeed, p.Duration, p.NightSpeed = decodeAreaAttrFields(pkt, idx, p.Attr, is2019)
	name, err := decodeName(pkt, idx, is2019)
	if err != nil {
		return nil, err
	}
	p.Name = name
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
	pkt = hex.WriteWord(pkt, uint16(len(r.Segments)))
	for _, s := range r.Segments {
		pkt = hex.WriteDoubleWord(pkt, s.ID)
		pkt = hex.WriteWord(pkt, s.Attr)
		pkt = hex.WriteWord(pkt, s.Length)
		segAttr := AreaAttr(s.Attr)
		if segAttr.HasSpeed() {
			pkt = hex.WriteWord(pkt, s.MaxSpeed)
			pkt = hex.WriteByte(pkt, s.Duration)
			if is2019 {
				pkt = hex.WriteWord(pkt, s.NightSpeed)
			}
		}
		if !is2019 && segAttr.HasNightSpeed() {
			pkt = hex.WriteWord(pkt, s.NightSpeed)
		}
		pkt = encodeVertices(pkt, s.Vertices)
	}
	pkt = encodeName(pkt, r.Name, is2019)
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
	if len(pkt)-*idx < 2 {
		return nil, errors.New("route segment count missing")
	}
	segCnt := int(hex.ReadWord(pkt, idx))
	r.Segments = make([]RouteSegment, 0, segCnt)
	for i := 0; i < segCnt; i++ {
		if len(pkt)-*idx < 4+2+2 {
			return nil, errors.New("route segment too short")
		}
		s := RouteSegment{}
		s.ID = hex.ReadDoubleWord(pkt, idx)
		s.Attr = hex.ReadWord(pkt, idx)
		s.Length = hex.ReadWord(pkt, idx)
		segAttr := AreaAttr(s.Attr)
		if segAttr.HasSpeed() {
			if len(pkt)-*idx < 3 {
				return nil, errors.New("route segment speed fields exceed payload")
			}
			s.MaxSpeed = hex.ReadWord(pkt, idx)
			s.Duration = hex.ReadByte(pkt, idx)
			if is2019 {
				if len(pkt)-*idx < 2 {
					return nil, errors.New("route segment night speed missing")
				}
				s.NightSpeed = hex.ReadWord(pkt, idx)
			}
		}
		if !is2019 && segAttr.HasNightSpeed() {
			if len(pkt)-*idx < 2 {
				return nil, errors.New("route segment night speed missing")
			}
			s.NightSpeed = hex.ReadWord(pkt, idx)
		}
		verts, err := decodeVertices(pkt, idx)
		if err != nil {
			return nil, err
		}
		s.Vertices = verts
		r.Segments = append(r.Segments, s)
	}
	name, err := decodeName(pkt, idx, is2019)
	if err != nil {
		return nil, err
	}
	r.Name = name
	return r, nil
}
