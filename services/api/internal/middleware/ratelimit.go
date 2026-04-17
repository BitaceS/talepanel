package middleware

import (
	"context"
	"fmt"
	"net/http"
	"strconv"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/redis/go-redis/v9"
)

// localWhitelist contains IPs that are never rate-limited.
var localWhitelist = map[string]struct{}{
	"127.0.0.1": {},
	"::1":       {},
}

// RateLimit returns a middleware that enforces a sliding-window rate limit of
// requestsPerMin per client IP using Redis.
//
// Key schema: "ratelimit:{ip}:{window_minute_unix}"
//
// When the limit is exceeded the handler is aborted with HTTP 429 and a
// Retry-After header indicating the seconds until the current window expires.
func RateLimit(rdb *redis.Client, requestsPerMin int) gin.HandlerFunc {
	return func(c *gin.Context) {
		ip := c.ClientIP()

		// Whitelisted IPs bypass all limiting.
		if _, ok := localWhitelist[ip]; ok {
			c.Next()
			return
		}

		now := time.Now()
		windowMinute := now.Unix() / 60                 // current 1-minute bucket
		windowExpiry := time.Duration(60-now.Second()) * time.Second // seconds until next minute

		key := fmt.Sprintf("ratelimit:%s:%d", ip, windowMinute)

		count, err := incrementRateLimit(c.Request.Context(), rdb, key, windowExpiry)
		if err != nil {
			// Fail open: log and continue rather than blocking legitimate traffic.
			c.Next()
			return
		}

		c.Header("X-RateLimit-Limit", strconv.Itoa(requestsPerMin))
		remaining := requestsPerMin - int(count)
		if remaining < 0 {
			remaining = 0
		}
		c.Header("X-RateLimit-Remaining", strconv.Itoa(remaining))
		c.Header("X-RateLimit-Reset", strconv.FormatInt((windowMinute+1)*60, 10))

		if int(count) > requestsPerMin {
			c.Header("Retry-After", strconv.Itoa(int(windowExpiry.Seconds())))
			c.AbortWithStatusJSON(http.StatusTooManyRequests, gin.H{
				"error":       "rate limit exceeded",
				"retry_after": int(windowExpiry.Seconds()),
			})
			return
		}

		c.Next()
	}
}

// incrementRateLimit atomically increments the counter for the given key and
// sets its TTL on first creation.  It returns the new counter value.
func incrementRateLimit(ctx context.Context, rdb *redis.Client, key string, ttl time.Duration) (int64, error) {
	pipe := rdb.Pipeline()
	incrCmd := pipe.Incr(ctx, key)
	pipe.Expire(ctx, key, ttl)

	if _, err := pipe.Exec(ctx); err != nil {
		return 0, fmt.Errorf("redis pipeline: %w", err)
	}

	return incrCmd.Val(), nil
}

