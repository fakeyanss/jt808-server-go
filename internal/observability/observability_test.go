package observability

import (
	"bytes"
	"context"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/stretchr/testify/require"

	"github.com/fakeyanss/jt808-server-go/internal/repository"
	"github.com/fakeyanss/jt808-server-go/internal/repository/memory"
)

func TestWritePrometheusExposesRequiredMetrics(t *testing.T) {
	var buf bytes.Buffer
	snapshot := Snapshot{
		ConnectedSessions:    2,
		AckPending:           3,
		AckTimeouts:          4,
		OutboxPending:        5,
		OutboxExpired:        6,
		MediaStreams:         7,
		AttachmentsActive:    8,
		AttachmentsCompleted: 9,
		MediaFiles:           10,
		MediaFileBytes:       11,
		Uplink: UplinkSnapshot{
			Delivered: 12, Failed: 13, Timeouts: 14, Dropped: 15,
			Rejected: 16, InFlight: 17, ShardQueues: []int{7, 11},
		},
	}
	require.NoError(t, WritePrometheus(&buf, snapshot))

	output := buf.String()
	for _, want := range []string{
		"jt808_sessions_connected 2",
		"jt808_ack_pending 3",
		"jt808_ack_timeouts_total 4",
		"jt808_outbox_pending 5",
		"jt808_outbox_expired 6",
		"jt808_media_streams 7",
		"jt808_attachments_active 8",
		"jt808_attachments_completed_total 9",
		"jt808_media_files 10",
		"jt808_media_file_bytes 11",
		"jt808_uplink_delivered_total 12",
		"jt808_uplink_failed_total 13",
		"jt808_uplink_timeouts_total 14",
		"jt808_uplink_dropped_total 15",
		"jt808_uplink_rejected_total 16",
		"jt808_uplink_in_flight 17",
		"jt808_uplink_queue_depth{shard=\"0\"} 7",
		"jt808_uplink_queue_depth{shard=\"1\"} 11",
	} {
		require.Contains(t, output, want)
	}
	require.Contains(t, output, "# TYPE jt808_ack_timeouts_total counter")
	require.Contains(t, output, "# TYPE jt808_sessions_connected gauge")
}

func TestRetentionMetricsExposed(t *testing.T) {
	metrics := NewMetrics()
	metrics.AddRetentionDeleted(RetentionCategoryLocations, 3)
	metrics.AddRetentionDeleted(RetentionCategoryMediaFiles, 2)
	metrics.IncRetentionFailure(RetentionCategoryAlarms)
	metrics.RecordRetentionRun(time.UnixMilli(1_700_000_000_000), 1500*time.Millisecond)

	snapshot := metrics.RetentionSnapshot()
	require.EqualValues(t, 3, snapshot.Deleted[RetentionCategoryLocations])
	require.EqualValues(t, 2, snapshot.Deleted[RetentionCategoryMediaFiles])
	require.EqualValues(t, 1, snapshot.Failures[RetentionCategoryAlarms])
	require.Equal(t, int64(1_700_000_000_000), snapshot.LastRunUnixMilli)
	require.Equal(t, 1500*time.Millisecond, snapshot.LastRunDuration)

	var buf bytes.Buffer
	require.NoError(t, WritePrometheus(&buf, Snapshot{Retention: snapshot}))
	output := buf.String()
	for _, want := range []string{
		`jt808_retention_deleted_total{category="locations"} 3`,
		`jt808_retention_deleted_total{category="mediaFiles"} 2`,
		`jt808_retention_deleted_total{category="alarms"} 0`,
		`jt808_retention_failures_total{category="alarms"} 1`,
		"jt808_retention_last_run_timestamp_seconds 1700000000",
		"jt808_retention_last_run_duration_seconds 1.5",
	} {
		require.Contains(t, output, want)
	}
}

func TestOrphanMetricsExposed(t *testing.T) {
	metrics := NewMetrics()
	metrics.AddOrphanScan(10, 3, 7, 1)
	metrics.RecordOrphanScan(time.UnixMilli(1_700_000_000_000), 250*time.Millisecond)

	snapshot := metrics.OrphanSnapshot()
	require.EqualValues(t, 10, snapshot.Scanned)
	require.EqualValues(t, 3, snapshot.Deleted)
	require.EqualValues(t, 7, snapshot.Skipped)
	require.EqualValues(t, 1, snapshot.Failures)
	require.Equal(t, int64(1_700_000_000_000), snapshot.LastRunUnixMilli)
	require.Equal(t, 250*time.Millisecond, snapshot.LastRunDuration)

	var buf bytes.Buffer
	require.NoError(t, WritePrometheus(&buf, Snapshot{Orphan: snapshot}))
	output := buf.String()
	for _, want := range []string{
		"jt808_orphan_files_scanned_total 10",
		"jt808_orphan_files_deleted_total 3",
		"jt808_orphan_files_skipped_total 7",
		"jt808_orphan_scan_failures_total 1",
		"jt808_orphan_scan_last_run_timestamp_seconds 1700000000",
		"jt808_orphan_scan_last_run_duration_seconds 0.25",
	} {
		require.Contains(t, output, want)
	}
}

func TestProviderReadyReportsEachCheck(t *testing.T) {
	failing := errors.New("database unavailable")
	provider := &Provider{Checks: []Check{
		{Name: "database", Fn: func(context.Context) error { return failing }},
		{Name: "outbox", Fn: func(context.Context) error { return nil }},
	}}

	results := provider.Ready(context.Background())
	require.Len(t, results, 2)
	require.Equal(t, "database", results[0].Name)
	require.ErrorIs(t, results[0].Err, failing)
	require.Equal(t, "outbox", results[1].Name)
	require.True(t, results[1].OK())
}

// 降级检查不算失败：OK 仍为 true，但必须能被调用方识别为 degraded。
func TestProviderReadyReportsDegraded(t *testing.T) {
	provider := &Provider{Checks: []Check{
		{Name: "embedded_database", Fn: func(context.Context) error {
			return Degraded("cluster is running on embedded sqlite")
		}},
	}}

	results := provider.Ready(context.Background())
	require.Len(t, results, 1)
	require.True(t, results[0].OK())
	require.True(t, results[0].IsDegraded())
	require.ErrorContains(t, results[0].Err, "embedded sqlite")
}

func TestProviderSnapshotAddsCounters(t *testing.T) {
	metrics := NewMetrics()
	metrics.IncAckTimeout()
	metrics.IncAckTimeout()
	provider := &Provider{
		Metrics: metrics,
		Collect: func(context.Context) Snapshot { return Snapshot{MediaStreams: 1} },
	}

	snapshot := provider.Snapshot(context.Background())
	require.Equal(t, 1, snapshot.MediaStreams)
	require.Equal(t, int64(2), snapshot.AckTimeouts)
}

func TestCheckOutboxBacklog(t *testing.T) {
	now := time.Now()
	repo := memory.NewCommandRepository()
	commands := []*repository.DownlinkCommand{
		{CommandID: "expired-1", IdempotencyKey: "idem-1", Phone: "1001", MsgID: 0x8105, Serial: 1, Status: repository.CommandStatusPending, ExpiresAt: now.Add(-time.Minute)},
		{CommandID: "expired-2", IdempotencyKey: "idem-2", Phone: "1002", MsgID: 0x8105, Serial: 2, Status: repository.CommandStatusPending, ExpiresAt: now.Add(-time.Second)},
		{CommandID: "live", IdempotencyKey: "idem-3", Phone: "1003", MsgID: 0x8105, Serial: 3, Status: repository.CommandStatusPending, ExpiresAt: now.Add(time.Minute)},
	}
	for _, command := range commands {
		_, _, err := repo.Enqueue(context.Background(), command)
		require.NoError(t, err)
	}

	require.Error(t, CheckOutboxBacklog(context.Background(), repo, 2))
	require.NoError(t, CheckOutboxBacklog(context.Background(), repo, 3))

	pending, expired, err := OutboxStats(context.Background(), repo)
	require.NoError(t, err)
	require.Equal(t, 3, pending)
	require.Equal(t, 2, expired)
	require.NoError(t, CheckOutboxBacklog(context.Background(), nil, 1))
}

func TestCheckWritableDir(t *testing.T) {
	dir := t.TempDir()
	require.NoError(t, CheckWritableDir(dir))

	missing := filepath.Join(dir, "missing")
	require.Error(t, CheckWritableDir(missing))

	file := filepath.Join(dir, "file")
	require.NoError(t, os.WriteFile(file, []byte("x"), 0o644))
	require.Error(t, CheckWritableDir(file))

	entries, err := os.ReadDir(dir)
	require.NoError(t, err)
	for _, entry := range entries {
		require.False(t, strings.HasPrefix(entry.Name(), ".jt808-ready-"), "probe file must be removed")
	}
}
