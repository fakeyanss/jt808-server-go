package model

import (
	"fmt"
	"time"

	"github.com/fakeYanss/jt808-server-go/internal/codec/bcd"
)

// 查询服务器时间应答
type Msg8004 struct {
	Header     *MsgHeader `json:"header"`
	ServerTime *time.Time `json:"serverTime"`
}

func (m *Msg8004) Encode() (pkt []byte, err error) {
	now := m.ServerTime
	year := now.Year()     // 年
	month := now.Month()   // 月
	day := now.Day()       // 日
	hour := now.Hour()     // 小时
	minute := now.Minute() // 分钟
	second := now.Second() // 秒
	fmtTime := fmt.Sprintf("%02d%02d%02d%02d%02d%02d", year, month, day, hour, minute, second)
	pkt = append(pkt, bcd.NumberStr2BCD(fmtTime)...)

	headerPkt, err := m.Header.Encode()
	if err != nil {
		return nil, err
	}

	pkt = append(headerPkt, pkt...)

	return pkt, nil
}

func (m *Msg8004) Decode(packet *PacketData) error {
	return nil
}

func (m *Msg8004) GetHeader() *MsgHeader {
	return m.Header
}

func (m *Msg8004) GenOutgoing(incoming JT808Msg) error {
	in := incoming.(*Msg0004)
	now := time.Now()
	m.ServerTime = &now

	m.Header = in.Header
	m.Header.MsgID = 0x8004

	return nil
}
