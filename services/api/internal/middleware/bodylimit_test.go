package middleware

import (
	"bytes"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/gin-gonic/gin"
)

func bodyLimitEngine() *gin.Engine {
	gin.SetMode(gin.TestMode)
	r := gin.New()
	r.Use(BodyLimit())
	read := func(c *gin.Context) {
		n, err := io.Copy(io.Discard, c.Request.Body)
		if err != nil {
			c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
			return
		}
		c.JSON(http.StatusOK, gin.H{"read": n})
	}
	r.POST("/api/v1/servers/:id/start", read)
	r.POST("/api/v1/servers/:id/files/upload", read)
	r.POST("/api/v1/servers/:id/daemon/logs", read)
	return r
}

// chunked hides the length so the request is sent without Content-Length.
type chunked struct{ io.Reader }

func TestBodyLimit(t *testing.T) {
	r := bodyLimitEngine()
	big := bytes.Repeat([]byte("a"), int(DefaultBodyLimit)+1)

	cases := []struct {
		name   string
		path   string
		body   io.Reader
		status int
	}{
		{"small default", "/api/v1/servers/x/start", strings.NewReader(`{}`), http.StatusOK},
		{"oversized default with length", "/api/v1/servers/x/start", bytes.NewReader(big), http.StatusRequestEntityTooLarge},
		{"oversized default chunked", "/api/v1/servers/x/start", chunked{bytes.NewReader(big)}, http.StatusBadRequest},
		{"upload is exempt", "/api/v1/servers/x/files/upload", bytes.NewReader(big), http.StatusOK},
		{"daemon logs above default", "/api/v1/servers/x/daemon/logs", bytes.NewReader(big), http.StatusOK},
		{"daemon logs over own cap", "/api/v1/servers/x/daemon/logs",
			bytes.NewReader(bytes.Repeat([]byte("a"), int(DaemonBodyLimit)+1)), http.StatusRequestEntityTooLarge},
	}
	for _, tc := range cases {
		req := httptest.NewRequest(http.MethodPost, tc.path, tc.body)
		if _, ok := tc.body.(chunked); ok {
			req.ContentLength = -1
		}
		w := httptest.NewRecorder()
		r.ServeHTTP(w, req)
		if w.Code != tc.status {
			t.Errorf("%s: status %d, want %d (%s)", tc.name, w.Code, tc.status, w.Body.String())
		}
	}
}
