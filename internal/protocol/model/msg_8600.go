package model

import (
	"github.com/pkg/errors"
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
