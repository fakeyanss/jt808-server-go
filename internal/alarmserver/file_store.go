package alarmserver

import (
	"crypto/md5"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"time"
)

const (
	partSuffix  = ".part"
	stateSuffix = ".part.state.json"
	metaSuffix  = ".meta.json"
)

// persistedState 是已覆盖区间与文件登记信息的磁盘副本，用于按 fileName 恢复进度。
type persistedState struct {
	FileName     string      `json:"fileName"`
	FileType     uint8       `json:"fileType"`
	Size         int64       `json:"size"`
	Phone        string      `json:"phone"`
	TerminalID   string      `json:"terminalId"`
	AlarmNo      string      `json:"alarmNo"`
	AlarmSign    []byte      `json:"alarmSign"`
	Date         string      `json:"date"`
	Ranges       []byteRange `json:"ranges"`
	LastActivity int64       `json:"lastActivity"` // Unix 纳秒
}

// validateFileName 校验附件文件名：只允许单层文件名与白名单字符，
// 拒绝绝对路径、路径分隔符、"." 与 ".."，防止路径穿越。
func validateFileName(name string) error {
	if name == "" || len(name) > 128 {
		return ErrInvalidFileName
	}
	if filepath.IsAbs(name) || filepath.Base(name) != name {
		return ErrInvalidFileName
	}
	if name == "." || name == ".." || strings.Contains(name, "..") {
		return ErrInvalidFileName
	}
	if strings.ContainsAny(name, `/\`) {
		return ErrInvalidFileName
	}
	for _, r := range name {
		switch {
		case r >= 'a' && r <= 'z':
		case r >= 'A' && r <= 'Z':
		case r >= '0' && r <= '9':
		case r == '_', r == '-', r == '.':
		default:
			return ErrInvalidFileName
		}
	}
	return nil
}

// validateSegment 校验目录段（终端手机号等），只允许字母、数字与少量分隔符。
func validateSegment(segment string) error {
	if segment == "" || segment == "." || segment == ".." || len(segment) > 64 {
		return ErrInvalidDirectory
	}
	for _, r := range segment {
		switch {
		case r >= 'a' && r <= 'z':
		case r >= 'A' && r <= 'Z':
		case r >= '0' && r <= '9':
		case r == '_', r == '-':
		default:
			return ErrInvalidDirectory
		}
	}
	return nil
}

// sanitizeLabel 把平台分配的报警编号转换成安全的目录名，非法字符替换为下划线。
func sanitizeLabel(label string) string {
	var b strings.Builder
	for _, r := range label {
		switch {
		case r >= 'a' && r <= 'z', r >= 'A' && r <= 'Z', r >= '0' && r <= '9', r == '_', r == '-':
			b.WriteRune(r)
		default:
			b.WriteByte('_')
		}
		if b.Len() >= 64 {
			break
		}
	}
	if b.Len() == 0 {
		return "unknown"
	}
	return b.String()
}

// validateAttachmentFileNameFormat 校验苏标 4.6.2 的附件命名规则：
//
//	<文件类型>_<通道号>_<报警类型>_<序号>_<报警编号>.<后缀名>
//
// 文件类型为 00~04；通道号为 0~37、64、65；报警类型为外设 ID(64~67) 加两位模块报警类型。
// 该函数只做格式标记，调用方仍以 validateFileName 作为路径安全边界。
func validateAttachmentFileNameFormat(name string) error {
	dot := strings.LastIndexByte(name, '.')
	if dot <= 0 || dot == len(name)-1 {
		return fmt.Errorf("%w: missing file type or suffix", ErrInvalidFileNameFormat)
	}
	if !isASCIIAlphaNumeric(name[dot+1:]) {
		return fmt.Errorf("%w: suffix must contain only letters and digits", ErrInvalidFileNameFormat)
	}
	parts := strings.Split(name[:dot], "_")
	if len(parts) != 5 {
		return fmt.Errorf("%w: expected 5 underscore-separated fields, got %d", ErrInvalidFileNameFormat, len(parts))
	}
	if len(parts[0]) != 2 || !isASCIIDigits(parts[0]) {
		return fmt.Errorf("%w: file type must be two digits", ErrInvalidFileNameFormat)
	}
	fileType, err := strconv.Atoi(parts[0])
	if err != nil || fileType > 4 {
		return fmt.Errorf("%w: file type must be between 00 and 04", ErrInvalidFileNameFormat)
	}
	if !isASCIIDigits(parts[1]) {
		return fmt.Errorf("%w: channel must be a decimal number", ErrInvalidFileNameFormat)
	}
	channel, err := strconv.Atoi(parts[1])
	if err != nil || !(channel <= 37 || channel == 64 || channel == 65) {
		return fmt.Errorf("%w: channel must be 0~37, 64 or 65", ErrInvalidFileNameFormat)
	}
	if len(parts[2]) != 4 || !isASCIIHex(parts[2]) {
		return fmt.Errorf("%w: alarm type must be four hexadecimal digits", ErrInvalidFileNameFormat)
	}
	peripheral, err := strconv.ParseUint(parts[2][:2], 16, 8)
	if err != nil || peripheral < 0x64 || peripheral > 0x67 {
		return fmt.Errorf("%w: alarm type must start with peripheral ID 64~67", ErrInvalidFileNameFormat)
	}
	if !isASCIIDigits(parts[3]) {
		return fmt.Errorf("%w: sequence must be a decimal number", ErrInvalidFileNameFormat)
	}
	if parts[4] == "" {
		return fmt.Errorf("%w: alarm number must not be empty", ErrInvalidFileNameFormat)
	}
	return nil
}

// validateAlarmSign 校验表 4-16 的 16 字节报警标识号：
// 终端 ID(7) + 时间 BCD[6] + 序号(1) + 附件数量(1) + 预留(1)。
// 终端 ID 允许 0x00 尾部填充；附件数量必须为 1~255。
func validateAlarmSign(sign []byte) error {
	const alarmSignLen = 16
	if len(sign) != alarmSignLen {
		return fmt.Errorf("%w: must be exactly %d bytes, got %d", ErrInvalidAlarmSign, alarmSignLen, len(sign))
	}
	terminalID := sign[:7]
	allZero := true
	for i, c := range terminalID {
		if c != 0 {
			allZero = false
		}
		if !(c >= 'A' && c <= 'Z') && !(c >= '0' && c <= '9') && c != 0 {
			return fmt.Errorf("%w: terminal ID byte %d must be an uppercase letter, digit or 0x00", ErrInvalidAlarmSign, i)
		}
	}
	if allZero {
		return fmt.Errorf("%w: terminal ID must not be empty", ErrInvalidAlarmSign)
	}
	if _, err := parseAlarmSignTime(sign[7:13]); err != nil {
		return err
	}
	if sign[14] == 0 {
		return fmt.Errorf("%w: attachment count must be between 1 and 255", ErrInvalidAlarmSign)
	}
	return nil
}

// parseAlarmSignTime 解析报警标识号中的 BCD[6] 时间（YY-MM-DD-hh-mm-ss，GMT+8）。
func parseAlarmSignTime(bcd []byte) (time.Time, error) {
	if len(bcd) != 6 {
		return time.Time{}, fmt.Errorf("%w: BCD time must be 6 bytes, got %d", ErrInvalidAlarmSign, len(bcd))
	}
	values := [6]int{}
	for i, b := range bcd {
		high, low := b>>4, b&0x0f
		if high > 9 || low > 9 {
			return time.Time{}, fmt.Errorf("%w: BCD time contains a non-decimal nibble at byte %d", ErrInvalidAlarmSign, i)
		}
		values[i] = int(high)*10 + int(low)
	}
	year := 2000 + values[0]
	t := time.Date(year, time.Month(values[1]), values[2], values[3], values[4], values[5], 0, time.UTC)
	// time.Date 会把越界值规范化，逐字段回比才能识别非法日期（如 2 月 30 日）。
	if t.Year() != year || int(t.Month()) != values[1] || t.Day() != values[2] ||
		t.Hour() != values[3] || t.Minute() != values[4] || t.Second() != values[5] {
		return time.Time{}, fmt.Errorf("%w: BCD time is not a valid calendar time", ErrInvalidAlarmSign)
	}
	return t, nil
}

func isASCIIDigits(s string) bool {
	if s == "" {
		return false
	}
	for i := 0; i < len(s); i++ {
		if s[i] < '0' || s[i] > '9' {
			return false
		}
	}
	return true
}

func isASCIIHex(s string) bool {
	if s == "" {
		return false
	}
	for i := 0; i < len(s); i++ {
		c := s[i]
		if !(c >= '0' && c <= '9') && !(c >= 'a' && c <= 'f') && !(c >= 'A' && c <= 'F') {
			return false
		}
	}
	return true
}

func isASCIIAlphaNumeric(s string) bool {
	if s == "" {
		return false
	}
	for i := 0; i < len(s); i++ {
		c := s[i]
		if !(c >= '0' && c <= '9') && !(c >= 'a' && c <= 'z') && !(c >= 'A' && c <= 'Z') {
			return false
		}
	}
	return true
}

// alarmDate 从报警标识号中的 BCD 时间推导日期（YYYYMMDD）。
// 报警标识号为：终端ID(7) + 时间(6 BCD) + 序号(1) + 附件数量(1) + 预留(1)。
// 时间非法时回退到 fallback。
func alarmDate(alarmSign []byte, fallback string) string {
	if len(alarmSign) >= 13 {
		if t, err := parseAlarmSignTime(alarmSign[7:13]); err == nil {
			return t.Format("20060102")
		}
	}
	if fallback != "" {
		if _, err := parseDate(fallback); err == nil {
			return fallback
		}
	}
	return nowDate()
}

func parseDate(date string) (string, error) {
	if len(date) != 8 {
		return "", ErrInvalidDirectory
	}
	for _, r := range date {
		if r < '0' || r > '9' {
			return "", ErrInvalidDirectory
		}
	}
	return date, nil
}

func statePath(partPath string) string { return partPath + stateSuffix }

func nowDate() string { return time.Now().Format("20060102") }

func readPersistedState(path string) (persistedState, error) {
	var state persistedState
	data, err := os.ReadFile(path)
	if err != nil {
		return state, err
	}
	if err := json.Unmarshal(data, &state); err != nil {
		return state, err
	}
	return state, nil
}

func writePersistedState(partPath string, state persistedState) error {
	data, err := json.Marshal(state)
	if err != nil {
		return err
	}
	return writeFileAtomic(statePath(partPath), data)
}

func writeMetadata(finalPath string, meta Metadata) error {
	data, err := json.MarshalIndent(meta, "", "  ")
	if err != nil {
		return err
	}
	return writeFileAtomic(finalPath+metaSuffix, data)
}

func readMetadata(path string) (Metadata, error) {
	var meta Metadata
	data, err := os.ReadFile(path)
	if err != nil {
		return meta, err
	}
	if err := json.Unmarshal(data, &meta); err != nil {
		return meta, err
	}
	// 校验结果按文件名与报警标识号重新推导，兼容旧版本生成的 .meta.json。
	meta.FileNameConformant = validateAttachmentFileNameFormat(meta.FileName) == nil
	meta.AlarmSignValid = false
	if sign, err := hex.DecodeString(meta.AlarmSignHex); err == nil {
		meta.AlarmSignValid = validateAlarmSign(sign) == nil
	}
	return meta, nil
}

// writeFileAtomic 先写临时文件再改名，避免进程中断留下半个 JSON。
func writeFileAtomic(path string, data []byte) error {
	tmp := path + ".tmp"
	if err := os.WriteFile(tmp, data, 0o644); err != nil {
		return err
	}
	return os.Rename(tmp, path)
}

// fileMD5 计算文件 MD5。
func fileMD5(path string) (string, error) {
	file, err := os.Open(path)
	if err != nil {
		return "", err
	}
	defer file.Close()
	hash := md5.New()
	if _, err := io.Copy(hash, file); err != nil {
		return "", err
	}
	return fmt.Sprintf("%x", hash.Sum(nil)), nil
}
