package services

import (
	"context"
	"errors"
	"strings"
	"testing"

	"github.com/BitaceS/talepanel/api/internal/models"
	"github.com/google/uuid"
)

func TestAPIKeyCreateValidation(t *testing.T) {
	pool := openTestDB(t)
	ctx := context.Background()
	perms := NewPermissionService(pool)
	svc := NewAPIKeyService(pool, perms)
	ownerID := seedTestUser(t, pool, models.RoleUser)
	otherID := seedTestUser(t, pool, models.RoleUser)
	own := seedTestServer(t, pool, ownerID)
	foreign := seedTestServer(t, pool, otherID)
	user := &models.User{ID: ownerID, Role: models.RoleUser, IsActive: true}

	bad := []CreateAPIKeyRequest{
		{Name: ""},
		{Name: strings.Repeat("x", 65)},
		{Name: "k", Scopes: []string{"admin"}},
		{Name: "k", ExpiresInDays: -1},
		{Name: "k", ServerIDs: &[]uuid.UUID{}},
		{Name: "k", ServerIDs: &[]uuid.UUID{foreign}}, // not the user's server
		{Name: "k", ServerIDs: &[]uuid.UUID{uuid.New()}},
	}
	for i, req := range bad {
		if _, _, err := svc.Create(ctx, user, req); !errors.Is(err, ErrInvalidAPIKey) {
			t.Errorf("case %d: err = %v, want ErrInvalidAPIKey", i, err)
		}
	}

	k, key, err := svc.Create(ctx, user, CreateAPIKeyRequest{
		Name: " bot ", Scopes: []string{models.ScopeConsole, models.ScopePower, models.ScopePower},
		ServerIDs: &[]uuid.UUID{own, own}, ExpiresInDays: 30,
	})
	if err != nil {
		t.Fatalf("create: %v", err)
	}
	if !strings.HasPrefix(key, models.APIKeyPrefix) || len(key) < 40 || !strings.HasPrefix(key, k.Prefix) {
		t.Fatalf("key %q / prefix %q", key, k.Prefix)
	}
	if k.Name != "bot" || strings.Join(k.Scopes, ",") != "power,console" || len(k.ServerIDs) != 1 || k.ExpiresAt == nil {
		t.Fatalf("stored %+v", k)
	}
	var hash string
	if err := pool.QueryRow(ctx, `SELECT key_hash FROM api_keys WHERE id = $1`, k.ID).Scan(&hash); err != nil {
		t.Fatal(err)
	}
	if hash != models.HashAPIKey(key) || strings.Contains(hash, key) {
		t.Fatal("the key must be stored only as its SHA-256 hash")
	}

	// A key cannot create keys, and the list never contains the key.
	keyUser := *user
	keyUser.APIKey = &models.APIKeyAuth{ID: k.ID}
	if _, _, err := svc.Create(ctx, &keyUser, CreateAPIKeyRequest{Name: "x"}); !errors.Is(err, ErrInvalidAPIKey) {
		t.Fatalf("key minting a key: err = %v", err)
	}
	list, err := svc.List(ctx, ownerID)
	if err != nil || len(list) != 1 || list[0].ID != k.ID {
		t.Fatalf("list = %v, %v", list, err)
	}
	if err := svc.Delete(ctx, otherID, k.ID); !errors.Is(err, ErrAPIKeyNotFound) {
		t.Fatalf("deleting someone else's key: err = %v", err)
	}
	if err := svc.Delete(ctx, ownerID, k.ID); err != nil {
		t.Fatal(err)
	}
}

func TestAPIKeyLimitsPermissions(t *testing.T) {
	pool := openTestDB(t)
	ctx := context.Background()
	perms := NewPermissionService(pool)
	ownerID := seedTestUser(t, pool, models.RoleOwner)
	a := seedTestServer(t, pool, ownerID)
	b := seedTestServer(t, pool, ownerID)
	// The panel owner passes every check, except through a limited key.
	user := &models.User{ID: ownerID, Role: models.RoleOwner, IsActive: true,
		APIKey: &models.APIKeyAuth{Scopes: []string{models.ScopePower}, ServerIDs: []uuid.UUID{a}}}

	check := func(server uuid.UUID, perm string, want bool) {
		t.Helper()
		got, err := perms.HasServerPermission(ctx, user, server, perm)
		if err != nil || got != want {
			t.Errorf("%s on %s = %v (%v), want %v", perm, server, got, err, want)
		}
	}
	check(a, "server.view", true)
	check(a, "server.start", true)
	check(a, "server.stop", true)
	check(a, "server.console", false)
	check(a, "server.files", false)
	check(a, "server.delete", false)
	check(b, "server.view", false)
	if ok, _ := perms.HasPermission(ctx, user, "admin.users"); ok {
		t.Error("a key got admin.users")
	}
}
