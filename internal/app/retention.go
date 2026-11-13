package app

import (
	"context"

	"github.com/rs/zerolog/log"

	"github.com/fakeyanss/jt808-server-go/internal/coordination"
	"github.com/fakeyanss/jt808-server-go/internal/retention"
	"github.com/fakeyanss/jt808-server-go/pkg/routines"
)

// startRetention 启动历史数据清理与孤儿文件扫描。
func (d *deps) startRetention() {
	cfg := d.cfg
	hasControl := d.hasControl
	storeSet := d.storeSet
	mediaUploadStore := d.mediaUploadStore
	metrics := d.metrics

	retentionPolicy := cfg.RetentionPolicy()
	leaseRunner := coordination.NewRunner(storeSet.Coordination, coordination.DefaultTTL,
		d.clock, d.settings.options.LeaseTolerance)
	cancelRetention := func() {}
	var retentionDones []<-chan struct{}
	var retentionCtx context.Context
	if retentionPolicy.Cleanup.Enabled || retentionPolicy.OrphanFiles.Enabled {
		ctx, cancel := context.WithCancel(context.Background())
		retentionCtx = ctx
		cancelRetention = cancel
	}
	if hasControl && retentionPolicy.Cleanup.Enabled {
		if storeSet.Retention == nil {
			log.Warn().Str("backend", storeSet.Backend).
				Msg("Retention cleanup is enabled but the database backend does not support it; skipped")
		} else {
			cleaner := retention.NewCleaner(storeSet.Retention, mediaUploadStore, retention.Config{
				Interval:           retentionPolicy.Cleanup.Interval,
				BatchSize:          retentionPolicy.Cleanup.BatchSize,
				Locations:          retentionPolicy.Locations.MaxAge,
				Alarms:             retentionPolicy.Alarms.MaxAge,
				MediaFiles:         retentionPolicy.MediaFiles.MaxAge,
				RecordingSegments:  retentionPolicy.MediaSegments.MaxAge,
				MediaCaptures:      retentionPolicy.MediaCaptures.MaxAge,
				MediaSearchResults: retentionPolicy.MediaSearchResults.MaxAge,
			}, metrics)
			cleaner.SetSegmentStore(storeSet.RecordingSegments, d.controlBlob)
			cleaner.SetLease(leaseRunner)
			retentionDones = append(retentionDones, cleaner.Done())
			routines.GoSafe(func() { cleaner.Run(retentionCtx) })
			log.Info().
				Dur("interval", retentionPolicy.Cleanup.Interval).
				Int("batch_size", retentionPolicy.Cleanup.BatchSize).
				Dur("locations", retentionPolicy.Locations.MaxAge).
				Dur("alarms", retentionPolicy.Alarms.MaxAge).
				Dur("media_files", retentionPolicy.MediaFiles.MaxAge).
				Dur("media_segments", retentionPolicy.MediaSegments.MaxAge).
				Dur("media_captures", retentionPolicy.MediaCaptures.MaxAge).
				Dur("media_search_results", retentionPolicy.MediaSearchResults.MaxAge).
				Msg("Retention cleanup enabled")
		}
	}
	if hasControl && retentionPolicy.OrphanFiles.Enabled {
		if storeSet.Retention == nil || mediaUploadStore == nil {
			log.Warn().Str("backend", storeSet.Backend).
				Msg("Orphan media scan is enabled but the database backend does not support it; skipped")
		} else {
			scanner := retention.NewOrphanScanner(storeSet.Retention, mediaUploadStore, retention.OrphanConfig{
				Enabled:        true,
				Grace:          retentionPolicy.OrphanFiles.Grace,
				Interval:       retentionPolicy.OrphanFiles.Interval,
				MaxFilesPerRun: retentionPolicy.OrphanFiles.MaxFilesPerRun,
				BatchSize:      retentionPolicy.OrphanFiles.BatchSize,
			}, metrics)
			scanner.SetLease(leaseRunner)
			retentionDones = append(retentionDones, scanner.Done())
			routines.GoSafe(func() { scanner.Run(retentionCtx) })
			log.Info().
				Dur("interval", retentionPolicy.OrphanFiles.Interval).
				Dur("grace", retentionPolicy.OrphanFiles.Grace).
				Int("max_files_per_run", retentionPolicy.OrphanFiles.MaxFilesPerRun).
				Int("batch_size", retentionPolicy.OrphanFiles.BatchSize).
				Msg("Orphan media scan enabled")
		}
	}

	d.retentionPolicy = retentionPolicy
	d.cancelRetention = cancelRetention
	d.retentionDones = retentionDones
	d.retentionCtx = retentionCtx
}
