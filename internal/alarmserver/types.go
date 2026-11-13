package alarmserver

import (
	"errors"
	"time"

	"github.com/fakeyanss/jt808-server-go/internal/contracts"
	"github.com/fakeyanss/jt808-server-go/internal/repository"
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
	ErrInvalidFileName        = errors.New("invalid attachment file name")
	ErrInvalidFileNameFormat  = errors.New("attachment file name does not match T/JSATL 12-2017 4.6.2")
	ErrInvalidDirectory       = errors.New("invalid attachment directory")
	ErrInvalidAlarmSign       = errors.New("invalid attachment alarm sign")
	ErrFileNotRegistered      = errors.New("attachment file is not registered")
	ErrOffsetOutOfRange       = errors.New("attachment chunk offset out of range")
	ErrFileTooLarge           = errors.New("attachment file exceeds max file size")
	ErrTooManyFiles           = errors.New("too many concurrent attachment files")
	ErrInvalidChunkLength     = errors.New("invalid attachment chunk length")
	ErrFrameMagic             = errors.New("invalid attachment frame magic")
	ErrStagingCapacity        = errors.New("attachment staging capacity exceeded")
	ErrTooManySessions        = errors.New("attachment concurrent session limit reached")
	ErrObjectStoreUnavailable = errors.New("attachment object store is not configured")
)

// StorageConfig 是附件对象存储模式。
//
// 设置后附件不再把完整文件落本地盘：收到数据写入有界本地暂存区，
// 每个切片窗口到齐后切段上传对象存储，并把水位与段列表写入共享库。
type StorageConfig struct {
	Blob   contracts.BlobStore
	Files  repository.AttachmentFileRepository
	Source string
	// SegmentSize 是单个对象切片大小；<=0 时使用 DefaultSegmentSize。
	SegmentSize int64
	// StagingDir 是本地暂存根目录；崩溃残留按 .part 标记 + 宽限期扫描清理。
	StagingDir string
	// MaxStagingBytes 是整个暂存区的总量上限；到顶拒绝新的文件登记。
	MaxStagingBytes int64
	// NodeID 用于日志与状态回报。
	NodeID string
}

// DefaultSegmentSize 是附件切片默认大小（8 MiB）。
const DefaultSegmentSize = 8 * 1024 * 1024

// Normalize 填充对象存储模式的默认值。
func (c *StorageConfig) Normalize() {
	if c == nil {
		return
	}
	if c.SegmentSize <= 0 {
		c.SegmentSize = DefaultSegmentSize
	}
	if c.Source == "" {
		c.Source = repository.AttachmentSourceSubia
	}
}

// Config 是附件服务器的运行参数。
type Config struct {
	Dir            string        // 附件落盘根目录
	MaxFileSize    int64         // 单文件大小上限（字节）
	MaxConcurrent  int           // 同时进行中的文件数上限
	SessionTimeout time.Duration // 文件无进展清理阈值
	CompletedTTL   time.Duration // 完成态内存状态保留时长；0 表示不淘汰
	Retention      time.Duration // 已完成附件文件保留时长；<=0 表示不清理
	// Storage 非空时启用对象存储模式；此时 Dir 只作为本地暂存的兜底。
	Storage *StorageConfig
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
	// FileNameConformant 标记文件名是否符合苏标 4.6.2 命名规则。
	// 不合规文件仍会被接收并落盘，仅用于运维排查与数据质量统计。
	FileNameConformant bool `json:"fileNameConformant"`
	// AlarmSignValid 标记报警标识号是否符合表 4-16 的字段约束。
	// 不合规时仍按降级策略接收，避免阻断终端补传。
	AlarmSignValid bool `json:"alarmSignValid"`
}
