package model

import "github.com/fakeyanss/jt808-server-go/internal/codec/hex"

// QuestionCandidate is one selectable answer in a 0x8302 question.
type QuestionCandidate struct {
	ID      uint32 `json:"id"`      // 答案ID
	Content string `json:"content"` // 答案内容 GBK
}

// Msg8302 is a platform question-issue command.
type Msg8302 struct {
	Header     *MsgHeader          `json:"header"`
	Flag       uint8               `json:"flag"`       // 标志 bit0:紧急 bit1:TTS
	Question   string              `json:"question"`   // 问题内容 GBK
	Candidates []QuestionCandidate `json:"candidates"` // 候选答案
}

func (m *Msg8302) Decode(p *PacketData) error {
	if p == nil || p.Header == nil || len(p.Body) < 3 {
		return ErrDecodeMsg
	}
	m.Header = p.Header
	idx := 0
	m.Flag = hex.ReadByte(p.Body, &idx)
	qLen := int(hex.ReadByte(p.Body, &idx))
	if idx+qLen > len(p.Body) {
		return ErrDecodeMsg
	}
	m.Question = hex.ReadGBK(p.Body, &idx, qLen)
	if idx >= len(p.Body) {
		return nil
	}
	count := int(hex.ReadByte(p.Body, &idx))
	m.Candidates = nil
	for n := 0; n < count; n++ {
		if idx+5 > len(p.Body) {
			return ErrDecodeMsg
		}
		c := QuestionCandidate{ID: hex.ReadDoubleWord(p.Body, &idx)}
		contentLen := int(hex.ReadByte(p.Body, &idx))
		if idx+contentLen > len(p.Body) {
			return ErrDecodeMsg
		}
		c.Content = hex.ReadGBK(p.Body, &idx, contentLen)
		m.Candidates = append(m.Candidates, c)
	}
	return nil
}

func (m *Msg8302) Encode() ([]byte, error) {
	if m.Header == nil {
		return nil, ErrEncodeMsg
	}
	gbk := hex.WriteGBK(nil, m.Question)
	pkt := []byte{m.Flag, uint8(len(gbk))}
	pkt = append(pkt, gbk...)
	pkt = append(pkt, uint8(len(m.Candidates)))
	for _, c := range m.Candidates {
		cgbk := hex.WriteGBK(nil, c.Content)
		pkt = hex.WriteDoubleWord(pkt, c.ID)
		pkt = append(pkt, uint8(len(cgbk)))
		pkt = append(pkt, cgbk...)
	}
	return writeHeader(m, pkt)
}

func (m *Msg8302) GetHeader() *MsgHeader        { return m.Header }
func (m *Msg8302) GenOutgoing(_ JT808Msg) error { return ErrGenOutgoingMsg }

// Msg0302 is a terminal question response.
type Msg0302 struct {
	Header       *MsgHeader `json:"header"`
	AnswerSerial uint16     `json:"answerSerial"` // 应答流水号
	AnswerID     uint32     `json:"answerId"`     // 答案ID
}

func (m *Msg0302) Decode(p *PacketData) error {
	if p == nil || p.Header == nil || len(p.Body) < 6 {
		return ErrDecodeMsg
	}
	m.Header = p.Header
	idx := 0
	m.AnswerSerial = hex.ReadWord(p.Body, &idx)
	m.AnswerID = hex.ReadDoubleWord(p.Body, &idx)
	return nil
}

func (m *Msg0302) Encode() ([]byte, error) {
	if m.Header == nil {
		return nil, ErrEncodeMsg
	}
	pkt := hex.WriteWord(nil, m.AnswerSerial)
	pkt = hex.WriteDoubleWord(pkt, m.AnswerID)
	return writeHeader(m, pkt)
}

func (m *Msg0302) GetHeader() *MsgHeader { return m.Header }
func (m *Msg0302) GenOutgoing(_ JT808Msg) error {
	return nil
}

// InfoMenuItem is one configurable menu entry in 0x8303.
type InfoMenuItem struct {
	ID      uint32 `json:"id"`      // 菜单ID
	Content string `json:"content"` // 菜单内容 GBK
}

// Msg8303 sets the terminal information on-demand menu.
type Msg8303 struct {
	Header  *MsgHeader     `json:"header"`
	SetType uint8          `json:"setType"` // 设置类型 0:删除全部 1:更新 2:追加 3:修改
	Items   []InfoMenuItem `json:"items"`
}

func (m *Msg8303) Decode(p *PacketData) error {
	if p == nil || p.Header == nil || len(p.Body) < 2 {
		return ErrDecodeMsg
	}
	m.Header = p.Header
	idx := 0
	m.SetType = hex.ReadByte(p.Body, &idx)
	count := int(hex.ReadByte(p.Body, &idx))
	m.Items = nil
	for n := 0; n < count; n++ {
		if idx+5 > len(p.Body) {
			return ErrDecodeMsg
		}
		item := InfoMenuItem{ID: hex.ReadDoubleWord(p.Body, &idx)}
		contentLen := int(hex.ReadByte(p.Body, &idx))
		if idx+contentLen > len(p.Body) {
			return ErrDecodeMsg
		}
		item.Content = hex.ReadGBK(p.Body, &idx, contentLen)
		m.Items = append(m.Items, item)
	}
	return nil
}

func (m *Msg8303) Encode() ([]byte, error) {
	if m.Header == nil {
		return nil, ErrEncodeMsg
	}
	pkt := []byte{m.SetType, uint8(len(m.Items))}
	for _, item := range m.Items {
		gbk := hex.WriteGBK(nil, item.Content)
		pkt = hex.WriteDoubleWord(pkt, item.ID)
		pkt = append(pkt, uint8(len(gbk)))
		pkt = append(pkt, gbk...)
	}
	return writeHeader(m, pkt)
}

func (m *Msg8303) GetHeader() *MsgHeader        { return m.Header }
func (m *Msg8303) GenOutgoing(_ JT808Msg) error { return ErrGenOutgoingMsg }

// Msg0303 is a terminal information on-demand / cancel report.
type Msg0303 struct {
	Header *MsgHeader `json:"header"`
	Action uint8      `json:"action"` // 0:取消 1:点播
	MenuID uint32     `json:"menuId"`
}

func (m *Msg0303) Decode(p *PacketData) error {
	if p == nil || p.Header == nil || len(p.Body) < 5 {
		return ErrDecodeMsg
	}
	m.Header = p.Header
	idx := 0
	m.Action = hex.ReadByte(p.Body, &idx)
	m.MenuID = hex.ReadDoubleWord(p.Body, &idx)
	return nil
}

func (m *Msg0303) Encode() ([]byte, error) {
	if m.Header == nil {
		return nil, ErrEncodeMsg
	}
	pkt := []byte{m.Action}
	pkt = hex.WriteDoubleWord(pkt, m.MenuID)
	return writeHeader(m, pkt)
}

func (m *Msg0303) GetHeader() *MsgHeader { return m.Header }
func (m *Msg0303) GenOutgoing(_ JT808Msg) error {
	return nil
}

// Msg8304 is a platform information-service command.
type Msg8304 struct {
	Header   *MsgHeader `json:"header"`
	InfoType uint8      `json:"infoType"` // 信息类型 0:紧急 1:通知 2:服务
	Content  string     `json:"content"`  // 信息内容 GBK
}

func (m *Msg8304) Decode(p *PacketData) error {
	if p == nil || p.Header == nil || len(p.Body) < 3 {
		return ErrDecodeMsg
	}
	m.Header = p.Header
	idx := 0
	m.InfoType = hex.ReadByte(p.Body, &idx)
	contentLen := int(hex.ReadWord(p.Body, &idx))
	if idx+contentLen > len(p.Body) {
		return ErrDecodeMsg
	}
	m.Content = hex.ReadGBK(p.Body, &idx, contentLen)
	return nil
}

func (m *Msg8304) Encode() ([]byte, error) {
	if m.Header == nil {
		return nil, ErrEncodeMsg
	}
	gbk := hex.WriteGBK(nil, m.Content)
	pkt := []byte{m.InfoType}
	pkt = hex.WriteWord(pkt, uint16(len(gbk)))
	pkt = append(pkt, gbk...)
	return writeHeader(m, pkt)
}

func (m *Msg8304) GetHeader() *MsgHeader        { return m.Header }
func (m *Msg8304) GenOutgoing(_ JT808Msg) error { return ErrGenOutgoingMsg }
