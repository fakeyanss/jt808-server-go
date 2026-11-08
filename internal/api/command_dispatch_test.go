package api

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/require"
)

// off 模式没有 outbox 记录：受理响应只表示「已下发」，不得伪造 commandId。
func TestWriteCommandAcceptedWithoutOutboxRecord(t *testing.T) {
	gin.SetMode(gin.TestMode)
	recorder := httptest.NewRecorder()
	c, _ := gin.CreateTestContext(recorder)

	writeCommandAccepted(c, nil, false, gin.H{"messageId": "0x8105"})

	require.Equal(t, http.StatusAccepted, recorder.Code)
	var body map[string]any
	require.NoError(t, json.Unmarshal(recorder.Body.Bytes(), &body))
	require.Equal(t, "0x8105", body["messageId"])
	_, hasCommandID := body["commandId"]
	require.False(t, hasCommandID, "off 模式不应返回 commandId")
}
