package services

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"sync"
	"testing"

	"go.uber.org/zap"
)

// fakeHytale imitates the OAuth, account-data and sessions hosts on one server.
type fakeHytale struct {
	mu            sync.Mutex
	authorized    bool   // device code approved by the user yet?
	refreshToken  string // the currently valid refresh token
	refreshCalls  int
	sessionsMade  int
	lastSessionFor string
}

func (f *fakeHytale) handler(t *testing.T) http.Handler {
	mux := http.NewServeMux()
	mux.HandleFunc("/oauth2/device/auth", func(w http.ResponseWriter, r *http.Request) {
		_ = r.ParseForm()
		if r.Form.Get("client_id") != "hytale-server" || r.Form.Get("scope") != "openid offline auth:server" {
			t.Errorf("device auth form = %v", r.Form)
		}
		_ = json.NewEncoder(w).Encode(map[string]any{
			"device_code": "dev-123", "user_code": "ABCD-1234",
			"verification_uri":          "https://accounts.hytale.com/device",
			"verification_uri_complete": "https://accounts.hytale.com/device?user_code=ABCD-1234",
			"expires_in": 900, "interval": 5,
		})
	})
	mux.HandleFunc("/oauth2/token", func(w http.ResponseWriter, r *http.Request) {
		f.mu.Lock()
		defer f.mu.Unlock()
		_ = r.ParseForm()
		switch r.Form.Get("grant_type") {
		case "urn:ietf:params:oauth:grant-type:device_code":
			if !f.authorized {
				w.WriteHeader(http.StatusBadRequest)
				_ = json.NewEncoder(w).Encode(map[string]string{"error": "authorization_pending"})
				return
			}
			f.refreshToken = "refresh-1"
			_ = json.NewEncoder(w).Encode(map[string]any{
				"access_token": "access-1", "expires_in": 3600, "refresh_token": "refresh-1",
			})
		case "refresh_token":
			f.refreshCalls++
			if r.Form.Get("refresh_token") != f.refreshToken {
				w.WriteHeader(http.StatusBadRequest)
				_ = json.NewEncoder(w).Encode(map[string]string{"error": "invalid_grant"})
				return
			}
			// Rotation: the old refresh token stops working.
			f.refreshToken = "refresh-" + string(rune('1'+f.refreshCalls))
			_ = json.NewEncoder(w).Encode(map[string]any{
				"access_token": "access-r", "expires_in": 3600, "refresh_token": f.refreshToken,
			})
		default:
			t.Errorf("unexpected grant %q", r.Form.Get("grant_type"))
		}
	})
	mux.HandleFunc("/my-account/get-profiles", func(w http.ResponseWriter, r *http.Request) {
		if r.Header.Get("Authorization") == "" {
			w.WriteHeader(http.StatusUnauthorized)
			return
		}
		_ = json.NewEncoder(w).Encode(map[string]any{
			"owner": "550e8400-e29b-41d4-a716-446655440000",
			"profiles": []map[string]string{
				{"uuid": "123e4567-e89b-12d3-a456-426614174000", "username": "ServerOperator"},
				{"uuid": "223e4567-e89b-12d3-a456-426614174000", "username": "AltProfile"},
			},
		})
	})
	mux.HandleFunc("/game-session/new", func(w http.ResponseWriter, r *http.Request) {
		f.mu.Lock()
		defer f.mu.Unlock()
		var body struct{ UUID string `json:"uuid"` }
		_ = json.NewDecoder(r.Body).Decode(&body)
		f.sessionsMade++
		f.lastSessionFor = body.UUID
		_ = json.NewEncoder(w).Encode(map[string]string{
			"sessionToken": "sess", "identityToken": "ident", "expiresAt": "2026-10-08T12:00:00Z",
		})
	})
	return mux
}

func newTestHytaleAuth(t *testing.T) (*HytaleAuthService, *fakeHytale, *AppSettingsService) {
	t.Helper()
	pool := openTestDB(t)
	settings := NewAppSettingsService(pool, []byte("0123456789abcdef0123456789abcdef"))
	clear := func() {
		for _, k := range hytaleAuthKeys {
			_ = settings.Set(context.Background(), k, "", nil)
		}
	}
	clear()
	t.Cleanup(clear)

	fake := &fakeHytale{}
	srv := httptest.NewServer(fake.handler(t))
	t.Cleanup(srv.Close)
	ep := HytaleAuthEndpoints{OAuth: srv.URL, AccountData: srv.URL, Sessions: srv.URL}
	return NewHytaleAuthService(settings, ep, zap.NewNop()), fake, settings
}

func TestHytaleAuth_DeviceLoginLinksTheAccount(t *testing.T) {
	svc, fake, settings := newTestHytaleAuth(t)
	ctx := context.Background()

	login, err := svc.StartDeviceLogin(ctx)
	if err != nil {
		t.Fatalf("start: %v", err)
	}
	if login.UserCode != "ABCD-1234" || login.VerificationURIComplete == "" {
		t.Fatalf("login = %+v", login)
	}

	if st, err := svc.PollDeviceLogin(ctx, nil); err != nil || st != DeviceLoginPending {
		t.Fatalf("poll before approval = %q, %v; want pending", st, err)
	}

	fake.authorized = true
	if st, err := svc.PollDeviceLogin(ctx, nil); err != nil || st != DeviceLoginLinked {
		t.Fatalf("poll after approval = %q, %v; want linked", st, err)
	}

	status, err := svc.Status(ctx)
	if err != nil {
		t.Fatalf("status: %v", err)
	}
	if !status.Linked || status.ProfileUUID != "123e4567-e89b-12d3-a456-426614174000" || status.ProfileName != "ServerOperator" {
		t.Fatalf("status = %+v", status)
	}
	if len(status.Profiles) != 2 {
		t.Fatalf("profiles = %v, want both listed for selection", status.Profiles)
	}
	if rt, _ := settings.Get(ctx, hytaleRefreshTokenKey); rt != "refresh-1" {
		t.Fatalf("stored refresh token = %q", rt)
	}
}

func TestHytaleAuth_GameSessionUsesTheSelectedProfileAndPersistsRotation(t *testing.T) {
	svc, fake, settings := newTestHytaleAuth(t)
	ctx := context.Background()
	link(t, svc, fake)

	if err := svc.SelectProfile(ctx, "223e4567-e89b-12d3-a456-426614174000"); err != nil {
		t.Fatalf("select: %v", err)
	}
	if err := svc.SelectProfile(ctx, "00000000-0000-0000-0000-000000000000"); err == nil {
		t.Fatal("selecting a profile the account does not have was accepted")
	}

	// Drop the cached access token so the session needs an OAuth refresh,
	// as after an API restart.
	svc.forgetAccessToken()
	gs, err := svc.NewGameSession(ctx)
	if err != nil {
		t.Fatalf("game session: %v", err)
	}
	if gs.SessionToken != "sess" || gs.IdentityToken != "ident" || gs.OwnerUUID != "223e4567-e89b-12d3-a456-426614174000" {
		t.Fatalf("session = %+v", gs)
	}
	if fake.lastSessionFor != "223e4567-e89b-12d3-a456-426614174000" {
		t.Fatalf("session created for %q", fake.lastSessionFor)
	}
	// The rotated refresh token must be stored, or the next refresh fails.
	if rt, _ := settings.Get(ctx, hytaleRefreshTokenKey); rt != fake.refreshToken {
		t.Fatalf("stored refresh token %q, provider expects %q", rt, fake.refreshToken)
	}

	// A second start within the access token's lifetime must not rotate again.
	if _, err := svc.NewGameSession(ctx); err != nil {
		t.Fatalf("second session: %v", err)
	}
	if fake.refreshCalls != 1 {
		t.Fatalf("refresh calls = %d, want 1", fake.refreshCalls)
	}
}

// Concurrent server starts must not each spend the same refresh token — the
// provider invalidates it on first use and the rest would unlink the panel.
func TestHytaleAuth_ConcurrentStartsRefreshOnce(t *testing.T) {
	svc, fake, _ := newTestHytaleAuth(t)
	ctx := context.Background()
	link(t, svc, fake)
	svc.forgetAccessToken()

	var wg sync.WaitGroup
	errs := make(chan error, 8)
	for range 8 {
		wg.Add(1)
		go func() {
			defer wg.Done()
			if _, err := svc.NewGameSession(ctx); err != nil {
				errs <- err
			}
		}()
	}
	wg.Wait()
	close(errs)
	for err := range errs {
		t.Errorf("concurrent session: %v", err)
	}
	if fake.refreshCalls != 1 {
		t.Fatalf("refresh calls = %d, want 1", fake.refreshCalls)
	}
}

func TestHytaleAuth_NotLinked(t *testing.T) {
	svc, _, _ := newTestHytaleAuth(t)
	if _, err := svc.NewGameSession(context.Background()); !errors.Is(err, ErrHytaleNotLinked) {
		t.Fatalf("err = %v, want ErrHytaleNotLinked", err)
	}
}

func TestHytaleAuth_RevokedRefreshTokenAsksForRelink(t *testing.T) {
	svc, fake, _ := newTestHytaleAuth(t)
	ctx := context.Background()
	link(t, svc, fake)
	svc.forgetAccessToken()
	fake.refreshToken = "revoked-elsewhere"

	if _, err := svc.NewGameSession(ctx); !errors.Is(err, ErrHytaleRelinkRequired) {
		t.Fatalf("err = %v, want ErrHytaleRelinkRequired", err)
	}
}

func TestHytaleAuth_UnlinkForgetsEverything(t *testing.T) {
	svc, fake, _ := newTestHytaleAuth(t)
	ctx := context.Background()
	link(t, svc, fake)

	if err := svc.Unlink(ctx); err != nil {
		t.Fatalf("unlink: %v", err)
	}
	st, _ := svc.Status(ctx)
	if st.Linked {
		t.Fatal("still linked after unlink")
	}
	if _, err := svc.NewGameSession(ctx); !errors.Is(err, ErrHytaleNotLinked) {
		t.Fatalf("err = %v, want ErrHytaleNotLinked", err)
	}
}

func link(t *testing.T, svc *HytaleAuthService, fake *fakeHytale) {
	t.Helper()
	ctx := context.Background()
	if _, err := svc.StartDeviceLogin(ctx); err != nil {
		t.Fatalf("start: %v", err)
	}
	fake.authorized = true
	if st, err := svc.PollDeviceLogin(ctx, nil); err != nil || st != DeviceLoginLinked {
		t.Fatalf("link: %q %v", st, err)
	}
}
