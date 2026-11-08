package runtime

import (
	"context"
	"sort"
	"sync"
	"time"

	"github.com/pkg/errors"
	"github.com/rs/zerolog/log"

	"github.com/fakeyanss/jt808-server-go/internal/protocol/model"
	"github.com/fakeyanss/jt808-server-go/pkg/routines"
)

// ErrSessionClosed 表示连接现场不存在，对应连接已关闭或尚未注册。
var ErrSessionClosed = errors.New("session closed")

// ErrPhoneNotBound 表示手机号尚未与任何活会话绑定（未注册或已断开）。
var ErrPhoneNotBound = errors.New("phone not bound")

const monitorSessionCntInterval = 10 * time.Second

// memoryConnectionRegistry 是 ConnectionRegistry 的内存实现。
// 两套索引在同一把锁内更新，保证“手机号 → 会话”始终指向唯一活会话。
type memoryConnectionRegistry struct {
	cacheByID map[string]*model.Session
	phoneByID map[string]string // sessionID -> phone
	phoneToID map[string]string // phone -> sessionID
	mutex     *sync.Mutex
}

// NewConnectionRegistry 创建独立的内存连接注册表，不启动会话数监控。
func NewConnectionRegistry() ConnectionRegistry {
	return newMemoryConnectionRegistry()
}

func newMemoryConnectionRegistry() *memoryConnectionRegistry {
	return &memoryConnectionRegistry{
		cacheByID: make(map[string]*model.Session),
		phoneByID: make(map[string]string),
		phoneToID: make(map[string]string),
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

// GetByPhone 通过终端手机号获取当前活会话。未绑定或会话已失效时返回 ErrPhoneNotBound。
func (cache *memoryConnectionRegistry) GetByPhone(_ context.Context, phone string) (*model.Session, error) {
	cache.mutex.Lock()
	defer cache.mutex.Unlock()
	id, ok := cache.phoneToID[phone]
	if !ok {
		return nil, ErrPhoneNotBound
	}
	session, ok := cache.cacheByID[id]
	if !ok {
		// 悬空索引不应存在；兜底清理，避免 phone 永远指向已关闭会话。
		delete(cache.phoneToID, phone)
		delete(cache.phoneByID, id)
		return nil, ErrPhoneNotBound
	}
	return session, nil
}

// Bind 缓存 session。此时尚未完成鉴权，不建立手机号索引。
func (cache *memoryConnectionRegistry) Bind(_ context.Context, s *model.Session) error {
	cache.mutex.Lock()
	defer cache.mutex.Unlock()
	cache.cacheByID[s.ID] = s
	return nil
}

// BindPhone 建立手机号索引，并返回被顶替的旧会话。
// 被顶替会话在锁内一并从两套索引摘除，避免它并发地再次绑定成功、
// 形成“互相顶替”的环（每个会话都关闭对方，最终没有活会话）。
// 调用方仍负责关闭旧连接、清空旧会话的等待项等残留清理。
func (cache *memoryConnectionRegistry) BindPhone(_ context.Context, phone, sessionID string) (*model.Session, error) {
	cache.mutex.Lock()
	defer cache.mutex.Unlock()

	if _, ok := cache.cacheByID[sessionID]; !ok {
		return nil, ErrSessionClosed
	}

	// 同一会话改绑到新手机号时，先摘掉旧反向索引。
	if previousPhone, ok := cache.phoneByID[sessionID]; ok && previousPhone != phone {
		if mapped, ok := cache.phoneToID[previousPhone]; ok && mapped == sessionID {
			delete(cache.phoneToID, previousPhone)
		}
	}

	var superseded *model.Session
	if previousID, ok := cache.phoneToID[phone]; ok && previousID != sessionID {
		superseded = cache.cacheByID[previousID]
		delete(cache.cacheByID, previousID)
		delete(cache.phoneByID, previousID)
	}

	cache.phoneToID[phone] = sessionID
	cache.phoneByID[sessionID] = phone
	return superseded, nil
}

// BindPhoneIfFree 仅在手机号未被其它会话占用时建立绑定。
// 返回 true 表示调用方现在是该手机号的拥有者；false 表示已有其它活会话占用。
func (cache *memoryConnectionRegistry) BindPhoneIfFree(_ context.Context, phone, sessionID string) (bool, error) {
	cache.mutex.Lock()
	defer cache.mutex.Unlock()

	if _, ok := cache.cacheByID[sessionID]; !ok {
		return false, ErrSessionClosed
	}
	if previousID, ok := cache.phoneToID[phone]; ok && previousID != sessionID {
		return false, nil
	}

	// 同一会话改绑到新手机号时，先摘掉旧反向索引。
	if previousPhone, ok := cache.phoneByID[sessionID]; ok && previousPhone != phone {
		if mapped, ok := cache.phoneToID[previousPhone]; ok && mapped == sessionID {
			delete(cache.phoneToID, previousPhone)
		}
	}
	cache.phoneToID[phone] = sessionID
	cache.phoneByID[sessionID] = phone
	return true, nil
}

// Unbind 清理 session 及对应的手机号索引。
// 仅当 phone 索引仍指向该 session 时才删除，避免误删顶替者的索引。
func (cache *memoryConnectionRegistry) Unbind(_ context.Context, id string) {
	cache.mutex.Lock()
	defer cache.mutex.Unlock()

	delete(cache.cacheByID, id)
	if phone, ok := cache.phoneByID[id]; ok {
		delete(cache.phoneByID, id)
		if mapped, ok := cache.phoneToID[phone]; ok && mapped == id {
			delete(cache.phoneToID, phone)
		}
	}
}

// Count 统计 session 个数。
func (cache *memoryConnectionRegistry) Count(_ context.Context) int {
	cache.mutex.Lock()
	defer cache.mutex.Unlock()
	return len(cache.cacheByID)
}

// ListBindings 返回手机号索引快照，按手机号升序，保证分页结果稳定可复现。
func (cache *memoryConnectionRegistry) ListBindings(_ context.Context) []SessionBinding {
	cache.mutex.Lock()
	defer cache.mutex.Unlock()

	bindings := make([]SessionBinding, 0, len(cache.phoneToID))
	for phone, sessionID := range cache.phoneToID {
		session, ok := cache.cacheByID[sessionID]
		if !ok {
			// 悬空索引不应存在；读取时顺带清理，避免管理面展示已失效会话。
			delete(cache.phoneToID, phone)
			delete(cache.phoneByID, sessionID)
			continue
		}
		bindings = append(bindings, SessionBinding{Phone: phone, Session: session})
	}
	sort.Slice(bindings, func(i, j int) bool { return bindings[i].Phone < bindings[j].Phone })
	return bindings
}

// 编译期确认内存实现满足接口。
var _ ConnectionRegistry = (*memoryConnectionRegistry)(nil)
