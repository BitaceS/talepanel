package services

import (
	"context"
	"errors"
	"sync"
	"testing"
	"time"

	"github.com/BitaceS/talepanel/api/internal/models"
	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgxpool"
)

// fakeTaskExecutor records what scheduled tasks would have sent.
type fakeTaskExecutor struct {
	mu       sync.Mutex
	commands []string
	restarts int
}

func (f *fakeTaskExecutor) SendCommand(_ context.Context, _ uuid.UUID, cmd string) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.commands = append(f.commands, cmd)
	return nil
}

func (f *fakeTaskExecutor) Restart(context.Context, uuid.UUID) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.restarts++
	return nil
}

func newTestTaskService(t *testing.T, pool *pgxpool.Pool) (*ServerTaskService, *fakeTaskExecutor) {
	t.Helper()
	exec := &fakeTaskExecutor{}
	return NewServerTaskService(pool, NewPermissionService(pool), exec, nil), exec
}

func setServerStatus(t *testing.T, pool *pgxpool.Pool, serverID uuid.UUID, status string) {
	t.Helper()
	if _, err := pool.Exec(context.Background(), `UPDATE servers SET status = $2 WHERE id = $1`, serverID, status); err != nil {
		t.Fatalf("set status: %v", err)
	}
}

// makeDue moves a task's next run into the past.
func makeDue(t *testing.T, pool *pgxpool.Pool, taskID uuid.UUID) {
	t.Helper()
	if _, err := pool.Exec(context.Background(),
		`UPDATE server_tasks SET next_run_at = NOW() - INTERVAL '1 minute' WHERE id = $1`, taskID); err != nil {
		t.Fatalf("make due: %v", err)
	}
}

func reloadTask(t *testing.T, svc *ServerTaskService, serverID, taskID uuid.UUID) *models.ServerTask {
	t.Helper()
	task, err := svc.getTask(context.Background(), serverID, taskID)
	if err != nil {
		t.Fatalf("reload task: %v", err)
	}
	return task
}

func TestCreateServerTaskValidation(t *testing.T) {
	pool := openTestDB(t)
	ctx := context.Background()
	svc, _ := newTestTaskService(t, pool)

	ownerID := seedTestUser(t, pool, models.RoleUser)
	serverID := seedTestServer(t, pool, ownerID)
	owner := &models.User{ID: ownerID, Role: models.RoleUser}

	long := make([]byte, 201)
	for i := range long {
		long[i] = 'a'
	}
	cases := []struct {
		name string
		req  CreateServerTaskRequest
	}{
		{"bad cron", CreateServerTaskRequest{Type: "restart", CronExpr: "every day"}},
		{"bad cron field", CreateServerTaskRequest{Type: "restart", CronExpr: "61 * * * *"}},
		{"too frequent", CreateServerTaskRequest{Type: "restart", CronExpr: "* * * * *"}},
		{"too frequent step", CreateServerTaskRequest{Type: "restart", CronExpr: "*/2 * * * *"}},
		{"too frequent list", CreateServerTaskRequest{Type: "restart", CronExpr: "0,1 4 * * *"}},
		{"unknown type", CreateServerTaskRequest{Type: "shutdown", CronExpr: "0 4 * * *"}},
		{"command newline", CreateServerTaskRequest{Type: "command", CronExpr: "0 4 * * *",
			Payload: models.ServerTaskPayload{Command: "say hi\nop add Mallory"}}},
		{"command escape", CreateServerTaskRequest{Type: "command", CronExpr: "0 4 * * *",
			Payload: models.ServerTaskPayload{Command: "say \x1b[2J"}}},
		{"empty command", CreateServerTaskRequest{Type: "command", CronExpr: "0 4 * * *"}},
		{"broadcast control", CreateServerTaskRequest{Type: "broadcast", CronExpr: "0 4 * * *",
			Payload: models.ServerTaskPayload{Message: "hi\rstop"}}},
		{"broadcast too long", CreateServerTaskRequest{Type: "broadcast", CronExpr: "0 4 * * *",
			Payload: models.ServerTaskPayload{Message: string(long)}}},
		{"warn too long", CreateServerTaskRequest{Type: "restart", CronExpr: "0 4 * * *",
			Payload: models.ServerTaskPayload{WarnMinutes: 16}}},
		{"warn negative", CreateServerTaskRequest{Type: "restart", CronExpr: "0 4 * * *",
			Payload: models.ServerTaskPayload{WarnMinutes: -1}}},
	}
	for _, tc := range cases {
		if _, err := svc.CreateTask(ctx, owner, serverID, tc.req); !errors.Is(err, ErrInvalidTask) {
			t.Errorf("%s: err=%v, want ErrInvalidTask", tc.name, err)
		}
	}

	// Valid presets are accepted, with next_run_at seeded.
	for _, cron := range []string{"0 * * * *", "0 4 * * *", "0 */6 * * *", "*/5 * * * *"} {
		task, err := svc.CreateTask(ctx, owner, serverID, CreateServerTaskRequest{Type: "restart", CronExpr: cron,
			Payload: models.ServerTaskPayload{WarnMinutes: 5}})
		if err != nil {
			t.Fatalf("cron %q: %v", cron, err)
		}
		if task.NextRunAt == nil || !task.NextRunAt.After(time.Now()) || !task.Enabled {
			t.Fatalf("cron %q: bad task %+v", cron, task)
		}
		if task.CreatedBy == nil || *task.CreatedBy != ownerID {
			t.Fatalf("created_by = %v, want %v", task.CreatedBy, ownerID)
		}
	}

	// A member who manages the server but is only a moderator cannot
	// schedule an admin-only console command.
	modID := seedTestUser(t, pool, models.RoleModerator)
	if _, err := pool.Exec(ctx,
		`INSERT INTO server_members (server_id, user_id, role, permissions) VALUES ($1, $2, 'viewer', $3)`,
		serverID, modID, `{"server.manage": true, "server.console": true}`); err != nil {
		t.Fatal(err)
	}
	mod := &models.User{ID: modID, Role: models.RoleModerator}
	if _, err := svc.CreateTask(ctx, mod, serverID, CreateServerTaskRequest{Type: "command", CronExpr: "0 4 * * *",
		Payload: models.ServerTaskPayload{Command: "stop"}}); !errors.Is(err, ErrTaskForbidden) {
		t.Fatalf("moderator scheduling stop: err=%v, want ErrTaskForbidden", err)
	}
	if _, err := svc.CreateTask(ctx, mod, serverID, CreateServerTaskRequest{Type: "command", CronExpr: "0 4 * * *",
		Payload: models.ServerTaskPayload{Command: "kick Bob"}}); err != nil {
		t.Fatalf("moderator scheduling kick: %v", err)
	}

	// Task ids are scoped to their server.
	tasks, err := svc.ListTasks(ctx, serverID)
	if err != nil || len(tasks) != 5 {
		t.Fatalf("ListTasks = %d tasks, err=%v; want 5", len(tasks), err)
	}
	otherServer := seedTestServer(t, pool, ownerID)
	if err := svc.DeleteTask(ctx, otherServer, tasks[0].ID); !errors.Is(err, ErrTaskNotFound) {
		t.Fatalf("cross-server delete: err=%v, want ErrTaskNotFound", err)
	}
	if _, err := svc.UpdateTask(ctx, owner, otherServer, tasks[0].ID, UpdateServerTaskRequest{}); !errors.Is(err, ErrTaskNotFound) {
		t.Fatalf("cross-server update: err=%v, want ErrTaskNotFound", err)
	}
}

func TestRunDueTasksClaimsOnceAndAdvances(t *testing.T) {
	pool := openTestDB(t)
	ctx := context.Background()
	svc, exec := newTestTaskService(t, pool)

	ownerID := seedTestUser(t, pool, models.RoleUser)
	serverID := seedTestServer(t, pool, ownerID)
	owner := &models.User{ID: ownerID, Role: models.RoleUser}

	task, err := svc.CreateTask(ctx, owner, serverID, CreateServerTaskRequest{Type: "broadcast", CronExpr: "0 4 * * *",
		Payload: models.ServerTaskPayload{Message: "Daily reminder"}})
	if err != nil {
		t.Fatal(err)
	}

	// Not running: skipped, but still advanced.
	makeDue(t, pool, task.ID)
	if _, err := svc.runDueTasks(ctx); err != nil {
		t.Fatal(err)
	}
	got := reloadTask(t, svc, serverID, task.ID)
	if got.LastStatus == nil || *got.LastStatus != models.TaskStatusSkipped ||
		got.LastError == nil || *got.LastError != "skipped: server not running" {
		t.Fatalf("stopped server: status=%v error=%v", got.LastStatus, got.LastError)
	}
	if got.NextRunAt == nil || !got.NextRunAt.After(time.Now()) {
		t.Fatalf("next_run_at not advanced: %v", got.NextRunAt)
	}
	if len(exec.commands) != 0 {
		t.Fatalf("commands sent to a stopped server: %v", exec.commands)
	}

	// Running: two concurrent ticks run the task exactly once.
	setServerStatus(t, pool, serverID, models.StatusRunning)
	makeDue(t, pool, task.ID)
	var wg sync.WaitGroup
	for i := 0; i < 4; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			if _, err := svc.runDueTasks(ctx); err != nil {
				t.Error(err)
			}
		}()
	}
	wg.Wait()
	exec.mu.Lock()
	cmds := append([]string(nil), exec.commands...)
	exec.mu.Unlock()
	if len(cmds) != 1 || cmds[0] != "say Daily reminder" {
		t.Fatalf("commands = %v, want exactly one say", cmds)
	}
	got = reloadTask(t, svc, serverID, task.ID)
	if got.LastStatus == nil || *got.LastStatus != models.TaskStatusOK || got.LastError != nil {
		t.Fatalf("status=%v error=%v, want ok", got.LastStatus, got.LastError)
	}
	if got.LastRunAt == nil || got.NextRunAt == nil || !got.NextRunAt.After(time.Now()) {
		t.Fatalf("run times not advanced: last=%v next=%v", got.LastRunAt, got.NextRunAt)
	}
	want, _ := nextCronTime("0 4 * * *", time.Now())
	if !got.NextRunAt.Equal(want) {
		t.Fatalf("next_run_at = %v, want %v", got.NextRunAt, want)
	}

	// No longer due: another tick does nothing.
	if n, err := svc.runDueTasks(ctx); err != nil || n != 0 {
		t.Fatalf("second tick claimed %d (err=%v), want 0", n, err)
	}

	// An immediate restart runs through the executor.
	restart, err := svc.CreateTask(ctx, owner, serverID, CreateServerTaskRequest{Type: "restart", CronExpr: "0 4 * * *"})
	if err != nil {
		t.Fatal(err)
	}
	makeDue(t, pool, restart.ID)
	if _, err := svc.runDueTasks(ctx); err != nil {
		t.Fatal(err)
	}
	if exec.restarts != 1 {
		t.Fatalf("restarts = %d, want 1", exec.restarts)
	}

	// A disabled task is never claimed.
	off := false
	if _, err := svc.UpdateTask(ctx, owner, serverID, task.ID, UpdateServerTaskRequest{Enabled: &off}); err != nil {
		t.Fatal(err)
	}
	makeDue(t, pool, task.ID)
	if n, err := svc.runDueTasks(ctx); err != nil || n != 0 {
		t.Fatalf("disabled task claimed %d (err=%v)", n, err)
	}
}

func TestCommandTaskSkippedWhenCreatorLosesPermission(t *testing.T) {
	pool := openTestDB(t)
	ctx := context.Background()
	svc, exec := newTestTaskService(t, pool)

	ownerID := seedTestUser(t, pool, models.RoleUser)
	serverID := seedTestServer(t, pool, ownerID)
	setServerStatus(t, pool, serverID, models.StatusRunning)

	modID := seedTestUser(t, pool, models.RoleModerator)
	if _, err := pool.Exec(ctx,
		`INSERT INTO server_members (server_id, user_id, role, permissions) VALUES ($1, $2, 'viewer', $3)`,
		serverID, modID, `{"server.manage": true, "server.console": true}`); err != nil {
		t.Fatal(err)
	}
	mod := &models.User{ID: modID, Role: models.RoleModerator}
	task, err := svc.CreateTask(ctx, mod, serverID, CreateServerTaskRequest{Type: "command", CronExpr: "30 3 * * *",
		Payload: models.ServerTaskPayload{Command: "kick Griefer"}})
	if err != nil {
		t.Fatal(err)
	}

	// While the creator holds the role, the command runs.
	makeDue(t, pool, task.ID)
	if _, err := svc.runDueTasks(ctx); err != nil {
		t.Fatal(err)
	}
	if len(exec.commands) != 1 || exec.commands[0] != "kick Griefer" {
		t.Fatalf("commands = %v", exec.commands)
	}

	// Demoted below the role "kick" needs: skipped with the reason.
	if _, err := pool.Exec(ctx, `UPDATE users SET role = 'user' WHERE id = $1`, modID); err != nil {
		t.Fatal(err)
	}
	makeDue(t, pool, task.ID)
	if _, err := svc.runDueTasks(ctx); err != nil {
		t.Fatal(err)
	}
	got := reloadTask(t, svc, serverID, task.ID)
	if got.LastStatus == nil || *got.LastStatus != models.TaskStatusSkipped || got.LastError == nil ||
		*got.LastError != "skipped: task creator no longer allowed (this command needs the moderator role)" {
		t.Fatalf("demoted: status=%v error=%v", got.LastStatus, deref(got.LastError))
	}

	// Removed from the server altogether: skipped too.
	if _, err := pool.Exec(ctx, `UPDATE users SET role = 'moderator' WHERE id = $1`, modID); err != nil {
		t.Fatal(err)
	}
	if _, err := pool.Exec(ctx, `DELETE FROM server_members WHERE server_id = $1 AND user_id = $2`, serverID, modID); err != nil {
		t.Fatal(err)
	}
	makeDue(t, pool, task.ID)
	if _, err := svc.runDueTasks(ctx); err != nil {
		t.Fatal(err)
	}
	got = reloadTask(t, svc, serverID, task.ID)
	if got.LastStatus == nil || *got.LastStatus != models.TaskStatusSkipped {
		t.Fatalf("removed member: status=%v error=%v", got.LastStatus, deref(got.LastError))
	}

	// Creator deleted: created_by is NULLed and the task is skipped.
	if _, err := pool.Exec(ctx, `DELETE FROM users WHERE id = $1`, modID); err != nil {
		t.Fatal(err)
	}
	makeDue(t, pool, task.ID)
	if _, err := svc.runDueTasks(ctx); err != nil {
		t.Fatal(err)
	}
	got = reloadTask(t, svc, serverID, task.ID)
	if got.CreatedBy != nil || got.LastStatus == nil || *got.LastStatus != models.TaskStatusSkipped {
		t.Fatalf("deleted creator: created_by=%v status=%v", got.CreatedBy, got.LastStatus)
	}
	if len(exec.commands) != 1 {
		t.Fatalf("commands sent after losing permission: %v", exec.commands)
	}
}

func deref(s *string) string {
	if s == nil {
		return "<nil>"
	}
	return *s
}
