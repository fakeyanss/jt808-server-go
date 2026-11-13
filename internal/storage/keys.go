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

// ParseAttachmentDoneKey 解析完成清单对象 key；非清单 key 返回错误。
//
// 清单与切片目录同级：<prefix><phone>/<alarmNo>/<fileName>.done，
// 比切片 key 少一层“偏移_大小_序号”，因此是 5 段而不是 6 段。
func ParseAttachmentDoneKey(key string) (AttachmentSegmentKeyInfo, error) {
	parts := strings.Split(strings.TrimPrefix(key, "/"), "/")
	if len(parts) != 5 || parts[0] != "attachment" || !IsAttachmentDoneKey(parts[4]) {
		return AttachmentSegmentKeyInfo{}, fmt.Errorf("invalid attachment manifest key %q", key)
	}
	source := parts[1]
	// 完成清单是 FTP 上传独有的语义；苏标附件由 0x1212 报文给出总长，不需要清单。
	if source != SourceFTP1078 {
		return AttachmentSegmentKeyInfo{}, fmt.Errorf("invalid attachment manifest source %q in key %q", source, key)
	}
	fileName := strings.TrimSuffix(parts[4], ".done")
	if parts[2] == "" || parts[3] == "" || fileName == "" {
		return AttachmentSegmentKeyInfo{}, fmt.Errorf("attachment manifest key has empty path field: %q", key)
	}
	return AttachmentSegmentKeyInfo{
		Source: source, Phone: parts[2], AlarmNo: parts[3],
		FileName: fileName, ObjectKey: key,
	}, nil
}

// AttachmentSegmentKeyInfo 是能从附件切片对象 key 独立解析出的元数据。
//
// 附件节点不连库：control 侧的索引允许丢失，由扫描 attachment/ 前缀重建。
// 因此终端、报警号、文件名、偏移、段大小与文件总长必须全部编码在 key 里，
// 扫描不依赖任何上报内容就能得出“覆盖到哪、是否完整”。
type AttachmentSegmentKeyInfo struct {
	Source    string
	Phone     string
	AlarmNo   string
	FileName  string
	Offset    int64
	Size      int64
	TotalSize int64
	Index     int
	ObjectKey string
}

// AttachmentSegmentObjectKey 生成附件切片的对象 key。
//
// 格式：<prefix><phone>/<alarmNo>/<fileName>/<offset>_<size>_<total>_<index>
// offset/size/total 以十进制写入，固定宽度便于人工排查；解析以字段值为准。
func AttachmentSegmentObjectKey(source, phone, alarmNo, fileName string, offset, size, totalSize int64, index int) string {
	return fmt.Sprintf("%s%s/%s/%s/%012d_%012d_%012d_%08d",
		AttachmentPrefix(source),
		SafeSegment(phone),
		SafeSegment(alarmNo),
		SafeSegment(fileName),
		offset, size, totalSize, index,
	)
}

// AttachmentFTPSegmentObjectKey 生成 1078 FTP 录像切片的对象 key。
//
// FTP 上传在连接结束前不知道文件总长，因此 key 不写 total，只写 offset/size/index；
// 总长与完整性由同目录的 <fileName>.done 清单对象给出。
// alarmNo 位置放任务流水号，与苏标附件保持同一层级，便于按任务前缀列举。
func AttachmentFTPSegmentObjectKey(phone, serial, fileName string, offset, size int64, index int) string {
	return fmt.Sprintf("%s%s/%s/%s/%012d_%012d_%08d",
		FTP1078Attachments,
		SafeSegment(phone),
		SafeSegment(serial),
		SafeSegment(fileName),
		offset, size, index,
	)
}

// AttachmentDoneObjectKey 返回附件完成清单对象 key。
//
// 清单只在所有切片 Promote 成功后才写入；扫描时它存在但切片不连续，
// 说明上传中断或对象被误删，按异常处理而不是判为完成。
func AttachmentDoneObjectKey(source, phone, alarmNo, fileName string) string {
	return fmt.Sprintf("%s%s/%s/%s.done",
		AttachmentPrefix(source),
		SafeSegment(phone),
		SafeSegment(alarmNo),
		SafeSegment(fileName),
	)
}

// IsAttachmentDoneKey 判断对象 key 是否为完成清单。
func IsAttachmentDoneKey(key string) bool {
	return strings.HasSuffix(strings.ToLower(strings.TrimSpace(key)), ".done")
}

// AttachmentDoneManifest 是 FTP 上传完成后写入的清单内容。
//
// 它记录当时已知的事实（总长、切片数、MD5），供 control 扫描重建索引时
// 判断“清单存在且切片连续”。清单缺失不影响切片本身的可用性。
type AttachmentDoneManifest struct {
	FileName    string `json:"fileName"`
	TotalSize   int64  `json:"totalSize"`
	SegmentSize int64  `json:"segmentSize"`
	Segments    int    `json:"segments"`
	MD5         string `json:"md5,omitempty"`
	CompletedAt int64  `json:"completedAt"`
}

// ParseAttachmentSegmentKey 从附件切片对象 key 解析元数据。
// 任何一段不符合约定都返回错误，调用方应跳过该对象而不是写坏索引。
func ParseAttachmentSegmentKey(key string) (AttachmentSegmentKeyInfo, error) {
	parts := strings.Split(strings.TrimPrefix(key, "/"), "/")
	if len(parts) != 6 || parts[0] != "attachment" {
		return AttachmentSegmentKeyInfo{}, fmt.Errorf("invalid attachment segment key %q", key)
	}
	source := parts[1]
	if source != SourceSubia && source != SourceFTP1078 {
		return AttachmentSegmentKeyInfo{}, fmt.Errorf("invalid attachment source %q in key %q", source, key)
	}
	info := AttachmentSegmentKeyInfo{
		Source:    source,
		Phone:     parts[2],
		AlarmNo:   parts[3],
		FileName:  parts[4],
		ObjectKey: key,
	}
	if info.Phone == "" || info.AlarmNo == "" || info.FileName == "" {
		return AttachmentSegmentKeyInfo{}, fmt.Errorf("attachment segment key has empty path field: %q", key)
	}
	if IsAttachmentDoneKey(parts[5]) {
		return AttachmentSegmentKeyInfo{}, fmt.Errorf("attachment key %q is a completion manifest, not a segment", key)
	}
	fields := strings.Split(parts[5], "_")
	if len(fields) != 4 && !(len(fields) == 3 && source == SourceFTP1078) {
		return AttachmentSegmentKeyInfo{}, fmt.Errorf("invalid attachment segment file name %q", parts[5])
	}
	values := make([]int64, len(fields))
	for i, field := range fields {
		parsed, err := strconv.ParseInt(field, 10, 64)
		if err != nil || parsed < 0 {
			return AttachmentSegmentKeyInfo{}, fmt.Errorf("invalid attachment segment field %q in %q", field, key)
		}
		values[i] = parsed
	}
	info.Offset, info.Size = values[0], values[1]
	if len(values) == 4 {
		info.TotalSize = values[2]
		info.Index = int(values[3])
	} else {
		// ftp1078 在连接结束前不知道总长：total 记为 0，由 .done 清单补齐。
		info.TotalSize = 0
		info.Index = int(values[2])
	}
	if info.Size <= 0 {
		return AttachmentSegmentKeyInfo{}, fmt.Errorf("attachment segment size must be positive in %q", key)
	}
	if info.TotalSize > 0 && info.Offset+info.Size > info.TotalSize {
		return AttachmentSegmentKeyInfo{}, fmt.Errorf("attachment segment exceeds total size in %q", key)
	}
	return info, nil
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
