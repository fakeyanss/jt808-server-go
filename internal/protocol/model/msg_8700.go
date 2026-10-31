package model

// MsgRecordCommand represents 0x8700/0x8701: a GB/T 19056 command byte and
// the corresponding standard-defined data block.
type MsgRecordCommand struct {
	Header    *MsgHeader `json:"header"`
	Command   uint8      `json:"command"`
	Data      []byte     `json:"data"`
	DataBlock any        `json:"dataBlock,omitempty"` // typed block, nil if command is unsupported
}

func (m *MsgRecordCommand) Decode(p *PacketData) error {
	if p == nil || p.Header == nil || len(p.Body) < 1 {
		return ErrDecodeMsg
	}
	m.Header = p.Header
	m.Command = p.Body[0]
	m.Data = append([]byte(nil), p.Body[1:]...)
	m.DataBlock = decodeRecorderCommandBlock(m.Command, m.Data)
	return nil
}
func (m *MsgRecordCommand) Encode() ([]byte, error) {
	if m.Header == nil {
		return nil, ErrEncodeMsg
	}
	data, err := encodeRecorderBlock(m.DataBlock, m.Data)
	if err != nil {
		return nil, err
	}
	b := append([]byte{m.Command}, data...)
	return writeHeader(m, b)
}
func (m *MsgRecordCommand) GetHeader() *MsgHeader        { return m.Header }
func (m *MsgRecordCommand) GenOutgoing(_ JT808Msg) error { return ErrGenOutgoingMsg }

// decodeRecorderCommandBlock decodes the GB/T 19056 data block of a platform
// command (0x8700/0x8701) into a typed struct. Unknown commands return nil.
func decodeRecorderCommandBlock(command uint8, data []byte) any {
	switch command {
	case RecorderCmdCollectInfo:
		b := &RecorderInfoRequest{}
		if b.Decode(data) == nil {
			return b
		}
	case RecorderCmdCollectFile:
		b := &RecorderFileRequest{}
		if b.Decode(data) == nil {
			return b
		}
	case RecorderCmdParamSet:
		b := &RecorderParamSetting{}
		if b.Decode(data) == nil {
			return b
		}
	case RecorderCmdPositionTestOn, RecorderCmdPositionTestOff:
		b := &RecorderPositionTestRequest{}
		if b.Decode(data) == nil {
			return b
		}
	case RecorderCmdDigestTest:
		b := &RecorderDigestTestRequest{}
		if b.Decode(data) == nil {
			return b
		}
	}
	return nil
}

// decodeRecorderResponseBlock decodes the GB/T 19056 data block of a terminal
// response (0x0700) into a typed struct. The 0x0700 command byte echoes the
// platform command, so request command values are mapped to response formats.
// Unknown commands return nil.
func decodeRecorderResponseBlock(command uint8, data []byte) any {
	switch command {
	case RecorderCmdCollectInfo:
		b := &RecorderInfoResponse{}
		if b.Decode(data) == nil {
			return b
		}
	case RecorderCmdCollectFile:
		b := &RecorderFileResponse{}
		if b.Decode(data) == nil {
			return b
		}
	case RecorderCmdParamSet:
		b := &RecorderParamSetting{}
		if b.Decode(data) == nil {
			return b
		}
	case RecorderCmdPositionTestOn, RecorderCmdPositionTestOff:
		b := &RecorderPositionTestResponse{}
		if b.Decode(data) == nil {
			return b
		}
	case RecorderCmdDigestTest:
		b := &RecorderDigestTestResponse{}
		if b.Decode(data) == nil {
			return b
		}
	}
	return nil
}

// encodeRecorderBlock returns the encoded bytes of a typed data block. If the
// block is nil or has a nil pointer, the raw fallback data is used.
func encodeRecorderBlock(block any, fallback []byte) ([]byte, error) {
	switch b := block.(type) {
	case *RecorderInfoRequest:
		return b.Encode()
	case *RecorderInfoResponse:
		return b.Encode()
	case *RecorderFileRequest:
		return b.Encode()
	case *RecorderFileResponse:
		return b.Encode()
	case *RecorderParamSetting:
		return b.Encode()
	case *RecorderPositionTestRequest:
		return b.Encode()
	case *RecorderPositionTestResponse:
		return b.Encode()
	case *RecorderDigestTestRequest:
		return b.Encode()
	case *RecorderDigestTestResponse:
		return b.Encode()
	}
	return append([]byte(nil), fallback...), nil
}
