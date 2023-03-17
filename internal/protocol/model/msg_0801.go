package model

// 多媒体数据上传
// 与JTT1078合用时，此消息只上传图片数据
type Msg0801 struct {
	Header              *MsgHeader `json:"header"`
	MultiMediaID        uint32     `json:"multiMediaId"`
	MultiMediaType      uint8      `json:"multiMediaType"`      // 多媒体类型。0:图像;1:音频;2:视频;
	MultiMediaContainer uint8      `json:"multiMediaContainer"` // 多媒体格式编码。0:JPEG;1:TIF;2:MP3;3:WAV;4:WMV; 其他保留
	EventID             uint8      `json:"eventId"`             // 事件项编码。0:平台下发指令;1:定时动作;2:抢劫报警触 发;3:碰撞侧翻报警触发;其他保留
	LogicChannelID      uint8      `json:"logicChannelId"`      // 逻辑通道ID
	GeoAlarmBody        []byte     `json:"geoAlarmBody"`        // 位置信息汇报消息体
	FragmentData        []byte     `json:"fragmentData"`        // 多媒体数据包
}

func (m *Msg0801) Decode(packet *PacketData) error {
	m.Header = packet.Header
	return nil
}

func (m *Msg0801) Encode() (pkt []byte, err error) {
	pkt, err = writeHeader(m, pkt)
	return pkt, err
}

func (m *Msg0801) GetHeader() *MsgHeader {
	return m.Header
}

func (m *Msg0801) GenOutgoing(incoming JT808Msg) error {
	return nil
}
