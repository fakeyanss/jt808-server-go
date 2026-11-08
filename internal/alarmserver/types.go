package alarmserver

import (
	"errors"
	"time"
)

const (
	// FrameHeaderSize 是帧头长度：魔数(4) + 文件名(50) + 偏移(4) + 长度(4)。
	FrameHeaderSize = 62
	// FileNameFieldSize 是帧内文件名固定字段长度。
	FileNameFieldSize = 50
	// DefaultBlockSize 是苏标文件码流默认数据体长度（64K），仅作为参考帧长；
	// 服务端补传记账按字节区间进行，不要求帧按 64K 对齐。
	DefaultBlockSize = 64 * 1024
	// FramePayloadLimit 是单个数据帧负载的硬上限，防止异常 length 字段撑爆内存。
	FramePayloadLimit = 8 * 1024 * 1024
	// MaxRetransmitRanges 是 0x9212 补传列表的协议上限（数量字段为 BYTE）。
	MaxRetransmitRanges = 255
	// DefaultSessionTimeout 是文件无进展后的清理阈值。
	DefaultSessionTimeout = 30 * time.Minute
	// DefaultMaxConcurrent 是同时进行中的附件文件数上限。
	DefaultMaxConcurrent = 16
	// DefaultMaxFileSize 是单个附件文件大小上限（100MB）。
	DefaultMaxFileSize = 100 * 1024 * 1024
	// DefaultDir 是附件落盘根目录。
	DefaultDir = "./data/alarms"
)

// FrameMagic 是苏标 4.6.4 表 4-26 定义的文件码流帧头标识 0x30 0x31 0x63 0x64。
var FrameMagic = [4]byte{0x30, 0x31, 0x63, 0x64}

var (
	ErrInvalidFileName    = errors.New("invalid attachment file name")
	ErrInvalidDirectory   = errors.New("invalid attachment directory")
	ErrFileNotRegistered  = errors.New("attachment file is not registered")
	ErrOffsetOutOfRange   = errors.New("attachment chunk offset out of range")
	ErrFileTooLarge       = errors.New("attachment file exceeds max file size")
	ErrTooManyFiles       = errors.New("too many concurrent attachment files")
	ErrInvalidChunkLength = errors.New("invalid attachment chunk length")
	ErrFrameMagic         = errors.New("invalid attachment frame magic")
)

// Config 是附件服务器的运行参数。
type Config struct {
	Dir            string        // 附件落盘根目录
	MaxFileSize    int64         // 单文件大小上限（字节）
	MaxConcurrent  int           // 同时进行中的文件数上限
	SessionTimeout time.Duration // 文件无进展清理阈值
}

// Normalize 填充未配置或非法为零值的字段，保证零值 Config 可用。
func (c Config) Normalize() Config {
	if c.Dir == "" {
		c.Dir = DefaultDir
	}
	if c.MaxFileSize <= 0 {
		c.MaxFileSize = DefaultMaxFileSize
	}
	if c.MaxConcurrent <= 0 {
		c.MaxConcurrent = DefaultMaxConcurrent
	}
	if c.SessionTimeout <= 0 {
		c.SessionTimeout = DefaultSessionTimeout
	}
	return c
}

// AttachmentEntry 是 0x1210 附件信息列表中的一项。
type AttachmentEntry struct {
	FileName string
	FileSize uint32
}

// AttachmentInfo 是 0x1210 登记信息。
type AttachmentInfo struct {
	Phone       string
	TerminalID  string
	AlarmSign   []byte
	AlarmNo     string
	InfoType    uint8
	Date        string // YYYYMMDD，空则按报警标识号时间或当前时间推导
	Attachments []AttachmentEntry
}

// MissingRange 是 0x9212 补传列表中的一项，单位为字节。
type MissingRange struct {
	Offset uint32 `json:"offset"`
	Length uint32 `json:"length"`
}

// CompletionResult 是 0x1212 完成校验结果。
type CompletionResult struct {
	Complete bool           `json:"complete"`
	Missing  []MissingRange `json:"missing"`
}

// Metadata 是附件完成后生成的元数据。
type Metadata struct {
	Phone        string    `json:"phone"`
	TerminalID   string    `json:"terminalId"`
	AlarmNo      string    `json:"alarmNo"`
	AlarmSignHex string    `json:"alarmSignHex"`
	FileName     string    `json:"fileName"`
	FileType     uint8     `json:"fileType"`
	Size         int64     `json:"size"`
	MD5          string    `json:"md5"`
	Path         string    `json:"path"`
	CompletedAt  time.Time `json:"completedAt"`
}
