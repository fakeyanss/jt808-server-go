// Package coordination 提供多副本环境下的轻量协调原语：
// 周期性任务的单实例租约，以及按业务键的共享 TTL 去重。
//
// 设计边界：
//   - 只依赖数据库，不引入 Redis 等额外中间件；
//   - 租约带过期时间，持有者崩溃后由其它副本在 TTL 到期后接手；
//   - 租约续期由 Runner 负责，长任务不会因为执行时间超过 TTL 被抢走；
//   - 去重是“首次写入成功者获得执行权”，不需要持有锁。
package coordination

import (
	"context"
	"fmt"
	"os"
	"sync"
	"time"

	"github.com/rs/zerolog/log"
)

// DefaultTTL 是任务租约的默认有效期。Runner 会在 TTL/3 时续期，
// 因此正常执行期间不会失效；持有者崩溃后最长 DefaultTTL 由其它副本接手。
const DefaultTTL = time.Minute

// Store 是任务租约与共享去重的持久化契约。
//
// 所有方法只接收相对 TTL，过期判定时刻由各后端自行取得：
// SQL 后端用数据库当前时间，内存后端用进程时钟。
//
// Acquire 在 (name) 上尝试取得租约：
//   - 行不存在、已过期或 owner 相同（续期）时成功；
//   - 其它 owner 持有且未过期时返回 false。
//
// MarkOnce 在 (key) 上尝试登记一次事件：
//   - 不存在或已过期时写入并返回 true；
//   - 未过期时返回 false。
//
// PruneExpired 删除已过期的租约与去重行，返回删除总行数。
type Store interface {
	Acquire(ctx context.Context, name, owner string, ttl time.Duration) (bool, error)
	Release(ctx context.Context, name, owner string) error
	MarkOnce(ctx context.Context, key string, ttl time.Duration) (bool, error)
	PruneExpired(ctx context.Context) (int64, error)
}

// Runner 在 Store 上执行带续期的单实例任务。
type Runner struct {
	store Store
	owner string
	ttl   time.Duration
}

// NewRunner 创建任务 Runner。store 为 nil 时退化为进程内直接执行，
// 仅用于单机或测试；多副本部署必须传入共享 Store。
func NewRunner(store Store, ttl time.Duration) *Runner {
	if ttl <= 0 {
		ttl = DefaultTTL
	}
	return &Runner{
		store: store,
		owner: Owner(),
		ttl:   ttl,
	}
}

// Owner 返回当前进程的租约标识。hostname+pid 足以区分同一集群内的副本；
// 同一进程内的多个 Runner 共用 owner，不会互相抢占不同任务名的租约。
func Owner() string {
	host, err := os.Hostname()
	if err != nil || host == "" {
		host = "unknown"
	}
	return fmt.Sprintf("%s-%d", host, os.Getpid())
}

// Run 尝试取得 name 租约后执行 fn。返回 false 表示租约被其它副本持有，
// 本次任务应跳过；返回 true 表示 fn 已执行（无论 fn 内部是否报错）。
//
// 执行期间每 ttl/3 续期一次。续期发现租约已被其它副本接手时，取消 fn 的 ctx，
// 避免两个副本同时继续写库。
func (r *Runner) Run(ctx context.Context, name string, fn func(context.Context)) bool {
	if r == nil || r.store == nil {
		fn(ctx)
		return true
	}
	acquired, err := r.store.Acquire(ctx, name, r.owner, r.ttl)
	if err != nil {
		log.Warn().Err(err).Str("task", name).Msg("Task lease acquire failed; skip this round")
		return false
	}
	if !acquired {
		log.Debug().Str("task", name).Str("owner", r.owner).Msg("Task lease held by another replica; skip this round")
		return false
	}

	runCtx, cancel := context.WithCancel(ctx)
	defer cancel()
	renewDone := make(chan struct{})
	renewStopped := make(chan struct{})
	go func() {
		defer close(renewStopped)
		r.renew(runCtx, name, renewDone, cancel)
	}()

	defer func() {
		close(renewDone)
		<-renewStopped
		releaseCtx, releaseCancel := context.WithTimeout(context.Background(), 2*time.Second)
		defer releaseCancel()
		if err := r.store.Release(releaseCtx, name, r.owner); err != nil {
			log.Warn().Err(err).Str("task", name).Msg("Task lease release failed; it will expire by TTL")
		}
	}()

	fn(runCtx)
	return true
}

func (r *Runner) renew(ctx context.Context, name string, done <-chan struct{}, cancel context.CancelFunc) {
	interval := r.ttl / 3
	if interval <= 0 {
		interval = time.Second
	}
	ticker := time.NewTicker(interval)
	defer ticker.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case <-done:
			return
		case <-ticker.C:
			ok, err := r.store.Acquire(ctx, name, r.owner, r.ttl)
			if err != nil {
				log.Warn().Err(err).Str("task", name).Msg("Task lease renew failed; will retry")
				continue
			}
			if !ok {
				log.Warn().Str("task", name).Msg("Task lease lost to another replica; cancel current run")
				cancel()
				return
			}
		}
	}
}

type memoryLease struct {
	owner     string
	expiresAt int64
}

type memoryStore struct {
	mu     sync.Mutex
	leases map[string]memoryLease
	dedup  map[string]int64
	// now 是内存后端的时钟；SQL 后端的等价物是数据库当前时间。
	now func() time.Time
}

// NewMemoryStore 创建进程内协调存储，供单机模式与测试使用。
func NewMemoryStore() Store {
	return NewMemoryStoreWithClock(time.Now)
}

// NewMemoryStoreWithClock 用指定时钟创建内存协调存储，供测试推进时间。
func NewMemoryStoreWithClock(now func() time.Time) Store {
	if now == nil {
		now = time.Now
	}
	return &memoryStore{
		leases: make(map[string]memoryLease),
		dedup:  make(map[string]int64),
		now:    now,
	}
}

func (m *memoryStore) Acquire(_ context.Context, name, owner string, ttl time.Duration) (bool, error) {
	nowMs := m.now().UnixMilli()
	expiresAt := nowMs + normalizeTTL(ttl).Milliseconds()
	m.mu.Lock()
	defer m.mu.Unlock()
	if current, ok := m.leases[name]; ok && current.expiresAt > nowMs && current.owner != owner {
		return false, nil
	}
	m.leases[name] = memoryLease{owner: owner, expiresAt: expiresAt}
	return true, nil
}

func (m *memoryStore) Release(_ context.Context, name, owner string) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	if current, ok := m.leases[name]; ok && current.owner == owner {
		delete(m.leases, name)
	}
	return nil
}

func (m *memoryStore) MarkOnce(_ context.Context, key string, ttl time.Duration) (bool, error) {
	nowMs := m.now().UnixMilli()
	expiresAt := nowMs + normalizeTTL(ttl).Milliseconds()
	m.mu.Lock()
	defer m.mu.Unlock()
	if current, ok := m.dedup[key]; ok && current > nowMs {
		return false, nil
	}
	m.dedup[key] = expiresAt
	return true, nil
}

func (m *memoryStore) PruneExpired(_ context.Context) (int64, error) {
	nowMs := m.now().UnixMilli()
	m.mu.Lock()
	defer m.mu.Unlock()
	var deleted int64
	for name, lease := range m.leases {
		if lease.expiresAt <= nowMs {
			delete(m.leases, name)
			deleted++
		}
	}
	for key, expiresAt := range m.dedup {
		if expiresAt <= nowMs {
			delete(m.dedup, key)
			deleted++
		}
	}
	return deleted, nil
}

func normalizeTTL(ttl time.Duration) time.Duration {
	if ttl <= 0 {
		return DefaultTTL
	}
	return ttl
}
