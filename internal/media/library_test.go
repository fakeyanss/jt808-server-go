package media

import (
	"context"
	"errors"
	"io"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/fakeyanss/jt808-server-go/internal/contracts"
)

func TestRecordingCoverageMergesAndReportsMissing(t *testing.T) {
	server := NewServer(Config{Dir: t.TempDir(), RecordMode: RecordModeOff})
	t.Cleanup(server.Stop)

	base := time.Date(2026, 11, 11, 10, 0, 0, 0, time.UTC)
	add := func(offset, length time.Duration) {
		start := base.Add(offset)
		server.segments.add(SegmentInfo{
			Phone:     "13800138000",
			Channel:   1,
			StartTime: start,
			EndTime:   start.Add(length),
			Size:      int64(length / time.Second),
		})
	}
	add(0, 10*time.Minute)
	add(10*time.Minute, 10*time.Minute)
	add(40*time.Minute, 10*time.Minute)

	coverage, err := server.RecordingCoverage(context.Background(), "13800138000", 1,
		base, base.Add(time.Hour))
	if err != nil {
		t.Fatalf("coverage: %v", err)
	}
	if coverage.Playable {
		t.Fatal("coverage with holes must not be playable")
	}
	if len(coverage.Available) != 2 {
		t.Fatalf("available = %d ranges, want 2: %+v", len(coverage.Available), coverage.Available)
	}
	if !coverage.Available[0].End.Equal(base.Add(20 * time.Minute)) {
		t.Fatalf("adjacent segments were not merged: %+v", coverage.Available)
	}
	if len(coverage.Missing) != 2 {
		t.Fatalf("missing = %d ranges, want 2: %+v", len(coverage.Missing), coverage.Missing)
	}
	if !coverage.Missing[0].Start.Equal(base.Add(20*time.Minute)) ||
		!coverage.Missing[0].End.Equal(base.Add(40*time.Minute)) {
		t.Fatalf("first missing range = %+v", coverage.Missing[0])
	}
	if len(coverage.Segments) != 3 {
		t.Fatalf("segments = %d, want 3", len(coverage.Segments))
	}

	if _, err := server.RecordingCoverage(context.Background(), "13800138000", 1,
		base, base); err == nil {
		t.Fatal("inverted range must fail")
	}

	full, err := server.RecordingCoverage(context.Background(), "13800138000", 1,
		base, base.Add(20*time.Minute))
	if err != nil {
		t.Fatalf("full coverage: %v", err)
	}
	if !full.Playable || len(full.Missing) != 0 {
		t.Fatalf("full coverage = %+v, want playable", full)
	}
}

func TestOpenRecordingSegmentReadsRangeFromLocalFile(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "seg.ts")
	payload := make([]byte, 100)
	for i := range payload {
		payload[i] = byte(i)
	}
	if err := os.WriteFile(path, payload, 0o600); err != nil {
		t.Fatalf("write fixture: %v", err)
	}

	server := NewServer(Config{Dir: dir, RecordMode: RecordModeOff})
	t.Cleanup(server.Stop)
	info := SegmentInfo{
		Phone:     "13800138000",
		Channel:   1,
		StartTime: time.UnixMilli(1_700_000_000_000),
		EndTime:   time.UnixMilli(1_700_000_010_000),
		LocalPath: path,
		Size:      int64(len(payload)),
	}
	server.segments.add(info)

	reader, size, err := server.OpenRecordingSegment(context.Background(), info.Phone, segmentID(info), 10, 20)
	if err != nil {
		t.Fatalf("open range: %v", err)
	}
	defer reader.Close()
	if size != int64(len(payload)) {
		t.Fatalf("size = %d, want %d", size, len(payload))
	}
	data, err := io.ReadAll(reader)
	if err != nil {
		t.Fatalf("read: %v", err)
	}
	if len(data) != 20 || data[0] != 10 || data[19] != 29 {
		t.Fatalf("range payload = %d bytes starting at %d", len(data), data[0])
	}

	// 超出末尾的 offset 直接报错；请求长度越界则截到文件尾。
	if _, _, err := server.OpenRecordingSegment(context.Background(), info.Phone, segmentID(info), 101, 10); !errors.Is(err, contracts.ErrRangeNotSatisfiable) {
		t.Fatalf("offset past end error = %v", err)
	}
	reader, _, err = server.OpenRecordingSegment(context.Background(), info.Phone, segmentID(info), 90, 100)
	if err != nil {
		t.Fatalf("open clamped range: %v", err)
	}
	defer reader.Close()
	if data, _ = io.ReadAll(reader); len(data) != 10 {
		t.Fatalf("clamped range = %d bytes, want 10", len(data))
	}

	if _, _, err := server.OpenRecordingSegment(context.Background(), info.Phone, "missing", 0, 1); !errors.Is(err, contracts.ErrRecordingNotFound) {
		t.Fatalf("missing segment error = %v", err)
	}
}
