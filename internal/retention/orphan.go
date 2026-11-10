// 媒体目录孤儿文件扫描。
//
// 设计边界：
//   - 判定依据是文件自身的 mtime，不使用 <手机号>/<YYYYMMDD> 目录名；
//     补传/重传会把旧数据写入旧目录，按目录名判断会误删；
//   - 只删除“库中无对应记录且文件年龄超过宽限期”的文件；
//   - 跳过临时文件（上传先写 .<name>.*.part 再原子 rename），避免误删进行中的上传；
//   - 仅 SQL 后端有比对依据，memory 后端不启动本任务；
//   - 扫描限量、可取消、按批次节流，避免一次遍历打满 IO。
package retention

import (
	"context"
	"errors"
	"io/fs"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/rs/zerolog/log"

	"github.com/fakeyanss/jt808-server-go/internal/observability"
	"github.com/fakeyanss/jt808-server-go/internal/repository"
)

const (
	defaultOrphanGrace       = 7 * 24 * time.Hour
	defaultOrphanInterval    = 24 * time.Hour
	defaultOrphanMaxFiles    = 10000
	defaultOrphanBatchSize   = 500
	orphanScanPause          = 2 * time.Millisecond
	orphanScanChunk          = 200
	orphanPartFileSuffix     = ".part"
	orphanLimitReachedReason = "max files per run reached"
)

// errOrphanScanLimit 用于在达到单次扫描上限时提前结束遍历。
var errOrphanScanLimit = errors.New("orphan scan limit reached")

// OrphanConfig 是孤儿文件扫描的归一化配置。
// Grace 是文件 mtime 的最小年龄；小于该年龄的文件一律保留。
type OrphanConfig struct {
	Enabled        bool
	Grace          time.Duration
	Interval       time.Duration
	MaxFilesPerRun int
	BatchSize      int
}

func (c OrphanConfig) normalized() OrphanConfig {
	if c.Grace <= 0 {
		c.Grace = defaultOrphanGrace
	}
	if c.Interval <= 0 {
		c.Interval = defaultOrphanInterval
	}
	if c.MaxFilesPerRun <= 0 {
		c.MaxFilesPerRun = defaultOrphanMaxFiles
	}
	if c.BatchSize <= 0 {
		c.BatchSize = defaultOrphanBatchSize
	}
	return c
}

// OrphanSummary 是一次孤儿文件扫描的结果统计。
// Scanned 为遍历到的普通文件数，Skipped 为保留的文件数（临时、未过宽限期或库中仍有记录）。
type OrphanSummary struct {
	Scanned  int64
	Deleted  int64
	Skipped  int64
	Failures int
	// Limited 表示本次扫描达到 MaxFilesPerRun 上限并提前结束。
	Limited bool
}

// OrphanScanner 按周期清理媒体目录中的孤儿文件。
type OrphanScanner struct {
	store   repository.RetentionStore
	media   MediaStore
	cfg     OrphanConfig
	metrics *observability.Metrics
	now     func() time.Time
	done    chan struct{}
}

// NewOrphanScanner 创建孤儿文件扫描任务。
func NewOrphanScanner(store repository.RetentionStore, media MediaStore, cfg OrphanConfig, metrics *observability.Metrics) *OrphanScanner {
	return &OrphanScanner{
		store:   store,
		media:   media,
		cfg:     cfg.normalized(),
		metrics: metrics,
		now:     time.Now,
		done:    make(chan struct{}),
	}
}

// Done 在 Run 退出后关闭，供优雅停机等待。
func (s *OrphanScanner) Done() <-chan struct{} {
	if s == nil {
		closed := make(chan struct{})
		close(closed)
		return closed
	}
	return s.done
}

// Run 按配置周期扫描，直到 ctx 被取消。首次扫描在第一个周期到达时执行。
func (s *OrphanScanner) Run(ctx context.Context) {
	defer close(s.done)
	if s == nil || !s.cfg.Enabled || s.store == nil || s.media == nil {
		log.Warn().Msg("Orphan media scan skipped: not enabled or backend does not support it")
		return
	}
	ticker := time.NewTicker(s.cfg.Interval)
	defer ticker.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
			started := s.now()
			summary := s.ScanOnce(ctx)
			event := log.Info()
			if summary.Failures > 0 {
				event = log.Warn()
			}
			event.
				Int64("scanned", summary.Scanned).
				Int64("deleted", summary.Deleted).
				Int64("skipped", summary.Skipped).
				Int("failures", summary.Failures).
				Bool("limited", summary.Limited).
				Dur("duration", s.now().Sub(started)).
				Msg("Orphan media scan finished")
			if summary.Limited {
				log.Warn().Int("limit", s.cfg.MaxFilesPerRun).Msg(orphanLimitReachedReason)
			}
		}
	}
}

type orphanCandidate struct {
	relative string
	absolute string
}

// ScanOnce 执行一次扫描。库中仍有记录的路径按相对路径精确比对，避免把
// “同一 mediaId 换了目录”的旧文件误判为已引用。
func (s *OrphanScanner) ScanOnce(ctx context.Context) OrphanSummary {
	var summary OrphanSummary
	if s == nil || !s.cfg.Enabled || s.store == nil || s.media == nil {
		return summary
	}
	root := s.media.Root()
	if root == "" {
		s.fail(errors.New("media upload root is empty"))
		summary.Failures++
		return summary
	}
	started := s.now()
	cutoff := started.Add(-s.cfg.Grace)
	candidates := make([]orphanCandidate, 0, s.cfg.BatchSize)

	err := filepath.WalkDir(root, func(path string, entry fs.DirEntry, walkErr error) error {
		if ctx.Err() != nil {
			return ctx.Err()
		}
		if walkErr != nil {
			summary.Failures++
			s.fail(walkErr)
			if entry != nil && entry.IsDir() {
				return fs.SkipDir
			}
			return nil
		}
		if entry.IsDir() {
			// 跳过隐藏目录，避免进入临时或平台自带目录。
			if path != root && strings.HasPrefix(entry.Name(), ".") {
				return fs.SkipDir
			}
			return nil
		}
		if !entry.Type().IsRegular() {
			return nil
		}
		summary.Scanned++
		if isTemporaryMediaFile(entry.Name()) {
			summary.Skipped++
			return nil
		}
		info, infoErr := entry.Info()
		if infoErr != nil {
			summary.Failures++
			s.fail(infoErr)
			return nil
		}
		// 等于宽限期边界的文件保留，只有严格更旧的文件才可能是孤儿。
		if !info.ModTime().Before(cutoff) {
			summary.Skipped++
			return nil
		}
		relative, relErr := filepath.Rel(root, path)
		if relErr != nil {
			summary.Failures++
			s.fail(relErr)
			return nil
		}
		candidates = append(candidates, orphanCandidate{
			relative: filepath.ToSlash(relative),
			absolute: path,
		})
		if len(candidates) >= s.cfg.MaxFilesPerRun {
			summary.Limited = true
			return errOrphanScanLimit
		}
		if summary.Scanned%orphanScanChunk == 0 && !s.pause(ctx) {
			return ctx.Err()
		}
		return nil
	})
	if err != nil && !errors.Is(err, errOrphanScanLimit) && ctx.Err() == nil {
		summary.Failures++
		s.fail(err)
	}

	for start := 0; start < len(candidates); start += s.cfg.BatchSize {
		if ctx.Err() != nil {
			break
		}
		end := start + s.cfg.BatchSize
		if end > len(candidates) {
			end = len(candidates)
		}
		batch := candidates[start:end]
		paths := make([]string, 0, len(batch))
		for _, candidate := range batch {
			paths = append(paths, candidate.relative)
		}
		existing, existsErr := s.store.ExistingMediaPaths(ctx, paths)
		if existsErr != nil {
			if ctx.Err() != nil {
				break
			}
			summary.Failures++
			s.fail(existsErr)
			continue
		}
		for _, candidate := range batch {
			if _, ok := existing[candidate.relative]; ok {
				summary.Skipped++
				continue
			}
			if removeErr := os.Remove(candidate.absolute); removeErr != nil {
				if errors.Is(removeErr, fs.ErrNotExist) {
					// 扫描后被并发删除，视为已完成。
					summary.Deleted++
					continue
				}
				summary.Failures++
				s.fail(removeErr)
				continue
			}
			summary.Deleted++
		}
		if !s.pause(ctx) {
			break
		}
	}

	s.metrics.AddOrphanScan(summary.Scanned, summary.Deleted, summary.Skipped, int64(summary.Failures))
	s.metrics.RecordOrphanScan(s.now(), s.now().Sub(started))
	return summary
}

// isTemporaryMediaFile 识别上传中的临时文件与隐藏文件。
// 上传实现为 ".<目标文件名>.<随机串>.part"，rename 之前都带 .part 后缀。
func isTemporaryMediaFile(name string) bool {
	return strings.HasPrefix(name, ".") || strings.HasSuffix(name, orphanPartFileSuffix)
}

func (s *OrphanScanner) fail(err error) {
	log.Warn().Err(err).Msg("Orphan media scan failed")
}

// pause 在批次之间让出，返回 false 表示 ctx 已取消。
func (s *OrphanScanner) pause(ctx context.Context) bool {
	timer := time.NewTimer(orphanScanPause)
	defer timer.Stop()
	select {
	case <-ctx.Done():
		return false
	case <-timer.C:
		return true
	}
}
