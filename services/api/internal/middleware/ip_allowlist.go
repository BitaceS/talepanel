package middleware

import (
	"net"
	"net/http"
	"net/netip"
	"strings"

	"github.com/gin-gonic/gin"
)

// IPAllowed reports whether ip falls inside one of the prefixes.
func IPAllowed(allow []netip.Prefix, ip string) bool {
	a, err := netip.ParseAddr(strings.TrimSpace(ip))
	if err != nil {
		// net.ParseIP accepts a few forms netip does not (e.g. zones dropped).
		parsed := net.ParseIP(ip)
		if parsed == nil {
			return false
		}
		var ok bool
		a, ok = netip.AddrFromSlice(parsed)
		if !ok {
			return false
		}
	}
	a = a.Unmap()
	for _, p := range allow {
		if p.Contains(a) {
			return true
		}
	}
	return false
}

// RequireIPAllowlist (see config.ParseIPAllowlist for the list format) rejects requests whose client IP (c.ClientIP(), which
// honours TRUSTED_PROXIES) is not in allow. With an empty list it lets every
// request through, so the feature is off unless configured.
func RequireIPAllowlist(allow []netip.Prefix) gin.HandlerFunc {
	return func(c *gin.Context) {
		if len(allow) == 0 {
			c.Next()
			return
		}
		if !IPAllowed(allow, c.ClientIP()) {
			c.AbortWithStatusJSON(http.StatusForbidden, gin.H{"error": "admin access is not allowed from this address"})
			return
		}
		c.Next()
	}
}
