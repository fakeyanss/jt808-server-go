package repository

import (
	"context"
	"encoding/json"
	"errors"
	"time"
)

// ErrAttachmentFileNotFound 表示附件元数据不存在。
var ErrAttachmentFileNotFound = errors.New("attachment file not found")

// 附件来源与会话状态。
const (
	AttachmentSourceSubia     = "subia"
	AttachmentSourceFTP1078   = "ftp1078"
	AttachmentStatusReceiving = "receiving"
	AttachmentStatusCompleted = "completed"
	AttachmentStatusFailed    = "failed"
	AttachmentStatusExpired   = "expired"
)

// AttachmentSegment 是一个已提交（或正在提交）的对象切片。
//
// 一个逻辑文件被拆成多个对象段：读取时按 Index 升序拼接，
// 保留期清理与 pending 回收都按这些 key 精确删除，不依赖目录列举。
type AttachmentSegment struct {
	Index  int    `json:"index"`
	Key    string `json:"key"`
	Offset int64  `json:"offset"`
	Size   int64  `json:"size"`
	MD5    string `json:"md5,omitempty"`
	State  string `json:"state"`
}

// AttachmentFile 是附件接收元数据，落共享库供控制面查询进度。
//
// Source 取值 subia / ftp1078；后续 FTP 服务端复用同一张表时不改结构。
type AttachmentFile struct {
	Source      string `json:"source"`
	Phone       string `json:"phone"`
	TerminalID  string `json:"terminalId"`
	AlarmSign   string `json:"alarmSign"`
	AlarmNo     string `json:"alarmNo"`
	AlarmSerial string `json:"alarmSerial"`
	FileName    string `json:"fileName"`
	FileType    uint8  `json:"fileType"`

	TotalSize     int64 `json:"totalSize"`
	ReceivedBytes int64 `json:"receivedBytes"`
	// Watermark 是已提交对象覆盖到的连续字节前缀；断线重连后从这里继续。
	Watermark int64 `json:"watermark"`

	// StagingPath 是当前节点的本地暂存文件；不保证跨节点有效，仅用于崩溃清理。
	StagingPath string `json:"stagingPath"`
	StagingBase string `json:"stagingBase"`
	// Ranges 是已落本地暂存但尚未切换成对象的覆盖区间（JSON 数组）。
	Ranges []byte `json:"ranges,omitempty"`

	Segments []AttachmentSegment `json:"segments"`
	Status   string              `json:"status"`

	CreatedAt   time.Time `json:"createdAt"`
	UpdatedAt   time.Time `json:"updatedAt"`
	CompletedAt time.Time `json:"completedAt"`
}

// AttachmentFileKey 是附件业务行的唯一键。
type AttachmentFileKey struct {
	Source   string
	Phone    string
	AlarmNo  string
	FileName string
}

// Key 返回唯一键。
func (f *AttachmentFile) Key() AttachmentFileKey {
	return AttachmentFileKey{Source: f.Source, Phone: f.Phone, AlarmNo: f.AlarmNo, FileName: f.FileName}
}

// MarshalSegments 把切片列表编码成数据库列。
func MarshalSegments(segments []AttachmentSegment) string {
	data, err := json.Marshal(segments)
	if err != nil {
		return "[]"
	}
	return string(data)
}

// UnmarshalSegments 解析数据库里的切片列；损坏时返回空列表而不是报错。
func UnmarshalSegments(raw string) []AttachmentSegment {
	var segments []AttachmentSegment
	if raw == "" || json.Unmarshal([]byte(raw), &segments) != nil {
		return nil
	}
	return segments
}

// MarshalRanges 把本地覆盖区间编码成数据库列。
func MarshalRanges(ranges []byte) string {
	if len(ranges) == 0 {
		return "[]"
	}
	return string(ranges)
}

// AttachmentFileRepository 是附件元数据仓储。
type AttachmentFileRepository interface {
	// Save 按唯一键 upsert，返回落库后的完整记录。
	Save(ctx context.Context, file *AttachmentFile) (*AttachmentFile, error)
	Get(ctx context.Context, key AttachmentFileKey) (*AttachmentFile, error)
	// FindByFileName 在未指定报警编号时按手机号 + 文件名查找最近一条。
	FindByFileName(ctx context.Context, source, phone, fileName string) (*AttachmentFile, error)
	ListByPhone(ctx context.Context, source, phone string, limit int) ([]*AttachmentFile, error)
	ListByAlarm(ctx context.Context, source, phone, alarmNo string) ([]*AttachmentFile, error)
	// ListCompletedSource 返回某个来源下已完成的附件，供运维与查询接口使用。
	ListCompletedSource(ctx context.Context, source string, limit int) ([]*AttachmentFile, error)
	// ListReceivingUpdatedBefore 返回状态为 receiving 且更新时间早于 cutoff 的记录，供崩溃对账。
	ListReceivingUpdatedBefore(ctx context.Context, cutoff time.Time, limit int) ([]*AttachmentFile, error)
	// ListCompletedBefore 返回完成时间早于 cutoff 的记录，供保留期清理。
	ListCompletedBefore(ctx context.Context, source string, cutoff time.Time, limit int) ([]*AttachmentFile, error)
	// ListTerminalBefore 返回 failed/expired 且更新时间早于 cutoff 的记录，供回收半成品切片。
	ListTerminalBefore(ctx context.Context, source string, cutoff time.Time, limit int) ([]*AttachmentFile, error)
	// ReferencedSegment 判断某个对象 key 是否仍被附件元数据引用。
	ReferencedSegment(ctx context.Context, source, key string) (bool, error)
	Delete(ctx context.Context, key AttachmentFileKey) error
	// Stats 返回某个来源的文件数、已完成数与总字节数。
	Stats(ctx context.Context, source string) (files, completed int64, bytes int64, err error)
}
