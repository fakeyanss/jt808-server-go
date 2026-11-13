package media

import (
	"context"
	"io"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/fakeyanss/jt808-server-go/internal/repository/memory"
)

// 切片索引持久化后，重启的媒体节点仍能按时间范围查到并读取切片。
func TestRecordingCoverageUsesPersistedIndexAcrossRestart(t *testing.T) {
	dir := t.TempDir()
	segments := memory.NewRecordingSegmentRepository()
	base := time.Date(2026, 11, 14, 9, 0, 0, 0, time.UTC)

	segmentPath := filepath.Join(dir, "clip.ts")
	requireNoError(t, os.WriteFile(segmentPath, []byte("0123456789"), 0o640))

	first := NewServer(Config{Dir: dir, RecordMode: RecordModeOff, SegmentRepository: segments})
	requireNoError(t, first.onSegment(context.Background(), SegmentInfo{
		Phone: "13800138000", Channel: 1, Kind: StreamKindLive,
		LocalPath: segmentPath, StartTime: base, EndTime: base.Add(10 * time.Minute),
		Size: 10, State: "local",
	}))
	first.Stop()

	second := NewServer(Config{Dir: dir, RecordMode: RecordModeOff, SegmentRepository: segments})
	t.Cleanup(second.Stop)

	coverage, err := second.RecordingCoverage(context.Background(), "13800138000", 1,
		base, base.Add(10*time.Minute))
	requireNoError(t, err)
	if !coverage.Playable || len(coverage.Segments) != 1 {
		t.Fatalf("coverage should come from persisted index, got %+v", coverage)
	}
	if coverage.Segments[0].ID != segmentID(SegmentInfo{Phone: "13800138000", Channel: 1, StartTime: base}) {
		t.Fatalf("unexpected segment id %q", coverage.Segments[0].ID)
	}

	reader, size, err := second.OpenRecordingSegment(context.Background(),
		"13800138000", coverage.Segments[0].ID, 2, 4)
	requireNoError(t, err)
	defer reader.Close()
	if size != 10 {
		t.Fatalf("want size 10, got %d", size)
	}
	payload, err := io.ReadAll(reader)
	requireNoError(t, err)
	if string(payload) != "2345" {
		t.Fatalf("unexpected range payload %q", payload)
	}
}

func requireNoError(t *testing.T, err error) {
	t.Helper()
	if err != nil {
		t.Fatal(err)
	}
}
