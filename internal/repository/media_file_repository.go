package repository

import (
	"context"
	"errors"
	"time"
)

// ErrMediaFileNotFound 表示该设备尚未上报对应的 0x0801 多媒体数据。
var ErrMediaFileNotFound = errors.New("media file not found")

// MediaFile 是 0x0801 多媒体数据落盘后的元数据记录。
// 文件本体保存在 mediaUpload.dir 下，这里只保存相对路径与校验信息。
//
// 注意：0x0801 的位置体只包含报警标志位，不包含报警流水号，
// 因此 AlarmSign 只能表达“属于哪类报警”，无法与某一条具体报警精确关联。
type MediaFile struct {
	Phone        string    `json:"phone"`
	MediaID      uint32    `json:"mediaId"`
	MediaType    uint8     `json:"mediaType"`    // 0:图像 1:音频 2:视频
	MediaFormat  uint8     `json:"mediaFormat"`  // 0:JPEG 1:TIF 2:MP3 3:WAV 4:WMV 其他:未知
	EventID      uint8     `json:"eventId"`      // 事件项编码
	ChannelID    uint8     `json:"channelId"`    // 逻辑通道号
	AlarmSign    uint32    `json:"alarmSign"`    // 报警标志位，仅类别，不含报警流水号
	StatusSign   uint32    `json:"statusSign"`   // 状态标志位
	Latitude     uint32    `json:"latitude"`     // 百万分之一度
	Longitude    uint32    `json:"longitude"`    // 百万分之一度
	Altitude     uint16    `json:"altitude"`     // 米
	Speed        uint16    `json:"speed"`        // 0.1 km/h
	Direction    uint16    `json:"direction"`    // 0-359
	LocationTime time.Time `json:"locationTime"` // 位置体 BCD 时间；缺失时为零值
	RelativePath string    `json:"relativePath"` // 相对 mediaUpload.dir 的路径
	FileSize     int64     `json:"fileSize"`     // 字节数
	MD5          string    `json:"md5"`          // 文件内容 MD5
	ReceivedAt   time.Time `json:"receivedAt"`   // 平台接收时间
}

// MediaFileFilter 描述媒体文件查询条件；nil 指针表示不限制。
type MediaFileFilter struct {
	ChannelID *uint8
	MediaType *uint8
	Start     *time.Time
	End       *time.Time
	AlarmOnly bool
	Limit     int
}

// MediaCaptureItem 是 0x0805 返回的单个媒体 ID 及对应文件是否已到达。
type MediaCaptureItem struct {
	MediaID  uint32 `json:"mediaId"`
	Arrived  bool   `json:"arrived"`
	FileSize int64  `json:"fileSize,omitempty"`
}

// MediaCapture 是 0x0805 摄像头立即拍摄命令应答的落库记录。
// MediaIDs 由 0x0805 上报，实际文件在后续 0x0801 中按 mediaId 落地。
type MediaCapture struct {
	Phone        string             `json:"phone"`
	RecordID     string             `json:"recordId"` // 平台生成的记录 ID，按设备唯一
	AnswerSerial uint16             `json:"answerSerial"`
	Result       uint8              `json:"result"`
	ReceivedAt   time.Time          `json:"receivedAt"`
	Items        []MediaCaptureItem `json:"items"`
}

// MediaFileRepository 多媒体文件元数据仓储（B 类）。
// 只保存元数据，文件内容由 mediaupload 负责；读取返回快照。
type MediaFileRepository interface {
	Save(ctx context.Context, file *MediaFile) error
	Get(ctx context.Context, phone string, mediaID uint32) (*MediaFile, error)
	List(ctx context.Context, phone string, filter MediaFileFilter) ([]*MediaFile, error)
	// Stats 返回全部媒体文件数量与总占用字节数，供指标采集使用。
	Stats(ctx context.Context) (count int64, bytes int64, err error)
}

// MediaCaptureRepository 摄像头拍摄应答仓储（B 类）。
type MediaCaptureRepository interface {
	SaveCapture(ctx context.Context, capture *MediaCapture) error
	ListCaptures(ctx context.Context, phone string, limit int) ([]*MediaCapture, error)
}
