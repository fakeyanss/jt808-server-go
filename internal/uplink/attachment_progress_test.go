package uplink

import (
	"bytes"
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"sync"
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/fakeyanss/jt808-server-go/internal/contracts"
)

type fakeAttachmentProgressStore struct {
	mu    sync.Mutex
	items map[contracts.AttachmentProgressKey]contracts.AttachmentProgress
}

func (s *fakeAttachmentProgressStore) Save(_ context.Context, progress contracts.AttachmentProgress) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.items[progress.Key()] = progress
	return nil
}

func (s *fakeAttachmentProgressStore) Load(_ context.Context, key contracts.AttachmentProgressKey) (*contracts.AttachmentProgress, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	progress, ok := s.items[key]
	if !ok {
		return nil, contracts.ErrAttachmentProgressNotFound
	}
	return &progress, nil
}

func TestAttachmentProgressReportAndLoad(t *testing.T) {
	store := &fakeAttachmentProgressStore{items: map[contracts.AttachmentProgressKey]contracts.AttachmentProgress{}}
	server, err := NewServer(ServerOptions{Token: testControlToken})
	require.NoError(t, err)
	server.SetAttachmentProgressStore(store)
	httpServer := httptest.NewServer(server.Handler())
	t.Cleanup(httpServer.Close)

	progress := contracts.AttachmentProgress{
		Source:    "subia",
		Phone:     "13800138000",
		AlarmNo:   "A1",
		FileName:  "clip.bin",
		TotalSize: 8,
		Watermark: 4,
		Status:    contracts.AttachmentStatusReceiving,
		Segments: []contracts.AttachmentProgressSegment{
			{Index: 0, Key: "k0", Offset: 0, Size: 4, State: "pending"},
		},
	}
	for i := 0; i < 2; i++ {
		body, marshalErr := json.Marshal(progress)
		require.NoError(t, marshalErr)
		request, requestErr := http.NewRequest(http.MethodPost, httpServer.URL+PathAttachmentProgress, bytes.NewReader(body))
		require.NoError(t, requestErr)
		request.Header.Set("Authorization", "Bearer "+testControlToken)
		response, doErr := http.DefaultClient.Do(request)
		require.NoError(t, doErr)
		require.Equal(t, http.StatusAccepted, response.StatusCode)
		_ = response.Body.Close()
	}

	query := httpServer.URL + PathAttachmentProgress +
		"?source=subia&phone=13800138000&alarmNo=A1&fileName=clip.bin"
	request, err := http.NewRequest(http.MethodGet, query, nil)
	require.NoError(t, err)
	request.Header.Set("Authorization", "Bearer "+testControlToken)
	response, err := http.DefaultClient.Do(request)
	require.NoError(t, err)
	require.Equal(t, http.StatusOK, response.StatusCode)
	var loaded contracts.AttachmentProgress
	require.NoError(t, json.NewDecoder(response.Body).Decode(&loaded))
	_ = response.Body.Close()
	require.EqualValues(t, 4, loaded.Watermark)

	missing := httpServer.URL + PathAttachmentProgress +
		"?source=subia&phone=13800138000&alarmNo=A1&fileName=missing.bin"
	request, err = http.NewRequest(http.MethodGet, missing, nil)
	require.NoError(t, err)
	request.Header.Set("Authorization", "Bearer "+testControlToken)
	response, err = http.DefaultClient.Do(request)
	require.NoError(t, err)
	require.Equal(t, http.StatusNotFound, response.StatusCode)
	_ = response.Body.Close()
}

func TestAttachmentProgressRequiresToken(t *testing.T) {
	server, err := NewServer(ServerOptions{Token: testControlToken})
	require.NoError(t, err)
	server.SetAttachmentProgressStore(&fakeAttachmentProgressStore{items: map[contracts.AttachmentProgressKey]contracts.AttachmentProgress{}})
	httpServer := httptest.NewServer(server.Handler())
	t.Cleanup(httpServer.Close)

	response, err := http.Post(httpServer.URL+PathAttachmentProgress, "application/json", bytes.NewReader([]byte(`{}`)))
	require.NoError(t, err)
	defer response.Body.Close()
	require.Equal(t, http.StatusUnauthorized, response.StatusCode)
}
