package model

import (
	"errors"

	"github.com/fakeyanss/jt808-server-go/internal/codec/hex"
)

// ErrExtItemLength 表示附加信息内容长度与当前协议域定义不一致。
// 该错误只在解析器内部使用，ParseExtItems 会把它降级为 raw 兜底项。
var ErrExtItemLength = errors.New("ext item length mismatch")

// ExtDomain 位置附加信息的协议域。
//
// 0x64~0x67 在苏标与 2019/粤标下的"报警标识号"长度不同：
//   - 苏标（T/JSATL 12-2017，基于 2011/2013）：报警标识号 16 字节，
//     其中终端ID 7 字节、预留 1 字节；
//   - base2019（JT/T 808-2019，含粤标主动安全扩展）：报警标识号 40 字节，
//     其中终端ID 30 字节、预留 2 字节。
//
// 因此必须按域分表解析。若用同一张全局表，会把 7 字节终端ID 当作 30 字节
// 终端ID 的前缀，导致字段跨域串读。
type ExtDomain string

const (
	ExtDomainSubiao   ExtDomain = "subiao"   // 苏标 T/JSATL 12-2017（基于 2011/2013 协议）
	ExtDomainBase2019 ExtDomain = "base2019" // JT/T 808-2019（含粤标主动安全扩展）
)

// 扩展信息类型标识，供回调消费方直接区分 ADAS/DSM/TPMS/BSD 与 raw 兜底项。
const (
	ExtTypeADAS = "adas"
	ExtTypeDSM  = "dsm"
	ExtTypeTPMS = "tpms"
	ExtTypeBSD  = "bsd"
	ExtTypeRaw  = "raw"
)

// ExtItemMeta 是所有已解析附加信息项共有的元数据。
type ExtItemMeta struct {
	ExtID     uint8     `json:"extId"`     // 附加信息ID
	ExtType   string    `json:"extType"`   // adas/dsm/tpms/bsd/raw
	ExtDomain ExtDomain `json:"extDomain"` // 实际采用的协议域
}

// ExtRawItem 是未注册或长度不符合当前协议域的附加信息项。
// 原始字节被完整保留，保证未知 ID 不丢包、解析失败不丢包。
type ExtRawItem struct {
	ExtItemMeta
	Length uint8  `json:"length"` // 附加信息长度（BYTE）
	Raw    []byte `json:"raw"`    // 原始字节
}

// AlarmIDNumber 是苏标/粤标报警标识号。
//
// 苏标文档没有单独的"车牌号/车辆类型"字段，报警标识号中的终端ID 是最接近的
// 车辆终端标识。车牌号与车辆类型仍需结合注册信息（0x0100）关联。
type AlarmIDNumber struct {
	DeviceID        string `json:"deviceId"`        // 终端ID：苏标 7 字节，base2019 30 字节
	Time            string `json:"time"`            // BCD 原始串 YYMMDDhhmmss
	Sequence        uint8  `json:"sequence"`        // 同一时间点报警的序号
	AttachmentCount uint8  `json:"attachmentCount"` // 附件数量
	Reserved        []byte `json:"reserved"`        // 预留：苏标 1 字节，base2019 2 字节
}

// ExtAlarmADAS 是 0x64 高级驾驶辅助系统报警信息。
type ExtAlarmADAS struct {
	ExtItemMeta
	AlarmID              uint32        `json:"alarmId"`
	FlagState            uint8         `json:"flagState"`
	AlarmOrEventType     uint8         `json:"alarmOrEventType"`
	AlarmLevel           uint8         `json:"alarmLevel"`
	FrontVehicleSpeed    uint8         `json:"frontVehicleSpeed"`
	FrontVehicleDistance uint8         `json:"frontVehicleDistance"`
	DeviationType        uint8         `json:"deviationType"`
	RoadSignType         uint8         `json:"roadSignType"`
	RoadSignData         uint8         `json:"roadSignData"`
	Speed                uint8         `json:"speed"`
	Altitude             uint16        `json:"altitude"`
	Latitude             uint32        `json:"latitude"`
	Longitude            uint32        `json:"longitude"`
	DateTime             string        `json:"dateTime"`
	VehicleState         uint16        `json:"vehicleState"`
	AlarmIDNumber        AlarmIDNumber `json:"alarmIdNumber"`
}

// ExtAlarmDSM 是 0x65 驾驶员状态监测系统报警信息。
type ExtAlarmDSM struct {
	ExtItemMeta
	AlarmID          uint32        `json:"alarmId"`
	FlagState        uint8         `json:"flagState"`
	AlarmOrEventType uint8         `json:"alarmOrEventType"`
	AlarmLevel       uint8         `json:"alarmLevel"`
	FatigueDegree    uint8         `json:"fatigueDegree"`
	Reserved         []byte        `json:"reserved"`
	Speed            uint8         `json:"speed"`
	Altitude         uint16        `json:"altitude"`
	Latitude         uint32        `json:"latitude"`
	Longitude        uint32        `json:"longitude"`
	DateTime         string        `json:"dateTime"`
	VehicleState     uint16        `json:"vehicleState"`
	AlarmIDNumber    AlarmIDNumber `json:"alarmIdNumber"`
}

// ExtTPMSItem 是 0x66 胎压报警/事件列表中的一项。
type ExtTPMSItem struct {
	Position         uint16 `json:"position"`
	AlarmOrEventType uint16 `json:"alarmOrEventType"`
	Pressure         uint16 `json:"pressure"`
	Temperature      uint16 `json:"temperature"`
	BatteryLevel     uint16 `json:"batteryLevel"`
}

// ExtAlarmTPMS 是 0x66 轮胎气压监测系统报警信息。
type ExtAlarmTPMS struct {
	ExtItemMeta
	AlarmID       uint32        `json:"alarmId"`
	FlagState     uint8         `json:"flagState"`
	Speed         uint8         `json:"speed"`
	Altitude      uint16        `json:"altitude"`
	Latitude      uint32        `json:"latitude"`
	Longitude     uint32        `json:"longitude"`
	DateTime      string        `json:"dateTime"`
	VehicleState  uint16        `json:"vehicleState"`
	AlarmIDNumber AlarmIDNumber `json:"alarmIdNumber"`
	Items         []ExtTPMSItem `json:"items"`
}

// ExtAlarmBSD 是 0x67 盲区监测系统报警信息。
type ExtAlarmBSD struct {
	ExtItemMeta
	AlarmID          uint32        `json:"alarmId"`
	FlagState        uint8         `json:"flagState"`
	AlarmOrEventType uint8         `json:"alarmOrEventType"`
	Speed            uint8         `json:"speed"`
	Altitude         uint16        `json:"altitude"`
	Latitude         uint32        `json:"latitude"`
	Longitude        uint32        `json:"longitude"`
	DateTime         string        `json:"dateTime"`
	VehicleState     uint16        `json:"vehicleState"`
	AlarmIDNumber    AlarmIDNumber `json:"alarmIdNumber"`
}

type extParserFunc func(data []byte) (any, error)

// 已注册 ID 按协议域分表维护：0x64/0x65/0x66/0x67 在两个域中的
// 报警标识号长度不同，后续若出现同 ID 语义差异也可直接覆盖对应表项。
var subiaoExtParsers = map[uint8]extParserFunc{
	0x64: func(data []byte) (any, error) { return parseADAS(data, ExtDomainSubiao) },
	0x65: func(data []byte) (any, error) { return parseDSM(data, ExtDomainSubiao) },
	0x66: func(data []byte) (any, error) { return parseTPMS(data, ExtDomainSubiao) },
	0x67: func(data []byte) (any, error) { return parseBSD(data, ExtDomainSubiao) },
}

var base2019ExtParsers = map[uint8]extParserFunc{
	0x64: func(data []byte) (any, error) { return parseADAS(data, ExtDomainBase2019) },
	0x65: func(data []byte) (any, error) { return parseDSM(data, ExtDomainBase2019) },
	0x66: func(data []byte) (any, error) { return parseTPMS(data, ExtDomainBase2019) },
	0x67: func(data []byte) (any, error) { return parseBSD(data, ExtDomainBase2019) },
}

var extParserTables = map[ExtDomain]map[uint8]extParserFunc{
	ExtDomainSubiao:   subiaoExtParsers,
	ExtDomainBase2019: base2019ExtParsers,
}

// ExtDomainForHeader 按消息头协议版本位选择附加信息域：
// 2019 版本使用 base2019 布局（粤标终端ID 30 字节），否则使用苏标布局。
func ExtDomainForHeader(h *MsgHeader) ExtDomain {
	if h != nil && h.Attr != nil && h.Attr.VersionDesc == Version2019 {
		return ExtDomainBase2019
	}
	return ExtDomainSubiao
}

// ParseExtItems 按协议域把位置附加信息解析为结构化项。
//
// 未注册的 ID、长度不符合域定义的项一律降级为 ExtRawItem，保留原始字节；
// 因此本函数不会因为未知 ID 而失败。
func ParseExtItems(items []LocationExtItem, domain ExtDomain) []any {
	table := extParserTables[domain]
	parsed := make([]any, 0, len(items))
	for _, item := range items {
		parser, ok := table[item.ID]
		if !ok {
			parsed = append(parsed, newExtRawItem(item, domain))
			continue
		}
		value, err := parser(item.Data)
		if err != nil {
			parsed = append(parsed, newExtRawItem(item, domain))
			continue
		}
		parsed = append(parsed, value)
	}
	return parsed
}

// ParseExtItems 使用消息头协议版本自动选择域并解析附加信息。
func (m *Msg0200) ParseExtItems() []any {
	if m == nil {
		return nil
	}
	return ParseExtItems(m.ExtItems, ExtDomainForHeader(m.Header))
}

// AlarmIDNumberBytes 从 0x0200 附加信息中的 0x64~0x67 提取苏标 16 字节报警标识号。
//
// 仅当协议域为苏标（T/JSATL 12-2017）且扩展项长度与定义一致时返回副本；
// base2019 域的报警标识号为 40 字节，语义与苏标 0x9208 不同，这里不做截断或转换。
// 取不到时返回 nil，调用方不得自行拼装报警标识号。
func (m *Msg0200) AlarmIDNumberBytes() []byte {
	if m == nil || ExtDomainForHeader(m.Header) != ExtDomainSubiao {
		return nil
	}
	for _, item := range m.ExtItems {
		if id := subiaoAlarmIDFromExtItem(item); id != nil {
			return id
		}
	}
	return nil
}

// subiaoAlarmIDFromExtItem 按苏标 4.3 的固定布局截取报警标识号。
// 返回 nil 表示该扩展项不是 0x64~0x67，或长度与定义不符。
func subiaoAlarmIDFromExtItem(item LocationExtItem) []byte {
	const alarmIDLen = 16
	switch item.ID {
	case 0x64, 0x65:
		// 固定字段 31 字节 + 报警标识号 16 字节
		if len(item.Data) == 31+alarmIDLen {
			return append([]byte(nil), item.Data[31:31+alarmIDLen]...)
		}
	case 0x66:
		// 固定字段 24 字节 + 报警标识号 16 字节 + 轮胎列表
		if len(item.Data) < 24+alarmIDLen+1 {
			return nil
		}
		listTotal := int(item.Data[24+alarmIDLen])
		if len(item.Data) == 24+alarmIDLen+1+listTotal*10 {
			return append([]byte(nil), item.Data[24:24+alarmIDLen]...)
		}
	case 0x67:
		// 固定字段 25 字节 + 报警标识号 16 字节
		if len(item.Data) == 25+alarmIDLen {
			return append([]byte(nil), item.Data[25:25+alarmIDLen]...)
		}
	}
	return nil
}

// ExtRawBytes 汇总所有 raw 兜底项的原始字节，便于业务侧留档与二次解析。
func ExtRawBytes(items []any) []byte {
	var raw []byte
	for _, item := range items {
		if fallback, ok := item.(ExtRawItem); ok {
			raw = append(raw, fallback.Raw...)
		}
	}
	return raw
}

func newExtRawItem(item LocationExtItem, domain ExtDomain) ExtRawItem {
	return ExtRawItem{
		ExtItemMeta: ExtItemMeta{ExtID: item.ID, ExtType: ExtTypeRaw, ExtDomain: domain},
		Length:      item.Length,
		Raw:         append([]byte(nil), item.Data...),
	}
}

func extMeta(id uint8, extType string, domain ExtDomain) ExtItemMeta {
	return ExtItemMeta{ExtID: id, ExtType: extType, ExtDomain: domain}
}

func alarmIDNumberLength(domain ExtDomain) int {
	if domain == ExtDomainBase2019 {
		return 40
	}
	return 16
}

func deviceIDLength(domain ExtDomain) int {
	if domain == ExtDomainBase2019 {
		return 30
	}
	return 7
}

func reservedLength(domain ExtDomain) int {
	if domain == ExtDomainBase2019 {
		return 2
	}
	return 1
}

func parseAlarmIDNumber(data []byte, domain ExtDomain) (AlarmIDNumber, error) {
	idLen := deviceIDLength(domain)
	reservedLen := reservedLength(domain)
	if len(data) != idLen+6+1+1+reservedLen {
		return AlarmIDNumber{}, ErrExtItemLength
	}
	idx := 0
	return AlarmIDNumber{
		DeviceID:        hex.ReadString(data, &idx, idLen),
		Time:            hex.ReadBCD(data, &idx, 6),
		Sequence:        hex.ReadByte(data, &idx),
		AttachmentCount: hex.ReadByte(data, &idx),
		Reserved:        append([]byte(nil), hex.ReadBytes(data, &idx, reservedLen)...),
	}, nil
}

func parseADAS(data []byte, domain ExtDomain) (any, error) {
	const fixedLen = 31
	if len(data) != fixedLen+alarmIDNumberLength(domain) {
		return nil, ErrExtItemLength
	}
	idx := 0
	msg := ExtAlarmADAS{
		ExtItemMeta:          extMeta(0x64, ExtTypeADAS, domain),
		AlarmID:              hex.ReadDoubleWord(data, &idx),
		FlagState:            hex.ReadByte(data, &idx),
		AlarmOrEventType:     hex.ReadByte(data, &idx),
		AlarmLevel:           hex.ReadByte(data, &idx),
		FrontVehicleSpeed:    hex.ReadByte(data, &idx),
		FrontVehicleDistance: hex.ReadByte(data, &idx),
		DeviationType:        hex.ReadByte(data, &idx),
		RoadSignType:         hex.ReadByte(data, &idx),
		RoadSignData:         hex.ReadByte(data, &idx),
		Speed:                hex.ReadByte(data, &idx),
		Altitude:             hex.ReadWord(data, &idx),
		Latitude:             hex.ReadDoubleWord(data, &idx),
		Longitude:            hex.ReadDoubleWord(data, &idx),
		DateTime:             hex.ReadBCD(data, &idx, 6),
		VehicleState:         hex.ReadWord(data, &idx),
	}
	idNumber, err := parseAlarmIDNumber(data[idx:], domain)
	if err != nil {
		return nil, err
	}
	msg.AlarmIDNumber = idNumber
	return msg, nil
}

func parseDSM(data []byte, domain ExtDomain) (any, error) {
	const fixedLen = 31
	if len(data) != fixedLen+alarmIDNumberLength(domain) {
		return nil, ErrExtItemLength
	}
	idx := 0
	msg := ExtAlarmDSM{
		ExtItemMeta:      extMeta(0x65, ExtTypeDSM, domain),
		AlarmID:          hex.ReadDoubleWord(data, &idx),
		FlagState:        hex.ReadByte(data, &idx),
		AlarmOrEventType: hex.ReadByte(data, &idx),
		AlarmLevel:       hex.ReadByte(data, &idx),
		FatigueDegree:    hex.ReadByte(data, &idx),
		Reserved:         append([]byte(nil), hex.ReadBytes(data, &idx, 4)...),
		Speed:            hex.ReadByte(data, &idx),
		Altitude:         hex.ReadWord(data, &idx),
		Latitude:         hex.ReadDoubleWord(data, &idx),
		Longitude:        hex.ReadDoubleWord(data, &idx),
		DateTime:         hex.ReadBCD(data, &idx, 6),
		VehicleState:     hex.ReadWord(data, &idx),
	}
	idNumber, err := parseAlarmIDNumber(data[idx:], domain)
	if err != nil {
		return nil, err
	}
	msg.AlarmIDNumber = idNumber
	return msg, nil
}

func parseTPMS(data []byte, domain ExtDomain) (any, error) {
	baseLen := 24 + alarmIDNumberLength(domain)
	if len(data) < baseLen+1 {
		return nil, ErrExtItemLength
	}
	idx := 0
	msg := ExtAlarmTPMS{
		ExtItemMeta:  extMeta(0x66, ExtTypeTPMS, domain),
		AlarmID:      hex.ReadDoubleWord(data, &idx),
		FlagState:    hex.ReadByte(data, &idx),
		Speed:        hex.ReadByte(data, &idx),
		Altitude:     hex.ReadWord(data, &idx),
		Latitude:     hex.ReadDoubleWord(data, &idx),
		Longitude:    hex.ReadDoubleWord(data, &idx),
		DateTime:     hex.ReadBCD(data, &idx, 6),
		VehicleState: hex.ReadWord(data, &idx),
	}
	idNumber, err := parseAlarmIDNumber(data[idx:idx+alarmIDNumberLength(domain)], domain)
	if err != nil {
		return nil, err
	}
	msg.AlarmIDNumber = idNumber

	listTotal := int(data[baseLen])
	const itemLen = 10
	if len(data) != baseLen+1+listTotal*itemLen {
		return nil, ErrExtItemLength
	}
	msg.Items = make([]ExtTPMSItem, 0, listTotal)
	for i := 0; i < listTotal; i++ {
		itemData := data[baseLen+1+i*itemLen : baseLen+1+(i+1)*itemLen]
		itemIdx := 0
		msg.Items = append(msg.Items, ExtTPMSItem{
			Position:         hex.ReadWord(itemData, &itemIdx),
			AlarmOrEventType: hex.ReadWord(itemData, &itemIdx),
			Pressure:         hex.ReadWord(itemData, &itemIdx),
			Temperature:      hex.ReadWord(itemData, &itemIdx),
			BatteryLevel:     hex.ReadWord(itemData, &itemIdx),
		})
	}
	return msg, nil
}

func parseBSD(data []byte, domain ExtDomain) (any, error) {
	const fixedLen = 25
	if len(data) != fixedLen+alarmIDNumberLength(domain) {
		return nil, ErrExtItemLength
	}
	idx := 0
	msg := ExtAlarmBSD{
		ExtItemMeta:      extMeta(0x67, ExtTypeBSD, domain),
		AlarmID:          hex.ReadDoubleWord(data, &idx),
		FlagState:        hex.ReadByte(data, &idx),
		AlarmOrEventType: hex.ReadByte(data, &idx),
		Speed:            hex.ReadByte(data, &idx),
		Altitude:         hex.ReadWord(data, &idx),
		Latitude:         hex.ReadDoubleWord(data, &idx),
		Longitude:        hex.ReadDoubleWord(data, &idx),
		DateTime:         hex.ReadBCD(data, &idx, 6),
		VehicleState:     hex.ReadWord(data, &idx),
	}
	idNumber, err := parseAlarmIDNumber(data[idx:], domain)
	if err != nil {
		return nil, err
	}
	msg.AlarmIDNumber = idNumber
	return msg, nil
}
