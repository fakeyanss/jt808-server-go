package model

import (
	"github.com/pkg/errors"

	"github.com/fakeyanss/jt808-server-go/internal/codec/hex"
)

// MsgRegionRouteSet represents 0x8600/0x8602/0x8604/0x8606 set area/route
// messages. The active item slice is selected by Type, which is derived from
// the message ID on Encode/Decode.
type MsgRegionRouteSet struct {
	Header      *MsgHeader      `json:"header"`
	SettingAttr AreaSettingAttr `json:"settingAttr"`
	Type        RegionRouteType `json:"type"`
	Circles     []CircleArea    `json:"circles,omitempty"`
	Rectangles  []RectangleArea `json:"rectangles,omitempty"`
	Polygons    []PolygonArea   `json:"polygons,omitempty"`
	Routes      []Route         `json:"routes,omitempty"`
}

// ItemCount returns the number of typed items in the active family.
func (m *MsgRegionRouteSet) ItemCount() int {
	switch m.Type {
	case RegionRouteCircle:
		return len(m.Circles)
	case RegionRouteRect:
		return len(m.Rectangles)
	case RegionRoutePolygon:
		return len(m.Polygons)
	case RegionRouteRoute:
		return len(m.Routes)
	}
	return 0
}

func (m *MsgRegionRouteSet) Decode(packet *PacketData) error {
	if packet == nil || packet.Header == nil {
		return ErrDecodeMsg
	}
	typ, ok := regionRouteTypeFromMsgID(packet.Header.MsgID)
	if !ok {
		return errors.Wrap(ErrDecodeMsg, "unsupported region/route set message id")
	}
	body := packet.Body
	if len(body) < 2 {
		return ErrDecodeMsg
	}
	setting := body[0]
	if setting > 7 {
		return errors.Wrap(ErrDecodeMsg, "invalid setting attribute")
	}
	count := int(body[1])
	if count == 0 {
		return errors.Wrap(ErrDecodeMsg, "item count must be nonzero")
	}
	m.Header = packet.Header
	m.SettingAttr = AreaSettingAttr(setting)
	m.Type = typ
	idx := 2
	for i := 0; i < count; i++ {
		switch typ {
		case RegionRouteCircle:
			c, err := decodeCircle(body, &idx)
			if err != nil {
				return errors.Wrap(err, "decode circle area")
			}
			m.Circles = append(m.Circles, *c)
		case RegionRouteRect:
			r, err := decodeRectangle(body, &idx)
			if err != nil {
				return errors.Wrap(err, "decode rectangle area")
			}
			m.Rectangles = append(m.Rectangles, *r)
		case RegionRoutePolygon:
			p, err := decodePolygon(body, &idx)
			if err != nil {
				return errors.Wrap(err, "decode polygon area")
			}
			m.Polygons = append(m.Polygons, *p)
		case RegionRouteRoute:
			rt, err := decodeRoute(body, &idx)
			if err != nil {
				return errors.Wrap(err, "decode route")
			}
			m.Routes = append(m.Routes, *rt)
		}
	}
	if idx != len(body) {
		return errors.Wrapf(ErrDecodeMsg, "trailing bytes after region/route items: idx=%d len=%d", idx, len(body))
	}
	return nil
}

func (m *MsgRegionRouteSet) Encode() ([]byte, error) {
	if m.Header == nil {
		return nil, ErrEncodeMsg
	}
	typ, ok := regionRouteTypeFromMsgID(m.Header.MsgID)
	if !ok {
		return nil, errors.Wrap(ErrEncodeMsg, "unsupported region/route set message id")
	}
	m.Type = typ
	count := m.ItemCount()
	if count == 0 || count > 255 {
		return nil, errors.Wrap(ErrEncodeMsg, "item count out of range")
	}
	body := []byte{byte(m.SettingAttr), byte(count)}
	switch typ {
	case RegionRouteCircle:
		for i := range m.Circles {
			body = append(body, m.Circles[i].encode()...)
		}
	case RegionRouteRect:
		for i := range m.Rectangles {
			body = append(body, m.Rectangles[i].encode()...)
		}
	case RegionRoutePolygon:
		for i := range m.Polygons {
			body = append(body, m.Polygons[i].encode()...)
		}
	case RegionRouteRoute:
		for i := range m.Routes {
			body = append(body, m.Routes[i].encode()...)
		}
	}
	return writeHeader(m, body)
}

func (m *MsgRegionRouteSet) GetHeader() *MsgHeader        { return m.Header }
func (m *MsgRegionRouteSet) GenOutgoing(_ JT808Msg) error { return ErrGenOutgoingMsg }

// Msg8608 requests a specific type of area/route data; an empty ID list asks
// for all data of that type.
type Msg8608 struct {
	Header    *MsgHeader `json:"header"`
	QueryType uint8      `json:"queryType"`
	IDs       []uint32   `json:"ids"`
}

func (m *Msg8608) Decode(packet *PacketData) error {
	if packet == nil || packet.Header == nil || len(packet.Body) < 5 {
		return ErrDecodeMsg
	}
	count := int(packet.Body[1])<<24 | int(packet.Body[2])<<16 | int(packet.Body[3])<<8 | int(packet.Body[4])
	if packet.Body[0] < 1 || packet.Body[0] > 4 || count > 125 || len(packet.Body) != 5+count*4 {
		return ErrDecodeMsg
	}
	m.Header, m.QueryType = packet.Header, packet.Body[0]
	m.IDs = make([]uint32, 0, count)
	idx := 5
	for i := 0; i < count; i++ {
		m.IDs = append(m.IDs, hex.ReadDoubleWord(packet.Body, &idx))
	}
	return nil
}
func (m *Msg8608) Encode() ([]byte, error) {
	if m.Header == nil || m.QueryType < 1 || m.QueryType > 4 || len(m.IDs) > 125 {
		return nil, ErrEncodeMsg
	}
	body := []byte{m.QueryType}
	body = hex.WriteDoubleWord(body, uint32(len(m.IDs)))
	for _, id := range m.IDs {
		body = hex.WriteDoubleWord(body, id)
	}
	return writeHeader(m, body)
}
func (m *Msg8608) GetHeader() *MsgHeader        { return m.Header }
func (m *Msg8608) GenOutgoing(_ JT808Msg) error { return ErrGenOutgoingMsg }

// Msg0608 is the terminal's response to a 0x8608 query. Records are decoded
// into the typed slice selected by QueryType.
type Msg0608 struct {
	Header     *MsgHeader      `json:"header"`
	QueryType  uint8           `json:"queryType"`
	Count      uint32          `json:"count"`
	Circles    []CircleArea    `json:"circles,omitempty"`
	Rectangles []RectangleArea `json:"rectangles,omitempty"`
	Polygons   []PolygonArea   `json:"polygons,omitempty"`
	Routes     []Route         `json:"routes,omitempty"`
}

func (m *Msg0608) decodeRecords(body []byte) error {
	typ, ok := regionRouteTypeFromQueryType(m.QueryType)
	if !ok {
		return errors.Wrap(ErrDecodeMsg, "invalid query type")
	}
	idx := 0
	for i := uint32(0); i < m.Count; i++ {
		switch typ {
		case RegionRouteCircle:
			c, err := decodeCircle(body, &idx)
			if err != nil {
				return err
			}
			m.Circles = append(m.Circles, *c)
		case RegionRouteRect:
			r, err := decodeRectangle(body, &idx)
			if err != nil {
				return err
			}
			m.Rectangles = append(m.Rectangles, *r)
		case RegionRoutePolygon:
			p, err := decodePolygon(body, &idx)
			if err != nil {
				return err
			}
			m.Polygons = append(m.Polygons, *p)
		case RegionRouteRoute:
			rt, err := decodeRoute(body, &idx)
			if err != nil {
				return err
			}
			m.Routes = append(m.Routes, *rt)
		}
	}
	if idx != len(body) {
		return errors.Wrapf(ErrDecodeMsg, "trailing bytes in 0x0608 records: idx=%d len=%d", idx, len(body))
	}
	return nil
}

func (m *Msg0608) Decode(packet *PacketData) error {
	if packet == nil || packet.Header == nil || len(packet.Body) < 5 {
		return ErrDecodeMsg
	}
	if packet.Body[0] < 1 || packet.Body[0] > 4 {
		return ErrDecodeMsg
	}
	m.Header, m.QueryType = packet.Header, packet.Body[0]
	idx := 1
	m.Count = hex.ReadDoubleWord(packet.Body, &idx)
	records := packet.Body[idx:]
	if m.Count == 0 {
		if len(records) != 0 {
			return ErrDecodeMsg
		}
		return nil
	}
	return m.decodeRecords(records)
}

func (m *Msg0608) Encode() ([]byte, error) {
	if m.Header == nil || m.QueryType < 1 || m.QueryType > 4 {
		return nil, ErrEncodeMsg
	}
	typ, ok := regionRouteTypeFromQueryType(m.QueryType)
	if !ok {
		return nil, ErrEncodeMsg
	}
	var records []byte
	switch typ {
	case RegionRouteCircle:
		m.Count = uint32(len(m.Circles))
		for i := range m.Circles {
			records = append(records, m.Circles[i].encode()...)
		}
	case RegionRouteRect:
		m.Count = uint32(len(m.Rectangles))
		for i := range m.Rectangles {
			records = append(records, m.Rectangles[i].encode()...)
		}
	case RegionRoutePolygon:
		m.Count = uint32(len(m.Polygons))
		for i := range m.Polygons {
			records = append(records, m.Polygons[i].encode()...)
		}
	case RegionRouteRoute:
		m.Count = uint32(len(m.Routes))
		for i := range m.Routes {
			records = append(records, m.Routes[i].encode()...)
		}
	}
	body := []byte{m.QueryType}
	body = hex.WriteDoubleWord(body, m.Count)
	body = append(body, records...)
	return writeHeader(m, body)
}
func (m *Msg0608) GetHeader() *MsgHeader { return m.Header }
func (m *Msg0608) GenOutgoing(incoming JT808Msg) error {
	query, ok := incoming.(*Msg8608)
	if !ok {
		return ErrGenOutgoingMsg
	}
	m.Header = cloneReplyHeader(query.Header, 0x0608)
	m.QueryType = query.QueryType
	return nil
}
