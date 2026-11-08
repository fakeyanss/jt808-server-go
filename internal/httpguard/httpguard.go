// Package httpguard 提供业务 API 的鉴权、限流与审计中间件。
//
// 与管理面中间件的差异：
//   - 业务 API 只有单一对外 token，不区分凭据维度；
//   - 审计只记录变更类请求与鉴权失败，避免 GET 查询刷日志；
//   - 不依赖管理面内部实现，保持两个服务可独立启停。
package httpguard

import (
	"crypto/subtle"
	"fmt"
	"math"
	"net/http"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/rs/zerolog/log"
)

const bearerScheme = "Bearer "

// Auth 校验 Authorization: Bearer <token>；token 为空表示鉴权关闭。
func Auth(token string) gin.HandlerFunc {
	token = strings.TrimSpace(token)
	if token == "" {
		return func(c *gin.Context) { c.Next() }
	}
	return func(c *gin.Context) {
		header := strings.TrimSpace(c.GetHeader("Authorization"))
		if len(header) < len(bearerScheme) || !strings.EqualFold(header[:len(bearerScheme)], bearerScheme) {
			c.AbortWithStatusJSON(http.StatusUnauthorized, gin.H{"err": "missing or malformed bearer token"})
			return
		}
		presented := strings.TrimSpace(header[len(bearerScheme):])
		if subtle.ConstantTimeCompare([]byte(presented), []byte(token)) != 1 {
			c.AbortWithStatusJSON(http.StatusUnauthorized, gin.H{"err": "invalid bearer token"})
			return
		}
		c.Next()
	}
}

// tokenBucket 是单个来源 IP 的令牌桶。
type tokenBucket struct {
	tokens float64
	last   time.Time
}

// limiter 是按来源 IP 隔离的令牌桶集合。
// 空闲桶在请求路径上按固定间隔清理，不启动后台 goroutine。
type limiter struct {
	mutex       sync.Mutex
	buckets     map[string]*tokenBucket
	rate        float64
	burst       float64
	lastCleanup time.Time
}

const (
	limiterCleanupInterval = time.Minute
	limiterIdleTTL         = 10 * time.Minute
)

// RateLimit 按来源 IP 限流；ratePerSecond 或 burst 非正时直接放行。
func RateLimit(ratePerSecond float64, burst int) gin.HandlerFunc {
	if ratePerSecond <= 0 || burst <= 0 {
		return func(c *gin.Context) { c.Next() }
	}
	l := &limiter{
		buckets: make(map[string]*tokenBucket),
		rate:    ratePerSecond,
		burst:   float64(burst),
	}
	return func(c *gin.Context) {
		allowed, wait := l.allow("source:"+c.ClientIP(), time.Now())
		if !allowed {
			retryAfter := int(math.Ceil(wait.Seconds()))
			if retryAfter < 1 {
				retryAfter = 1
			}
			c.Header("Retry-After", strconv.Itoa(retryAfter))
			c.AbortWithStatusJSON(http.StatusTooManyRequests, gin.H{
				"err": fmt.Sprintf("rate limit exceeded, retry after %d seconds", retryAfter),
			})
			return
		}
		c.Next()
	}
}

func (l *limiter) allow(key string, now time.Time) (bool, time.Duration) {
	l.mutex.Lock()
	defer l.mutex.Unlock()

	l.cleanupIdle(now)

	bucket, ok := l.buckets[key]
	if !ok {
		bucket = &tokenBucket{tokens: l.burst, last: now}
		l.buckets[key] = bucket
	}
	if elapsed := now.Sub(bucket.last); elapsed > 0 {
		bucket.tokens = math.Min(l.burst, bucket.tokens+elapsed.Seconds()*l.rate)
		bucket.last = now
	}
	if bucket.tokens >= 1 {
		bucket.tokens--
		return true, 0
	}
	wait := time.Duration((1 - bucket.tokens) / l.rate * float64(time.Second))
	if wait <= 0 {
		wait = time.Millisecond
	}
	return false, wait
}

func (l *limiter) cleanupIdle(now time.Time) {
	if now.Sub(l.lastCleanup) < limiterCleanupInterval {
		return
	}
	l.lastCleanup = now
	for key, bucket := range l.buckets {
		if now.Sub(bucket.last) >= limiterIdleTTL && bucket.tokens >= l.burst {
			delete(l.buckets, key)
		}
	}
}

// Audit 记录变更类请求（POST/PUT/PATCH/DELETE）与鉴权失败，作为下发与配置变更的操作审计。
// 只记录请求元数据，不记录请求体与 token。
func Audit() gin.HandlerFunc {
	return func(c *gin.Context) {
		start := time.Now()
		c.Next()

		status := c.Writer.Status()
		mutating := c.Request.Method != http.MethodGet && c.Request.Method != http.MethodHead && c.Request.Method != http.MethodOptions
		if !mutating && status != http.StatusUnauthorized && status != http.StatusTooManyRequests {
			return
		}
		log.Info().
			Str("audit", "business_api").
			Str("method", c.Request.Method).
			Str("path", c.FullPath()).
			Str("client_ip", c.ClientIP()).
			Int("status", status).
			Str("phone", c.Param("phone")).
			Dur("latency", time.Since(start)).
			Msg("Business API request")
	}
}
