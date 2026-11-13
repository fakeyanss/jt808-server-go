// Package storage 提供对象存储能力：BlobStore 后端、key 前缀编排与文件落地。
//
// keys.go 集中定义对象存储的 key 前缀与拼接规则，禁止各角色自行拼字符串。
//
// 全集群只有一套 objectStore，媒体切片、苏标附件与后续的 1078 FTP 录像
// 各占一个来源前缀。前缀是清理与生命周期的分区依据：
// pending 回收与保留期清理都必须限定在某个来源前缀内，禁止全桶扫描。
package storage

import (
	"errors"
	"fmt"
	"path"
	"strconv"
	"strings"
	"time"
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

// MediaSegmentKeyInfo 是能从对象 key 独立解析出的切片元数据。
//
// 对象存储是切片的事实来源：即使 media 的上报全部丢失，control 也能仅凭
// key 重建索引，因此终端、通道、类型、起止时间与序号必须全部编码在 key 里。
type MediaSegmentKeyInfo struct {
	Phone     string
	Channel   uint8
	Kind      string
	StartAt   time.Time
	EndAt     time.Time
	Duration  time.Duration
	Sequence  int
	ObjectKey string
}

// MediaSegmentObjectKey 生成自带元数据的切片对象 key。
//
// 格式：media/segments/<phone>/ch<channel>/<kind>/<YYYYMMDD>/<startMs>_<durationMs>_<endMs>_<seq>.ts
// 日期目录只用于人工排查，解析时以文件名里的毫秒时间为准。
func MediaSegmentObjectKey(phone string, channel uint8, kind string, startAt time.Time, duration time.Duration, seq int) string {
	if duration < 0 {
		duration = 0
	}
	name := fmt.Sprintf("%d_%d_%d_%d.ts",
		startAt.UnixMilli(), duration.Milliseconds(), startAt.Add(duration).UnixMilli(), seq)
	return MediaSegments + path.Join(
		SafeSegment(phone),
		fmt.Sprintf("ch%d", channel),
		SafeSegment(kind),
		startAt.Format("20060102"),
		name,
	)
}

// ParseMediaSegmentKey 从对象 key 解析切片元数据。
// 任何一段不符合约定都返回错误，调用方应跳过该对象而不是写坏索引。
func ParseMediaSegmentKey(key string) (MediaSegmentKeyInfo, error) {
	parts := strings.Split(strings.TrimPrefix(key, "/"), "/")
	if len(parts) != 7 || parts[0] != "media" || parts[1] != "segments" {
		return MediaSegmentKeyInfo{}, fmt.Errorf("invalid media segment key %q", key)
	}
	phone := parts[2]
	if phone == "" {
		return MediaSegmentKeyInfo{}, errors.New("media segment key has empty phone")
	}
	if !strings.HasPrefix(parts[3], "ch") {
		return MediaSegmentKeyInfo{}, fmt.Errorf("invalid channel segment %q", parts[3])
	}
	channel, err := strconv.ParseUint(strings.TrimPrefix(parts[3], "ch"), 10, 8)
	if err != nil {
		return MediaSegmentKeyInfo{}, fmt.Errorf("invalid channel in %q: %w", key, err)
	}
	kind := parts[4]
	if kind == "" {
		return MediaSegmentKeyInfo{}, errors.New("media segment key has empty kind")
	}
	if len(parts[5]) != 8 {
		return MediaSegmentKeyInfo{}, fmt.Errorf("invalid date segment %q", parts[5])
	}
	name := strings.TrimSuffix(parts[6], ".ts")
	fields := strings.Split(name, "_")
	if len(fields) != 4 {
		return MediaSegmentKeyInfo{}, fmt.Errorf("invalid media segment file name %q", parts[6])
	}
	startMS, err := strconv.ParseInt(fields[0], 10, 64)
	if err != nil || startMS <= 0 {
		return MediaSegmentKeyInfo{}, fmt.Errorf("invalid start time in %q", parts[6])
	}
	durationMS, err := strconv.ParseInt(fields[1], 10, 64)
	if err != nil || durationMS < 0 {
		return MediaSegmentKeyInfo{}, fmt.Errorf("invalid duration in %q", parts[6])
	}
	endMS, err := strconv.ParseInt(fields[2], 10, 64)
	if err != nil || endMS != startMS+durationMS {
		return MediaSegmentKeyInfo{}, fmt.Errorf("inconsistent end time in %q", parts[6])
	}
	seq, err := strconv.Atoi(fields[3])
	if err != nil || seq < 0 {
		return MediaSegmentKeyInfo{}, fmt.Errorf("invalid sequence in %q", parts[6])
	}
	startAt := time.UnixMilli(startMS)
	return MediaSegmentKeyInfo{
		Phone:     phone,
		Channel:   uint8(channel),
		Kind:      kind,
		StartAt:   startAt,
		EndAt:     startAt.Add(time.Duration(durationMS) * time.Millisecond),
		Duration:  time.Duration(durationMS) * time.Millisecond,
		Sequence:  seq,
		ObjectKey: key,
	}, nil
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
