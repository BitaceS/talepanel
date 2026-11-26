package services

import (
	"context"
	"crypto/rand"
	"crypto/rsa"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/coreos/go-oidc/v3/oidc"
	jose "github.com/go-jose/go-jose/v4"
	"golang.org/x/oauth2"
)

// fakeIdP is a minimal OpenID provider: discovery, JWKS and a token
// endpoint that answers with whatever ID token the test sets.
type fakeIdP struct {
	srv     *httptest.Server
	key     *rsa.PrivateKey
	idToken string
	// gotVerifier is the PKCE code_verifier the token endpoint received.
	gotVerifier string
}

func newFakeIdP(t *testing.T) *fakeIdP {
	t.Helper()
	key, err := rsa.GenerateKey(rand.Reader, 2048)
	if err != nil {
		t.Fatal(err)
	}
	f := &fakeIdP{key: key}
	mux := http.NewServeMux()
	f.srv = httptest.NewServer(mux)
	t.Cleanup(f.srv.Close)
	mux.HandleFunc("/.well-known/openid-configuration", func(w http.ResponseWriter, _ *http.Request) {
		_ = json.NewEncoder(w).Encode(map[string]any{
			"issuer":                                f.srv.URL,
			"authorization_endpoint":                f.srv.URL + "/authorize",
			"token_endpoint":                        f.srv.URL + "/token",
			"jwks_uri":                              f.srv.URL + "/jwks",
			"id_token_signing_alg_values_supported": []string{"RS256"},
		})
	})
	mux.HandleFunc("/jwks", func(w http.ResponseWriter, _ *http.Request) {
		_ = json.NewEncoder(w).Encode(jose.JSONWebKeySet{Keys: []jose.JSONWebKey{
			{Key: &key.PublicKey, KeyID: "k1", Algorithm: "RS256", Use: "sig"},
		}})
	})
	mux.HandleFunc("/token", func(w http.ResponseWriter, r *http.Request) {
		_ = r.ParseForm()
		f.gotVerifier = r.PostForm.Get("code_verifier")
		if r.PostForm.Get("code") != "good-code" {
			w.WriteHeader(http.StatusBadRequest)
			_, _ = w.Write([]byte(`{"error":"invalid_grant"}`))
			return
		}
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(map[string]any{
			"access_token": "at", "token_type": "Bearer", "expires_in": 300, "id_token": f.idToken,
		})
	})
	return f
}

// sign makes an ID token with the given claims, signed by key.
func sign(t *testing.T, key *rsa.PrivateKey, claims map[string]any) string {
	t.Helper()
	signer, err := jose.NewSigner(jose.SigningKey{Algorithm: jose.RS256, Key: key},
		(&jose.SignerOptions{}).WithHeader("kid", "k1").WithType("JWT"))
	if err != nil {
		t.Fatal(err)
	}
	payload, _ := json.Marshal(claims)
	obj, err := signer.Sign(payload)
	if err != nil {
		t.Fatal(err)
	}
	out, err := obj.CompactSerialize()
	if err != nil {
		t.Fatal(err)
	}
	return out
}

func (f *fakeIdP) claims(nonce string) map[string]any {
	now := time.Now()
	return map[string]any{
		"iss": f.srv.URL, "sub": "user-123", "aud": "talepanel",
		"iat": now.Unix(), "exp": now.Add(5 * time.Minute).Unix(),
		"nonce": nonce, "email": "Alice@Example.com", "email_verified": true,
		"preferred_username": "alice.w",
	}
}

func (f *fakeIdP) setup(t *testing.T) (context.Context, OIDCConfig, *oidc.Provider) {
	t.Helper()
	ctx := oidc.ClientContext(context.Background(), f.srv.Client())
	p, err := oidc.NewProvider(ctx, f.srv.URL)
	if err != nil {
		t.Fatal(err)
	}
	cfg := OIDCConfig{
		Enabled: true, IssuerURL: f.srv.URL, ClientID: "talepanel", ClientSecret: "s3cret",
		PanelURL: "https://panel.example.com",
	}.Normalize()
	return ctx, cfg, p
}

func TestOIDCExchangeVerifiesTheIDToken(t *testing.T) {
	f := newFakeIdP(t)
	ctx, cfg, p := f.setup(t)
	flow := oidcFlow{Nonce: "n-1", Verifier: oauth2.GenerateVerifier()}
	f.idToken = sign(t, f.key, f.claims("n-1"))

	id, err := exchange(ctx, cfg, p, "good-code", flow)
	if err != nil {
		t.Fatalf("exchange: %v", err)
	}
	if id.Subject != "user-123" || id.Issuer != f.srv.URL {
		t.Fatalf("identity = %+v", id)
	}
	if id.Email != "alice@example.com" || !id.EmailVerified || id.Username != "alice.w" {
		t.Fatalf("claims = %+v", id)
	}
	if f.gotVerifier != flow.Verifier {
		t.Fatalf("token endpoint got code_verifier %q, want the flow's (PKCE)", f.gotVerifier)
	}
}

func TestOIDCExchangeRefusesBadTokens(t *testing.T) {
	f := newFakeIdP(t)
	ctx, cfg, p := f.setup(t)
	otherKey, err := rsa.GenerateKey(rand.Reader, 2048)
	if err != nil {
		t.Fatal(err)
	}
	cases := map[string]func(map[string]any) (string, string){
		"wrong nonce": func(c map[string]any) (string, string) { return sign(t, f.key, c), "other-nonce" },
		"no nonce": func(c map[string]any) (string, string) {
			delete(c, "nonce")
			return sign(t, f.key, c), "n-1"
		},
		"wrong audience": func(c map[string]any) (string, string) {
			c["aud"] = "someone-else"
			return sign(t, f.key, c), "n-1"
		},
		"wrong issuer": func(c map[string]any) (string, string) {
			c["iss"] = "https://evil.example.com"
			return sign(t, f.key, c), "n-1"
		},
		"expired": func(c map[string]any) (string, string) {
			c["exp"] = time.Now().Add(-time.Hour).Unix()
			c["iat"] = time.Now().Add(-2 * time.Hour).Unix()
			return sign(t, f.key, c), "n-1"
		},
		"foreign key": func(c map[string]any) (string, string) { return sign(t, otherKey, c), "n-1" },
		"not a jwt":   func(map[string]any) (string, string) { return "garbage", "n-1" },
	}
	for name, mk := range cases {
		t.Run(name, func(t *testing.T) {
			tok, nonce := mk(f.claims("n-1"))
			f.idToken = tok
			_, err := exchange(ctx, cfg, p, "good-code", oidcFlow{Nonce: nonce, Verifier: oauth2.GenerateVerifier()})
			if !errors.Is(err, ErrOIDCToken) {
				t.Fatalf("err = %v, want ErrOIDCToken", err)
			}
		})
	}
}

func TestOIDCExchangeRefusedCode(t *testing.T) {
	f := newFakeIdP(t)
	ctx, cfg, p := f.setup(t)
	f.idToken = sign(t, f.key, f.claims("n-1"))
	_, err := exchange(ctx, cfg, p, "stolen-code", oidcFlow{Nonce: "n-1", Verifier: oauth2.GenerateVerifier()})
	if !errors.Is(err, ErrOIDCToken) {
		t.Fatalf("err = %v, want ErrOIDCToken", err)
	}
}

func TestOIDCEmailVerifiedAsString(t *testing.T) {
	f := newFakeIdP(t)
	ctx, _, p := f.setup(t)
	c := f.claims("n-1")
	c["email_verified"] = "true"
	id, err := verifyIDToken(ctx, p, "talepanel", sign(t, f.key, c), "n-1")
	if err != nil || !id.EmailVerified {
		t.Fatalf("id = %+v, err = %v", id, err)
	}
	c["email_verified"] = "false"
	id, err = verifyIDToken(ctx, p, "talepanel", sign(t, f.key, c), "n-1")
	if err != nil || id.EmailVerified {
		t.Fatalf("id = %+v, err = %v", id, err)
	}
}

func TestOIDCAuthURLHasPKCEAndNonce(t *testing.T) {
	f := newFakeIdP(t)
	_, cfg, p := f.setup(t)
	u := oauthConfig(cfg, p).AuthCodeURL("st", oidc.Nonce("nn"), oauth2.S256ChallengeOption(oauth2.GenerateVerifier()))
	for _, want := range []string{"code_challenge_method=S256", "code_challenge=", "nonce=nn", "state=st",
		"redirect_uri=https%3A%2F%2Fpanel.example.com%2Fapi%2Fv1%2Fauth%2Foidc%2Fcallback", "scope=openid+email+profile"} {
		if !strings.Contains(u, want) {
			t.Errorf("auth URL %s lacks %s", u, want)
		}
	}
}

func TestOIDCDomainAllowed(t *testing.T) {
	cfg := OIDCConfig{AllowedDomains: []string{"Example.com", "@corp.example"}}.Normalize()
	cases := []struct {
		email    string
		verified bool
		want     bool
	}{
		{"a@example.com", true, true},
		{"a@corp.example", true, true},
		{"a@sub.example.com", true, false},
		{"a@example.com.evil", true, false},
		{"a@example.com", false, false},
		{"", true, false},
	}
	for _, c := range cases {
		got := domainAllowed(cfg, &oidcIdentity{Email: c.email, EmailVerified: c.verified})
		if got != c.want {
			t.Errorf("domainAllowed(%q, verified=%v) = %v, want %v", c.email, c.verified, got, c.want)
		}
	}
	if !domainAllowed(OIDCConfig{}, &oidcIdentity{}) {
		t.Error("an empty list must allow everyone")
	}
}

// go-oidc compares the issuer with the discovery document exactly, so a
// trailing slash (Auth0: "https://tenant.auth0.com/") must survive.
func TestOIDCNormalizeKeepsTheIssuerAsEntered(t *testing.T) {
	c := OIDCConfig{IssuerURL: "  https://tenant.auth0.com/ ", PanelURL: "https://panel.example.com/"}.Normalize()
	if c.IssuerURL != "https://tenant.auth0.com/" {
		t.Fatalf("issuer = %q", c.IssuerURL)
	}
	if c.PanelURL != "https://panel.example.com" {
		t.Fatalf("panel URL = %q", c.PanelURL)
	}
}

func TestOIDCConfigValidate(t *testing.T) {
	good := OIDCConfig{Enabled: true, IssuerURL: "https://id.example.com/realms/x/", ClientID: "tp",
		PanelURL: "https://panel.example.com/", DefaultRole: "user"}.Normalize()
	if err := good.Validate(false); err != nil {
		t.Fatalf("valid config refused: %v", err)
	}
	if good.PanelURL != "https://panel.example.com" {
		t.Fatalf("panel URL slash not trimmed: %+v", good)
	}
	bad := map[string]func(*OIDCConfig){
		"http issuer":   func(c *OIDCConfig) { c.IssuerURL = "http://id.example.com" },
		"no client id":  func(c *OIDCConfig) { c.ClientID = "" },
		"admin role":    func(c *OIDCConfig) { c.DefaultRole = "admin" },
		"owner role":    func(c *OIDCConfig) { c.DefaultRole = "owner" },
		"bad domain":    func(c *OIDCConfig) { c.AllowedDomains = []string{"not a domain"} },
		"query in url":  func(c *OIDCConfig) { c.PanelURL = "https://panel.example.com/?x=1" },
		"userinfo":      func(c *OIDCConfig) { c.IssuerURL = "https://u:p@id.example.com" },
		"relative url":  func(c *OIDCConfig) { c.PanelURL = "/panel" },
		"javascript":    func(c *OIDCConfig) { c.PanelURL = "javascript:alert(1)" },
		"long name":     func(c *OIDCConfig) { c.DisplayName = strings.Repeat("x", 41) },
		"ftp panel url": func(c *OIDCConfig) { c.PanelURL = "ftp://panel.example.com" },
	}
	for name, mut := range bad {
		c := good
		mut(&c)
		if err := c.Validate(false); !errors.Is(err, ErrOIDCConfig) {
			t.Errorf("%s: err = %v, want ErrOIDCConfig", name, err)
		}
	}
	dev := good
	dev.IssuerURL = "http://localhost:8081"
	if err := dev.Validate(true); err != nil {
		t.Errorf("http issuer refused in development: %v", err)
	}
}

func TestSSOUsername(t *testing.T) {
	cases := map[string]oidcIdentity{
		"alice_w":          {Username: "alice.w"},
		"bob":              {Email: "bob@example.com"},
		"x__":              {Username: "x"},
		"a_very_long_name": {Username: "a.very.long.name.indeed"},
		"j_rgen":           {Username: "jürgen"},
	}
	for want, id := range cases {
		if got := ssoUsername(&id); got != want {
			t.Errorf("ssoUsername(%+v) = %q, want %q", id, got, want)
		}
		if !usernameRegex.MatchString(ssoUsername(&id)) {
			t.Errorf("ssoUsername(%+v) = %q does not pass the username rules", id, ssoUsername(&id))
		}
	}
}
