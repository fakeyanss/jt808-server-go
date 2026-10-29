package model

import "github.com/fakeyanss/jt808-server-go/internal/codec/hex"

// GB/T 19056-2021 command words carried by JT808 0x8700 / 0x8701.
const (
	RecorderCmdCollectInfo     uint8 = 0x31 // 采集记录仪信息
	RecorderCmdCollectFile     uint8 = 0x33 // 采集指定的数据记录文件
	RecorderCmdParamSet        uint8 = 0x41 // 记录仪参数设置 (0x8701)
	RecorderCmdPositionTestOn  uint8 = 0x56 // 定位性能测试开始
	RecorderCmdPositionTestOff uint8 = 0x57 // 定位性能测试结束
	RecorderCmdDigestTest      uint8 = 0x58 // 数据摘要测试

	// Response command words are the request word with bit 7 set.
	RecorderRespCollectInfo  uint8 = 0xB1
	RecorderRespCollectFile  uint8 = 0xB3
	RecorderRespParamSet     uint8 = 0xC1
	RecorderRespPositionTest uint8 = 0xD6
	RecorderRespDigestTest   uint8 = 0xD8
)

// Data record file category identifiers used by 0x33/0xB3.
const (
	FileCategoryDrivingState uint8 = 0x21 // 行驶状态记录
	FileCategoryAccident     uint8 = 0x22 // 事故疑点记录
	FileCategoryOvertime     uint8 = 0x23 // 超时驾驶记录
	FileCategoryDriver       uint8 = 0x24 // 驾驶人记录
	FileCategoryLog          uint8 = 0x25 // 日志记录
)

// RecorderInfoRequest is the 0x31 command data block.
type RecorderInfoRequest struct {
	StandardVersion string // BCD[1], last two digits of the standard year
	RevisionNo      uint8
	CommTime        string // BCD[6] YYMMDDhhmmss
	CommDeviceName  string // STR[16]
}

func (r *RecorderInfoRequest) Decode(data []byte) error {
	if len(data) != 24 {
		return ErrDecodeMsg
	}
	i := 0
	r.StandardVersion = hex.ReadBCD(data, &i, 1)
	r.RevisionNo = hex.ReadByte(data, &i)
	r.CommTime = hex.ReadBCD(data, &i, 6)
	r.CommDeviceName = trimNUL(hex.ReadString(data, &i, 16))
	return nil
}
func (r *RecorderInfoRequest) Encode() ([]byte, error) {
	if len(r.StandardVersion) != 2 || len(r.CommTime) != 12 || len(r.CommDeviceName) > 16 {
		return nil, ErrEncodeMsg
	}
	b := hex.WriteBCD(nil, r.StandardVersion)
	b = append(b, r.RevisionNo)
	b = hex.WriteBCD(b, r.CommTime)
	b = appendPadded(b, r.CommDeviceName, 16)
	return b, nil
}

// RecorderInfoResponse is the 0xB1 response data block.
type RecorderInfoResponse struct {
	Magic           string // ASCII[8], fixed "GBT19056"
	StandardVersion string // BCD[1]
	RevisionNo      uint8
	RecorderTime    string // BCD[6]
	RecorderID      []byte // BIN[35]
	PlateNo         string // STR[14]
	PlateCategory   string // STR[16]
	VIN             string // ASCII[17]
	SerialNo        string // BCD[6]
	PulseCoeff      uint16
	InstallTime     string // BCD[6]
}

func (r *RecorderInfoResponse) Decode(data []byte) error {
	if len(data) != 112 {
		return ErrDecodeMsg
	}
	i := 0
	r.Magic = hex.ReadString(data, &i, 8)
	r.StandardVersion = hex.ReadBCD(data, &i, 1)
	r.RevisionNo = hex.ReadByte(data, &i)
	r.RecorderTime = hex.ReadBCD(data, &i, 6)
	r.RecorderID = append([]byte(nil), hex.ReadBytes(data, &i, 35)...)
	r.PlateNo = trimNUL(hex.ReadString(data, &i, 14))
	r.PlateCategory = trimNUL(hex.ReadString(data, &i, 16))
	r.VIN = trimNUL(hex.ReadString(data, &i, 17))
	r.SerialNo = hex.ReadBCD(data, &i, 6)
	r.PulseCoeff = hex.ReadWord(data, &i)
	r.InstallTime = hex.ReadBCD(data, &i, 6)
	return nil
}
func (r *RecorderInfoResponse) Encode() ([]byte, error) {
	if r.Magic != "GBT19056" || len(r.StandardVersion) != 2 || len(r.RecorderTime) != 12 ||
		len(r.RecorderID) != 35 || len(r.PlateNo) > 14 || len(r.PlateCategory) > 16 ||
		len(r.VIN) > 17 || len(r.SerialNo) != 12 || len(r.InstallTime) != 12 {
		return nil, ErrEncodeMsg
	}
	b := []byte(r.Magic)
	b = hex.WriteBCD(b, r.StandardVersion)
	b = append(b, r.RevisionNo)
	b = hex.WriteBCD(b, r.RecorderTime)
	b = append(b, r.RecorderID...)
	b = appendPadded(b, r.PlateNo, 14)
	b = appendPadded(b, r.PlateCategory, 16)
	b = appendPadded(b, r.VIN, 17)
	b = hex.WriteBCD(b, r.SerialNo)
	b = hex.WriteWord(b, r.PulseCoeff)
	b = hex.WriteBCD(b, r.InstallTime)
	return b, nil
}

// RecorderFileRequestItem is one entry of the 0x33 command data block.
type RecorderFileRequestItem struct {
	Category  uint8
	StartTime string // BCD[6]
	EndTime   string // BCD[6]
}

// RecorderFileRequest is the 0x33 command data block (repeated items).
type RecorderFileRequest struct {
	Items []RecorderFileRequestItem
}

func (r *RecorderFileRequest) Decode(data []byte) error {
	if len(data) == 0 || len(data)%13 != 0 {
		return ErrDecodeMsg
	}
	r.Items = r.Items[:0]
	for i := 0; i < len(data); i += 13 {
		item := RecorderFileRequestItem{}
		j := i
		item.Category = hex.ReadByte(data, &j)
		if item.Category < FileCategoryDrivingState || item.Category > FileCategoryLog {
			return ErrDecodeMsg
		}
		item.StartTime = hex.ReadBCD(data, &j, 6)
		item.EndTime = hex.ReadBCD(data, &j, 6)
		r.Items = append(r.Items, item)
	}
	return nil
}
func (r *RecorderFileRequest) Encode() ([]byte, error) {
	if len(r.Items) == 0 {
		return nil, ErrEncodeMsg
	}
	b := make([]byte, 0, len(r.Items)*13)
	for _, item := range r.Items {
		if item.Category < FileCategoryDrivingState || item.Category > FileCategoryLog ||
			len(item.StartTime) != 12 || len(item.EndTime) != 12 {
			return nil, ErrEncodeMsg
		}
		b = append(b, item.Category)
		b = hex.WriteBCD(b, item.StartTime)
		b = hex.WriteBCD(b, item.EndTime)
	}
	return b, nil
}

// RecorderFileResponseItem is one entry of the 0xB3 response data block.
type RecorderFileResponseItem struct {
	Category   uint8
	FileLength uint32
	FileData   []byte
}

// RecorderFileResponse is the 0xB3 response data block (repeated items).
type RecorderFileResponse struct {
	Items []RecorderFileResponseItem
}

func (r *RecorderFileResponse) Decode(data []byte) error {
	r.Items = r.Items[:0]
	i := 0
	for i < len(data) {
		if i+5 > len(data) {
			return ErrDecodeMsg
		}
		item := RecorderFileResponseItem{}
		item.Category = hex.ReadByte(data, &i)
		if item.Category < FileCategoryDrivingState || item.Category > FileCategoryLog {
			return ErrDecodeMsg
		}
		item.FileLength = hex.ReadDoubleWord(data, &i)
		if i+int(item.FileLength) > len(data) {
			return ErrDecodeMsg
		}
		item.FileData = append([]byte(nil), data[i:i+int(item.FileLength)]...)
		i += int(item.FileLength)
		r.Items = append(r.Items, item)
	}
	if len(r.Items) == 0 {
		return ErrDecodeMsg
	}
	return nil
}
func (r *RecorderFileResponse) Encode() ([]byte, error) {
	if len(r.Items) == 0 {
		return nil, ErrEncodeMsg
	}
	b := make([]byte, 0)
	for _, item := range r.Items {
		if item.Category < FileCategoryDrivingState || item.Category > FileCategoryLog {
			return nil, ErrEncodeMsg
		}
		b = append(b, item.Category)
		b = hex.WriteDoubleWord(b, item.FileLength)
		b = append(b, item.FileData...)
	}
	return b, nil
}

// RecorderParamSetting is the 0x41 command (0x8701) and 0xC1 response data block.
// The Salt field is only present in the 0x41 command, not the 0xC1 response.
type RecorderParamSetting struct {
	ParamFlags    uint8
	RecorderID    []byte // BIN[35]
	PlateNo       string // STR[14]
	PlateCategory string // STR[16]
	VIN           string // ASCII[17]
	SerialNo      string // BCD[6]
	PulseCoeff    uint16
	InstallTime   string // BCD[6]
	Salt          []byte // BIN[16], only for 0x41 command
}

func (r *RecorderParamSetting) Decode(data []byte) error {
	if len(data) != 97 && len(data) != 113 {
		return ErrDecodeMsg
	}
	i := 0
	r.ParamFlags = hex.ReadByte(data, &i)
	r.RecorderID = append([]byte(nil), hex.ReadBytes(data, &i, 35)...)
	r.PlateNo = trimNUL(hex.ReadString(data, &i, 14))
	r.PlateCategory = trimNUL(hex.ReadString(data, &i, 16))
	r.VIN = trimNUL(hex.ReadString(data, &i, 17))
	r.SerialNo = hex.ReadBCD(data, &i, 6)
	r.PulseCoeff = hex.ReadWord(data, &i)
	r.InstallTime = hex.ReadBCD(data, &i, 6)
	if i == len(data) {
		r.Salt = nil
		return nil
	}
	if len(data)-i != 16 {
		return ErrDecodeMsg
	}
	r.Salt = append([]byte(nil), hex.ReadBytes(data, &i, 16)...)
	return nil
}
func (r *RecorderParamSetting) Encode() ([]byte, error) {
	if len(r.RecorderID) != 35 || len(r.PlateNo) > 14 || len(r.PlateCategory) > 16 ||
		len(r.VIN) > 17 || len(r.SerialNo) != 12 || len(r.InstallTime) != 12 {
		return nil, ErrEncodeMsg
	}
	b := []byte{r.ParamFlags}
	b = append(b, r.RecorderID...)
	b = appendPadded(b, r.PlateNo, 14)
	b = appendPadded(b, r.PlateCategory, 16)
	b = appendPadded(b, r.VIN, 17)
	b = hex.WriteBCD(b, r.SerialNo)
	b = hex.WriteWord(b, r.PulseCoeff)
	b = hex.WriteBCD(b, r.InstallTime)
	if len(r.Salt) > 0 {
		if len(r.Salt) != 16 {
			return nil, ErrEncodeMsg
		}
		b = append(b, r.Salt...)
	}
	return b, nil
}

// RecorderPositionTestRequest is the 0x56/0x57 command data block.
// 0x57 (stop) has an empty data block.
type RecorderPositionTestRequest struct {
	Duration uint16 // seconds, only for 0x56
}

func (r *RecorderPositionTestRequest) Decode(data []byte) error {
	if len(data) == 0 {
		r.Duration = 0
		return nil
	}
	if len(data) != 2 {
		return ErrDecodeMsg
	}
	i := 0
	r.Duration = hex.ReadWord(data, &i)
	return nil
}
func (r *RecorderPositionTestRequest) Encode() ([]byte, error) {
	if r.Duration == 0 {
		return nil, nil
	}
	return hex.WriteWord(nil, r.Duration), nil
}

// RecorderPositionTestResponse is the 0xD6 response data block.
type RecorderPositionTestResponse struct {
	Time    string // BCD[6]
	GGALen  uint16
	RMCLen  uint16
	GGAData string
	RMCData string
}

func (r *RecorderPositionTestResponse) Decode(data []byte) error {
	if len(data) < 10 {
		return ErrDecodeMsg
	}
	i := 0
	r.Time = hex.ReadBCD(data, &i, 6)
	r.GGALen = hex.ReadWord(data, &i)
	r.RMCLen = hex.ReadWord(data, &i)
	if i+int(r.GGALen)+int(r.RMCLen) != len(data) {
		return ErrDecodeMsg
	}
	r.GGAData = hex.ReadString(data, &i, int(r.GGALen))
	r.RMCData = hex.ReadString(data, &i, int(r.RMCLen))
	return nil
}
func (r *RecorderPositionTestResponse) Encode() ([]byte, error) {
	if len(r.Time) != 12 || len(r.GGAData) > 65535 || len(r.RMCData) > 65535 {
		return nil, ErrEncodeMsg
	}
	b := hex.WriteBCD(nil, r.Time)
	b = hex.WriteWord(b, uint16(len(r.GGAData)))
	b = hex.WriteWord(b, uint16(len(r.RMCData)))
	b = append(b, []byte(r.GGAData)...)
	b = append(b, []byte(r.RMCData)...)
	return b, nil
}

// RecorderDigestTestRequest is the 0x58 command data block.
type RecorderDigestTestRequest struct {
	CommTime string // BCD[6]
}

func (r *RecorderDigestTestRequest) Decode(data []byte) error {
	if len(data) != 6 {
		return ErrDecodeMsg
	}
	i := 0
	r.CommTime = hex.ReadBCD(data, &i, 6)
	return nil
}
func (r *RecorderDigestTestRequest) Encode() ([]byte, error) {
	if len(r.CommTime) != 12 {
		return nil, ErrEncodeMsg
	}
	return hex.WriteBCD(nil, r.CommTime), nil
}

// RecorderDigestTestResponse is the 0xD8 response data block.
type RecorderDigestTestResponse struct {
	RecorderTime string // BCD[6]
	RecorderID   []byte // BIN[35]
	Digest       []byte // BIN[32]
}

func (r *RecorderDigestTestResponse) Decode(data []byte) error {
	if len(data) != 73 {
		return ErrDecodeMsg
	}
	i := 0
	r.RecorderTime = hex.ReadBCD(data, &i, 6)
	r.RecorderID = append([]byte(nil), hex.ReadBytes(data, &i, 35)...)
	r.Digest = append([]byte(nil), hex.ReadBytes(data, &i, 32)...)
	return nil
}
func (r *RecorderDigestTestResponse) Encode() ([]byte, error) {
	if len(r.RecorderTime) != 12 || len(r.RecorderID) != 35 || len(r.Digest) != 32 {
		return nil, ErrEncodeMsg
	}
	b := hex.WriteBCD(nil, r.RecorderTime)
	b = append(b, r.RecorderID...)
	b = append(b, r.Digest...)
	return b, nil
}
