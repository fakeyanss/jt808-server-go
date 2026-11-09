package repository

import (
	"context"
	"errors"
	"fmt"
	"sort"
	"sync"
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

// defaultMediaQueryLimit 是仓储层单次查询的默认与最大条数。
const (
	defaultMediaQueryLimit = 100
	maxMediaQueryLimit     = 1000
)

func normalizeMediaLimit(limit int) int {
	if limit <= 0 {
		return defaultMediaQueryLimit
	}
	if limit > maxMediaQueryLimit {
		return maxMediaQueryLimit
	}
	return limit
}

// memoryMediaFileRepository 是 MediaFileRepository 与 MediaCaptureRepository 的内存实现。
type memoryMediaFileRepository struct {
	mu       sync.RWMutex
	files    map[mediaFileKey]*MediaFile
	captures map[string][]*MediaCapture
}

type mediaFileKey struct {
	phone   string
	mediaID uint32
}

// NewMediaFileRepository 创建独立的内存媒体文件仓储。
func NewMediaFileRepository() *memoryMediaFileRepository {
	return &memoryMediaFileRepository{
		files:    map[mediaFileKey]*MediaFile{},
		captures: map[string][]*MediaCapture{},
	}
}

func (r *memoryMediaFileRepository) Save(_ context.Context, file *MediaFile) error {
	if file == nil || file.Phone == "" || file.MediaID == 0 {
		return fmt.Errorf("save media file: nil file, empty phone or zero media id")
	}
	copy := *file
	r.mu.Lock()
	defer r.mu.Unlock()
	r.files[mediaFileKey{phone: file.Phone, mediaID: file.MediaID}] = &copy
	return nil
}

func (r *memoryMediaFileRepository) Get(_ context.Context, phone string, mediaID uint32) (*MediaFile, error) {
	r.mu.RLock()
	defer r.mu.RUnlock()
	file := r.files[mediaFileKey{phone: phone, mediaID: mediaID}]
	if file == nil {
		return nil, ErrMediaFileNotFound
	}
	copy := *file
	return &copy, nil
}

func (r *memoryMediaFileRepository) List(_ context.Context, phone string, filter MediaFileFilter) ([]*MediaFile, error) {
	r.mu.RLock()
	defer r.mu.RUnlock()
	matched := make([]*MediaFile, 0)
	for key, file := range r.files {
		if key.phone != phone || !mediaFileMatches(file, filter) {
			continue
		}
		copy := *file
		matched = append(matched, &copy)
	}
	sort.Slice(matched, func(i, j int) bool {
		if !matched[i].ReceivedAt.Equal(matched[j].ReceivedAt) {
			return matched[i].ReceivedAt.After(matched[j].ReceivedAt)
		}
		return matched[i].MediaID > matched[j].MediaID
	})
	if limit := normalizeMediaLimit(filter.Limit); len(matched) > limit {
		matched = matched[:limit]
	}
	return matched, nil
}

func (r *memoryMediaFileRepository) Stats(_ context.Context) (int64, int64, error) {
	r.mu.RLock()
	defer r.mu.RUnlock()
	var count, bytes int64
	for _, file := range r.files {
		count++
		bytes += file.FileSize
	}
	return count, bytes, nil
}

func (r *memoryMediaFileRepository) SaveCapture(_ context.Context, capture *MediaCapture) error {
	if capture == nil || capture.Phone == "" || capture.RecordID == "" {
		return fmt.Errorf("save media capture: nil capture, empty phone or record id")
	}
	copy := *capture
	copy.Items = append([]MediaCaptureItem(nil), capture.Items...)
	r.mu.Lock()
	defer r.mu.Unlock()
	list := r.captures[capture.Phone]
	for i, existing := range list {
		if existing.RecordID == capture.RecordID {
			list[i] = &copy
			r.captures[capture.Phone] = list
			return nil
		}
	}
	r.captures[capture.Phone] = append(list, &copy)
	return nil
}

func (r *memoryMediaFileRepository) ListCaptures(_ context.Context, phone string, limit int) ([]*MediaCapture, error) {
	r.mu.RLock()
	defer r.mu.RUnlock()
	stored := r.captures[phone]
	result := make([]*MediaCapture, 0, len(stored))
	for i := len(stored) - 1; i >= 0; i-- {
		copy := *stored[i]
		copy.Items = make([]MediaCaptureItem, len(stored[i].Items))
		for j, item := range stored[i].Items {
			copy.Items[j] = item
			if file, ok := r.files[mediaFileKey{phone: phone, mediaID: item.MediaID}]; ok {
				copy.Items[j].Arrived = true
				copy.Items[j].FileSize = file.FileSize
			}
		}
		result = append(result, &copy)
		if len(result) >= normalizeMediaLimit(limit) {
			break
		}
	}
	return result, nil
}

func mediaFileMatches(file *MediaFile, filter MediaFileFilter) bool {
	if filter.ChannelID != nil && file.ChannelID != *filter.ChannelID {
		return false
	}
	if filter.MediaType != nil && file.MediaType != *filter.MediaType {
		return false
	}
	if filter.AlarmOnly && file.AlarmSign == 0 {
		return false
	}
	if filter.Start != nil && file.LocationTime.Before(*filter.Start) {
		return false
	}
	if filter.End != nil && file.LocationTime.After(*filter.End) {
		return false
	}
	return true
}

var (
	_ MediaFileRepository    = (*memoryMediaFileRepository)(nil)
	_ MediaCaptureRepository = (*memoryMediaFileRepository)(nil)
)
