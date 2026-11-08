package uplink

import (
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"time"

	"github.com/fakeyanss/jt808-server-go/internal/repository"
)

// SpoolRecord 是一条待兜底重放的上行定位记录。
// 内嵌 LocationRecord 复用仓储字段；Serial 仅 0x0200 有值，用于报警去重键。
type SpoolRecord struct {
	repository.LocationRecord
	Serial uint16 `json:"serial,omitempty"`
}

// Spool 是落盘的上行 spool。
// 每条记录写一个文件：先写临时文件并 fsync，再 rename，最后 fsync 目录，
// 保证 Put 返回时记录已真正落到磁盘。
type Spool struct {
	dir string
}

// NewSpool 打开或创建 spool 目录。
func NewSpool(dir string) (*Spool, error) {
	if strings.TrimSpace(dir) == "" {
		return nil, errors.New("uplink spool dir is empty")
	}
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return nil, fmt.Errorf("create uplink spool dir: %w", err)
	}
	if err := syncDir(dir); err != nil {
		return nil, fmt.Errorf("sync uplink spool dir: %w", err)
	}
	return &Spool{dir: dir}, nil
}

// Dir 返回 spool 目录，便于测试与运维观测。
func (s *Spool) Dir() string { return s.dir }

// Put 持久化一条记录，返回记录 ID。
func (s *Spool) Put(record SpoolRecord) (string, error) {
	payload, err := json.Marshal(record)
	if err != nil {
		return "", fmt.Errorf("marshal spool record: %w", err)
	}
	id := newRecordID()
	tmpPath := filepath.Join(s.dir, id+".tmp")
	finalPath := filepath.Join(s.dir, id+".json")

	file, err := os.OpenFile(tmpPath, os.O_CREATE|os.O_EXCL|os.O_WRONLY, 0o644)
	if err != nil {
		return "", fmt.Errorf("create spool temp file: %w", err)
	}
	if _, err := file.Write(payload); err != nil {
		_ = file.Close()
		_ = os.Remove(tmpPath)
		return "", fmt.Errorf("write spool record: %w", err)
	}
	if err := file.Sync(); err != nil {
		_ = file.Close()
		_ = os.Remove(tmpPath)
		return "", fmt.Errorf("sync spool record: %w", err)
	}
	if err := file.Close(); err != nil {
		_ = os.Remove(tmpPath)
		return "", fmt.Errorf("close spool record: %w", err)
	}
	if err := os.Rename(tmpPath, finalPath); err != nil {
		_ = os.Remove(tmpPath)
		return "", fmt.Errorf("publish spool record: %w", err)
	}
	if err := syncDir(s.dir); err != nil {
		return "", fmt.Errorf("sync spool dir: %w", err)
	}
	return id, nil
}

// Ack 删除一条已成功入库的记录；记录不存在视为已确认。
func (s *Spool) Ack(id string) error {
	err := os.Remove(filepath.Join(s.dir, id+".json"))
	if err != nil && !errors.Is(err, os.ErrNotExist) {
		return fmt.Errorf("remove spool record: %w", err)
	}
	return syncDir(s.dir)
}

// Pending 返回全部未确认记录，按文件名（即写入时间）升序。
func (s *Spool) Pending() ([]pendingRecord, error) {
	entries, err := os.ReadDir(s.dir)
	if err != nil {
		return nil, fmt.Errorf("read spool dir: %w", err)
	}
	names := make([]string, 0, len(entries))
	for _, entry := range entries {
		if entry.IsDir() || !strings.HasSuffix(entry.Name(), ".json") {
			continue
		}
		names = append(names, entry.Name())
	}
	sort.Strings(names)

	records := make([]pendingRecord, 0, len(names))
	for _, name := range names {
		payload, readErr := os.ReadFile(filepath.Join(s.dir, name))
		if readErr != nil {
			return nil, fmt.Errorf("read spool record %s: %w", name, readErr)
		}
		var record SpoolRecord
		if decodeErr := json.Unmarshal(payload, &record); decodeErr != nil {
			return nil, fmt.Errorf("decode spool record %s: %w", name, decodeErr)
		}
		records = append(records, pendingRecord{ID: strings.TrimSuffix(name, ".json"), Record: record})
	}
	return records, nil
}

type pendingRecord struct {
	ID     string
	Record SpoolRecord
}

func newRecordID() string {
	var random [6]byte
	if _, err := rand.Read(random[:]); err != nil {
		// crypto/rand 失败极罕见；退化为纳秒时间戳，仍保证同一进程内唯一。
		return fmt.Sprintf("%d", time.Now().UnixNano())
	}
	return fmt.Sprintf("%d-%s", time.Now().UnixNano(), hex.EncodeToString(random[:]))
}

func syncDir(dir string) error {
	file, err := os.Open(dir)
	if err != nil {
		return err
	}
	defer file.Close()
	return file.Sync()
}
