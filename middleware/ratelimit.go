package middleware

import (
	"net/http"
	"sync"
	"time"

	"github.com/gin-gonic/gin"
)

// rlBucket 令牌桶
type rlBucket struct {
	tokens   float64
	lastSeen time.Time
}

var (
	rlMu sync.Mutex
	// rlBuckets 按用户ID限流（/v1 认证端点），ipBuckets 按来源 IP 限流
	//（公开端点）。两者共用一把锁与同一套令牌桶算法。
	rlBuckets   = make(map[int]*rlBucket)
	ipBuckets   = make(map[string]*rlBucket)
	rlLastSweep time.Time
	ipLastSweep time.Time
)

// maxRateLimitBuckets 同时跟踪的用户数上限，防止内存无界增长
const maxRateLimitBuckets = 100000

// rateLimitSweepInterval 过期桶清理间隔
const rateLimitSweepInterval = 10 * time.Minute

// RateLimit 按用户维度限流（需在 APIAuth 之后使用，依赖上下文中已设置 user_id）。
// 用于 /v1 业务端点：这些端点每次调用都会真实消耗上游额度，
// 没有限流时单个 Key 即可把上游配额或本地资源打满。
// perMinute 为稳态速率，burst 为允许的瞬时突发量。
func RateLimit(perMinute int, burst int) gin.HandlerFunc {
	if perMinute <= 0 {
		perMinute = 60
	}
	if burst <= 0 {
		burst = 10
	}
	ratePerSecond := float64(perMinute) / 60.0

	return func(c *gin.Context) {
		userID := c.GetInt("user_id")
		if userID <= 0 {
			// 未通过 APIAuth 的场景不限流，交由认证中间件处理
			c.Next()
			return
		}

		if !allowRequest(userID, ratePerSecond, float64(burst)) {
			c.Header("Retry-After", "1")
			c.JSON(http.StatusTooManyRequests, gin.H{
				"error": gin.H{
					"message": "请求过于频繁，请稍后再试",
					"type":    "rate_limit_error",
				},
			})
			c.Abort()
			return
		}

		c.Next()
	}
}

// IPRateLimit 按来源 IP 限流，用于无需认证的公开端点。
//
// 公开端点没有 API Key 或登录态可用于区分调用方，IP 是唯一可用的维度。
// 与 RateLimit 一样使用令牌桶；突发量按每分钟速率的 1/6 计算（最少 10），
// 既能容忍浏览器一次加载多张图片，又不会让单个来源瞬间拉起大量上游请求。
//
// 依赖 gin 的 ClientIP()，因此 TRUSTED_PROXIES 必须配置正确：
// 部署在代理之后却未配置时，所有请求都会被算成网关那一个 IP，共享同一份额度。
func IPRateLimit(perMinute int) gin.HandlerFunc {
	if perMinute <= 0 {
		perMinute = 1000
	}
	ratePerSecond := float64(perMinute) / 60.0
	burst := float64(perMinute / 6)
	if burst < 10 {
		burst = 10
	}

	return func(c *gin.Context) {
		ip := c.ClientIP()
		if ip == "" {
			// 取不到来源地址时归入同一个桶，而不是放行：
			// 公开端点没有其它凭证，放行等于让限流形同虚设。
			ip = "unknown"
		}

		if !allowIPRequest(ip, ratePerSecond, burst) {
			c.Header("Retry-After", "1")
			c.JSON(http.StatusTooManyRequests, gin.H{
				"error": gin.H{
					"message": "请求过于频繁，请稍后再试",
					"type":    "rate_limit_error",
				},
			})
			c.Abort()
			return
		}

		c.Next()
	}
}

// allowRequest 按用户 ID 的令牌桶判定（线程安全）
func allowRequest(userID int, ratePerSecond, burst float64) bool {
	return allowKeyed(rlBuckets, &rlLastSweep, userID, ratePerSecond, burst)
}

// allowIPRequest 按来源 IP 的令牌桶判定（线程安全）
func allowIPRequest(ip string, ratePerSecond, burst float64) bool {
	return allowKeyed(ipBuckets, &ipLastSweep, ip, ratePerSecond, burst)
}

// allowKeyed 是令牌桶的通用实现（线程安全）
func allowKeyed[K comparable](buckets map[K]*rlBucket, lastSweep *time.Time, key K, ratePerSecond, burst float64) bool {
	now := time.Now()

	rlMu.Lock()
	defer rlMu.Unlock()

	sweepRateLimitBucketsLocked(buckets, lastSweep, now)

	b := buckets[key]
	if b == nil {
		if len(buckets) >= maxRateLimitBuckets {
			// 满载时放弃对新调用方限流，宁可放宽也不让内存无界增长
			return true
		}
		b = &rlBucket{tokens: burst, lastSeen: now}
		buckets[key] = b
	}

	// 按经过的时间补充令牌，上限为 burst
	elapsed := now.Sub(b.lastSeen).Seconds()
	b.lastSeen = now
	b.tokens = min(burst, b.tokens+elapsed*ratePerSecond)

	if b.tokens < 1 {
		return false
	}
	b.tokens--
	return true
}

// sweepRateLimitBucketsLocked 清理长期不活跃的桶（调用方须持有 rlMu）
func sweepRateLimitBucketsLocked[K comparable](buckets map[K]*rlBucket, lastSweep *time.Time, now time.Time) {
	if !lastSweep.IsZero() && now.Sub(*lastSweep) < rateLimitSweepInterval {
		return
	}
	*lastSweep = now

	for key, b := range buckets {
		if now.Sub(b.lastSeen) > rateLimitSweepInterval {
			delete(buckets, key)
		}
	}
}
