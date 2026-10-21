package middleware

import (
	"net/http"
	"strings"

	"github.com/gin-gonic/gin"
)

// Request body caps. Nearly every endpoint takes a small JSON document, so
// the default is tight; the few routes that legitimately carry more get their
// own limit in bodyLimitFor.
const (
	DefaultBodyLimit     int64 = 1 << 20   // 1 MiB
	DaemonBodyLimit      int64 = 8 << 20   // daemon callbacks: log batches, reports
	FileContentBodyLimit int64 = 8 << 20   // in-panel file editor saves
	ModUploadBodyLimit   int64 = 512 << 20 // single mod/plugin archive
	// NoBodyLimit disables the cap: the streaming file upload proxies multi-GB
	// bodies straight to the daemon without buffering them.
	NoBodyLimit int64 = -1
)

// bodyLimitFor returns the cap for a matched route (gin's FullPath, which
// still holds the :param placeholders).
func bodyLimitFor(fullPath string) int64 {
	switch fullPath {
	case "/api/v1/servers/:id/files/upload":
		return NoBodyLimit
	case "/api/v1/servers/:id/mods/upload":
		return ModUploadBodyLimit
	case "/api/v1/servers/:id/files/content":
		return FileContentBodyLimit
	case "/api/v1/nodes/:id/register",
		"/api/v1/nodes/:id/heartbeat",
		"/api/v1/nodes/:id/commands/:cmd_id/ack",
		"/api/v1/nodes/:id/ddos":
		return DaemonBodyLimit
	}
	if strings.HasPrefix(fullPath, "/api/v1/servers/:id/daemon/") {
		return DaemonBodyLimit
	}
	return DefaultBodyLimit
}

// BodyLimit caps the size of every request body. A declared Content-Length
// over the cap is rejected up front with 413; bodies without one (chunked)
// are wrapped in http.MaxBytesReader so reading past the cap fails and the
// handler's bind/parse step rejects the request instead of buffering it.
// Must be registered with r.Use so it runs after route matching (FullPath).
func BodyLimit() gin.HandlerFunc {
	return func(c *gin.Context) {
		limit := bodyLimitFor(c.FullPath())
		if limit == NoBodyLimit || c.Request.Body == nil || c.Request.Body == http.NoBody {
			c.Next()
			return
		}
		if c.Request.ContentLength > limit {
			c.Header("Connection", "close")
			c.AbortWithStatusJSON(http.StatusRequestEntityTooLarge, gin.H{"error": "request body too large"})
			return
		}
		c.Request.Body = http.MaxBytesReader(c.Writer, c.Request.Body, limit)
		c.Next()
	}
}
