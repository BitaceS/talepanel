package services

import (
	"context"
	"errors"
	"testing"

	"github.com/BitaceS/talepanel/api/internal/models"
)

func TestValidateModDownloadURL(t *testing.T) {
	cf := "https://edge.forgecdn.net/files/1234/567/mod.jar"
	cases := []struct {
		url, role string
		ok        bool
	}{
		{cf, models.RoleUser, true},
		{"https://mediafilez.forgecdn.net/files/1/2/m.jar", models.RoleUser, true},
		{"https://example.com/mod.jar", models.RoleUser, false},
		{"https://edge.forgecdn.net.evil.com/m.jar", models.RoleUser, false},
		{"https://edge.forgecdn.net:8443/m.jar", models.RoleUser, false},
		{"http://edge.forgecdn.net/m.jar", models.RoleUser, false},
		{"https://user@edge.forgecdn.net/m.jar", models.RoleUser, false},
		{"http://169.254.169.254/latest", models.RoleModerator, false},
		{"file:///etc/passwd", models.RoleUser, false},
		{"https://example.com/mod.jar", models.RoleAdmin, true},
		{"https://example.com/mod.jar", models.RoleOwner, true},
		{"http://example.com/mod.jar", models.RoleAdmin, false},
	}
	for _, tc := range cases {
		err := ValidateModDownloadURL(tc.url, tc.role)
		if (err == nil) != tc.ok {
			t.Errorf("ValidateModDownloadURL(%q, %s) err=%v, want ok=%v", tc.url, tc.role, err, tc.ok)
		}
		if err != nil && !errors.Is(err, ErrModURLNotAllowed) {
			t.Errorf("unexpected error type: %v", err)
		}
	}
}

// The check runs before anything is stored or enqueued.
func TestInstallModRejectsForeignURLForUser(t *testing.T) {
	pool := openTestDB(t)
	ctx := context.Background()
	svc := NewModService(pool)

	owner := seedTestUser(t, pool, models.RoleUser)
	serverID := seedTestServer(t, pool, owner)
	t.Cleanup(func() {
		_, _ = pool.Exec(context.Background(), `DELETE FROM node_commands WHERE server_id = $1`, serverID)
	})

	_, err := svc.InstallMod(ctx, models.RoleUser, serverID, InstallModRequest{
		Filename: "x.jar", DownloadURL: "https://attacker.example/x.jar",
	})
	if !errors.Is(err, ErrModURLNotAllowed) {
		t.Fatalf("InstallMod err=%v, want ErrModURLNotAllowed", err)
	}
	var n int
	if err := pool.QueryRow(ctx, `SELECT COUNT(*) FROM server_mods WHERE server_id = $1`, serverID).Scan(&n); err != nil {
		t.Fatal(err)
	}
	if n != 0 {
		t.Fatalf("%d mod rows stored for a rejected install", n)
	}
	if err := svc.SwitchModVersion(ctx, models.RoleUser, serverID, "old.jar",
		ModVersionSwitchRequest{FileURL: "https://attacker.example/x.jar"}); !errors.Is(err, ErrModURLNotAllowed) {
		t.Fatalf("SwitchModVersion err=%v, want ErrModURLNotAllowed", err)
	}

	if _, err := svc.InstallMod(ctx, models.RoleUser, serverID, InstallModRequest{
		Filename: "mod.jar", DownloadURL: "https://edge.forgecdn.net/files/1/2/mod.jar",
	}); err != nil {
		t.Fatalf("CurseForge install: %v", err)
	}
}
