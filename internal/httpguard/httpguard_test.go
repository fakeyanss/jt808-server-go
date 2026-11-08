package httpguard

import (
	"bytes"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/gin-gonic/gin"
	"github.com/rs/zerolog"
	"github.com/rs/zerolog/log"
	"github.com/stretchr/testify/require"
)

func newRouter(middleware ...gin.HandlerFunc) *gin.Engine {
	gin.SetMode(gin.TestMode)
	router := gin.New()
	router.Use(middleware...)
	router.POST("/device/:phone/control", func(c *gin.Context) {
		c.JSON(http.StatusOK, gin.H{"status": "ok"})
	})
	router.GET("/device", func(c *gin.Context) {
		c.JSON(http.StatusOK, gin.H{"status": "ok"})
	})
	return router
}

func request(router *gin.Engine, method, path, token string) *httptest.ResponseRecorder {
	req := httptest.NewRequest(method, path, nil)
	if token != "" {
		req.Header.Set("Authorization", "Bearer "+token)
	}
	recorder := httptest.NewRecorder()
	router.ServeHTTP(recorder, req)
	return recorder
}

func TestAuth(t *testing.T) {
	router := newRouter(Auth("secret"))

	require.Equal(t, http.StatusUnauthorized, request(router, http.MethodGet, "/device", "").Code)
	require.Equal(t, http.StatusUnauthorized, request(router, http.MethodGet, "/device", "wrong").Code)
	require.Equal(t, http.StatusOK, request(router, http.MethodGet, "/device", "secret").Code)
}

func TestAuthDisabledWithoutToken(t *testing.T) {
	router := newRouter(Auth(""))
	require.Equal(t, http.StatusOK, request(router, http.MethodGet, "/device", "").Code)
}

func TestRateLimitRejectsWhenBurstExhausted(t *testing.T) {
	router := newRouter(RateLimit(1, 1))

	require.Equal(t, http.StatusOK, request(router, http.MethodGet, "/device", "").Code)
	rejected := request(router, http.MethodGet, "/device", "")
	require.Equal(t, http.StatusTooManyRequests, rejected.Code)
	require.NotEmpty(t, rejected.Header().Get("Retry-After"))
}

func TestAuditLogsMutationsAndSkipsQueries(t *testing.T) {
	var buf bytes.Buffer
	previous := log.Logger
	log.Logger = zerolog.New(&buf)
	t.Cleanup(func() { log.Logger = previous })

	router := newRouter(Audit())
	require.Equal(t, http.StatusOK, request(router, http.MethodPost, "/device/18800001111/control", "").Code)
	require.Equal(t, http.StatusOK, request(router, http.MethodGet, "/device", "").Code)

	output := buf.String()
	require.Contains(t, output, `"audit":"business_api"`)
	require.Contains(t, output, `"phone":"18800001111"`)
	require.Contains(t, output, `"method":"POST"`)
	require.NotContains(t, output, `"method":"GET"`)
}

func TestAuditLogsUnauthorizedQueries(t *testing.T) {
	var buf bytes.Buffer
	previous := log.Logger
	log.Logger = zerolog.New(&buf)
	t.Cleanup(func() { log.Logger = previous })

	router := newRouter(Audit(), Auth("secret"))
	require.Equal(t, http.StatusUnauthorized, request(router, http.MethodGet, "/device", "").Code)
	require.Contains(t, buf.String(), `"status":401`)
}
