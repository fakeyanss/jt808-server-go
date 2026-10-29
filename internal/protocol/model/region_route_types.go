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

// encodeAttrFields writes the conditional time/speed/night-speed fields that
// are shared by circle, rectangle and polygon items.
func encodeAreaAttrFields(pkt []byte, attr AreaAttr, start, end string, maxSpeed uint16, duration uint8, nightSpeed uint16) []byte {
	if attr.HasTime() {
		pkt = hex.WriteBCD(pkt, start)
		pkt = hex.WriteBCD(pkt, end)
	}
	if attr.HasSpeed() {
		pkt = hex.WriteWord(pkt, maxSpeed)
		pkt = hex.WriteByte(pkt, duration)
	}
	if attr.HasNightSpeed() {
		pkt = hex.WriteWord(pkt, nightSpeed)
	}
	return pkt
}

func decodeAreaAttrFields(pkt []byte, idx *int, attr AreaAttr) (start, end string, maxSpeed uint16, duration uint8, nightSpeed uint16) {
	if attr.HasTime() {
		start = hex.ReadBCD(pkt, idx, 6)
		end = hex.ReadBCD(pkt, idx, 6)
	}
	if attr.HasSpeed() {
		maxSpeed = hex.ReadWord(pkt, idx)
		duration = hex.ReadByte(pkt, idx)
	}
	if attr.HasNightSpeed() {
		nightSpeed = hex.ReadWord(pkt, idx)
	}
	return
}

func encodeName(pkt []byte, name string) []byte {
	gbk := hex.WriteGBK(nil, name)
	pkt = hex.WriteByte(pkt, uint8(len(gbk)))
	pkt = append(pkt, gbk...)
	return pkt
}

func decodeName(pkt []byte, idx *int) (string, error) {
	if *idx >= len(pkt) {
		return "", errors.New("name length missing")
	}
	n := int(hex.ReadByte(pkt, idx))
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
	cnt := int(hex.ReadWord(pkt, idx))
	if cnt == 0 {
		return nil, errors.New("vertex count must be nonzero")
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

func (c *CircleArea) encode() []byte {
	pkt := hex.WriteDoubleWord(nil, c.ID)
	pkt = hex.WriteWord(pkt, uint16(c.Attr))
	pkt = hex.WriteDoubleWord(pkt, c.Latitude)
	pkt = hex.WriteDoubleWord(pkt, c.Longitude)
	pkt = hex.WriteDoubleWord(pkt, c.Radius)
	pkt = encodeAreaAttrFields(pkt, c.Attr, c.StartTime, c.EndTime, c.MaxSpeed, c.Duration, c.NightSpeed)
	pkt = encodeName(pkt, c.Name)
	return pkt
}

func decodeCircle(pkt []byte, idx *int) (*CircleArea, error) {
	c := &CircleArea{}
	c.ID = hex.ReadDoubleWord(pkt, idx)
	c.Attr = AreaAttr(hex.ReadWord(pkt, idx))
	c.Latitude = hex.ReadDoubleWord(pkt, idx)
	c.Longitude = hex.ReadDoubleWord(pkt, idx)
	c.Radius = hex.ReadDoubleWord(pkt, idx)
	c.StartTime, c.EndTime, c.MaxSpeed, c.Duration, c.NightSpeed = decodeAreaAttrFields(pkt, idx, c.Attr)
	name, err := decodeName(pkt, idx)
	if err != nil {
		return nil, err
	}
	c.Name = name
	return c, nil
}

// ---- Rectangle ----

func (r *RectangleArea) encode() []byte {
	pkt := hex.WriteDoubleWord(nil, r.ID)
	pkt = hex.WriteWord(pkt, uint16(r.Attr))
	pkt = hex.WriteDoubleWord(pkt, r.LeftTopLat)
	pkt = hex.WriteDoubleWord(pkt, r.LeftTopLng)
	pkt = hex.WriteDoubleWord(pkt, r.RightBottomLat)
	pkt = hex.WriteDoubleWord(pkt, r.RightBottomLng)
	pkt = encodeAreaAttrFields(pkt, r.Attr, r.StartTime, r.EndTime, r.MaxSpeed, r.Duration, r.NightSpeed)
	pkt = encodeName(pkt, r.Name)
	return pkt
}

func decodeRectangle(pkt []byte, idx *int) (*RectangleArea, error) {
	r := &RectangleArea{}
	r.ID = hex.ReadDoubleWord(pkt, idx)
	r.Attr = AreaAttr(hex.ReadWord(pkt, idx))
	r.LeftTopLat = hex.ReadDoubleWord(pkt, idx)
	r.LeftTopLng = hex.ReadDoubleWord(pkt, idx)
	r.RightBottomLat = hex.ReadDoubleWord(pkt, idx)
	r.RightBottomLng = hex.ReadDoubleWord(pkt, idx)
	r.StartTime, r.EndTime, r.MaxSpeed, r.Duration, r.NightSpeed = decodeAreaAttrFields(pkt, idx, r.Attr)
	name, err := decodeName(pkt, idx)
	if err != nil {
		return nil, err
	}
	r.Name = name
	return r, nil
}

// ---- Polygon ----

func (p *PolygonArea) encode() []byte {
	pkt := hex.WriteDoubleWord(nil, p.ID)
	pkt = hex.WriteWord(pkt, uint16(p.Attr))
	pkt = encodeVertices(pkt, p.Vertices)
	pkt = encodeAreaAttrFields(pkt, p.Attr, p.StartTime, p.EndTime, p.MaxSpeed, p.Duration, p.NightSpeed)
	pkt = encodeName(pkt, p.Name)
	return pkt
}

func decodePolygon(pkt []byte, idx *int) (*PolygonArea, error) {
	p := &PolygonArea{}
	p.ID = hex.ReadDoubleWord(pkt, idx)
	p.Attr = AreaAttr(hex.ReadWord(pkt, idx))
	verts, err := decodeVertices(pkt, idx)
	if err != nil {
		return nil, err
	}
	p.Vertices = verts
	p.StartTime, p.EndTime, p.MaxSpeed, p.Duration, p.NightSpeed = decodeAreaAttrFields(pkt, idx, p.Attr)
	name, err := decodeName(pkt, idx)
	if err != nil {
		return nil, err
	}
	p.Name = name
	return p, nil
}

// ---- Route ----

func (r *Route) encode() []byte {
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
		}
		if segAttr.HasNightSpeed() {
			pkt = hex.WriteWord(pkt, s.NightSpeed)
		}
		pkt = encodeVertices(pkt, s.Vertices)
	}
	pkt = encodeName(pkt, r.Name)
	return pkt
}

func decodeRoute(pkt []byte, idx *int) (*Route, error) {
	r := &Route{}
	r.ID = hex.ReadDoubleWord(pkt, idx)
	r.Attr = AreaAttr(hex.ReadWord(pkt, idx))
	if r.Attr.HasTime() {
		r.StartTime = hex.ReadBCD(pkt, idx, 6)
		r.EndTime = hex.ReadBCD(pkt, idx, 6)
	}
	segCnt := int(hex.ReadWord(pkt, idx))
	r.Segments = make([]RouteSegment, 0, segCnt)
	for i := 0; i < segCnt; i++ {
		s := RouteSegment{}
		s.ID = hex.ReadDoubleWord(pkt, idx)
		s.Attr = hex.ReadWord(pkt, idx)
		s.Length = hex.ReadWord(pkt, idx)
		segAttr := AreaAttr(s.Attr)
		if segAttr.HasSpeed() {
			s.MaxSpeed = hex.ReadWord(pkt, idx)
			s.Duration = hex.ReadByte(pkt, idx)
		}
		if segAttr.HasNightSpeed() {
			s.NightSpeed = hex.ReadWord(pkt, idx)
		}
		verts, err := decodeVertices(pkt, idx)
		if err != nil {
			return nil, err
		}
		s.Vertices = verts
		r.Segments = append(r.Segments, s)
	}
	name, err := decodeName(pkt, idx)
	if err != nil {
		return nil, err
	}
	r.Name = name
	return r, nil
}
