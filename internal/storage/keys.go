// Package storage 提供对象存储能力：BlobStore 后端、key 前缀编排与文件落地。
//
// keys.go 集中定义对象存储的 key 前缀与拼接规则，禁止各角色自行拼字符串。
//
// 全集群只有一套 objectStore，媒体切片、苏标附件与后续的 1078 FTP 录像
// 各占一个来源前缀。前缀是清理与生命周期的分区依据：
// pending 回收与保留期清理都必须限定在某个来源前缀内，禁止全桶扫描。
package storage

import (
	"fmt"
	"path"
	"strings"
)

// 来源前缀。结尾统一带 "/"，拼接时不要再补分隔符。
const (
	// MediaSegments 是 1078 媒体切片。
	MediaSegments = "media/segments/"
	// MediaUploads 是网关 0x0801 上报的多媒体文件（图片等）。
	MediaUploads = "media/uploads/"
	// SubiaAttachments 是苏标主动安全附件（0x1210/0x1211/0x1212）。
	SubiaAttachments = "attachment/subia/"
	// FTP1078Attachments 是 1078 FTP 上传录像（预留，本轮不实现服务端）。
	FTP1078Attachments = "attachment/ftp1078/"

	// PendingRoot 是对象存储的 pending 标记根前缀。
	PendingRoot = "_pending/"

	// SourceSubia 是附件来源：苏标附件。
	SourceSubia = "subia"
	// SourceFTP1078 是附件来源：1078 FTP 录像。
	SourceFTP1078 = "ftp1078"
)

// MediaSegment 返回媒体切片的对象 key。
func MediaSegment(phone string, channel uint8, date, name string) string {
	return MediaSegments + path.Join(SafeSegment(phone), fmt.Sprintf("ch%d", channel), SafeSegment(date), SafeSegment(name))
}

// AttachmentPrefix 返回某个附件来源的根前缀。
func AttachmentPrefix(source string) string {
	switch strings.ToLower(strings.TrimSpace(source)) {
	case SourceFTP1078:
		return FTP1078Attachments
	default:
		return SubiaAttachments
	}
}

// AttachmentSegment 返回附件切片的对象 key。
// 同一逻辑文件的切片按序号自然排序，读取时按序号拼接。
func AttachmentSegment(source, phone, alarmNo, fileName string, index int) string {
	return fmt.Sprintf("%s%s/%s/%s/%08d",
		AttachmentPrefix(source), SafeSegment(phone), SafeSegment(alarmNo), SafeSegment(fileName), index)
}

// SafeSegment 把任意业务标识转换成安全的单层路径段。
func SafeSegment(value string) string {
	value = strings.TrimSpace(value)
	if value == "" {
		return "unknown"
	}
	var b strings.Builder
	for _, r := range value {
		switch {
		case r >= 'a' && r <= 'z', r >= 'A' && r <= 'Z', r >= '0' && r <= '9', r == '_', r == '-', r == '.':
			b.WriteRune(r)
		default:
			b.WriteByte('_')
		}
		if b.Len() >= 96 {
			break
		}
	}
	if b.Len() == 0 {
		return "unknown"
	}
	return b.String()
}
