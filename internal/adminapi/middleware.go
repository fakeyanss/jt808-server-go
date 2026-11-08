package adminapi

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

	"github.com/fakeyanss/jt808-server-go/internal/config"
)

// credentialContextKey 保存已通过鉴权的凭据，供按凭据维度的限流复用。
const credentialContextKey = "admin-credential"

// recoveryMiddleware 捕获 panic 并输出统一 JSON 错误，避免默认的纯文本 500。
func recoveryMiddleware() gin.HandlerFunc {
	return gin.CustomRecovery(func(c *gin.Context, recovered any) {
		log.Error().Interface("panic", recovered).Str("path", c.FullPath()).Msg("Recovered from admin API panic")
		writeJSONError(c, http.StatusInternalServerError, codeInternal, "internal server error")
	})
}

// authMiddleware 校验 Authorization: Bearer <token>。
// 缺少凭据、方案不匹配与 token 错误统一返回 401，避免用状态码泄露 token 是否正确。
func authMiddleware(token string) gin.HandlerFunc {
	const scheme = "Bearer "
	return func(c *gin.Context) {
		header := strings.TrimSpace(c.GetHeader("Authorization"))
		if len(header) < len(scheme) || !strings.EqualFold(header[:len(scheme)], scheme) {
			writeJSONError(c, http.StatusUnauthorized, codeUnauthorized, "missing or malformed bearer token")
			return
		}
		presented := strings.TrimSpace(header[len(scheme):])
		if subtle.ConstantTimeCompare([]byte(presented), []byte(token)) != 1 {
			writeJSONError(c, http.StatusUnauthorized, codeUnauthorized, "invalid bearer token")
			return
		}
		c.Set(credentialContextKey, token)
		c.Next()
	}
}

// tokenBucket 是单个限流维度的令牌桶。
type tokenBucket struct {
	tokens float64
	last   time.Time
}

// rateLimiter 是按维度隔离的内存令牌桶集合。
// 不引入额外依赖，也不启动后台 goroutine；空闲桶在请求路径上按固定间隔清理。
type rateLimiter struct {
	mutex       sync.Mutex
	buckets     map[string]*tokenBucket
	rate        float64
	burst       float64
	lastCleanup time.Time
}

const rateLimiterCleanupInterval = time.Minute
const rateLimiterIdleTTL = 10 * time.Minute

func newRateLimiter(ratePerSecond float64, burst int) *rateLimiter {
	return &rateLimiter{
		buckets: make(map[string]*tokenBucket),
		rate:    ratePerSecond,
		burst:   float64(burst),
	}
}

// allow 消费一个令牌，返回是否放行与建议的重试等待时长。
func (l *rateLimiter) allow(key string, now time.Time) (bool, time.Duration) {
	l.mutex.Lock()
	defer l.mutex.Unlock()

	l.cleanupIdle(now)

	bucket, ok := l.buckets[key]
	if !ok {
		bucket = &tokenBucket{tokens: l.burst, last: now}
		l.buckets[key] = bucket
	}
	elapsed := now.Sub(bucket.last)
	if elapsed > 0 {
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

// cleanupIdle 删除长时间空闲且已回满的桶，避免来源维度无限增长。
func (l *rateLimiter) cleanupIdle(now time.Time) {
	if now.Sub(l.lastCleanup) < rateLimiterCleanupInterval {
		return
	}
	l.lastCleanup = now
	for key, bucket := range l.buckets {
		if now.Sub(bucket.last) >= rateLimiterIdleTTL && bucket.tokens >= l.burst {
			delete(l.buckets, key)
		}
	}
}

// rateLimitMiddleware 按配置维度对已鉴权请求限流。
// keyBy=source 按来源 IP 隔离，keyBy=credential 按凭据隔离（当前单 token 等价于全局）。
func rateLimitMiddleware(limiter *rateLimiter, keyBy string) gin.HandlerFunc {
	return func(c *gin.Context) {
		key := c.ClientIP()
		if keyBy == config.AdminRateKeyByCredential {
			if credential, ok := c.Get(credentialContextKey); ok {
				if value, ok := credential.(string); ok {
					key = "credential:" + value
				}
			}
		} else {
			key = "source:" + key
		}
		allowed, wait := limiter.allow(key, time.Now())
		if !allowed {
			retryAfter := int(math.Ceil(wait.Seconds()))
			if retryAfter < 1 {
				retryAfter = 1
			}
			c.Header("Retry-After", strconv.Itoa(retryAfter))
			writeJSONError(c, http.StatusTooManyRequests, codeRateLimited,
				fmt.Sprintf("rate limit exceeded, retry after %d seconds", retryAfter))
			return
		}
		c.Next()
	}
}
