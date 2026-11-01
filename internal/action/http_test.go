package action

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
)

func TestHTTPCallbackPostsJSONEvent(t *testing.T) {
	received := make(chan map[string]any, 1)
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		var body map[string]any
		require.NoError(t, json.NewDecoder(r.Body).Decode(&body))
		received <- body
		w.WriteHeader(http.StatusOK)
	}))
	defer server.Close()

	callback := NewHTTPCallback(server.URL, time.Second)
	callback.OnLocation(context.Background(), LocationEvent{Phone: "188765499003", AlarmSign: 1})

	select {
	case body := <-received:
		require.Equal(t, "location", body["event"])
		payload, ok := body["payload"].(map[string]any)
		require.True(t, ok)
		require.Equal(t, "188765499003", payload["phone"])
	case <-time.After(2 * time.Second):
		t.Fatal("HTTP callback was not delivered")
	}
}
