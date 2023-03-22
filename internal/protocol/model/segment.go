package model

// 分包消息结构
type Segment struct {
	Phone    string `json:"phone"`
	MsgID    uint16 `json:"msgId"`
	SegTotal uint16 `json:"total"`
	SegNo    uint16 `json:"no"`
	Data     []byte `json:"data"`
}

func (s *Segment) IsComplete() bool {
	return s.SegNo == s.SegTotal
}

func (s *Segment) Merge(ns *Segment) {
	s.SegNo = ns.SegNo
	s.Data = append(s.Data, ns.Data...)
}

func NewSegment(pd *PacketData) *Segment {
	return &Segment{
		Phone:    pd.Header.PhoneNumber,
		MsgID:    pd.Header.MsgID,
		SegTotal: pd.Header.Frag.Total,
		SegNo:    pd.Header.Frag.Index,
		Data:     pd.Body,
	}
}
