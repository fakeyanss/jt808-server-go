package model

import (
	"github.com/pkg/errors"

	"github.com/fakeyanss/jt808-server-go/internal/codec/hex"
)

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
