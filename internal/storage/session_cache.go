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

// SessionCache 是 ConnectionRegistry 的内存实现。
type SessionCache struct {
	cacheByID map[string]*model.Session
	mutex     *sync.Mutex
}

var sessionCacheSingleton *SessionCache
var sessionCacheInitOnce sync.Once

// newSessionCache 创建独立的内存连接注册表，不启动会话数监控。
func newSessionCache() *SessionCache {
	return &SessionCache{
		cacheByID: make(map[string]*model.Session),
		mutex:     &sync.Mutex{},
	}
}

// 实例化SessionCache
func getSessionCache() *SessionCache {
	sessionCacheInitOnce.Do(func() {
		sessionCacheSingleton = newSessionCache()
		monitorSessionCnt() // 监控session个数
	})
	return sessionCacheSingleton
}

func monitorSessionCnt() {
	routines.GoSafe(func() {
		for {
			log.Debug().Int("total_conn_cnt", countSession()).Msg("Monitoring total conn count")
			time.Sleep(monitorSessionCntInterval)
		}
	})
}

// Get 通过 id 获取 session。不存在时返回 ErrSessionClosed。
func (cache *SessionCache) Get(_ context.Context, id string) (*model.Session, error) {
	cache.mutex.Lock()
	defer cache.mutex.Unlock()
	if s, ok := cache.cacheByID[id]; ok {
		return s, nil
	}
	return nil, ErrSessionClosed
}

// Bind 缓存 session。
func (cache *SessionCache) Bind(_ context.Context, s *model.Session) error {
	cache.mutex.Lock()
	defer cache.mutex.Unlock()
	cache.cacheByID[s.ID] = s
	return nil
}

// Unbind 清理 session。
func (cache *SessionCache) Unbind(_ context.Context, id string) {
	cache.mutex.Lock()
	defer cache.mutex.Unlock()
	delete(cache.cacheByID, id)
}

// Count 统计 session 个数。
func (cache *SessionCache) Count(_ context.Context) int {
	cache.mutex.Lock()
	defer cache.mutex.Unlock()
	return len(cache.cacheByID)
}

// 以下为兼容既有调用点的包级薄封装。

// GetSession 通过id获取session。如果不存在，返回ErrSessionClosed(正常情况不会出现关闭session后还来获取session)
func GetSession(id string) (*model.Session, error) {
	return getSessionCache().Get(context.Background(), id)
}

// StoreSession 缓存session
func StoreSession(s *model.Session) {
	_ = getSessionCache().Bind(context.Background(), s)
}

// ClearSession 清理session
func ClearSession(id string) {
	getSessionCache().Unbind(context.Background(), id)
}

// 统计session个数
func countSession() int {
	return getSessionCache().Count(context.Background())
}
