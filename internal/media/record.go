package media

import (
	"bufio"
	"context"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"time"

	"github.com/rs/zerolog/log"

	"github.com/fakeyanss/jt808-server-go/internal/clock"
	"github.com/fakeyanss/jt808-server-go/internal/contracts"
	"github.com/fakeyanss/jt808-server-go/internal/media/playback"
	"github.com/fakeyanss/jt808-server-go/internal/storage"
)

// RecordMode 是录制模式。
//
// 直播本身不落盘：帧只进内存环。录制是独立开关，默认在 cluster 下关闭。
type RecordMode string

const (
	// RecordModeOff 只保留内存帧环，不产生任何文件或对象。
	RecordModeOff RecordMode = "off"
	// RecordModeLegacy 保持旧行为：收到即持续写 .h264/.g711 文件。
	RecordModeLegacy RecordMode = "legacy"
	// RecordModeSegmented 按周期切 TS 片，片完成后上传对象存储。
	RecordModeSegmented RecordMode = "segmented"
)

// DefaultSegmentDuration 是切片周期。
const DefaultSegmentDuration = 10 * time.Minute

// ParseRecordMode 校验录制模式。
func ParseRecordMode(value string) (RecordMode, error) {
	switch RecordMode(strings.ToLower(strings.TrimSpace(value))) {
	case "", RecordModeLegacy:
		return RecordModeLegacy, nil
	case RecordModeOff:
		return RecordModeOff, nil
	case RecordModeSegmented:
		return RecordModeSegmented, nil
	default:
		return "", fmt.Errorf("record.mode must be off, legacy or segmented, got %q", value)
	}
}

// SegmentKeyframe 是切片内的关键帧索引。
type SegmentKeyframe struct {
	PTS    uint64 `json:"pts"`
	Offset int64  `json:"offset"`
}

// SegmentInfo 是一片录制完成的元数据。
type SegmentInfo struct {
	SessionID string            `json:"sessionId,omitempty"`
	Phone     string            `json:"phone"`
	Channel   uint8             `json:"channel"`
	Kind      StreamKind        `json:"kind"`
	ObjectKey string            `json:"objectKey,omitempty"`
	LocalPath string            `json:"localPath,omitempty"`
	StartTime time.Time         `json:"startTime"`
	EndTime   time.Time         `json:"endTime"`
	Duration  time.Duration     `json:"duration"`
	Size      int64             `json:"size"`
	Keyframes []SegmentKeyframe `json:"keyframes"`
	State     string            `json:"state"`
}

// SegmentSink 接收切片完成事件，由管控侧持久化索引。
type SegmentSink interface {
	OnSegment(ctx context.Context, info SegmentInfo) error
}

// SegmentSinkFunc 让函数直接满足 SegmentSink。
type SegmentSinkFunc func(ctx context.Context, info SegmentInfo) error

func (f SegmentSinkFunc) OnSegment(ctx context.Context, info SegmentInfo) error {
	return f(ctx, info)
}

type recorderConfig struct {
	dir       string
	sessionID string
	phone     string
	channel   uint8
	stem      string
	duration  time.Duration
	blob      contracts.BlobStore
	sink      SegmentSink
	kind      StreamKind
	clock     clock.Clock
}

type recorder struct {
	cfg recorderConfig

	mu       sync.Mutex
	closed   bool
	file     *os.File
	buffer   *bufio.Writer
	muxer    *tsMuxer
	partPath string
	startAt  time.Time
	startPTS uint64
	lastPTS  uint64
	timeline playback.Timeline
	index    []SegmentKeyframe
	sps      []byte
	pps      []byte
	seq      int
	wg       sync.WaitGroup
}

func newRecorder(cfg recorderConfig) *recorder {
	if cfg.duration <= 0 {
		cfg.duration = DefaultSegmentDuration
	}
	if cfg.clock == nil {
		cfg.clock = clock.System()
	}
	return &recorder{cfg: cfg}
}

// WriteVideo 写入一帧；只在关键帧处开片或换片。
func (r *recorder) WriteVideo(pts uint64, key bool, frame []byte) error {
	r.mu.Lock()
	defer r.mu.Unlock()
	if r.closed {
		return errors.New("recorder is closed")
	}
	// 8 字节原始时间戳在 32 位回绕后必须纠正，否则 TS 的 PTS 会后退。
	pts = r.timeline.Normalize(pts)
	r.captureParamSets(frame)
	if r.file == nil {
		if !key {
			return nil
		}
		if err := r.startLocked(pts); err != nil {
			return err
		}
	} else if key && r.cfg.clock.Now().Sub(r.startAt) >= r.cfg.duration {
		if err := r.finishLocked(); err != nil {
			return err
		}
		if err := r.startLocked(pts); err != nil {
			return err
		}
	}
	data := frame
	if len(r.index) == 0 && key && !hasParamSets(frame) && len(r.sps) > 0 && len(r.pps) > 0 {
		data = prependParamSets(frame, r.sps, r.pps)
	}
	if key {
		r.index = append(r.index, SegmentKeyframe{PTS: pts, Offset: r.muxer.Offset()})
	}
	if err := r.muxer.WriteFrame(pts, key, data); err != nil {
		return err
	}
	r.lastPTS = pts
	return nil
}

// Close 收尾当前切片并等待上传结束。
func (r *recorder) Close() error {
	r.mu.Lock()
	if r.closed {
		r.mu.Unlock()
		return nil
	}
	r.closed = true
	err := r.finishLocked()
	r.mu.Unlock()
	r.wg.Wait()
	return err
}

func (r *recorder) captureParamSets(frame []byte) {
	for _, nal := range splitAnnexB(frame) {
		switch nal.typ {
		case nalTypeSPS:
			r.sps = withStartCode(nal.body)
		case nalTypePPS:
			r.pps = withStartCode(nal.body)
		}
	}
}

func (r *recorder) startLocked(pts uint64) error {
	dir := filepath.Join(r.cfg.dir, "segments", r.cfg.phone, fmt.Sprintf("ch%d", r.cfg.channel))
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return err
	}
	name := fmt.Sprintf("%s-%d-%d.ts", r.cfg.stem, r.cfg.clock.Now().UnixMilli(), pts)
	r.partPath = filepath.Join(dir, name+".part")
	file, err := os.OpenFile(r.partPath, os.O_CREATE|os.O_WRONLY|os.O_TRUNC, 0o640)
	if err != nil {
		return err
	}
	r.file = file
	r.buffer = bufio.NewWriterSize(file, 64*1024)
	r.muxer = newTSMuxer(r.buffer)
	r.startAt = r.cfg.clock.Now()
	r.startPTS = pts
	r.index = nil
	return r.muxer.WriteTables()
}

func (r *recorder) finishLocked() error {
	if r.file == nil {
		return nil
	}
	var errs []error
	if r.buffer != nil {
		if err := r.buffer.Flush(); err != nil {
			errs = append(errs, err)
		}
	}
	if err := r.file.Close(); err != nil {
		errs = append(errs, err)
	}
	finalPath := strings.TrimSuffix(r.partPath, ".part")
	if err := os.Rename(r.partPath, finalPath); err != nil {
		errs = append(errs, err)
	}
	stat, statErr := os.Stat(finalPath)
	size := int64(0)
	if statErr == nil {
		size = stat.Size()
	}
	endAt := r.cfg.clock.Now()
	info := SegmentInfo{
		SessionID: r.cfg.sessionID,
		Phone:     r.cfg.phone,
		Channel:   r.cfg.channel,
		Kind:      r.cfg.kind,
		LocalPath: finalPath,
		StartTime: r.startAt,
		EndTime:   endAt,
		Duration:  endAt.Sub(r.startAt),
		Size:      size,
		Keyframes: append([]SegmentKeyframe(nil), r.index...),
		State:     "local",
	}
	if r.cfg.blob != nil {
		info.ObjectKey = storage.MediaSegmentObjectKey(
			r.cfg.phone, r.cfg.channel, string(r.cfg.kind), r.startAt, info.Duration, r.seq)
		r.seq++
		info.State = "uploading"
		r.wg.Add(1)
		go func() {
			defer r.wg.Done()
			r.upload(info)
		}()
	} else {
		r.notify(info)
	}
	r.file = nil
	r.buffer = nil
	r.muxer = nil
	r.partPath = ""
	return errors.Join(errs...)
}

// upload 把完成的切片写入对象存储：Put 暂存 → Promote 提级 → MarkCommitted。
// 失败时保留本地片，由运维或下一次重试处理，不丢数据。
func (r *recorder) upload(info SegmentInfo) {
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Minute)
	defer cancel()
	var err error
	for attempt := 0; attempt < 3; attempt++ {
		err = r.uploadOnce(ctx, &info)
		if err == nil {
			break
		}
		log.Warn().Err(err).Str("path", info.LocalPath).Int("attempt", attempt+1).
			Msg("Fail to upload media segment")
		time.Sleep(time.Duration(attempt+1) * time.Second)
	}
	if err != nil {
		info.State = "local"
		r.notify(info)
		return
	}
	_ = os.Remove(info.LocalPath)
	info.LocalPath = ""
	info.State = "committed"
	r.notify(info)
}

func (r *recorder) uploadOnce(ctx context.Context, info *SegmentInfo) error {
	file, err := os.Open(info.LocalPath)
	if err != nil {
		return err
	}
	defer file.Close()
	blobInfo, err := r.cfg.blob.Put(ctx, info.ObjectKey, file, contracts.BlobOptions{
		ContentType: "video/mp2t",
		Metadata: map[string]string{
			"phone":   info.Phone,
			"channel": fmt.Sprintf("%d", info.Channel),
			"kind":    string(info.Kind),
		},
	})
	if err != nil {
		return err
	}
	if _, err := r.cfg.blob.Promote(ctx, blobInfo); err != nil {
		return err
	}
	if err := r.cfg.blob.MarkCommitted(ctx, info.ObjectKey); err != nil {
		return err
	}
	// 对象存储已提级且标记 committed，本地片不再需要保留。
	cleanupLocalSegment(info.LocalPath)
	return nil
}

func (r *recorder) notify(info SegmentInfo) {
	if r.cfg.sink == nil {
		log.Info().Str("phone", info.Phone).Uint8("channel", info.Channel).
			Str("objectKey", info.ObjectKey).Str("state", info.State).Int64("size", info.Size).
			Msg("Media segment completed")
		return
	}
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	if err := r.cfg.sink.OnSegment(ctx, info); err != nil {
		log.Warn().Err(err).Str("phone", info.Phone).Uint8("channel", info.Channel).
			Msg("Fail to deliver media segment metadata")
	}
}

func hasParamSets(frame []byte) bool {
	sps, pps := false, false
	for _, nal := range splitAnnexB(frame) {
		switch nal.typ {
		case nalTypeSPS:
			sps = true
		case nalTypePPS:
			pps = true
		}
	}
	return sps && pps
}

func prependParamSets(frame, sps, pps []byte) []byte {
	out := make([]byte, 0, len(sps)+len(pps)+len(frame))
	out = append(out, sps...)
	out = append(out, pps...)
	out = append(out, frame...)
	return out
}

// cleanupPartFiles 删除上次进程退出遗留的未完成切片。
func cleanupPartFiles(dir string) {
	root := filepath.Join(dir, "segments")
	_ = filepath.WalkDir(root, func(path string, entry os.DirEntry, err error) error {
		if err != nil || entry.IsDir() {
			return nil
		}
		if strings.HasSuffix(entry.Name(), ".part") {
			_ = os.Remove(path)
		}
		return nil
	})
}
