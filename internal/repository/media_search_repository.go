package repository

import (
	"context"
	"fmt"
	"sort"
	"sync"
	"time"
)

// MediaSearchRecord 是 0x0802 存储多媒体数据检索应答中的一条媒体记录。
// 位置字段来自记录内嵌的 28 字节 0x0200 位置体。
type MediaSearchRecord struct {
	Phone        string    `json:"phone"`
	AnswerSerial uint16    `json:"answerSerial"`
	Index        int       `json:"index"`
	MediaID      uint32    `json:"mediaId"`
	MediaType    uint8     `json:"mediaType"`
	ChannelID    uint8     `json:"channelId"`
	EventID      uint8     `json:"eventId"`
	AlarmSign    uint32    `json:"alarmSign"`
	StatusSign   uint32    `json:"statusSign"`
	Latitude     uint32    `json:"latitude"`
	Longitude    uint32    `json:"longitude"`
	Altitude     uint16    `json:"altitude"`
	Speed        uint16    `json:"speed"`
	Direction    uint16    `json:"direction"`
	LocationTime time.Time `json:"locationTime,omitempty"`
	ReceivedAt   time.Time `json:"receivedAt"`
}

// MediaSearchRepository 持久化终端对 0x8802 检索命令的应答（0x0802）。
// 同一次应答以 (phone, answerSerial) 为单位整体覆盖，重复上报不会累积脏数据。
type MediaSearchRepository interface {
	Replace(ctx context.Context, phone string, answerSerial uint16, records []MediaSearchRecord) error
	List(ctx context.Context, phone string, limit int) ([]MediaSearchRecord, error)
}

// 检索结果查询的默认与最大条数，与媒体文件查询保持一致。
const (
	defaultMediaSearchLimit = 100
	maxMediaSearchLimit     = 500
)

// memoryMediaSearchRepository 是 MediaSearchRepository 的内存实现。
type memoryMediaSearchRepository struct {
	mu      sync.RWMutex
	results map[string]map[uint16][]MediaSearchRecord
}

// NewMediaSearchRepository 创建独立的内存检索结果仓储。
func NewMediaSearchRepository() MediaSearchRepository {
	return &memoryMediaSearchRepository{results: map[string]map[uint16][]MediaSearchRecord{}}
}

func (r *memoryMediaSearchRepository) Replace(_ context.Context, phone string, answerSerial uint16, records []MediaSearchRecord) error {
	if err := ValidateMediaSearch(phone, records); err != nil {
		return err
	}
	normalized := NormalizeMediaSearchRecords(phone, answerSerial, records)
	r.mu.Lock()
	defer r.mu.Unlock()
	if r.results[phone] == nil {
		r.results[phone] = map[uint16][]MediaSearchRecord{}
	}
	r.results[phone][answerSerial] = normalized
	return nil
}

func (r *memoryMediaSearchRepository) List(_ context.Context, phone string, limit int) ([]MediaSearchRecord, error) {
	r.mu.RLock()
	bySerial := r.results[phone]
	all := make([]MediaSearchRecord, 0)
	for _, records := range bySerial {
		all = append(all, records...)
	}
	r.mu.RUnlock()

	sort.Slice(all, func(i, j int) bool {
		if !all[i].ReceivedAt.Equal(all[j].ReceivedAt) {
			return all[i].ReceivedAt.After(all[j].ReceivedAt)
		}
		if all[i].AnswerSerial != all[j].AnswerSerial {
			return all[i].AnswerSerial > all[j].AnswerSerial
		}
		return all[i].Index < all[j].Index
	})
	if limit = normalizeMediaSearchLimit(limit); len(all) > limit {
		all = all[:limit]
	}
	return all, nil
}

// ValidateMediaSearch 拒绝会导致数据库截断的入参。
func ValidateMediaSearch(phone string, records []MediaSearchRecord) error {
	if phone == "" {
		return fmt.Errorf("save media search: empty phone")
	}
	if len(phone) > 32 {
		return fmt.Errorf("save media search: phone exceeds 32 bytes")
	}
	for index, record := range records {
		if record.MediaID == 0 {
			return fmt.Errorf("save media search: record %d has zero media id", index)
		}
	}
	return nil
}

// NormalizeMediaSearchRecords 复制记录并补齐设备号、应答流水号与接收时间。
func NormalizeMediaSearchRecords(phone string, answerSerial uint16, records []MediaSearchRecord) []MediaSearchRecord {
	normalized := make([]MediaSearchRecord, 0, len(records))
	for index, record := range records {
		copyRecord := record
		copyRecord.Phone = phone
		copyRecord.AnswerSerial = answerSerial
		copyRecord.Index = index
		if copyRecord.ReceivedAt.IsZero() {
			copyRecord.ReceivedAt = time.Now()
		}
		normalized = append(normalized, copyRecord)
	}
	return normalized
}

// normalizeMediaSearchLimit 约束查询条数，避免单次请求拖垮进程。
func normalizeMediaSearchLimit(limit int) int {
	if limit <= 0 {
		return defaultMediaSearchLimit
	}
	if limit > maxMediaSearchLimit {
		return maxMediaSearchLimit
	}
	return limit
}

var _ MediaSearchRepository = (*memoryMediaSearchRepository)(nil)
