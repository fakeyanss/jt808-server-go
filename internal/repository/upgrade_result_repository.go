package repository

import (
	"context"
	"errors"
	"fmt"
	"sync"
	"time"
)

// ErrUpgradeResultNotFound 表示设备尚未上报升级结果。
var ErrUpgradeResultNotFound = errors.New("upgrade result not found")

// UpgradeResult 是终端 0x0108 升级结果通知的落库记录。
// 只保留业务字段，不保存消息头，避免跨进程残留会话信息。
type UpgradeResult struct {
	Phone       string    `json:"phone"`
	UpgradeType uint8     `json:"upgradeType"` // 0:终端 1:IC卡读卡器 2:北斗
	Result      uint8     `json:"result"`      // 0:成功 1:失败 2:取消
	Serial      uint16    `json:"serial"`      // 终端上报的报文流水号
	ReceivedAt  time.Time `json:"receivedAt"`  // 平台接收时间
}

// UpgradeResultRepository 终端升级结果仓储（B 类）。
// 保存每个设备最近一次升级结果，供升级验收查询；读取返回快照。
type UpgradeResultRepository interface {
	Save(ctx context.Context, result *UpgradeResult) error
	Latest(ctx context.Context, phone string) (*UpgradeResult, error)
}

// memoryUpgradeResultRepository 是 UpgradeResultRepository 的内存实现。
type memoryUpgradeResultRepository struct {
	mu      sync.RWMutex
	results map[string]UpgradeResult
}

// NewUpgradeResultRepository 创建独立的内存升级结果仓储。
func NewUpgradeResultRepository() UpgradeResultRepository {
	return &memoryUpgradeResultRepository{results: map[string]UpgradeResult{}}
}

func (r *memoryUpgradeResultRepository) Save(_ context.Context, result *UpgradeResult) error {
	if result == nil || result.Phone == "" {
		return fmt.Errorf("save upgrade result: nil result or empty phone")
	}
	r.mu.Lock()
	defer r.mu.Unlock()
	r.results[result.Phone] = *result
	return nil
}

func (r *memoryUpgradeResultRepository) Latest(_ context.Context, phone string) (*UpgradeResult, error) {
	r.mu.RLock()
	defer r.mu.RUnlock()
	result, ok := r.results[phone]
	if !ok {
		return nil, ErrUpgradeResultNotFound
	}
	copy := result
	return &copy, nil
}

var _ UpgradeResultRepository = (*memoryUpgradeResultRepository)(nil)
