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
	} {
		require.Contains(t, output, want)
	}
	require.Contains(t, output, "# TYPE jt808_ack_timeouts_total counter")
	require.Contains(t, output, "# TYPE jt808_sessions_connected gauge")
}

func TestProviderReadyReportsEachCheck(t *testing.T) {
	failing := errors.New("storage unavailable")
	provider := &Provider{Checks: []Check{
		{Name: "storage", Fn: func(context.Context) error { return failing }},
		{Name: "outbox", Fn: func(context.Context) error { return nil }},
	}}

	results := provider.Ready(context.Background())
	require.Len(t, results, 2)
	require.Equal(t, "outbox", results[0].Name)
	require.True(t, results[0].OK())
	require.Equal(t, "storage", results[1].Name)
	require.ErrorIs(t, results[1].Err, failing)
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
	repo := repository.NewCommandRepository()
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
