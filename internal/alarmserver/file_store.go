package alarmserver

import (
	"crypto/md5"
	"encoding/json"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"
	"time"
)

const (
	partSuffix  = ".part"
	stateSuffix = ".part.state.json"
	metaSuffix  = ".meta.json"
)

// persistedState 是位图与文件登记信息的磁盘副本，用于按 fileName 恢复进度。
type persistedState struct {
	FileName     string `json:"fileName"`
	FileType     uint8  `json:"fileType"`
	Size         int64  `json:"size"`
	Phone        string `json:"phone"`
	TerminalID   string `json:"terminalId"`
	AlarmNo      string `json:"alarmNo"`
	AlarmSign    []byte `json:"alarmSign"`
	Date         string `json:"date"`
	Received     int64  `json:"received"`
	LastActivity int64  `json:"lastActivity"` // Unix 纳秒
	Bitmap       []byte `json:"bitmap"`
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

// alarmDate 从报警标识号中的 BCD 时间推导日期（YYYYMMDD）。
// 报警标识号为：终端ID(7) + 时间(6 BCD) + 序号(1) + 附件数量(1) + 预留(1)。
// 时间非法时回退到 fallback。
func alarmDate(alarmSign []byte, fallback string) string {
	if len(alarmSign) >= 13 {
		bcd := alarmSign[7:13]
		valid := true
		for _, v := range bcd {
			if v>>4 > 9 || v&0x0f > 9 {
				valid = false
				break
			}
		}
		if valid {
			year := 2000 + int(bcd[0]>>4)*10 + int(bcd[0]&0x0f)
			month := int(bcd[1]>>4)*10 + int(bcd[1]&0x0f)
			day := int(bcd[2]>>4)*10 + int(bcd[2]&0x0f)
			if month >= 1 && month <= 12 && day >= 1 && day <= 31 {
				date := fmt.Sprintf("%04d%02d%02d", year, month, day)
				if _, err := parseDate(date); err == nil {
					return date
				}
			}
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
