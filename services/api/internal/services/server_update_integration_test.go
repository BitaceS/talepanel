package services

import (
	"context"
	"errors"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/BitaceS/talepanel/api/internal/daemon"
	"github.com/BitaceS/talepanel/api/internal/models"
	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgxpool"
	"go.uber.org/zap"
)

// fakeUpdateDaemon plays the node: stop/start flip the status, a reinstall
// reports its provision result the way the real daemon does.
type fakeUpdateDaemon struct {
	svc       *ServerService
	result    string // provision result the reinstall reports
	mu        sync.Mutex
	calls     []string
	startedAs *daemon.ServerConfig
}

func (f *fakeUpdateDaemon) record(c string) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.calls = append(f.calls, c)
}

func (f *fakeUpdateDaemon) Calls() []string {
	f.mu.Lock()
	defer f.mu.Unlock()
	return append([]string(nil), f.calls...)
}

func (f *fakeUpdateDaemon) StopServer(ctx context.Context, id string) error {
	f.record("stop")
	return f.svc.UpdateServerStatus(ctx, uuid.MustParse(id), models.StatusStopped)
}

func (f *fakeUpdateDaemon) ReinstallServer(_ context.Context, id, _, _ string) error {
	f.record("reinstall")
	go func() {
		time.Sleep(20 * time.Millisecond)
		ctx := context.Background()
		sid := uuid.MustParse(id)
		_ = f.svc.RecordProvisionResult(ctx, sid, f.result)
		_ = f.svc.UpdateServerStatus(ctx, sid, models.StatusStopped)
	}()
	return nil
}

func (f *fakeUpdateDaemon) StartServer(ctx context.Context, cfg daemon.ServerConfig) error {
	f.record("start")
	f.mu.Lock()
	f.startedAs = &cfg
	f.mu.Unlock()
	return f.svc.UpdateServerStatus(ctx, uuid.MustParse(cfg.ServerID), models.StatusRunning)
}

type fakeUpdateBackups struct {
	fail    bool
	created int
	pruned  int
}

func (b *fakeUpdateBackups) CreateUpdateBackup(context.Context, uuid.UUID) (*models.Backup, error) {
	b.created++
	return &models.Backup{ID: uuid.New()}, nil
}

func (b *fakeUpdateBackups) BackupStatus(context.Context, uuid.UUID) (string, string, error) {
	if b.fail {
		return "failed", "disk full", nil
	}
	return "completed", "", nil
}

func (b *fakeUpdateBackups) PruneUpdateBackups(context.Context, uuid.UUID, int) error {
	b.pruned++
	return nil
}

func newTestUpdateService(t *testing.T, pool *pgxpool.Pool, provisionResult string) (*ServerUpdateService, *fakeUpdateDaemon, *fakeUpdateBackups) {
	t.Helper()
	maven := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch {
		case strings.HasPrefix(r.URL.Path, "/release/"):
			_, _ = w.Write([]byte(releaseMetadata)) // latest 0.6.8
		case strings.HasPrefix(r.URL.Path, "/pre-release/"):
			_, _ = w.Write([]byte(preReleaseMetadata)) // latest 0.7.0-pre.5.1
		default:
			http.NotFound(w, r)
		}
	}))
	t.Cleanup(maven.Close)
	old := updatePoll
	updatePoll = 10 * time.Millisecond
	t.Cleanup(func() { updatePoll = old })

	servers := NewServerService(pool)
	d := &fakeUpdateDaemon{svc: servers, result: provisionResult}
	b := &fakeUpdateBackups{}
	svc := &ServerUpdateService{
		servers:  servers,
		versions: NewHytaleVersionService(nil, maven.URL, zap.NewNop()),
		backups:  b,
		daemon:   func(string) (UpdateDaemon, error) { return d, nil },
		log:      zap.NewNop(),
		running:  map[uuid.UUID]bool{},
	}
	return svc, d, b
}

func setInstalled(t *testing.T, pool *pgxpool.Pool, serverID uuid.UUID, build, patchline, hytaleVersion string) {
	t.Helper()
	if _, err := pool.Exec(context.Background(),
		`UPDATE servers SET installed_build = NULLIF($2, ''), installed_patchline = NULLIF($3, ''), hytale_version = $4,
		        cpu_limit = COALESCE(cpu_limit, 100), ram_limit_mb = COALESCE(ram_limit_mb, 1024),
		        disk_limit_mb = COALESCE(disk_limit_mb, 10240), active_world = COALESCE(active_world, '')
		 WHERE id = $1`,
		serverID, build, patchline, hytaleVersion); err != nil {
		t.Fatalf("set installed: %v", err)
	}
}

func installedBuild(t *testing.T, pool *pgxpool.Pool, serverID uuid.UUID) string {
	t.Helper()
	var b *string
	if err := pool.QueryRow(context.Background(), `SELECT installed_build FROM servers WHERE id = $1`, serverID).Scan(&b); err != nil {
		t.Fatalf("read installed build: %v", err)
	}
	if b == nil {
		return ""
	}
	return *b
}

func TestServerUpdate_RunningServerIsBackedUpStoppedReinstalledAndStarted(t *testing.T) {
	pool := openTestDB(t)
	owner := seedTestUser(t, pool, models.RoleUser)
	sid := seedTestServer(t, pool, owner)
	setInstalled(t, pool, sid, "0.6.7", "release", "latest")
	setServerStatus(t, pool, sid, models.StatusRunning)
	svc, d, b := newTestUpdateService(t, pool, ProvisionDownloaded)

	status, msg := svc.Run(context.Background(), sid, 0)
	if status != models.TaskStatusOK {
		t.Fatalf("status = %s (%s), want ok", status, msg)
	}
	if got := strings.Join(d.Calls(), ","); got != "stop,reinstall,start" {
		t.Fatalf("daemon calls = %s", got)
	}
	if b.created != 1 || b.pruned != 1 {
		t.Fatalf("backups created %d pruned %d, want 1 and 1", b.created, b.pruned)
	}
	if got := installedBuild(t, pool, sid); got != "0.6.8" {
		t.Fatalf("installed build = %q, want 0.6.8", got)
	}
	if st, _ := NewServerService(pool).GetServerStatus(context.Background(), sid); st != models.StatusRunning {
		t.Fatalf("status after update = %s, want running", st)
	}
}

func TestServerUpdate_StoppedServerStaysStopped(t *testing.T) {
	pool := openTestDB(t)
	owner := seedTestUser(t, pool, models.RoleUser)
	sid := seedTestServer(t, pool, owner)
	setInstalled(t, pool, sid, "", "", "latest") // unknown build: update installs the newest
	svc, d, _ := newTestUpdateService(t, pool, ProvisionDownloaded)

	if status, msg := svc.Run(context.Background(), sid, 0); status != models.TaskStatusOK {
		t.Fatalf("status = %s (%s)", status, msg)
	}
	if got := strings.Join(d.Calls(), ","); got != "reinstall" {
		t.Fatalf("daemon calls = %s, want only the reinstall", got)
	}
	if got := installedBuild(t, pool, sid); got != "0.6.8" {
		t.Fatalf("installed build = %q", got)
	}
}

func TestServerUpdate_UpToDateIsSkipped(t *testing.T) {
	pool := openTestDB(t)
	owner := seedTestUser(t, pool, models.RoleUser)
	sid := seedTestServer(t, pool, owner)
	setInstalled(t, pool, sid, "0.6.8", "release", "latest")
	setServerStatus(t, pool, sid, models.StatusRunning)
	svc, d, b := newTestUpdateService(t, pool, ProvisionDownloaded)

	if status, _ := svc.Run(context.Background(), sid, 5); status != models.TaskStatusSkipped {
		t.Fatalf("status = %s, want skipped", status)
	}
	if len(d.Calls()) != 0 || b.created != 0 {
		t.Fatalf("an up-to-date server was touched: %v, %d backups", d.Calls(), b.created)
	}
}

func TestServerUpdate_FailedBackupLeavesServerAlone(t *testing.T) {
	pool := openTestDB(t)
	owner := seedTestUser(t, pool, models.RoleUser)
	sid := seedTestServer(t, pool, owner)
	setInstalled(t, pool, sid, "0.6.7", "release", "latest")
	setServerStatus(t, pool, sid, models.StatusRunning)
	svc, d, b := newTestUpdateService(t, pool, ProvisionDownloaded)
	b.fail = true

	status, msg := svc.Run(context.Background(), sid, 0)
	if status != models.TaskStatusFailed || !strings.Contains(msg, "backup") {
		t.Fatalf("status = %s (%s), want a backup failure", status, msg)
	}
	if len(d.Calls()) != 0 {
		t.Fatalf("server touched after a failed backup: %v", d.Calls())
	}
}

func TestServerUpdate_KeptFilesFailAndServerComesBack(t *testing.T) {
	pool := openTestDB(t)
	owner := seedTestUser(t, pool, models.RoleUser)
	sid := seedTestServer(t, pool, owner)
	setInstalled(t, pool, sid, "0.6.7", "release", "latest")
	setServerStatus(t, pool, sid, models.StatusRunning)
	svc, d, _ := newTestUpdateService(t, pool, ProvisionRestored)

	status, _ := svc.Run(context.Background(), sid, 0)
	if status != models.TaskStatusFailed {
		t.Fatalf("status = %s, want failed", status)
	}
	if got := installedBuild(t, pool, sid); got != "0.6.7" {
		t.Fatalf("installed build = %q, want the old 0.6.7", got)
	}
	if got := strings.Join(d.Calls(), ","); got != "stop,reinstall,start" {
		t.Fatalf("daemon calls = %s; the server must be started again on the old files", got)
	}
}

func TestServerUpdate_PreReleaseIsNotSwitchedBackToRelease(t *testing.T) {
	pool := openTestDB(t)
	owner := seedTestUser(t, pool, models.RoleUser)
	sid := seedTestServer(t, pool, owner)
	setInstalled(t, pool, sid, "0.7.0-pre.5.1", "pre-release", "latest")
	svc, d, _ := newTestUpdateService(t, pool, ProvisionDownloaded)

	server, err := NewServerService(pool).GetServerByID(context.Background(), sid)
	if err != nil {
		t.Fatal(err)
	}
	if info := svc.Info(context.Background(), server); !info.Blocked || info.Available {
		t.Fatalf("info = %+v, want blocked", info)
	}
	if _, err := svc.Begin(context.Background(), sid, 0); !errors.Is(err, ErrUpdateUnavailable) {
		t.Fatalf("Begin err = %v, want ErrUpdateUnavailable", err)
	}
	if len(d.Calls()) != 0 {
		t.Fatalf("daemon called: %v", d.Calls())
	}
}

func TestRecordProvisionResult_OnlyADownloadInstallsTheExpectedBuild(t *testing.T) {
	pool := openTestDB(t)
	ctx := context.Background()
	owner := seedTestUser(t, pool, models.RoleUser)
	sid := seedTestServer(t, pool, owner)
	svc := NewServerService(pool)

	setInstalled(t, pool, sid, "0.6.7", "release", "latest")
	for _, result := range []string{ProvisionKept, ProvisionRestored, ""} {
		if err := svc.SetInstallingBuild(ctx, sid, "release", "0.6.8"); err != nil {
			t.Fatal(err)
		}
		if err := svc.RecordProvisionResult(ctx, sid, result); err != nil {
			t.Fatal(err)
		}
		if got := installedBuild(t, pool, sid); got != "0.6.7" {
			t.Fatalf("result %q changed the installed build to %q", result, got)
		}
	}
	if err := svc.SetInstallingBuild(ctx, sid, "release", "0.6.8"); err != nil {
		t.Fatal(err)
	}
	if err := svc.RecordProvisionResult(ctx, sid, ProvisionDownloaded); err != nil {
		t.Fatal(err)
	}
	if got := installedBuild(t, pool, sid); got != "0.6.8" {
		t.Fatalf("installed build = %q after a download, want 0.6.8", got)
	}
	if err := svc.RecordProvisionResult(ctx, sid, ProvisionStub); err != nil {
		t.Fatal(err)
	}
	if got := installedBuild(t, pool, sid); got != "" {
		t.Fatalf("a stub install left installed build %q, want unknown", got)
	}
}

func TestUpdateTask_ValidatesAndRunsOnStoppedServers(t *testing.T) {
	pool := openTestDB(t)
	ctx := context.Background()
	svc, exec := newTestTaskService(t, pool)
	owner := seedTestUser(t, pool, models.RoleUser)
	sid := seedTestServer(t, pool, owner)
	user := &models.User{ID: owner, Role: models.RoleUser, IsActive: true}

	if _, err := svc.CreateTask(ctx, user, sid, CreateServerTaskRequest{Type: "update", CronExpr: "0 4 * * *",
		Payload: models.ServerTaskPayload{WarnMinutes: 16}}); !errors.Is(err, ErrInvalidTask) {
		t.Fatalf("warn 16: err = %v, want ErrInvalidTask", err)
	}
	task, err := svc.CreateTask(ctx, user, sid, CreateServerTaskRequest{Type: "update", CronExpr: "0 4 * * *",
		Payload: models.ServerTaskPayload{WarnMinutes: 5, Command: "dropped"}})
	if err != nil {
		t.Fatalf("create: %v", err)
	}
	if task.Payload.Command != "" || task.Payload.WarnMinutes != 5 {
		t.Fatalf("payload = %+v", task.Payload)
	}

	// The server is stopped: an update still runs (a restart would skip).
	makeDue(t, pool, task.ID)
	if _, err := svc.runDueTasks(ctx); err != nil {
		t.Fatal(err)
	}
	svc.delayed.Wait()
	got := reloadTask(t, svc, sid, task.ID)
	if got.LastStatus == nil || *got.LastStatus != models.TaskStatusOK {
		t.Fatalf("last status = %v, want ok", got.LastStatus)
	}
	exec.mu.Lock()
	defer exec.mu.Unlock()
	if len(exec.updates) != 1 || exec.updates[0] != 5 {
		t.Fatalf("updates = %v, want one with warn 5", exec.updates)
	}
}

func TestDecideUpdate(t *testing.T) {
	str := func(s string) *string { return &s }
	cases := []struct {
		name                       string
		info                       ServerUpdateInfo
		installedPatchline         *string
		wantAvailable, wantBlocked bool
	}{
		{"newer", ServerUpdateInfo{Patchline: "release", Installed: str("1"), Latest: "2"}, str("release"), true, false},
		{"same", ServerUpdateInfo{Patchline: "release", Installed: str("2"), Latest: "2"}, str("release"), false, false},
		{"unknown", ServerUpdateInfo{Patchline: "release", Latest: "2"}, nil, true, false},
		{"maven down", ServerUpdateInfo{Patchline: "release", Installed: str("1")}, str("release"), false, false},
		{"pre to release", ServerUpdateInfo{Patchline: "release", Installed: str("0.7-pre"), Latest: "0.6"}, str("pre-release"), false, true},
		{"release to pre", ServerUpdateInfo{Patchline: "pre-release", Installed: str("0.6"), Latest: "0.7-pre"}, str("release"), true, false},
	}
	for _, c := range cases {
		info := c.info
		decideUpdate(&info, c.installedPatchline)
		if info.Available != c.wantAvailable || info.Blocked != c.wantBlocked || info.Reason == "" {
			t.Errorf("%s: %+v", c.name, info)
		}
	}
}
