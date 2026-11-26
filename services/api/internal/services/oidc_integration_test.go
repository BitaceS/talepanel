package services

import (
	"context"
	"errors"
	"net/url"
	"strings"
	"testing"

	"github.com/google/uuid"

	"github.com/BitaceS/talepanel/api/internal/config"
	"github.com/BitaceS/talepanel/api/internal/models"
)

// oidcTestEnv: a real Postgres, miniredis and the fake provider.
type oidcTestEnv struct {
	svc *OIDCService
	idp *fakeIdP
}

func newOIDCTestEnv(t *testing.T, mutate func(*OIDCConfig)) *oidcTestEnv {
	t.Helper()
	pool := openTestDB(t)
	_, rdb := newMiniRedis(t)
	idp := newFakeIdP(t)
	auth := NewAuthService(pool, rdb, &config.Config{
		JWTSecret:        "test-access-secret-0123456789abcdef",
		JWTRefreshSecret: "test-refresh-secret-0123456789abcdef",
	})
	settings := NewAppSettingsService(pool, make([]byte, 32))
	svc := NewOIDCService(pool, rdb, settings, auth, nil, true)
	svc.client = idp.srv.Client()

	cfg := OIDCConfig{Enabled: true, IssuerURL: idp.srv.URL, ClientID: "talepanel", ClientSecret: "s3cret",
		PanelURL: "https://panel.example.com"}
	if mutate != nil {
		mutate(&cfg)
	}
	ctx := context.Background()
	if _, err := svc.SetConfig(ctx, cfg, seedTestUser(t, pool, models.RoleOwner)); err != nil {
		t.Fatalf("SetConfig: %v", err)
	}
	t.Cleanup(func() {
		_ = settings.Set(context.Background(), oidcSettingsKey, "", nil)
		_, _ = pool.Exec(context.Background(), `DELETE FROM user_oidc_links WHERE issuer = $1`, idp.srv.URL)
	})
	return &oidcTestEnv{svc: svc, idp: idp}
}

// signIn runs Start and Callback with an ID token for subject/email.
func (e *oidcTestEnv) signIn(t *testing.T, subject, email string) (*LoginResult, error) {
	t.Helper()
	ctx := context.Background()
	authURL, state, err := e.svc.Start(ctx)
	if err != nil {
		t.Fatalf("Start: %v", err)
	}
	u, _ := url.Parse(authURL)
	nonce := u.Query().Get("nonce")
	claims := e.idp.claims(nonce)
	claims["sub"] = subject
	claims["email"] = email
	e.idp.idToken = sign(t, e.idp.key, claims)
	return e.svc.Callback(ctx, state, "good-code", "127.0.0.1", "test")
}

func userEmail(t *testing.T, e *oidcTestEnv, id uuid.UUID) string {
	var email string
	if err := e.svc.db.QueryRow(context.Background(), `SELECT email FROM users WHERE id = $1`, id).Scan(&email); err != nil {
		t.Fatal(err)
	}
	return email
}

func TestOIDCLinksAnExistingUserByVerifiedEmail(t *testing.T) {
	e := newOIDCTestEnv(t, nil)
	id := seedTestUser(t, e.svc.db, models.RoleUser)
	res, err := e.signIn(t, "sub-"+id.String(), userEmail(t, e, id))
	if err != nil {
		t.Fatalf("Callback: %v", err)
	}
	if res.User == nil || res.User.ID != id || res.RefreshToken == "" {
		t.Fatalf("result = %+v", res)
	}
	// The second sign-in finds the link by subject, even with another email.
	res, err = e.signIn(t, "sub-"+id.String(), "changed@elsewhere.example")
	if err != nil || res.User.ID != id {
		t.Fatalf("second sign-in: %+v, %v", res, err)
	}
}

func TestOIDCStateIsSingleUse(t *testing.T) {
	e := newOIDCTestEnv(t, nil)
	ctx := context.Background()
	id := seedTestUser(t, e.svc.db, models.RoleUser)
	authURL, state, err := e.svc.Start(ctx)
	if err != nil {
		t.Fatal(err)
	}
	u, _ := url.Parse(authURL)
	claims := e.idp.claims(u.Query().Get("nonce"))
	claims["email"] = userEmail(t, e, id)
	e.idp.idToken = sign(t, e.idp.key, claims)
	if _, err := e.svc.Callback(ctx, state, "good-code", "127.0.0.1", "test"); err != nil {
		t.Fatalf("first callback: %v", err)
	}
	if _, err := e.svc.Callback(ctx, state, "good-code", "127.0.0.1", "test"); !errors.Is(err, ErrOIDCState) {
		t.Fatalf("replayed state: err = %v, want ErrOIDCState", err)
	}
	if _, err := e.svc.Callback(ctx, "made-up", "good-code", "127.0.0.1", "test"); !errors.Is(err, ErrOIDCState) {
		t.Fatalf("unknown state: err = %v, want ErrOIDCState", err)
	}
}

func TestOIDCUnknownUserWithoutAutoCreate(t *testing.T) {
	e := newOIDCTestEnv(t, nil)
	_, err := e.signIn(t, "sub-new-"+uuid.NewString(), "nobody-"+uuid.NewString()[:8]+"@example.com")
	if !errors.Is(err, ErrOIDCNoAccount) {
		t.Fatalf("err = %v, want ErrOIDCNoAccount", err)
	}
}

func TestOIDCAutoCreateUsesTheDefaultRole(t *testing.T) {
	e := newOIDCTestEnv(t, func(c *OIDCConfig) { c.AutoCreate = true; c.DefaultRole = models.RoleModerator })
	email := "new-" + uuid.NewString()[:8] + "@example.com"
	res, err := e.signIn(t, "sub-"+uuid.NewString(), email)
	if err != nil {
		t.Fatalf("Callback: %v", err)
	}
	t.Cleanup(func() { _, _ = e.svc.db.Exec(context.Background(), `DELETE FROM users WHERE id = $1`, res.User.ID) })
	if res.User.Email != email || res.User.Role != models.RoleModerator {
		t.Fatalf("created user = %+v", res.User)
	}
}

func TestOIDCUnverifiedEmailDoesNotLink(t *testing.T) {
	e := newOIDCTestEnv(t, func(c *OIDCConfig) { c.AutoCreate = true })
	id := seedTestUser(t, e.svc.db, models.RoleUser)
	ctx := context.Background()
	authURL, state, _ := e.svc.Start(ctx)
	u, _ := url.Parse(authURL)
	claims := e.idp.claims(u.Query().Get("nonce"))
	claims["email"] = userEmail(t, e, id)
	claims["email_verified"] = false
	e.idp.idToken = sign(t, e.idp.key, claims)
	if _, err := e.svc.Callback(ctx, state, "good-code", "127.0.0.1", "test"); !errors.Is(err, ErrOIDCUnverified) {
		t.Fatalf("err = %v, want ErrOIDCUnverified", err)
	}
}

func TestOIDCDomainListIsEnforced(t *testing.T) {
	e := newOIDCTestEnv(t, func(c *OIDCConfig) { c.AllowedDomains = []string{"corp.example"} })
	id := seedTestUser(t, e.svc.db, models.RoleUser) // email @test.local
	if _, err := e.signIn(t, "sub-"+id.String(), userEmail(t, e, id)); !errors.Is(err, ErrOIDCDomain) {
		t.Fatalf("err = %v, want ErrOIDCDomain", err)
	}
}

func TestOIDCStaffWithout2FAIsNotLinked(t *testing.T) {
	e := newOIDCTestEnv(t, nil)
	id := seedTestUser(t, e.svc.db, models.RoleAdmin)
	if _, err := e.signIn(t, "sub-"+id.String(), userEmail(t, e, id)); !errors.Is(err, ErrOIDCStaffLink) {
		t.Fatalf("err = %v, want ErrOIDCStaffLink", err)
	}
}

func TestOIDCUsersWith2FAGetTheTOTPStep(t *testing.T) {
	for _, trust := range []bool{false, true} {
		e := newOIDCTestEnv(t, func(c *OIDCConfig) { c.TrustMFA = trust })
		id := seedTestUser(t, e.svc.db, models.RoleAdmin)
		if _, err := e.svc.db.Exec(context.Background(), `UPDATE users SET totp_enabled = true WHERE id = $1`, id); err != nil {
			t.Fatal(err)
		}
		res, err := e.signIn(t, "sub-"+id.String(), userEmail(t, e, id))
		if err != nil {
			t.Fatalf("trust=%v: %v", trust, err)
		}
		if trust {
			if res.RequiresTOTP || res.RefreshToken == "" {
				t.Fatalf("trusted provider: want a full session, got %+v", res)
			}
		} else if !res.RequiresTOTP || res.PartialToken == "" || res.RefreshToken != "" {
			t.Fatalf("untrusted provider: want the TOTP step, got %+v", res)
		}
	}
}

func TestOIDCDisabledUserIsRefused(t *testing.T) {
	e := newOIDCTestEnv(t, nil)
	id := seedTestUser(t, e.svc.db, models.RoleUser)
	if _, err := e.svc.db.Exec(context.Background(), `UPDATE users SET is_active = false WHERE id = $1`, id); err != nil {
		t.Fatal(err)
	}
	if _, err := e.signIn(t, "sub-"+id.String(), userEmail(t, e, id)); !errors.Is(err, ErrUserInactive) {
		t.Fatalf("err = %v, want ErrUserInactive", err)
	}
}

func TestOIDCSecretIsKeptWhenLeftEmpty(t *testing.T) {
	e := newOIDCTestEnv(t, nil)
	ctx := context.Background()
	cfg, _ := e.svc.Config(ctx)
	cfg.ClientSecret = ""
	cfg.DisplayName = "Keycloak"
	saved, err := e.svc.SetConfig(ctx, cfg, seedTestUser(t, e.svc.db, models.RoleOwner))
	if err != nil {
		t.Fatalf("SetConfig: %v", err)
	}
	if saved.ClientSecret != "s3cret" || saved.DisplayName != "Keycloak" {
		t.Fatalf("saved = %+v", saved)
	}
	raw, err := e.svc.db.Query(ctx, `SELECT value FROM app_settings WHERE key = $1`, oidcSettingsKey)
	if err != nil {
		t.Fatal(err)
	}
	defer raw.Close()
	for raw.Next() {
		var v string
		_ = raw.Scan(&v)
		if strings.Contains(v, "s3cret") {
			t.Fatal("client secret stored in plain text")
		}
	}
}
