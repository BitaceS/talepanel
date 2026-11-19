package services

import (
	"context"
	"errors"
	"fmt"
	"sync"
	"time"

	"github.com/BitaceS/talepanel/api/internal/daemon"
	"github.com/BitaceS/talepanel/api/internal/models"
	"github.com/google/uuid"
	"go.uber.org/zap"
)

// Errors of ServerUpdateService.Begin; the handler maps them to 409.
var (
	ErrUpdateRunning     = errors.New("an update of this server is already running")
	ErrUpdateUnavailable = errors.New("no update available")
	ErrServerBusy        = errors.New("the server is busy (starting, stopping or installing); try again when it has settled")
)

const (
	// keepUpdateBackups is how many pre-update backups are kept per server.
	keepUpdateBackups = 3
	updateStopTimeout = 3 * time.Minute
	// updateInstallTimeout covers the download of a server build.
	updateInstallTimeout = 30 * time.Minute
	updateBackupTimeout  = 60 * time.Minute
	updatePollInterval   = 2 * time.Second
)

// updatePoll is how often an update checks status; shortened in tests.
var updatePoll = updatePollInterval

// ServerUpdateInfo is what GET /servers/:id/update reports.
type ServerUpdateInfo struct {
	// Patchline the server installs from ("release" or "pre-release").
	Patchline string `json:"patchline"`
	// Installed build, nil when unknown.
	Installed *string `json:"installed"`
	// Newest build of the patchline, "" when Maven could not be reached.
	Latest string `json:"latest"`
	// Available: an update would install another build.
	Available bool `json:"available"`
	// Reason explains Available (or why no update is possible).
	Reason string `json:"reason"`
	// Blocked: an update would be refused (e.g. pre-release → release).
	Blocked bool `json:"blocked"`
	// Running: an update of this server is in progress.
	Running bool `json:"running"`
}

// UpdateDaemon is the part of the node daemon client an update uses.
type UpdateDaemon interface {
	StopServer(ctx context.Context, serverID string) error
	ReinstallServer(ctx context.Context, serverID, version, dataPath string) error
	StartServer(ctx context.Context, cfg daemon.ServerConfig) error
}

// UpdateBackups is the part of BackupService an update uses.
type UpdateBackups interface {
	CreateUpdateBackup(ctx context.Context, serverID uuid.UUID) (*models.Backup, error)
	BackupStatus(ctx context.Context, backupID uuid.UUID) (string, string, error)
	PruneUpdateBackups(ctx context.Context, serverID uuid.UUID, keep int) error
}

// ServerUpdateService updates a server to the newest build of its patchline:
// warn players, back up, stop, reinstall, start again if it was running.
type ServerUpdateService struct {
	servers  *ServerService
	versions *HytaleVersionService
	backups  UpdateBackups
	daemon   func(nodeID string) (UpdateDaemon, error)
	log      *zap.Logger

	mu      sync.Mutex
	running map[uuid.UUID]bool
}

func NewServerUpdateService(servers *ServerService, versions *HytaleVersionService, backups *BackupService, daemons *daemon.ClientPool, log *zap.Logger) *ServerUpdateService {
	if log == nil {
		log = zap.NewNop()
	}
	return &ServerUpdateService{
		servers:  servers,
		versions: versions,
		backups:  backups,
		daemon: func(nodeID string) (UpdateDaemon, error) {
			return daemons.Get(nodeID)
		},
		log:     log,
		running: map[uuid.UUID]bool{},
	}
}

// latestBuild returns the newest build of a patchline, "" when unknown.
func (s *ServerUpdateService) latestBuild(ctx context.Context, patchline string) string {
	for _, p := range s.versions.Versions(ctx).Patchlines {
		if p.Patchline == patchline {
			return p.Latest
		}
	}
	return ""
}

// Info compares the server's installed build with its patchline's newest.
func (s *ServerUpdateService) Info(ctx context.Context, server *models.Server) *ServerUpdateInfo {
	info := &ServerUpdateInfo{
		Patchline: models.HytalePatchline(server.HytaleVersion),
		Installed: server.InstalledBuild,
	}
	s.mu.Lock()
	info.Running = s.running[server.ID]
	s.mu.Unlock()
	info.Latest = s.latestBuild(ctx, info.Patchline)
	decideUpdate(info, server.InstalledPatchline)
	return info
}

// decideUpdate fills Available, Blocked and Reason.
func decideUpdate(info *ServerUpdateInfo, installedPatchline *string) {
	switch {
	case info.Latest == "":
		info.Reason = "could not check the newest build (Hytale's update server is unreachable)"
	case installedPatchline != nil && *installedPatchline == "pre-release" && info.Patchline == "release":
		info.Blocked = true
		info.Reason = "the server runs a pre-release build; release servers cannot open pre-release worlds, so it is not switched back automatically"
	case info.Installed == nil:
		info.Available = true
		info.Reason = "installed build unknown; updating installs " + info.Latest
	case *info.Installed != info.Latest:
		info.Available = true
		info.Reason = fmt.Sprintf("build %s is available (installed: %s)", info.Latest, *info.Installed)
	default:
		info.Reason = "up to date"
	}
}

// ExpectBuild records the newest build of the server's patchline as the
// build a provisioning run dispatched now will install, so the daemon's
// "downloaded" report can be matched to it. Best effort: without a known
// build the installed build stays unknown.
func (s *ServerUpdateService) ExpectBuild(ctx context.Context, server *models.Server) {
	patchline := models.HytalePatchline(server.HytaleVersion)
	build := s.latestBuild(ctx, patchline)
	if err := s.servers.SetInstallingBuild(ctx, server.ID, patchline, build); err != nil {
		s.log.Warn("recording the expected build failed", zap.String("server_id", server.ID.String()), zap.Error(err))
	}
}

// staleInstallAfter is how long a server may stay "installing" before it is
// treated as an install that will never report back (API restarted mid-update,
// daemon lost the job). Longer than any download the panel waits for.
const staleInstallAfter = 60 * time.Minute

// ReleaseStaleInstalls sets servers stuck in "installing" for longer than
// staleInstallAfter back to "stopped" and forgets their expected build, so
// they can be started or reinstalled again. Servers with an update running
// in this process are left alone. Returns how many were released.
func (s *ServerUpdateService) ReleaseStaleInstalls(ctx context.Context) (int, error) {
	s.mu.Lock()
	busy := make([]uuid.UUID, 0, len(s.running))
	for id := range s.running {
		busy = append(busy, id)
	}
	s.mu.Unlock()

	ids, err := s.servers.releaseStaleInstalls(ctx, staleInstallAfter, busy)
	if err != nil {
		return 0, err
	}
	for _, id := range ids {
		s.servers.PanelLog(ctx, id, "WARN", fmt.Sprintf(
			"The install did not report back within %s; the server is set to stopped. Start it to check the files, or reinstall.",
			staleInstallAfter))
		s.log.Warn("released a server stuck in installing", zap.String("server_id", id.String()))
	}
	return len(ids), nil
}

// RunStaleInstallSweeper calls ReleaseStaleInstalls at start and every 10
// minutes until ctx is cancelled.
func (s *ServerUpdateService) RunStaleInstallSweeper(ctx context.Context) {
	ticker := time.NewTicker(10 * time.Minute)
	defer ticker.Stop()
	for {
		if _, err := s.ReleaseStaleInstalls(ctx); err != nil && ctx.Err() == nil {
			s.log.Warn("releasing stale installs failed", zap.Error(err))
		}
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
		}
	}
}

// claim marks an update of serverID as running; false if one already is.
func (s *ServerUpdateService) claim(serverID uuid.UUID) bool {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.running[serverID] {
		return false
	}
	s.running[serverID] = true
	return true
}

func (s *ServerUpdateService) release(serverID uuid.UUID) {
	s.mu.Lock()
	defer s.mu.Unlock()
	delete(s.running, serverID)
}

// Begin checks that an update can start now and runs it in the background
// (the panel's "Update now"). The result is written to the server's console.
func (s *ServerUpdateService) Begin(ctx context.Context, serverID uuid.UUID, warnMinutes int) (*ServerUpdateInfo, error) {
	server, err := s.servers.GetServerByID(ctx, serverID)
	if err != nil {
		return nil, err
	}
	info := s.Info(ctx, server)
	if info.Running {
		return info, ErrUpdateRunning
	}
	if !info.Available {
		return info, fmt.Errorf("%w: %s", ErrUpdateUnavailable, info.Reason)
	}
	if !settled(server.Status) {
		return info, ErrServerBusy
	}
	if !s.claim(serverID) {
		return info, ErrUpdateRunning
	}
	go func() {
		defer s.release(serverID)
		status, msg := s.run(context.Background(), serverID, warnMinutes)
		s.log.Info("server update finished", zap.String("server_id", serverID.String()),
			zap.String("status", status), zap.String("message", msg))
	}()
	info.Running = true
	return info, nil
}

// Run performs an update now and returns a task status and message: skipped
// when there is nothing to do, ok when the new build is installed, failed
// otherwise. For scheduled update tasks; blocks for the whole update.
func (s *ServerUpdateService) Run(ctx context.Context, serverID uuid.UUID, warnMinutes int) (string, string) {
	if !s.claim(serverID) {
		return models.TaskStatusSkipped, "skipped: an update of this server is already running"
	}
	defer s.release(serverID)
	return s.run(ctx, serverID, warnMinutes)
}

// settled reports whether a server is in a state an update may start from.
func settled(status string) bool {
	switch status {
	case models.StatusRunning, models.StatusStopped, models.StatusCrashed:
		return true
	}
	return false
}

func updateWarning(warnMinutes int) string {
	unit := "minutes"
	if warnMinutes == 1 {
		unit = "minute"
	}
	return fmt.Sprintf("say Server restarts for an update in %d %s", warnMinutes, unit)
}

func (s *ServerUpdateService) run(ctx context.Context, serverID uuid.UUID, warnMinutes int) (string, string) {
	server, err := s.servers.GetServerByID(ctx, serverID)
	if err != nil {
		return models.TaskStatusFailed, "loading server: " + err.Error()
	}
	info := s.Info(ctx, server)
	if info.Blocked || !info.Available {
		if info.Latest == "" {
			return models.TaskStatusFailed, info.Reason
		}
		return models.TaskStatusSkipped, "skipped: " + info.Reason
	}
	if !settled(server.Status) {
		return models.TaskStatusSkipped, "skipped: server is " + server.Status
	}
	target := info.Latest
	logf := func(level, format string, args ...any) {
		lctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()
		s.servers.PanelLog(lctx, serverID, level, fmt.Sprintf(format, args...))
	}
	fail := func(format string, args ...any) (string, string) {
		msg := fmt.Sprintf(format, args...)
		logf("ERROR", "Update failed: %s", msg)
		return models.TaskStatusFailed, msg
	}

	client, err := s.daemon(server.NodeID.String())
	if err != nil {
		return fail("node daemon unavailable: %v", err)
	}
	wasRunning := server.Status == models.StatusRunning
	logf("INFO", "Update to Hytale %s (%s) started", target, info.Patchline)

	// 1. Warn the players and wait.
	if wasRunning && warnMinutes > 0 {
		if err := s.servers.SendConsoleCommand(ctx, serverID, updateWarning(warnMinutes)); err != nil {
			logf("WARN", "Could not warn the players: %v", err)
		}
		select {
		case <-ctx.Done():
			return fail("interrupted: the panel shut down before the update")
		case <-time.After(time.Duration(warnMinutes) * time.Minute):
		}
		status, err := s.servers.GetServerStatus(ctx, serverID)
		if err != nil {
			return fail("reading server status: %v", err)
		}
		if !settled(status) {
			return fail("server became %s during the warning period", status)
		}
		wasRunning = status == models.StatusRunning
	}

	// 2. Back up, and do not touch the server if that fails.
	logf("INFO", "Creating a backup before the update")
	if err := s.backupAndWait(ctx, serverID); err != nil {
		return fail("backup before the update failed, server left unchanged: %v", err)
	}

	// 3. Stop.
	if wasRunning {
		_ = s.servers.SendConsoleCommand(ctx, serverID, "say Server restarts for an update now")
		logf("INFO", "Stopping the server")
		_ = s.servers.UpdateServerStatus(ctx, serverID, models.StatusStopping)
		sctx, cancel := context.WithTimeout(ctx, 60*time.Second)
		err := client.StopServer(sctx, serverID.String())
		cancel()
		if err != nil {
			_ = s.servers.UpdateServerStatus(ctx, serverID, models.StatusRunning)
			return fail("stopping the server: %v", err)
		}
		if _, err := s.waitForStatus(ctx, serverID, updateStopTimeout, models.StatusStopped, models.StatusCrashed); err != nil {
			return fail("server did not stop: %v", err)
		}
	}

	// 4. Reinstall. The daemon keeps the old files if no new ones arrive.
	logf("INFO", "Installing Hytale %s", target)
	if err := s.servers.SetInstallingBuild(ctx, serverID, info.Patchline, target); err != nil {
		return fail("recording the target build: %v", err)
	}
	_ = s.servers.UpdateServerStatus(ctx, serverID, models.StatusInstalling)
	rctx, cancel := context.WithTimeout(ctx, 60*time.Second)
	err = client.ReinstallServer(rctx, serverID.String(), server.HytaleVersion, server.DataPath)
	cancel()
	if err != nil {
		_ = s.servers.SetInstallingBuild(ctx, serverID, "", "")
		_ = s.servers.UpdateServerStatus(ctx, serverID, models.StatusStopped)
		return fail("dispatching the reinstall: %v", err)
	}
	final, err := s.waitForStatus(ctx, serverID, updateInstallTimeout, models.StatusStopped, models.StatusCrashed)
	if err != nil {
		return fail("install did not finish: %v", err)
	}
	after, err := s.servers.GetServerByID(ctx, serverID)
	if err != nil {
		return fail("reading the server after the install: %v", err)
	}
	if final == models.StatusCrashed || after.InstalledBuild == nil || *after.InstalledBuild != target {
		reason := "the node did not report a new download; the previous server files were kept"
		if final == models.StatusCrashed {
			reason = "the install failed; see the console for the downloader output"
		}
		if wasRunning {
			s.start(ctx, client, after, logf)
		}
		return fail("%s", reason)
	}
	logf("INFO", "Hytale %s installed", target)

	// 5. Start again if it was running.
	if wasRunning {
		if err := s.start(ctx, client, after, logf); err != nil {
			return fail("updated to %s, but the start failed: %v", target, err)
		}
	}
	s.pruneUpdateBackups(ctx, serverID)
	logf("INFO", "Update to Hytale %s complete", target)
	return models.TaskStatusOK, "updated to " + target
}

func (s *ServerUpdateService) start(ctx context.Context, client UpdateDaemon, server *models.Server, logf func(string, string, ...any)) error {
	logf("INFO", "Starting the server")
	_ = s.servers.UpdateServerStatus(ctx, server.ID, models.StatusStarting)
	sctx, cancel := context.WithTimeout(ctx, 30*time.Second)
	defer cancel()
	if err := client.StartServer(sctx, DaemonStartConfig(server)); err != nil {
		_ = s.servers.UpdateServerStatus(ctx, server.ID, models.StatusCrashed)
		logf("ERROR", "Start failed: %v", err)
		return err
	}
	return nil
}

// waitForStatus polls until the server reaches one of want and returns it.
func (s *ServerUpdateService) waitForStatus(ctx context.Context, serverID uuid.UUID, timeout time.Duration, want ...string) (string, error) {
	deadline := time.Now().Add(timeout)
	for {
		status, err := s.servers.GetServerStatus(ctx, serverID)
		if err == nil {
			for _, w := range want {
				if status == w {
					return status, nil
				}
			}
		}
		if time.Now().After(deadline) {
			return status, fmt.Errorf("still %q after %s", status, timeout)
		}
		select {
		case <-ctx.Done():
			return status, ctx.Err()
		case <-time.After(updatePoll):
		}
	}
}

func (s *ServerUpdateService) backupAndWait(ctx context.Context, serverID uuid.UUID) error {
	b, err := s.backups.CreateUpdateBackup(ctx, serverID)
	if err != nil {
		return err
	}
	deadline := time.Now().Add(updateBackupTimeout)
	for {
		status, msg, err := s.backups.BackupStatus(ctx, b.ID)
		if err == nil {
			switch status {
			case "completed":
				return nil
			case "failed":
				return errors.New(msg)
			}
		}
		if time.Now().After(deadline) {
			return fmt.Errorf("backup still %q after %s", status, updateBackupTimeout)
		}
		select {
		case <-ctx.Done():
			return ctx.Err()
		case <-time.After(updatePoll):
		}
	}
}

func (s *ServerUpdateService) pruneUpdateBackups(ctx context.Context, serverID uuid.UUID) {
	if err := s.backups.PruneUpdateBackups(ctx, serverID, keepUpdateBackups); err != nil {
		s.log.Warn("pruning pre-update backups failed", zap.String("server_id", serverID.String()), zap.Error(err))
	}
}

// DaemonStartConfig is the daemon start request for a server loaded with
// GetServer (launch flags included).
func DaemonStartConfig(server *models.Server) daemon.ServerConfig {
	return daemon.ServerConfig{
		ServerID:     server.ID.String(),
		Name:         server.Name,
		DataPath:     server.DataPath,
		Port:         uint16(server.Port),
		RAMLimitMB:   uint32(server.RAMLimitMB),
		CPULimit:     float32(max(server.CPULimit, 0)),
		AutoRestart:  server.AutoRestart,
		CrashLimit:   uint32(max(server.CrashLimit, 0)),
		CrashWindowS: uint32(max(server.CrashWindowS, 0)),
		LaunchFlags:  server.LaunchFlags,
	}
}
