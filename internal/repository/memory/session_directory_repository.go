package memory

import (
	"context"
	"sync"
	"time"

	"github.com/fakeyanss/jt808-server-go/internal/repository"
)

// SessionDirectoryRepository 是进程内会话目录，仅用于单机与测试。
// 集群生产路径使用 SQL 后端，claimed_at 由数据库时间写入。
type SessionDirectoryRepository struct {
	mu      sync.Mutex
	records map[string]repository.SessionRecord
}

// NewSessionDirectoryRepository 创建内存会话目录。
func NewSessionDirectoryRepository() *SessionDirectoryRepository {
	return &SessionDirectoryRepository{records: make(map[string]repository.SessionRecord)}
}

func (r *SessionDirectoryRepository) Claim(_ context.Context, phone string, owner repository.SessionOwner) (uint64, error) {
	r.mu.Lock()
	defer r.mu.Unlock()
	record, ok := r.records[phone]
	if !ok {
		record = repository.SessionRecord{
			Phone:        phone,
			SessionOwner: owner,
			Generation:   1,
			ClaimedAt:    time.Now(),
		}
		r.records[phone] = record
		return record.Generation, nil
	}
	if record.SessionOwner != owner {
		record.SessionOwner = owner
		record.Generation++
		record.ClaimedAt = time.Now()
		r.records[phone] = record
	}
	return record.Generation, nil
}

func (r *SessionDirectoryRepository) Release(_ context.Context, phone string, owner repository.SessionOwner, generation uint64) error {
	r.mu.Lock()
	defer r.mu.Unlock()
	record, ok := r.records[phone]
	if !ok || record.SessionOwner != owner || record.Generation != generation {
		return nil
	}
	delete(r.records, phone)
	return nil
}

func (r *SessionDirectoryRepository) Validate(_ context.Context, phone string, owner repository.SessionOwner, generation uint64) (bool, error) {
	r.mu.Lock()
	defer r.mu.Unlock()
	record, ok := r.records[phone]
	if !ok || record.SessionOwner != owner {
		return false, nil
	}
	return generation >= record.Generation, nil
}

func (r *SessionDirectoryRepository) Get(_ context.Context, phone string) (*repository.SessionRecord, error) {
	r.mu.Lock()
	defer r.mu.Unlock()
	record, ok := r.records[phone]
	if !ok {
		return nil, nil
	}
	copy := record
	return &copy, nil
}
