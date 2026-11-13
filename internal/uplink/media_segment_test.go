package uplink

import (
	"bytes"
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strconv"
	"testing"
	"time"

	"github.com/stretchr/testify/require"

	"github.com/fakeyanss/jt808-server-go/internal/repository/memory"
)

func TestMediaSegmentReportIsPersistedIdempotently(t *testing.T) {
	segments := memory.NewRecordingSegmentRepository()
	server, err := NewServer(ServerOptions{Token: testControlToken})
	require.NoError(t, err)
	server.SetRecordingSegmentRepository(segments)
	httpServer := httptest.NewServer(server.Handler())
	t.Cleanup(httpServer.Close)

	start := time.Date(2026, 11, 14, 8, 0, 0, 0, time.UTC)
	payload := map[string]any{
		"phone":     "13800138000",
		"channel":   1,
		"kind":      "live",
		"objectKey": "media/segments/13800138000/ch1/live/20261114/1_600000_2_0.ts",
		"startTime": start,
		"endTime":   start.Add(10 * time.Minute),
		"size":      2048,
		"state":     "committed",
	}
	for i := 0; i < 2; i++ {
		body, err := json.Marshal(payload)
		require.NoError(t, err)
		request, err := http.NewRequest(http.MethodPost, httpServer.URL+PathMediaSegment, bytes.NewReader(body))
		require.NoError(t, err)
		request.Header.Set("Authorization", "Bearer "+testControlToken)
		response, err := http.DefaultClient.Do(request)
		require.NoError(t, err)
		require.Equal(t, http.StatusAccepted, response.StatusCode)
		_ = response.Body.Close()
	}

	row, err := segments.Get(context.Background(), "13800138000", "13800138000_1_"+strconv.FormatInt(start.UnixMilli(), 10))
	require.NoError(t, err)
	require.Equal(t, 10*time.Minute, row.Duration())
	require.EqualValues(t, 2048, row.Size)
	count, _, err := segments.Stats(context.Background())
	require.NoError(t, err)
	require.EqualValues(t, 1, count)
}

func TestMediaSegmentReportRequiresToken(t *testing.T) {
	server, err := NewServer(ServerOptions{Token: testControlToken})
	require.NoError(t, err)
	server.SetRecordingSegmentRepository(memory.NewRecordingSegmentRepository())
	httpServer := httptest.NewServer(server.Handler())
	t.Cleanup(httpServer.Close)

	response, err := http.Post(httpServer.URL+PathMediaSegment, "application/json", bytes.NewReader([]byte(`{}`)))
	require.NoError(t, err)
	defer response.Body.Close()
	require.Equal(t, http.StatusUnauthorized, response.StatusCode)
}
