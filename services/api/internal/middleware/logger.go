package middleware

import (
	"net/url"
	"time"

	"github.com/gin-gonic/gin"
	"go.uber.org/zap"
)

// GinLogger returns a Gin middleware that logs each HTTP request using the
// provided zap.Logger.  All fields are emitted as structured key-value pairs;
// no string formatting is used.
func GinLogger(logger *zap.Logger) gin.HandlerFunc {
	return func(c *gin.Context) {
		start := time.Now()
		path := c.Request.URL.Path
		rawQuery := c.Request.URL.RawQuery

		c.Next()

		latency := time.Since(start)
		status := c.Writer.Status()
		clientIP := c.ClientIP()
		method := c.Request.Method
		requestID := GetRequestID(c)

		if rawQuery != "" {
			path = path + "?" + redactQuery(rawQuery)
		}

		fields := []zap.Field{
			zap.String("request_id", requestID),
			zap.String("method", method),
			zap.String("path", path),
			zap.Int("status", status),
			zap.Duration("latency", latency),
			zap.String("ip", clientIP),
			zap.Int("bytes_out", c.Writer.Size()),
		}

		// Attach any errors accumulated by gin handlers.
		if len(c.Errors) > 0 {
			fields = append(fields, zap.String("gin_errors", c.Errors.ByType(gin.ErrorTypePrivate).String()))
		}

		switch {
		case status >= 500:
			logger.Error("request completed", fields...)
		case status >= 400:
			logger.Warn("request completed", fields...)
		default:
			logger.Info("request completed", fields...)
		}
	}
}

// sensitiveQueryParams are query parameters whose values never reach the
// request log. "ticket" is the single-use event-stream ticket: dead once
// used, but a log is no place for a credential either way.
var sensitiveQueryParams = []string{"ticket", "token", "access_token"}

// redactQuery replaces the values of sensitive query parameters with
// "REDACTED". A query that cannot be parsed is dropped rather than logged
// as-is.
func redactQuery(rawQuery string) string {
	values, err := url.ParseQuery(rawQuery)
	if err != nil {
		return "[unparseable query redacted]"
	}
	changed := false
	for _, k := range sensitiveQueryParams {
		if _, ok := values[k]; ok {
			values.Set(k, "REDACTED")
			changed = true
		}
	}
	if !changed {
		return rawQuery
	}
	return values.Encode()
}
