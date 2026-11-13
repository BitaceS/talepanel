package services

import (
	"context"
	"errors"
	"slices"
	"testing"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/BitaceS/talepanel/api/internal/models"
)

// Fixed IDs of the built-in templates seeded by migration 045.
var (
	builtinSurvivalSmall = uuid.MustParse("7a1c0e2e-0001-4a00-8000-000000000001")
	builtinLobby         = uuid.MustParse("7a1c0e2e-0001-4a00-8000-000000000004")
	builtinSandbox       = uuid.MustParse("7a1c0e2e-0001-4a00-8000-000000000005")
)

// seedOnlineNode inserts an online node CreateServer can place servers on and
// removes it, and every server created on it, on cleanup.
func seedOnlineNode(t *testing.T, pool *pgxpool.Pool, ramMB int) uuid.UUID {
	t.Helper()
	ctx := context.Background()
	nodeID := uuid.New()
	if _, err := pool.Exec(ctx,
		`INSERT INTO nodes (id, name, fqdn, port, total_cpu, total_ram_mb, total_disk_mb, max_servers, token_hash, status)
		 VALUES ($1,$2,$3,$4,4,$5,102400,50,$6,'online')`,
		nodeID, "n"+nodeID.String()[:8], "daemon", 8444, ramMB, nodeID.String()); err != nil {
		t.Fatalf("seed node: %v", err)
	}
	t.Cleanup(func() {
		_, _ = pool.Exec(ctx, `DELETE FROM servers WHERE node_id = $1`, nodeID)
		_, _ = pool.Exec(ctx, `DELETE FROM nodes WHERE id = $1`, nodeID)
	})
	return nodeID
}

func loadUser(t *testing.T, pool *pgxpool.Pool, id uuid.UUID) *models.User {
	t.Helper()
	u := &models.User{ID: id}
	if err := pool.QueryRow(context.Background(), `SELECT role FROM users WHERE id = $1`, id).Scan(&u.Role); err != nil {
		t.Fatalf("load user: %v", err)
	}
	return u
}

func seedTemplate(t *testing.T, svc *ServerTemplateService, adminID uuid.UUID, settings ServerTemplateSettings) *ServerTemplate {
	t.Helper()
	tmpl, err := svc.Create(context.Background(), adminID, CreateServerTemplateRequest{
		Name: "test " + uuid.NewString()[:8], Icon: "dns", Settings: settings,
	})
	if err != nil {
		t.Fatalf("create template: %v", err)
	}
	t.Cleanup(func() { _ = svc.Delete(context.Background(), tmpl.ID) })
	return tmpl
}

func TestServerTemplates_BuiltinsAreSeeded(t *testing.T) {
	pool := openTestDB(t)
	svc := NewServerTemplateService(pool)

	list, err := svc.List(context.Background())
	if err != nil {
		t.Fatalf("list: %v", err)
	}
	builtins := 0
	for _, tmpl := range list {
		if tmpl.IsBuiltin {
			builtins++
		}
	}
	if builtins != 5 {
		t.Fatalf("built-in templates = %d, want 5", builtins)
	}
	if !list[0].IsBuiltin {
		t.Fatal("built-in templates are not listed first")
	}

	small, err := svc.Get(context.Background(), builtinSurvivalSmall)
	if err != nil {
		t.Fatalf("get survival template: %v", err)
	}
	if deref(small.Settings.RAMLimitMB, 0) != 3072 || !deref(small.Settings.AutoRestart, false) {
		t.Fatalf("survival settings = %+v", small.Settings)
	}
	lobby, _ := svc.Get(context.Background(), builtinLobby)
	if deref(lobby.Settings.NetworkRole, "") != "hub" {
		t.Fatalf("lobby network_role = %v, want hub", lobby.Settings.NetworkRole)
	}
	sandbox, _ := svc.Get(context.Background(), builtinSandbox)
	if deref(sandbox.Settings.HytaleVersion, "") != "pre-release" ||
		!slices.Equal(deref(sandbox.Settings.LaunchFlags, nil), []string{"--accept-early-plugins"}) {
		t.Fatalf("sandbox settings = %+v", sandbox.Settings)
	}
}

func TestCreateServer_TemplateDefaultsApply(t *testing.T) {
	pool := openTestDB(t)
	ctx := context.Background()
	svc := NewServerService(pool)

	user := loadUser(t, pool, seedTestUser(t, pool, models.RoleUser))
	seedOnlineNode(t, pool, 8192)

	srv, err := svc.CreateServer(ctx, CreateServerRequest{Name: "from template", TemplateID: &builtinSurvivalSmall}, user)
	if err != nil {
		t.Fatalf("create from template: %v", err)
	}
	if srv.RAMLimitMB != 3072 || srv.CPULimit != 200 || srv.DiskLimitMB != 10240 ||
		!srv.AutoRestart || srv.HytaleVersion != "release" || srv.CrashLimit != 3 || srv.CrashWindowS != 300 {
		t.Fatalf("server = %+v, want the survival template's settings", srv)
	}
	if srv.OwnerID != user.ID || srv.Status != models.StatusInstalling {
		t.Fatalf("owner/status = %v/%s", srv.OwnerID, srv.Status)
	}

	// --accept-early-plugins is not admin-only, so any creator gets it.
	sandbox, err := svc.CreateServer(ctx, CreateServerRequest{Name: "sandbox", TemplateID: &builtinSandbox}, user)
	if err != nil {
		t.Fatalf("create from sandbox template: %v", err)
	}
	if sandbox.HytaleVersion != "pre-release" || !slices.Equal(sandbox.LaunchFlags, []string{"--accept-early-plugins"}) || sandbox.AutoRestart {
		t.Fatalf("sandbox server = %+v", sandbox)
	}

	// Without a template the old defaults still apply.
	plain, err := svc.CreateServer(ctx, CreateServerRequest{Name: "plain"}, user)
	if err != nil {
		t.Fatalf("create without template: %v", err)
	}
	if plain.RAMLimitMB != DefaultServerRAMMB || plain.DiskLimitMB != DefaultServerDiskMB || plain.AutoRestart ||
		plain.HytaleVersion != "latest" || plain.CrashLimit != 3 || plain.CrashWindowS != 300 {
		t.Fatalf("plain server = %+v", plain)
	}

	missing := uuid.New()
	if _, err := svc.CreateServer(ctx, CreateServerRequest{Name: "x", TemplateID: &missing}, user); !errors.Is(err, ErrServerTemplateNotFound) {
		t.Fatalf("unknown template: err = %v, want ErrServerTemplateNotFound", err)
	}
}

func TestCreateServer_ExplicitFieldsOverrideTemplate(t *testing.T) {
	pool := openTestDB(t)
	ctx := context.Background()
	svc := NewServerService(pool)

	user := loadUser(t, pool, seedTestUser(t, pool, models.RoleUser))
	seedOnlineNode(t, pool, 8192)

	srv, err := svc.CreateServer(ctx, CreateServerRequest{
		Name:          "override",
		TemplateID:    &builtinSurvivalSmall,
		RAMLimitMB:    ptr(4096),
		CPULimit:      ptr(0), // explicit "unlimited" beats the template's 200
		AutoRestart:   ptr(false),
		HytaleVersion: ptr("pre-release"),
	}, user)
	if err != nil {
		t.Fatalf("create: %v", err)
	}
	if srv.RAMLimitMB != 4096 || srv.CPULimit != 0 || srv.AutoRestart || srv.HytaleVersion != "pre-release" {
		t.Fatalf("server = %+v, want the explicit values", srv)
	}
	// Fields the request left out still come from the template.
	if srv.DiskLimitMB != 10240 || srv.CrashLimit != 3 {
		t.Fatalf("disk/crash = %d/%d, want the template's", srv.DiskLimitMB, srv.CrashLimit)
	}
}

// A template is applied before validation, so it cannot give a creator what
// they could not request by hand.
func TestCreateServer_TemplateCannotEscalate(t *testing.T) {
	pool := openTestDB(t)
	ctx := context.Background()
	svc := NewServerService(pool)
	tsvc := NewServerTemplateService(pool)

	adminID := seedTestUser(t, pool, models.RoleAdmin)
	admin := loadUser(t, pool, adminID)
	user := loadUser(t, pool, seedTestUser(t, pool, models.RoleUser))
	seedOnlineNode(t, pool, 8192)

	allowOp := seedTemplate(t, tsvc, adminID, ServerTemplateSettings{LaunchFlags: ptr([]string{"--allow-op"})})
	if _, err := svc.CreateServer(ctx, CreateServerRequest{Name: "op", TemplateID: &allowOp.ID}, user); !errors.Is(err, ErrServerForbidden) {
		t.Fatalf("user with --allow-op template: err = %v, want ErrServerForbidden", err)
	}
	if _, err := svc.CreateServer(ctx, CreateServerRequest{Name: "op", LaunchFlags: ptr([]string{"--allow-op"})}, user); !errors.Is(err, ErrServerForbidden) {
		t.Fatalf("user with explicit --allow-op: err = %v, want ErrServerForbidden", err)
	}
	// An explicit flag list overrides the template's and is then allowed.
	if _, err := svc.CreateServer(ctx, CreateServerRequest{Name: "op", TemplateID: &allowOp.ID, LaunchFlags: ptr([]string{})}, user); err != nil {
		t.Fatalf("user overriding the template's flags: %v", err)
	}
	adminSrv, err := svc.CreateServer(ctx, CreateServerRequest{Name: "op", TemplateID: &allowOp.ID}, admin)
	if err != nil {
		t.Fatalf("admin with --allow-op template: %v", err)
	}
	if !slices.Contains(adminSrv.LaunchFlags, "--allow-op") {
		t.Fatalf("admin server flags = %v", adminSrv.LaunchFlags)
	}

	// The lobby template sets a network role; a plain user lacks network.manage.
	if _, err := svc.CreateServer(ctx, CreateServerRequest{Name: "hub", TemplateID: &builtinLobby}, user); !errors.Is(err, ErrServerForbidden) {
		t.Fatalf("user with hub template: err = %v, want ErrServerForbidden", err)
	}
	noRole, err := svc.CreateServer(ctx, CreateServerRequest{Name: "hub", TemplateID: &builtinLobby, NetworkRole: ptr("none")}, user)
	if err != nil {
		t.Fatalf("user with hub template and network_role none: %v", err)
	}
	if noRole.NetworkRole != nil {
		t.Fatalf("network_role = %v, want none", *noRole.NetworkRole)
	}
	hub, err := svc.CreateServer(ctx, CreateServerRequest{Name: "hub", TemplateID: &builtinLobby}, admin)
	if err != nil {
		t.Fatalf("admin with hub template: %v", err)
	}
	if hub.NetworkRole == nil || *hub.NetworkRole != "hub" {
		t.Fatalf("admin hub network_role = %v", hub.NetworkRole)
	}

	// RAM beyond the node is refused for everyone, template or not.
	bigRAM := seedTemplate(t, tsvc, adminID, ServerTemplateSettings{RAMLimitMB: ptr(16384)})
	for _, who := range []*models.User{user, admin} {
		if _, err := svc.CreateServer(ctx, CreateServerRequest{Name: "big", TemplateID: &bigRAM.ID}, who); !errors.Is(err, ErrInvalidServerSettings) {
			t.Fatalf("%s with 16 GB template on an 8 GB node: err = %v, want ErrInvalidServerSettings", who.Role, err)
		}
	}
	if _, err := svc.CreateServer(ctx, CreateServerRequest{Name: "big", RAMLimitMB: ptr(16384)}, user); !errors.Is(err, ErrInvalidServerSettings) {
		t.Fatalf("explicit 16 GB on an 8 GB node: err = %v, want ErrInvalidServerSettings", err)
	}
}

func TestServerTemplates_BuiltinsAreReadOnly(t *testing.T) {
	pool := openTestDB(t)
	ctx := context.Background()
	svc := NewServerTemplateService(pool)

	if _, err := svc.Update(ctx, builtinSurvivalSmall, UpdateServerTemplateRequest{Name: ptr("renamed")}); !errors.Is(err, ErrServerTemplateBuiltin) {
		t.Fatalf("update built-in: err = %v, want ErrServerTemplateBuiltin", err)
	}
	if err := svc.Delete(ctx, builtinSurvivalSmall); !errors.Is(err, ErrServerTemplateBuiltin) {
		t.Fatalf("delete built-in: err = %v, want ErrServerTemplateBuiltin", err)
	}
	got, err := svc.Get(ctx, builtinSurvivalSmall)
	if err != nil || got.Name != "Survival (small, 2–10 players)" {
		t.Fatalf("built-in after refused edits = %+v, %v", got, err)
	}
}

func TestServerTemplates_CustomCRUD(t *testing.T) {
	pool := openTestDB(t)
	ctx := context.Background()
	svc := NewServerTemplateService(pool)
	adminID := seedTestUser(t, pool, models.RoleAdmin)

	bad := []ServerTemplateSettings{
		{LaunchFlags: ptr([]string{"--universe=/etc"})},
		{RAMLimitMB: ptr(128)},
		{NetworkRole: ptr("proxy")},
		{HytaleVersion: ptr("1.2.3")},
		{CrashWindowS: ptr(0)},
	}
	for _, s := range bad {
		if _, err := svc.Create(ctx, adminID, CreateServerTemplateRequest{Name: "bad", Settings: s}); !errors.Is(err, ErrInvalidServerTemplate) {
			t.Fatalf("settings %+v: err = %v, want ErrInvalidServerTemplate", s, err)
		}
	}
	if _, err := svc.Create(ctx, adminID, CreateServerTemplateRequest{Name: " "}); !errors.Is(err, ErrInvalidServerTemplate) {
		t.Fatalf("empty name: err = %v", err)
	}
	if _, err := svc.Create(ctx, adminID, CreateServerTemplateRequest{Name: "x", Icon: "<script>"}); !errors.Is(err, ErrInvalidServerTemplate) {
		t.Fatalf("bad icon: err = %v", err)
	}

	tmpl := seedTemplate(t, svc, adminID, ServerTemplateSettings{RAMLimitMB: ptr(1024), NetworkRole: ptr("none")})
	if tmpl.IsBuiltin || tmpl.CreatedBy == nil || *tmpl.CreatedBy != adminID {
		t.Fatalf("custom template = %+v", tmpl)
	}
	if tmpl.Settings.NetworkRole != nil {
		t.Fatalf("network_role none stored as %v", *tmpl.Settings.NetworkRole)
	}
	upd, err := svc.Update(ctx, tmpl.ID, UpdateServerTemplateRequest{
		Description: ptr("bigger"),
		Settings:    &ServerTemplateSettings{RAMLimitMB: ptr(2048)},
	})
	if err != nil {
		t.Fatalf("update: %v", err)
	}
	if upd.Description != "bigger" || deref(upd.Settings.RAMLimitMB, 0) != 2048 || upd.Name != tmpl.Name {
		t.Fatalf("updated = %+v", upd)
	}
	if err := svc.Delete(ctx, tmpl.ID); err != nil {
		t.Fatalf("delete: %v", err)
	}
	if _, err := svc.Get(ctx, tmpl.ID); !errors.Is(err, ErrServerTemplateNotFound) {
		t.Fatalf("get after delete: err = %v", err)
	}
}

func TestServerTemplates_CreateFromServer(t *testing.T) {
	pool := openTestDB(t)
	ctx := context.Background()
	svc := NewServerTemplateService(pool)

	adminID := seedTestUser(t, pool, models.RoleAdmin)
	ownerID := seedTestUser(t, pool, models.RoleUser)
	serverID := seedServerWithLimits(t, pool, ownerID)
	if _, err := pool.Exec(ctx, `
		UPDATE servers SET hytale_version = 'pre-release', auto_restart = true, crash_limit = 5,
		       crash_window_s = 120, launch_flags = '{--accept-early-plugins,--allow-op}', network_role = 'gamemode'
		WHERE id = $1`, serverID); err != nil {
		t.Fatalf("configure server: %v", err)
	}

	tmpl, err := svc.CreateFromServer(ctx, adminID, serverID, TemplateFromServerRequest{Name: "Saved", Icon: "save"})
	if err != nil {
		t.Fatalf("from server: %v", err)
	}
	t.Cleanup(func() { _ = svc.Delete(ctx, tmpl.ID) })

	s := tmpl.Settings
	if deref(s.HytaleVersion, "") != "pre-release" || deref(s.RAMLimitMB, 0) != 512 || deref(s.CPULimit, -1) != 100 ||
		deref(s.DiskLimitMB, 0) != 1024 || !deref(s.AutoRestart, false) || deref(s.CrashLimit, 0) != 5 ||
		deref(s.CrashWindowS, 0) != 120 || deref(s.NetworkRole, "") != "gamemode" ||
		!slices.Equal(deref(s.LaunchFlags, nil), []string{"--accept-early-plugins", "--allow-op"}) {
		t.Fatalf("copied settings = %+v", s)
	}
	if tmpl.IsBuiltin || tmpl.Name != "Saved" {
		t.Fatalf("template = %+v", tmpl)
	}

	if _, err := svc.CreateFromServer(ctx, adminID, uuid.New(), TemplateFromServerRequest{Name: "x"}); !errors.Is(err, ErrServerNotFound) {
		t.Fatalf("unknown server: err = %v, want ErrServerNotFound", err)
	}
}
