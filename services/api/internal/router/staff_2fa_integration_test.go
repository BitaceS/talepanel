package router

import (
	"context"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/golang-jwt/jwt/v5"
	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/BitaceS/talepanel/api/internal/middleware"
	"github.com/BitaceS/talepanel/api/internal/models"
)

// sessionToken signs an access token the way AuthService does (testRouter's
// JWT_SECRET).
func sessionToken(t *testing.T, userID uuid.UUID, role string) string {
	t.Helper()
	tok, err := jwt.NewWithClaims(jwt.SigningMethodHS256, jwt.MapClaims{
		"sub": userID.String(), "role": role, "jti": uuid.NewString(),
		"iat": time.Now().Unix(), "exp": time.Now().Add(5 * time.Minute).Unix(),
	}).SignedString([]byte(strings.Repeat("a1", 20)))
	if err != nil {
		t.Fatalf("sign: %v", err)
	}
	return tok
}

func callJSON(r *gin.Engine, method, path, token, body string) *responseBody {
	var rd io.Reader
	if body != "" {
		rd = strings.NewReader(body)
	}
	req := httptest.NewRequest(method, path, rd)
	req.RemoteAddr = "127.0.0.1:1234"
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Authorization", "Bearer "+token)
	w := httptest.NewRecorder()
	r.ServeHTTP(w, req)
	out := &responseBody{Status: w.Code}
	_ = json.Unmarshal(w.Body.Bytes(), &out.JSON)
	return out
}

type responseBody struct {
	Status int
	JSON   map[string]any
}

func setTOTP(t *testing.T, pool *pgxpool.Pool, userID uuid.UUID, on bool) {
	t.Helper()
	if _, err := pool.Exec(context.Background(), `UPDATE users SET totp_enabled = $2 WHERE id = $1`, userID, on); err != nil {
		t.Fatalf("set totp: %v", err)
	}
}

// With the staff 2FA policy on, an admin or owner without 2FA reaches only
// their own account (to set 2FA up), and with API keys nothing at all.
// Regular users are not affected. Only an owner who has 2FA can turn it on.
func TestStaff2FAPolicy(t *testing.T) {
	r, pool := testRouter(t)
	t.Cleanup(func() {
		_, _ = pool.Exec(context.Background(), `DELETE FROM app_settings WHERE key = 'security.require_staff_2fa'`)
	})

	ownerID := seedUser(t, pool, models.RoleOwner)
	adminID := seedUser(t, pool, models.RoleAdmin)
	userID := seedUser(t, pool, models.RoleUser)
	owner := sessionToken(t, ownerID, models.RoleOwner)
	admin := sessionToken(t, adminID, models.RoleAdmin)
	user := sessionToken(t, userID, models.RoleUser)

	on := `{"require_staff_2fa": true}`

	// An admin may read but not change the policy.
	if got := callJSON(r, http.MethodGet, "/api/v1/admin/security-policy", admin, ""); got.Status != http.StatusOK {
		t.Fatalf("admin GET policy = %d", got.Status)
	}
	if got := callJSON(r, http.MethodPut, "/api/v1/admin/security-policy", admin, on); got.Status != http.StatusForbidden {
		t.Fatalf("admin PUT policy = %d, want 403", got.Status)
	}
	// The owner must have 2FA before requiring it.
	if got := callJSON(r, http.MethodPut, "/api/v1/admin/security-policy", owner, on); got.Status != http.StatusConflict {
		t.Fatalf("owner without 2FA PUT policy = %d, want 409", got.Status)
	}
	setTOTP(t, pool, ownerID, true)
	if got := callJSON(r, http.MethodPut, "/api/v1/admin/security-policy", owner, on); got.Status != http.StatusOK {
		t.Fatalf("owner PUT policy = %d", got.Status)
	}

	// Admin without 2FA: blocked outside /auth/*, Me says why.
	got := callJSON(r, http.MethodGet, "/api/v1/servers", admin, "")
	if got.Status != http.StatusForbidden || got.JSON["code"] != middleware.Code2FASetupRequired {
		t.Fatalf("admin without 2FA GET /servers = %d %v, want 403 %s", got.Status, got.JSON, middleware.Code2FASetupRequired)
	}
	if got := callJSON(r, http.MethodGet, "/api/v1/admin/users", admin, ""); got.Status != http.StatusForbidden {
		t.Fatalf("admin without 2FA GET /admin/users = %d, want 403", got.Status)
	}
	got = callJSON(r, http.MethodGet, "/api/v1/auth/me", admin, "")
	if got.Status != http.StatusOK || got.JSON["two_factor_setup_required"] != true {
		t.Fatalf("admin /auth/me = %d %v, want two_factor_setup_required", got.Status, got.JSON)
	}
	if got := callJSON(r, http.MethodPost, "/api/v1/auth/totp/setup", admin, ""); got.Status != http.StatusOK {
		t.Fatalf("admin without 2FA POST /auth/totp/setup = %d %v", got.Status, got.JSON)
	}

	// Regular users are not affected.
	if got := callJSON(r, http.MethodGet, "/api/v1/servers", user, ""); got.Status != http.StatusOK {
		t.Fatalf("user GET /servers = %d", got.Status)
	}
	got = callJSON(r, http.MethodGet, "/api/v1/auth/me", user, "")
	if got.JSON["two_factor_setup_required"] != false {
		t.Fatalf("user /auth/me two_factor_setup_required = %v", got.JSON["two_factor_setup_required"])
	}

	// Staff with 2FA cannot switch it off while the policy is on.
	if got := callJSON(r, http.MethodPost, "/api/v1/auth/totp/disable", owner, `{"password":"x"}`); got.Status != http.StatusConflict {
		t.Fatalf("owner disabling 2FA under the policy = %d, want 409", got.Status)
	}

	// Once the admin has 2FA, everything works again.
	setTOTP(t, pool, adminID, true)
	if got := callJSON(r, http.MethodGet, "/api/v1/servers", admin, ""); got.Status != http.StatusOK {
		t.Fatalf("admin with 2FA GET /servers = %d", got.Status)
	}

	// Policy off: staff without 2FA are back to normal.
	setTOTP(t, pool, adminID, false)
	if got := callJSON(r, http.MethodPut, "/api/v1/admin/security-policy", owner, `{"require_staff_2fa": false}`); got.Status != http.StatusOK {
		t.Fatalf("owner turning policy off = %d", got.Status)
	}
	if got := callJSON(r, http.MethodGet, "/api/v1/servers", admin, ""); got.Status != http.StatusOK {
		t.Fatalf("admin without 2FA, policy off, GET /servers = %d", got.Status)
	}
}
