// Package mediaupload 负责 0x0801 多媒体数据的落盘与路径安全。
//
// 设计边界：
//   - 只处理已经完整重组的多媒体消息，不做分包拼接（由 protocol/runtime 完成）；
//   - 文件按 <dir>/<手机号>/<YYYYMMDD>/<媒体ID>.<扩展名> 存放，同名 mediaId 幂等覆盖；
//   - 先写临时文件再原子 rename，避免进程中断留下半个文件；
//   - 只保存文件本体，元数据由 repository.MediaFileRepository 承担。
package mediaupload

import (
	"crypto/md5"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/fakeyanss/jt808-server-go/internal/codec/hex"
	"github.com/fakeyanss/jt808-server-go/internal/protocol/model"
	"github.com/fakeyanss/jt808-server-go/internal/repository"
)

// ErrInvalidMediaPath 表示待访问路径越出了媒体根目录或包含非法字符。
var ErrInvalidMediaPath = errors.New("invalid media path")

// locationTimeLayout 与 0x0200 位置体的 BCD 时间格式保持一致（YY-MM-DD-hh-mm-ss）。
const locationTimeLayout = "060102150405"

// Store 是多媒体文件存储。
type Store struct {
	root string // 绝对路径，所有文件都限制在该目录下
}

// NewStore 创建媒体文件存储并确保根目录存在。
func NewStore(root string) (*Store, error) {
	root = strings.TrimSpace(root)
	if root == "" {
		return nil, fmt.Errorf("media upload dir is required")
	}
	abs, err := filepath.Abs(root)
	if err != nil {
		return nil, fmt.Errorf("resolve media upload dir %s: %w", root, err)
	}
	if err := os.MkdirAll(abs, 0o755); err != nil {
		return nil, fmt.Errorf("create media upload dir %s: %w", abs, err)
	}
	return &Store{root: abs}, nil
}

// Root 返回媒体文件根目录的绝对路径。
func (s *Store) Root() string {
	if s == nil {
		return ""
	}
	return s.root
}

// Save 把一条完整的 0x0801 消息写入磁盘，并返回可落库的元数据。
// 同一 (phone, mediaId) 重复上报时覆盖旧文件，保证幂等。
func (s *Store) Save(upload *model.Msg0801) (*repository.MediaFile, error) {
	if s == nil {
		return nil, fmt.Errorf("media upload store is not configured")
	}
	if upload == nil || upload.Header == nil {
		return nil, fmt.Errorf("save media upload: nil message or header")
	}
	phone := upload.Header.PhoneNumber
	if err := validateSegment(phone); err != nil {
		return nil, fmt.Errorf("save media upload: %w", err)
	}

	location := decodeLocation(upload.GeoAlarmBody)
	date := time.Now().Format("20060102")
	if !location.locationTime.IsZero() {
		date = location.locationTime.Format("20060102")
	}
	relativePath := filepath.Join(phone, date,
		fmt.Sprintf("%d.%s", upload.MultiMediaID, extension(upload.MultiMediaContainer)))
	finalPath, err := s.resolve(relativePath)
	if err != nil {
		return nil, fmt.Errorf("save media upload %s/%d: %w", phone, upload.MultiMediaID, err)
	}
	if err := writeFileAtomic(finalPath, upload.FragmentData); err != nil {
		return nil, fmt.Errorf("save media upload %s/%d: %w", phone, upload.MultiMediaID, err)
	}
	sum := md5.Sum(upload.FragmentData)
	return &repository.MediaFile{
		Phone:        phone,
		MediaID:      upload.MultiMediaID,
		MediaType:    upload.MultiMediaType,
		MediaFormat:  upload.MultiMediaContainer,
		EventID:      upload.EventID,
		ChannelID:    upload.LogicChannelID,
		AlarmSign:    location.alarmSign,
		StatusSign:   location.statusSign,
		Latitude:     location.latitude,
		Longitude:    location.longitude,
		Altitude:     location.altitude,
		Speed:        location.speed,
		Direction:    location.direction,
		LocationTime: location.locationTime,
		RelativePath: relativePath,
		FileSize:     int64(len(upload.FragmentData)),
		MD5:          fmt.Sprintf("%x", sum),
		ReceivedAt:   time.Now(),
	}, nil
}

// Open 打开相对路径对应的媒体文件，供下载接口读取。
func (s *Store) Open(relativePath string) (*os.File, error) {
	if s == nil {
		return nil, fmt.Errorf("media upload store is not configured")
	}
	path, err := s.resolve(relativePath)
	if err != nil {
		return nil, err
	}
	file, err := os.Open(path)
	if err != nil {
		return nil, fmt.Errorf("open media file %s: %w", relativePath, err)
	}
	return file, nil
}

// Resolve 返回相对路径对应的绝对路径，并校验其仍位于根目录内。
func (s *Store) Resolve(relativePath string) (string, error) {
	if s == nil {
		return "", fmt.Errorf("media upload store is not configured")
	}
	return s.resolve(relativePath)
}

// resolve 校验相对路径并拼接绝对路径，防止路径穿越。
func (s *Store) resolve(relativePath string) (string, error) {
	if relativePath == "" {
		return "", fmt.Errorf("%w: empty path", ErrInvalidMediaPath)
	}
	cleaned := filepath.Clean(relativePath)
	if filepath.IsAbs(cleaned) || cleaned == "." || cleaned == ".." ||
		strings.HasPrefix(cleaned, ".."+string(os.PathSeparator)) {
		return "", fmt.Errorf("%w: %s", ErrInvalidMediaPath, relativePath)
	}
	final := filepath.Join(s.root, cleaned)
	rel, err := filepath.Rel(s.root, final)
	if err != nil || rel == ".." || strings.HasPrefix(rel, ".."+string(os.PathSeparator)) {
		return "", fmt.Errorf("%w: %s", ErrInvalidMediaPath, relativePath)
	}
	return final, nil
}

// writeFileAtomic 先写同目录临时文件，再 rename 到目标路径。
func writeFileAtomic(path string, data []byte) error {
	dir := filepath.Dir(path)
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return fmt.Errorf("create media dir %s: %w", dir, err)
	}
	tmp, err := os.CreateTemp(dir, "."+filepath.Base(path)+".*.part")
	if err != nil {
		return fmt.Errorf("create temporary media file in %s: %w", dir, err)
	}
	tmpName := tmp.Name()
	if _, err := tmp.Write(data); err != nil {
		_ = tmp.Close()
		_ = os.Remove(tmpName)
		return fmt.Errorf("write temporary media file %s: %w", tmpName, err)
	}
	if err := tmp.Close(); err != nil {
		_ = os.Remove(tmpName)
		return fmt.Errorf("close temporary media file %s: %w", tmpName, err)
	}
	if err := os.Rename(tmpName, path); err != nil {
		_ = os.Remove(tmpName)
		return fmt.Errorf("rename media file to %s: %w", path, err)
	}
	return nil
}

// extension 把 0x0801 多媒体格式编码映射为文件扩展名；未知格式保留为 bin。
func extension(format uint8) string {
	switch format {
	case 0:
		return "jpg"
	case 1:
		return "tif"
	case 2:
		return "mp3"
	case 3:
		return "wav"
	case 4:
		return "wmv"
	default:
		return "bin"
	}
}

// mediaLocation 是 0x0801 位置体中与媒体关联的字段。
type mediaLocation struct {
	alarmSign    uint32
	statusSign   uint32
	latitude     uint32
	longitude    uint32
	altitude     uint16
	speed        uint16
	direction    uint16
	locationTime time.Time
}

// decodeLocation 解析 0x0801 携带的 28 字节位置体。
// 位置体缺少报警流水号，只能提供报警标志位；解析失败时全部字段留空。
func decodeLocation(body []byte) mediaLocation {
	if len(body) != 28 {
		return mediaLocation{}
	}
	var location model.Msg0200
	if err := location.Decode(&model.PacketData{Body: body}); err != nil {
		return mediaLocation{}
	}
	result := mediaLocation{
		alarmSign:  location.AlarmSign,
		statusSign: location.StatusSign,
		latitude:   location.Latitude,
		longitude:  location.Longitude,
		altitude:   location.Altitude,
		speed:      location.Speed,
		direction:  location.Direction,
	}
	// 与全项目统一口径：协议 BCD 时间固定为 GMT+8，不随服务器时区变化。
	if parsed, err := time.ParseInLocation(locationTimeLayout, location.Time, hex.ProtocolTimeZone); err == nil {
		result.locationTime = parsed
	}
	return result
}

// validateSegment 校验目录段（如手机号），只允许字母、数字与少量分隔符。
func validateSegment(segment string) error {
	if segment == "" || segment == "." || segment == ".." || len(segment) > 64 {
		return fmt.Errorf("%w: invalid directory segment", ErrInvalidMediaPath)
	}
	for _, r := range segment {
		switch {
		case r >= 'a' && r <= 'z':
		case r >= 'A' && r <= 'Z':
		case r >= '0' && r <= '9':
		case r == '_', r == '-':
		default:
			return fmt.Errorf("%w: invalid directory segment %q", ErrInvalidMediaPath, segment)
		}
	}
	return nil
}
