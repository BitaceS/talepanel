package services

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"sync"
	"time"
	"unicode"
	"unicode/utf8"

	"github.com/BitaceS/talepanel/api/internal/daemon"
	"github.com/BitaceS/talepanel/api/internal/models"
	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
	"go.uber.org/zap"
)

var (
	ErrTaskNotFound = errors.New("scheduled task not found")
	// ErrInvalidTask wraps every validation failure of a task request; the
	// handler answers 400 with the wrapped message.
	ErrInvalidTask = errors.New("invalid task")
	// ErrTaskForbidden: the caller may manage tasks but not schedule this
	// particular action (e.g. a console command above their role).
	ErrTaskForbidden = errors.New("not allowed to schedule this task")
)

const (
	// minTaskInterval is the shortest gap allowed between two runs of a task.
	minTaskInterval = 5 * time.Minute
	// maxTaskWarnMinutes caps the restart warning period.
	maxTaskWarnMinutes = 15
	// maxBroadcastLen caps a broadcast message (runes).
	maxBroadcastLen = 200
	// maxTaskCommandLen caps a scheduled console command (runes).
	maxTaskCommandLen = 500
	// maxTaskErrorLen caps the stored last_error text.
	maxTaskErrorLen = 1000
	// taskClaimBatch bounds how many due tasks one tick claims.
	taskClaimBatch = 100
	// taskActionTimeout bounds one daemon call (restart) or command enqueue.
	taskActionTimeout = 60 * time.Second
)

// TaskExecutor performs the server actions scheduled tasks trigger. The
// production implementation talks to the node daemon; tests inject a fake.
type TaskExecutor interface {
	SendCommand(ctx context.Context, serverID uuid.UUID, cmd string) error
	Restart(ctx context.Context, serverID uuid.UUID) error
}

// DaemonTaskExecutor sends console commands through the node command queue
// (like the console handler) and restarts through the daemon API (like
// POST /servers/:id/restart).
type DaemonTaskExecutor struct {
	servers *ServerService
	daemons *daemon.ClientPool
}

func NewDaemonTaskExecutor(servers *ServerService, daemons *daemon.ClientPool) *DaemonTaskExecutor {
	return &DaemonTaskExecutor{servers: servers, daemons: daemons}
}

func (e *DaemonTaskExecutor) SendCommand(ctx context.Context, serverID uuid.UUID, cmd string) error {
	return e.servers.SendConsoleCommand(ctx, serverID, cmd)
}

// Restart mirrors the handler's dispatchRestart: mark the server stopping,
// ask its daemon to restart it, and mark it crashed if the daemon call fails.
func (e *DaemonTaskExecutor) Restart(ctx context.Context, serverID uuid.UUID) error {
	var nodeID uuid.UUID
	if err := e.servers.db.QueryRow(ctx, `SELECT node_id FROM servers WHERE id = $1`, serverID).Scan(&nodeID); err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return ErrServerNotFound
		}
		return fmt.Errorf("fetching server node: %w", err)
	}
	client, err := e.daemons.Get(nodeID.String())
	if err != nil {
		return fmt.Errorf("node daemon unavailable: %w", err)
	}
	_ = e.servers.UpdateServerStatus(ctx, serverID, models.StatusStopping)
	if err := client.RestartServer(ctx, serverID.String()); err != nil {
		_ = e.servers.UpdateServerStatus(ctx, serverID, models.StatusCrashed)
		return fmt.Errorf("daemon restart failed: %w", err)
	}
	return nil
}

// ServerTaskService stores scheduled server tasks and runs them.
type ServerTaskService struct {
	db    *pgxpool.Pool
	perms *PermissionService
	exec  TaskExecutor
	log   *zap.Logger
	now   func() time.Time
	// delayed tracks restarts waiting out their warning period.
	delayed sync.WaitGroup
}

func NewServerTaskService(db *pgxpool.Pool, perms *PermissionService, exec TaskExecutor, log *zap.Logger) *ServerTaskService {
	if log == nil {
		log = zap.NewNop()
	}
	return &ServerTaskService{db: db, perms: perms, exec: exec, log: log, now: time.Now}
}

// CreateServerTaskRequest is the body of POST /servers/:id/tasks.
type CreateServerTaskRequest struct {
	Type     string                   `json:"type"`
	CronExpr string                   `json:"cron_expr"`
	Payload  models.ServerTaskPayload `json:"payload"`
	Enabled  *bool                    `json:"enabled"`
}

// UpdateServerTaskRequest is the body of PATCH /servers/:id/tasks/:taskId.
// Omitted fields stay unchanged; the type cannot change.
type UpdateServerTaskRequest struct {
	CronExpr *string                   `json:"cron_expr"`
	Payload  *models.ServerTaskPayload `json:"payload"`
	Enabled  *bool                     `json:"enabled"`
}

const serverTaskColumns = `id, server_id, created_by, type, cron_expr, payload, enabled,
	next_run_at, last_run_at, last_status, last_error, created_at`

func scanServerTask(row pgx.Row) (*models.ServerTask, error) {
	t := &models.ServerTask{}
	var payload []byte
	if err := row.Scan(&t.ID, &t.ServerID, &t.CreatedBy, &t.Type, &t.CronExpr, &payload, &t.Enabled,
		&t.NextRunAt, &t.LastRunAt, &t.LastStatus, &t.LastError, &t.CreatedAt); err != nil {
		return nil, err
	}
	if len(payload) > 0 {
		if err := json.Unmarshal(payload, &t.Payload); err != nil {
			return nil, fmt.Errorf("decoding task payload: %w", err)
		}
	}
	return t, nil
}

// ─── Validation ───────────────────────────────────────────────────────────────

func invalidTask(format string, args ...any) error {
	return fmt.Errorf("%w: %s", ErrInvalidTask, fmt.Sprintf(format, args...))
}

// validateCron checks the expression parses and does not fire more often
// than minTaskInterval. Irregular expressions (lists, ranges) can have their
// shortest gap anywhere in the cycle, so the gaps of the runs within the
// next eight days (at least a few runs) are checked, not just the first.
func validateCron(expr string, now time.Time) (time.Time, error) {
	expr = strings.TrimSpace(expr)
	first, err := nextCronTime(expr, now)
	if err != nil {
		return time.Time{}, invalidTask("invalid cron expression: %v", err)
	}
	horizon := first.Add(8 * 24 * time.Hour)
	prev := first
	for i := 0; i < 2000; i++ {
		next, err := nextCronTime(expr, prev)
		if err != nil {
			break // fires once a year or less
		}
		if next.Sub(prev) < minTaskInterval {
			return time.Time{}, invalidTask("cron expression fires more often than every %d minutes", int(minTaskInterval/time.Minute))
		}
		if i >= 2 && next.After(horizon) {
			break
		}
		prev = next
	}
	return first, nil
}

func hasControlChar(s string) bool {
	return strings.IndexFunc(s, unicode.IsControl) >= 0
}

// normalizeTaskPayload validates a payload for the task type and drops the
// fields the type does not use.
func normalizeTaskPayload(typ string, p models.ServerTaskPayload) (models.ServerTaskPayload, error) {
	switch typ {
	case models.TaskTypeRestart:
		if p.WarnMinutes < 0 || p.WarnMinutes > maxTaskWarnMinutes {
			return p, invalidTask("warn_minutes must be between 0 and %d", maxTaskWarnMinutes)
		}
		return models.ServerTaskPayload{WarnMinutes: p.WarnMinutes}, nil
	case models.TaskTypeCommand:
		cmd := strings.TrimSpace(p.Command)
		if cmd == "" {
			return p, invalidTask("command is required")
		}
		if hasControlChar(cmd) {
			return p, invalidTask("command must not contain line breaks or control characters")
		}
		if utf8.RuneCountInString(cmd) > maxTaskCommandLen {
			return p, invalidTask("command must be at most %d characters", maxTaskCommandLen)
		}
		return models.ServerTaskPayload{Command: cmd}, nil
	case models.TaskTypeBroadcast:
		msg := strings.TrimSpace(p.Message)
		if msg == "" {
			return p, invalidTask("message is required")
		}
		if hasControlChar(msg) {
			return p, invalidTask("message must not contain line breaks or control characters")
		}
		if utf8.RuneCountInString(msg) > maxBroadcastLen {
			return p, invalidTask("message must be at most %d characters", maxBroadcastLen)
		}
		return models.ServerTaskPayload{Message: msg}, nil
	default:
		return p, invalidTask("unknown task type %q (want restart, command or broadcast)", typ)
	}
}

// taskConsoleCommand is the console line a task sends, or "" for a restart.
func taskConsoleCommand(typ string, p models.ServerTaskPayload) string {
	switch typ {
	case models.TaskTypeCommand:
		return p.Command
	case models.TaskTypeBroadcast:
		return "say " + p.Message
	}
	return ""
}

// restartWarning is the broadcast sent warnMinutes before a restart.
func restartWarning(warnMinutes int) string {
	unit := "minutes"
	if warnMinutes == 1 {
		unit = "minute"
	}
	return fmt.Sprintf("say Server restarts in %d %s", warnMinutes, unit)
}

// authorizeTask reports (as an ErrTaskForbidden-wrapped error) whether user
// may have this task run on the server. It is checked when the task is saved
// and again before every run, against the user the task runs as:
//   - server.manage on the server (who may schedule tasks at all);
//   - restart: server.start, the permission behind POST /restart;
//   - command/broadcast: server.console and the console role the command
//     needs, exactly as the console handler enforces it.
func (s *ServerTaskService) authorizeTask(ctx context.Context, user *models.User, serverID, serverOwner uuid.UUID, typ string, p models.ServerTaskPayload) error {
	need := []string{"server.manage"}
	if typ == models.TaskTypeRestart {
		need = append(need, "server.start")
	} else {
		need = append(need, "server.console")
	}
	for _, perm := range need {
		ok, err := s.perms.HasServerPermission(ctx, user, serverID, perm)
		if err != nil {
			return err
		}
		if !ok {
			return fmt.Errorf("%w: missing the %s permission on this server", ErrTaskForbidden, perm)
		}
	}
	if typ == models.TaskTypeRestart && p.WarnMinutes == 0 {
		return nil
	}
	cmd := taskConsoleCommand(typ, p)
	if typ == models.TaskTypeRestart {
		cmd = restartWarning(p.WarnMinutes)
	}
	if role := models.ConsoleRole(user, serverOwner); !models.CanRunConsoleCommand(role, cmd) {
		return fmt.Errorf("%w: this command needs the %s role", ErrTaskForbidden, models.ConsoleCommandMinRole(cmd))
	}
	return nil
}

func (s *ServerTaskService) serverOwner(ctx context.Context, serverID uuid.UUID) (uuid.UUID, string, error) {
	var owner uuid.UUID
	var status string
	err := s.db.QueryRow(ctx, `SELECT owner_id, status FROM servers WHERE id = $1`, serverID).Scan(&owner, &status)
	if errors.Is(err, pgx.ErrNoRows) {
		return uuid.Nil, "", ErrServerNotFound
	}
	if err != nil {
		return uuid.Nil, "", fmt.Errorf("fetching server: %w", err)
	}
	return owner, status, nil
}

// ─── CRUD ─────────────────────────────────────────────────────────────────────

// ListTasks returns the server's tasks, newest first.
func (s *ServerTaskService) ListTasks(ctx context.Context, serverID uuid.UUID) ([]*models.ServerTask, error) {
	rows, err := s.db.Query(ctx, `SELECT `+serverTaskColumns+`
		FROM server_tasks WHERE server_id = $1 ORDER BY created_at DESC`, serverID)
	if err != nil {
		return nil, fmt.Errorf("querying tasks: %w", err)
	}
	defer rows.Close()
	tasks := []*models.ServerTask{}
	for rows.Next() {
		t, err := scanServerTask(rows)
		if err != nil {
			return nil, fmt.Errorf("scanning task: %w", err)
		}
		tasks = append(tasks, t)
	}
	return tasks, rows.Err()
}

// CreateTask validates and stores a task that runs as user.
func (s *ServerTaskService) CreateTask(ctx context.Context, user *models.User, serverID uuid.UUID, req CreateServerTaskRequest) (*models.ServerTask, error) {
	typ := strings.TrimSpace(req.Type)
	payload, err := normalizeTaskPayload(typ, req.Payload)
	if err != nil {
		return nil, err
	}
	cronExpr := strings.Join(strings.Fields(req.CronExpr), " ")
	next, err := validateCron(cronExpr, s.now())
	if err != nil {
		return nil, err
	}
	owner, _, err := s.serverOwner(ctx, serverID)
	if err != nil {
		return nil, err
	}
	if err := s.authorizeTask(ctx, user, serverID, owner, typ, payload); err != nil {
		return nil, err
	}
	enabled := true
	if req.Enabled != nil {
		enabled = *req.Enabled
	}
	payloadJSON, _ := json.Marshal(payload)
	return scanServerTask(s.db.QueryRow(ctx, `
		INSERT INTO server_tasks (server_id, created_by, type, cron_expr, payload, enabled, next_run_at)
		VALUES ($1, $2, $3, $4, $5, $6, $7)
		RETURNING `+serverTaskColumns,
		serverID, user.ID, typ, cronExpr, payloadJSON, enabled, next))
}

func (s *ServerTaskService) getTask(ctx context.Context, serverID, taskID uuid.UUID) (*models.ServerTask, error) {
	t, err := scanServerTask(s.db.QueryRow(ctx, `SELECT `+serverTaskColumns+`
		FROM server_tasks WHERE id = $1 AND server_id = $2`, taskID, serverID))
	if errors.Is(err, pgx.ErrNoRows) {
		return nil, ErrTaskNotFound
	}
	return t, err
}

// UpdateTask changes a task's schedule, payload or enabled state. A changed
// payload is re-authorized against user, who then becomes the user the task
// runs as, so nobody can edit a task into a command they could not schedule
// themselves.
func (s *ServerTaskService) UpdateTask(ctx context.Context, user *models.User, serverID, taskID uuid.UUID, req UpdateServerTaskRequest) (*models.ServerTask, error) {
	t, err := s.getTask(ctx, serverID, taskID)
	if err != nil {
		return nil, err
	}
	runAs := t.CreatedBy
	if req.Payload != nil {
		payload, err := normalizeTaskPayload(t.Type, *req.Payload)
		if err != nil {
			return nil, err
		}
		owner, _, err := s.serverOwner(ctx, serverID)
		if err != nil {
			return nil, err
		}
		if err := s.authorizeTask(ctx, user, serverID, owner, t.Type, payload); err != nil {
			return nil, err
		}
		t.Payload = payload
		runAs = &user.ID
	}
	if req.CronExpr != nil {
		t.CronExpr = strings.Join(strings.Fields(*req.CronExpr), " ")
	}
	if req.Enabled != nil {
		t.Enabled = *req.Enabled
	}
	// Recompute the next run from now on any change, so re-enabling a task
	// that was off for a while does not fire it at once for a missed slot.
	next, err := validateCron(t.CronExpr, s.now())
	if err != nil {
		return nil, err
	}
	payloadJSON, _ := json.Marshal(t.Payload)
	updated, err := scanServerTask(s.db.QueryRow(ctx, `
		UPDATE server_tasks
		   SET cron_expr = $3, payload = $4, enabled = $5, next_run_at = $6, created_by = $7
		 WHERE id = $1 AND server_id = $2
		RETURNING `+serverTaskColumns,
		taskID, serverID, t.CronExpr, payloadJSON, t.Enabled, next, runAs))
	if errors.Is(err, pgx.ErrNoRows) {
		return nil, ErrTaskNotFound
	}
	return updated, err
}

// DeleteTask removes a task of the server.
func (s *ServerTaskService) DeleteTask(ctx context.Context, serverID, taskID uuid.UUID) error {
	ct, err := s.db.Exec(ctx, `DELETE FROM server_tasks WHERE id = $1 AND server_id = $2`, taskID, serverID)
	if err != nil {
		return fmt.Errorf("deleting task: %w", err)
	}
	if ct.RowsAffected() == 0 {
		return ErrTaskNotFound
	}
	return nil
}

// ─── Scheduler ────────────────────────────────────────────────────────────────

// StartScheduler runs due tasks once a minute until ctx is cancelled.
func (s *ServerTaskService) StartScheduler(ctx context.Context) {
	ticker := time.NewTicker(time.Minute)
	defer ticker.Stop()
	s.log.Info("server task scheduler started")
	for {
		select {
		case <-ctx.Done():
			s.delayed.Wait()
			return
		case <-ticker.C:
			if _, err := s.runDueTasks(ctx); err != nil {
				s.log.Warn("server task scheduler: run failed", zap.Error(err))
			}
		}
	}
}

// claimDueTasks locks the due tasks, advances their next_run_at and commits,
// all in one transaction. FOR UPDATE SKIP LOCKED makes concurrent API
// instances claim disjoint rows, and once committed a claimed row is no
// longer due, so every run happens exactly once.
func (s *ServerTaskService) claimDueTasks(ctx context.Context, now time.Time) ([]*models.ServerTask, error) {
	tx, err := s.db.Begin(ctx)
	if err != nil {
		return nil, fmt.Errorf("begin: %w", err)
	}
	defer func() { _ = tx.Rollback(ctx) }()

	rows, err := tx.Query(ctx, `SELECT `+serverTaskColumns+`
		FROM server_tasks
		WHERE enabled AND next_run_at IS NOT NULL AND next_run_at <= $1
		ORDER BY next_run_at
		LIMIT $2
		FOR UPDATE SKIP LOCKED`, now, taskClaimBatch)
	if err != nil {
		return nil, fmt.Errorf("querying due tasks: %w", err)
	}
	var due []*models.ServerTask
	for rows.Next() {
		t, err := scanServerTask(rows)
		if err != nil {
			rows.Close()
			return nil, fmt.Errorf("scanning due task: %w", err)
		}
		due = append(due, t)
	}
	rows.Close()
	if err := rows.Err(); err != nil {
		return nil, err
	}

	claimed := due[:0]
	for _, t := range due {
		next, cErr := nextCronTime(t.CronExpr, now)
		if cErr != nil {
			if _, err := tx.Exec(ctx, `
				UPDATE server_tasks SET enabled = false, next_run_at = NULL, last_run_at = $2,
				       last_status = $3, last_error = $4
				 WHERE id = $1`,
				t.ID, now, models.TaskStatusFailed, truncateTaskError("invalid cron expression, task disabled: "+cErr.Error())); err != nil {
				return nil, fmt.Errorf("disabling task: %w", err)
			}
			continue
		}
		if _, err := tx.Exec(ctx,
			`UPDATE server_tasks SET next_run_at = $2, last_run_at = $3 WHERE id = $1`,
			t.ID, next, now); err != nil {
			return nil, fmt.Errorf("advancing task: %w", err)
		}
		t.NextRunAt = &next
		claimed = append(claimed, t)
	}
	if err := tx.Commit(ctx); err != nil {
		return nil, fmt.Errorf("commit: %w", err)
	}
	return claimed, nil
}

// runDueTasks claims and runs every due task, returning how many it claimed.
func (s *ServerTaskService) runDueTasks(ctx context.Context) (int, error) {
	tasks, err := s.claimDueTasks(ctx, s.now())
	if err != nil {
		return 0, err
	}
	for _, t := range tasks {
		status, msg := s.runTask(ctx, t)
		s.recordResult(t.ID, status, msg)
		if status == models.TaskStatusFailed {
			s.log.Warn("scheduled task failed",
				zap.String("task_id", t.ID.String()), zap.String("server_id", t.ServerID.String()),
				zap.String("type", t.Type), zap.String("error", msg))
		}
	}
	return len(tasks), nil
}

func truncateTaskError(msg string) string {
	if len(msg) > maxTaskErrorLen {
		return msg[:maxTaskErrorLen]
	}
	return msg
}

// recordResult stores a run's outcome. It uses its own context so a result
// is still written while the scheduler is shutting down.
func (s *ServerTaskService) recordResult(taskID uuid.UUID, status, msg string) {
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	var errText *string
	if msg != "" {
		m := truncateTaskError(msg)
		errText = &m
	}
	if _, err := s.db.Exec(ctx,
		`UPDATE server_tasks SET last_status = $2, last_error = $3 WHERE id = $1`,
		taskID, status, errText); err != nil {
		s.log.Warn("recording task result failed", zap.String("task_id", taskID.String()), zap.Error(err))
	}
}

// runAsUser loads the user a task runs as; nil if gone or deactivated.
func (s *ServerTaskService) runAsUser(ctx context.Context, id *uuid.UUID) (*models.User, error) {
	if id == nil {
		return nil, nil
	}
	u := &models.User{}
	err := s.db.QueryRow(ctx, `SELECT id, role, is_active FROM users WHERE id = $1`, *id).Scan(&u.ID, &u.Role, &u.IsActive)
	if errors.Is(err, pgx.ErrNoRows) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	if !u.IsActive {
		return nil, nil
	}
	return u, nil
}

// preflight checks a claimed task may run now: the server is running and the
// task's user still holds every permission the task needs. It returns
// ("", "") when the task may run, else the status and message to record.
func (s *ServerTaskService) preflight(ctx context.Context, t *models.ServerTask) (string, string) {
	owner, status, err := s.serverOwner(ctx, t.ServerID)
	if err != nil {
		return models.TaskStatusFailed, err.Error()
	}
	if status != models.StatusRunning {
		return models.TaskStatusSkipped, "skipped: server not running"
	}
	user, err := s.runAsUser(ctx, t.CreatedBy)
	if err != nil {
		return models.TaskStatusFailed, "loading task creator: " + err.Error()
	}
	if user == nil {
		return models.TaskStatusSkipped, "skipped: the user who created this task no longer exists or is deactivated"
	}
	if err := s.authorizeTask(ctx, user, t.ServerID, owner, t.Type, t.Payload); err != nil {
		if errors.Is(err, ErrTaskForbidden) {
			return models.TaskStatusSkipped, "skipped: task creator no longer allowed (" + strings.TrimPrefix(err.Error(), ErrTaskForbidden.Error()+": ") + ")"
		}
		return models.TaskStatusFailed, err.Error()
	}
	return "", ""
}

// runTask performs one claimed task and returns the status and message to
// record. A restart with a warning period sends the warning now, returns
// "pending" and restarts from a goroutine once the period is over.
func (s *ServerTaskService) runTask(ctx context.Context, t *models.ServerTask) (string, string) {
	if status, msg := s.preflight(ctx, t); status != "" {
		return status, msg
	}

	actx, cancel := context.WithTimeout(ctx, taskActionTimeout)
	defer cancel()
	switch t.Type {
	case models.TaskTypeCommand, models.TaskTypeBroadcast:
		if err := s.exec.SendCommand(actx, t.ServerID, taskConsoleCommand(t.Type, t.Payload)); err != nil {
			return models.TaskStatusFailed, "sending command: " + err.Error()
		}
		return models.TaskStatusOK, ""
	case models.TaskTypeRestart:
		if t.Payload.WarnMinutes <= 0 {
			if err := s.exec.Restart(actx, t.ServerID); err != nil {
				return models.TaskStatusFailed, err.Error()
			}
			return models.TaskStatusOK, ""
		}
		if err := s.exec.SendCommand(actx, t.ServerID, restartWarning(t.Payload.WarnMinutes)); err != nil {
			return models.TaskStatusFailed, "sending restart warning: " + err.Error()
		}
		s.delayed.Add(1)
		go s.delayedRestart(ctx, t, time.Duration(t.Payload.WarnMinutes)*time.Minute)
		return models.TaskStatusPending, fmt.Sprintf("restarting in %d minute(s)", t.Payload.WarnMinutes)
	default:
		return models.TaskStatusFailed, "unknown task type " + t.Type
	}
}

// delayedRestart waits out a restart warning, then re-runs the preflight
// (the server may have been stopped, or the creator's access revoked, in
// the meantime) and restarts. Panel shutdown cancels ctx and abandons it.
func (s *ServerTaskService) delayedRestart(ctx context.Context, t *models.ServerTask, wait time.Duration) {
	defer s.delayed.Done()
	timer := time.NewTimer(wait)
	defer timer.Stop()
	select {
	case <-ctx.Done():
		s.recordResult(t.ID, models.TaskStatusFailed, "interrupted: the panel shut down before the restart")
		return
	case <-timer.C:
	}
	if status, msg := s.preflight(ctx, t); status != "" {
		s.recordResult(t.ID, status, msg)
		return
	}
	actx, cancel := context.WithTimeout(ctx, taskActionTimeout)
	defer cancel()
	if err := s.exec.Restart(actx, t.ServerID); err != nil {
		s.recordResult(t.ID, models.TaskStatusFailed, err.Error())
		return
	}
	s.recordResult(t.ID, models.TaskStatusOK, "")
}
