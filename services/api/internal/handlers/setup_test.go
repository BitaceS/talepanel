package handlers

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/gin-gonic/gin"

	"github.com/BitaceS/talepanel/api/internal/config"
	"github.com/BitaceS/talepanel/api/internal/services"
)

func TestCompleteSetupDisabledWithoutHash(t *testing.T) {
	gin.SetMode(gin.TestMode)
	h := NewAuthHandler(services.NewAuthService(nil, nil, &config.Config{}), "secret", false)
	r := gin.New()
	r.POST("/setup/complete", h.CompleteSetup)

	w := httptest.NewRecorder()
	body := `{"setup_code":"ABCD-EFGH-JKLM","email":"a@example.com","username":"owner","password":"Correct-Horse-4!"}`
	req := httptest.NewRequest(http.MethodPost, "/setup/complete", strings.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	r.ServeHTTP(w, req)

	if w.Code != http.StatusNotFound {
		t.Fatalf("got %d, want 404", w.Code)
	}
}
