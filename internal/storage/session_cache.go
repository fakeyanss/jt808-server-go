package storage

import (
	"context"
	"sync"
	"time"

	"github.com/pkg/errors"
	"github.com/rs/zerolog/log"

	"github.com/fakeyanss/jt808-server-go/internal/protocol/model"
	"github.com/fakeyanss/jt808-server-go/pkg/routines"
)

var ErrSessionClosed = errors.New("session closed")

const monitorSessionCntInterval = 10 * time.Second

// memoryConnectionRegistry 是 ConnectionRegistry 的内存实现。
type memoryConnectionRegistry struct {
	cacheByID map[string]*model.Session
	mutex     *sync.Mutex
}

// newMemoryConnectionRegistry 创建独立的内存连接注册表，不启动会话数监控。
func newMemoryConnectionRegistry() *memoryConnectionRegistry {
	return &memoryConnectionRegistry{
		cacheByID: make(map[string]*model.Session),
		mutex:     &sync.Mutex{},
	}
}

// newDefaultConnectionRegistry 创建进程默认连接注册表，并启动会话数监控。
func newDefaultConnectionRegistry() *memoryConnectionRegistry {
	registry := newMemoryConnectionRegistry()
	monitorSessionCnt(registry)
	return registry
}

func monitorSessionCnt(registry *memoryConnectionRegistry) {
	routines.GoSafe(func() {
		for {
			log.Debug().Int("total_conn_cnt", registry.Count(context.Background())).Msg("Monitoring total conn count")
			time.Sleep(monitorSessionCntInterval)
		}
	})
}

// Get 通过 id 获取 session。不存在时返回 ErrSessionClosed。
func (cache *memoryConnectionRegistry) Get(_ context.Context, id string) (*model.Session, error) {
	cache.mutex.Lock()
	defer cache.mutex.Unlock()
	if s, ok := cache.cacheByID[id]; ok {
		return s, nil
	}
	return nil, ErrSessionClosed
}

// Bind 缓存 session。
func (cache *memoryConnectionRegistry) Bind(_ context.Context, s *model.Session) error {
	cache.mutex.Lock()
	defer cache.mutex.Unlock()
	cache.cacheByID[s.ID] = s
	return nil
}

// Unbind 清理 session。
func (cache *memoryConnectionRegistry) Unbind(_ context.Context, id string) {
	cache.mutex.Lock()
	defer cache.mutex.Unlock()
	delete(cache.cacheByID, id)
}

// Count 统计 session 个数。
func (cache *memoryConnectionRegistry) Count(_ context.Context) int {
	cache.mutex.Lock()
	defer cache.mutex.Unlock()
	return len(cache.cacheByID)
}
