// Package retention 按配置的保留天数定期清理历史数据与对应媒体文件。
//
// 设计边界：
//   - 只清理按时间累积的历史表，不触碰设备主数据、配置快照与 outbox；
//   - 仅 SQL 后端提供清理能力，memory 后端跳过（数据本身随进程结束而释放）；
//   - 所有删除按主键分批执行，单条 SQL 最多处理 batchSize 行；
//   - media_files 先删文件再删元数据，文件删除失败时保留元数据等待下次重试。
package retention

import (
	"context"
	"errors"
	"os"
	"time"

	"github.com/rs/zerolog/log"

	"github.com/fakeyanss/jt808-server-go/internal/observability"
	"github.com/fakeyanss/jt808-server-go/internal/repository"
)

// defaultBatchPause 是批与批之间的让出时间，避免长时间连续写库。
const defaultBatchPause = 50 * time.Millisecond

// 清理批次的默认值，与配置层默认值保持一致。
const (
	defaultInterval  = time.Hour
	defaultBatchSize = 1000
)

// MediaStore 是清理媒体文件所需的最小接口，由 mediaupload.Store 实现。
type MediaStore interface {
	// Root 返回媒体根目录的绝对路径。
	Root() string
	// Resolve 返回相对路径对应的绝对路径，并保证路径位于媒体根目录内。
	Resolve(relativePath string) (string, error)
}

// Config 是清理任务的归一化配置；MaxAge <= 0 表示该类别不清理。
type Config struct {
	Interval           time.Duration
	BatchSize          int
	Locations          time.Duration
	Alarms             time.Duration
	MediaFiles         time.Duration
	MediaCaptures      time.Duration
	MediaSearchResults time.Duration
}

// normalized 填充零值，保证直接构造 Config 也可用。
func (c Config) normalized() Config {
	if c.Interval <= 0 {
		c.Interval = defaultInterval
	}
	if c.BatchSize <= 0 {
		c.BatchSize = defaultBatchSize
	}
	return c
}

// Summary 是一次清理任务的结果，Failures 为发生错误的类别数。
type Summary struct {
	Locations          int64
	Alarms             int64
	MediaFiles         int64
	MediaCaptures      int64
	MediaSearchResults int64
	Failures           int
}

// Deleted 返回本次清理删除的总行数。
func (s Summary) Deleted() int64 {
	return s.Locations + s.Alarms + s.MediaFiles + s.MediaCaptures + s.MediaSearchResults
}

// Cleaner 执行历史数据清理。并发调用 CleanOnce 不安全，Run 内部串行执行。
type Cleaner struct {
	store   repository.RetentionStore
	media   MediaStore
	cfg     Config
	metrics *observability.Metrics
	now     func() time.Time
	done    chan struct{}
}

// NewCleaner 创建清理任务。store 为 nil 时用于表达“后端不支持”的场景。
func NewCleaner(store repository.RetentionStore, media MediaStore, cfg Config, metrics *observability.Metrics) *Cleaner {
	return &Cleaner{
		store:   store,
		media:   media,
		cfg:     cfg.normalized(),
		metrics: metrics,
		now:     time.Now,
		done:    make(chan struct{}),
	}
}

// Done 在 Run 退出后关闭，供优雅停机等待。
func (c *Cleaner) Done() <-chan struct{} {
	if c == nil {
		closed := make(chan struct{})
		close(closed)
		return closed
	}
	return c.done
}

// Run 按配置周期执行清理，直到 ctx 被取消。
// 首次清理在第一个周期到达时执行，避免服务启动瞬间触发大量删除。
func (c *Cleaner) Run(ctx context.Context) {
	defer close(c.done)
	if c == nil || c.store == nil {
		log.Warn().Msg("Retention cleanup skipped: storage backend does not support cleanup")
		return
	}
	ticker := time.NewTicker(c.cfg.Interval)
	defer ticker.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
			started := c.now()
			summary := c.CleanOnce(ctx)
			log.Info().
				Int64("locations", summary.Locations).
				Int64("alarms", summary.Alarms).
				Int64("media_files", summary.MediaFiles).
				Int64("media_captures", summary.MediaCaptures).
				Int64("media_search_results", summary.MediaSearchResults).
				Int("failures", summary.Failures).
				Dur("duration", c.now().Sub(started)).
				Msg("Retention cleanup finished")
		}
	}
}

// CleanOnce 执行一轮清理，各类别相互独立，单类失败不影响其余类别。
func (c *Cleaner) CleanOnce(ctx context.Context) Summary {
	var summary Summary
	if c == nil || c.store == nil {
		return summary
	}
	now := c.now()

	if c.cfg.Locations > 0 {
		summary.Locations, summary.Failures = c.purgeCategory(ctx, observability.RetentionCategoryLocations,
			c.cfg.Locations, now, summary.Failures, c.store.PurgeLocations)
	}
	if c.cfg.Alarms > 0 {
		summary.Alarms, summary.Failures = c.purgeCategory(ctx, observability.RetentionCategoryAlarms,
			c.cfg.Alarms, now, summary.Failures, c.store.PurgeAlarms)
	}
	if c.cfg.MediaFiles > 0 {
		deleted, failures := c.purgeMediaFiles(ctx, now.Add(-c.cfg.MediaFiles))
		summary.MediaFiles = deleted
		summary.Failures += failures
	}
	if c.cfg.MediaCaptures > 0 {
		summary.MediaCaptures, summary.Failures = c.purgeCategory(ctx, observability.RetentionCategoryMediaCaptures,
			c.cfg.MediaCaptures, now, summary.Failures, c.store.PurgeMediaCaptures)
	}
	if c.cfg.MediaSearchResults > 0 {
		summary.MediaSearchResults, summary.Failures = c.purgeCategory(ctx, observability.RetentionCategoryMediaSearchResults,
			c.cfg.MediaSearchResults, now, summary.Failures, c.store.PurgeMediaSearchResults)
	}
	c.metrics.RecordRetentionRun(c.now(), c.now().Sub(now))
	return summary
}

// purgeCategory 循环删除某一类别，直到不足一批、出错或 ctx 取消。
func (c *Cleaner) purgeCategory(ctx context.Context, category string, maxAge time.Duration, now time.Time,
	failures int, purge func(context.Context, time.Time, int) (int64, error)) (int64, int) {
	cutoff := now.Add(-maxAge)
	var total int64
	for {
		deleted, err := purge(ctx, cutoff, c.cfg.BatchSize)
		if err != nil {
			if ctx.Err() != nil {
				return total, failures
			}
			c.fail(category, err)
			return total, failures + 1
		}
		total += deleted
		if deleted < int64(c.cfg.BatchSize) {
			break
		}
		if !c.pause(ctx) {
			break
		}
	}
	c.metrics.AddRetentionDeleted(category, total)
	return total, failures
}

// purgeMediaFiles 先删除文件，再删除对应元数据。
//
// 顺序取舍：文件删除失败时保留元数据，下次清理重试；文件不存在视为已删除，
// 因此进程在“文件已删、元数据未删”之间崩溃时，下次运行会继续删元数据。
// 这样不会产生“元数据已删但文件仍在”的永久孤儿文件。
func (c *Cleaner) purgeMediaFiles(ctx context.Context, cutoff time.Time) (int64, int) {
	category := observability.RetentionCategoryMediaFiles
	if c.media == nil {
		c.fail(category, errors.New("media upload store is not configured"))
		return 0, 1
	}
	var total int64
	var failures int
	for {
		files, err := c.store.ListExpiredMediaFiles(ctx, cutoff, c.cfg.BatchSize)
		if err != nil {
			if ctx.Err() != nil {
				return total, failures
			}
			c.fail(category, err)
			return total, failures + 1
		}
		if len(files) == 0 {
			break
		}

		removed := make([]repository.MediaFile, 0, len(files))
		for _, file := range files {
			path, resolveErr := c.media.Resolve(file.RelativePath)
			if resolveErr != nil {
				c.fail(category, resolveErr)
				failures++
				continue
			}
			if removeErr := os.Remove(path); removeErr != nil && !errors.Is(removeErr, os.ErrNotExist) {
				c.fail(category, removeErr)
				failures++
				continue
			}
			removed = append(removed, file)
		}
		if len(removed) > 0 {
			deleted, deleteErr := c.store.DeleteMediaFiles(ctx, removed)
			if deleteErr != nil {
				if ctx.Err() != nil {
					return total, failures
				}
				c.fail(category, deleteErr)
				return total + deleted, failures + 1
			}
			total += deleted
		}
		// 存在无法删除的文件时结束本轮，避免同一批数据被反复查询；下次运行重试。
		if failures > 0 || len(files) < c.cfg.BatchSize {
			break
		}
		if !c.pause(ctx) {
			break
		}
	}
	c.metrics.AddRetentionDeleted(category, total)
	return total, failures
}

// fail 记录一次类别级失败，同时输出日志与指标。
func (c *Cleaner) fail(category string, err error) {
	c.metrics.IncRetentionFailure(category)
	log.Warn().Err(err).Str("category", category).Msg("Retention cleanup failed")
}

// pause 在批次之间让出，返回 false 表示 ctx 已取消。
func (c *Cleaner) pause(ctx context.Context) bool {
	timer := time.NewTimer(defaultBatchPause)
	defer timer.Stop()
	select {
	case <-ctx.Done():
		return false
	case <-timer.C:
		return true
	}
}
