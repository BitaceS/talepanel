package services

import (
	"context"
	"errors"
	"fmt"
	"io"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
	"go.uber.org/zap"
	"github.com/BitaceS/talepanel/api/internal/daemon"
	"github.com/BitaceS/talepanel/api/internal/models"
)

// backupDispatchTimeout bounds a single archive/restore operation on a node.
const backupDispatchTimeout = 30 * time.Minute

var (
	ErrBackupNotFound   = errors.New("backup not found")
	ErrScheduleNotFound = errors.New("backup schedule not found")
)

type CreateBackupRequest struct {
	ServerID  uuid.UUID `json:"server_id" binding:"required"`
	WorldName string    `json:"world_name"`
	Type      string    `json:"type"`
	Storage   string    `json:"storage"`
}

type CreateScheduleRequest struct {
	ServerID       uuid.UUID `json:"server_id" binding:"required"`
	CronExpr       string    `json:"cron_expr" binding:"required"`
	Type           string    `json:"type"`
	Storage        string    `json:"storage"`
	RetentionCount *int      `json:"retention_count"`
	RetentionDays  *int      `json:"retention_days"`
}

type BackupService struct {
	db      *pgxpool.Pool
	daemons *daemon.ClientPool
	log     *zap.Logger
}

func NewBackupService(db *pgxpool.Pool, daemons *daemon.ClientPool, log *zap.Logger) *BackupService {
	if log == nil {
		log = zap.NewNop()
	}
	return &BackupService{db: db, daemons: daemons, log: log}
}

// serverNode returns the node UUID a server is assigned to.
func (s *BackupService) serverNode(ctx context.Context, serverID uuid.UUID) (string, error) {
	var nodeID uuid.UUID
	err := s.db.QueryRow(ctx, `SELECT node_id FROM servers WHERE id = $1`, serverID).Scan(&nodeID)
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return "", ErrServerNotFound
		}
		return "", fmt.Errorf("resolving server node: %w", err)
	}
	return nodeID.String(), nil
}

func (s *BackupService) ListBackups(ctx context.Context, serverID *uuid.UUID) ([]*models.Backup, error) {
	var q string
	var args []any
	if serverID != nil {
		q = `SELECT id, server_id, world_name, type, storage, storage_path,
		            size_bytes, checksum, status, triggered_by, created_at,
		            completed_at, expires_at, error
		     FROM backups WHERE server_id = $1 ORDER BY created_at DESC LIMIT 100`
		args = []any{*serverID}
	} else {
		q = `SELECT id, server_id, world_name, type, storage, storage_path,
		            size_bytes, checksum, status, triggered_by, created_at,
		            completed_at, expires_at, error
		     FROM backups ORDER BY created_at DESC LIMIT 100`
	}

	rows, err := s.db.Query(ctx, q, args...)
	if err != nil {
		return nil, fmt.Errorf("querying backups: %w", err)
	}
	defer rows.Close()

	var backups []*models.Backup
	for rows.Next() {
		b := &models.Backup{}
		if err := rows.Scan(&b.ID, &b.ServerID, &b.WorldName, &b.Type, &b.Storage,
			&b.StoragePath, &b.SizeBytes, &b.Checksum, &b.Status, &b.TriggeredBy,
			&b.CreatedAt, &b.CompletedAt, &b.ExpiresAt, &b.Error); err != nil {
			return nil, fmt.Errorf("scanning backup row: %w", err)
		}
		backups = append(backups, b)
	}
	return backups, rows.Err()
}

func (s *BackupService) CreateBackup(ctx context.Context, req CreateBackupRequest) (*models.Backup, error) {
	return s.createBackup(ctx, req, "manual")
}

// createBackup inserts a backup row and dispatches the archive job to the node.
// triggeredBy distinguishes manual from scheduled backups.
func (s *BackupService) createBackup(ctx context.Context, req CreateBackupRequest, triggeredBy string) (*models.Backup, error) {
	if req.Type == "" {
		req.Type = "full"
	}
	if req.Storage == "" {
		req.Storage = "local"
	}

	// Resolve the node up front so an unreachable/unknown server fails fast
	// rather than leaving a permanently-pending row.
	nodeID, err := s.serverNode(ctx, req.ServerID)
	if err != nil {
		return nil, err
	}

	storagePath := fmt.Sprintf("backups/%s", req.ServerID)

	b := &models.Backup{}
	err = s.db.QueryRow(ctx, `
		INSERT INTO backups (server_id, world_name, type, storage, storage_path, status, triggered_by)
		VALUES ($1, $2, $3, $4, $5, 'pending', $6)
		RETURNING id, server_id, world_name, type, storage, storage_path,
		          size_bytes, checksum, status, triggered_by, created_at,
		          completed_at, expires_at, error
	`, req.ServerID, req.WorldName, req.Type, req.Storage, storagePath, triggeredBy).Scan(
		&b.ID, &b.ServerID, &b.WorldName, &b.Type, &b.Storage,
		&b.StoragePath, &b.SizeBytes, &b.Checksum, &b.Status, &b.TriggeredBy,
		&b.CreatedAt, &b.CompletedAt, &b.ExpiresAt, &b.Error,
	)
	if err != nil {
		return nil, fmt.Errorf("creating backup: %w", err)
	}

	// Archive on the node asynchronously; the row starts 'pending' and the
	// worker transitions it to running -> completed/failed.
	go s.runBackup(b.ID, req.ServerID, nodeID)

	return b, nil
}

// runBackup performs the archive on the node and updates the backup row's status.
// Runs in its own goroutine with a fresh context (the request context is gone).
func (s *BackupService) runBackup(backupID, serverID uuid.UUID, nodeID string) {
	ctx, cancel := context.WithTimeout(context.Background(), backupDispatchTimeout)
	defer cancel()

	client, err := s.daemons.Get(nodeID)
	if err != nil {
		s.failBackup(ctx, backupID, "daemon not connected")
		return
	}

	_, _ = s.db.Exec(ctx, `UPDATE backups SET status = 'running' WHERE id = $1`, backupID)

	res, err := client.CreateBackup(ctx, serverID.String(), backupID.String())
	if err != nil {
		s.log.Warn("backup archive failed", zap.String("backup_id", backupID.String()), zap.Error(err))
		s.failBackup(ctx, backupID, err.Error())
		return
	}

	_, err = s.db.Exec(ctx, `
		UPDATE backups
		SET status = 'completed', size_bytes = $2, storage_path = $3, completed_at = NOW(), error = NULL
		WHERE id = $1
	`, backupID, res.SizeBytes, res.StoragePath)
	if err != nil {
		s.log.Error("failed to mark backup completed", zap.String("backup_id", backupID.String()), zap.Error(err))
	}
}

func (s *BackupService) failBackup(ctx context.Context, backupID uuid.UUID, msg string) {
	_, err := s.db.Exec(ctx, `UPDATE backups SET status = 'failed', error = $2 WHERE id = $1`, backupID, msg)
	if err != nil {
		s.log.Error("failed to mark backup failed", zap.String("backup_id", backupID.String()), zap.Error(err))
	}
}

// BackupServerID returns the server a backup belongs to, for ownership checks.
func (s *BackupService) BackupServerID(ctx context.Context, backupID uuid.UUID) (uuid.UUID, error) {
	var sid uuid.UUID
	err := s.db.QueryRow(ctx, `SELECT server_id FROM backups WHERE id = $1`, backupID).Scan(&sid)
	if errors.Is(err, pgx.ErrNoRows) {
		return uuid.Nil, ErrBackupNotFound
	}
	return sid, err
}

// ScheduleServerID returns the server a backup schedule belongs to.
func (s *BackupService) ScheduleServerID(ctx context.Context, scheduleID uuid.UUID) (uuid.UUID, error) {
	var sid uuid.UUID
	err := s.db.QueryRow(ctx, `SELECT server_id FROM backup_schedules WHERE id = $1`, scheduleID).Scan(&sid)
	if errors.Is(err, pgx.ErrNoRows) {
		return uuid.Nil, ErrBackupNotFound
	}
	return sid, err
}

func (s *BackupService) DeleteBackup(ctx context.Context, backupID uuid.UUID) error {
	// Best-effort removal of the on-node archive before dropping the record.
	var serverID uuid.UUID
	if err := s.db.QueryRow(ctx, `SELECT server_id FROM backups WHERE id = $1`, backupID).Scan(&serverID); err == nil {
		if nodeID, nErr := s.serverNode(ctx, serverID); nErr == nil {
			if client, cErr := s.daemons.Get(nodeID); cErr == nil {
				if dErr := client.DeleteBackupArchive(ctx, serverID.String(), backupID.String()); dErr != nil {
					s.log.Warn("failed to delete backup archive on node",
						zap.String("backup_id", backupID.String()), zap.Error(dErr))
				}
			}
		}
	}

	ct, err := s.db.Exec(ctx, `DELETE FROM backups WHERE id = $1`, backupID)
	if err != nil {
		return fmt.Errorf("deleting backup: %w", err)
	}
	if ct.RowsAffected() == 0 {
		return ErrBackupNotFound
	}
	return nil
}

func (s *BackupService) getBackup(ctx context.Context, backupID uuid.UUID) (*models.Backup, error) {
	b := &models.Backup{}
	err := s.db.QueryRow(ctx, `
		SELECT id, server_id, world_name, type, storage, storage_path,
		       size_bytes, checksum, status, triggered_by, created_at,
		       completed_at, expires_at, error
		FROM backups WHERE id = $1
	`, backupID).Scan(
		&b.ID, &b.ServerID, &b.WorldName, &b.Type, &b.Storage,
		&b.StoragePath, &b.SizeBytes, &b.Checksum, &b.Status, &b.TriggeredBy,
		&b.CreatedAt, &b.CompletedAt, &b.ExpiresAt, &b.Error,
	)
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return nil, ErrBackupNotFound
		}
		return nil, fmt.Errorf("fetching backup: %w", err)
	}
	return b, nil
}

func (s *BackupService) RestoreBackup(ctx context.Context, backupID uuid.UUID) (*models.Backup, error) {
	b, err := s.getBackup(ctx, backupID)
	if err != nil {
		return nil, err
	}
	if b.Status != "completed" {
		return nil, fmt.Errorf("backup is not restorable (status: %s)", b.Status)
	}
	if b.ServerID == nil {
		return nil, fmt.Errorf("backup has no associated server")
	}
	serverID := *b.ServerID

	nodeID, err := s.serverNode(ctx, serverID)
	if err != nil {
		return nil, err
	}

	// Extract on the node asynchronously; the backup record itself is unchanged.
	go func() {
		bgCtx, cancel := context.WithTimeout(context.Background(), backupDispatchTimeout)
		defer cancel()
		client, cErr := s.daemons.Get(nodeID)
		if cErr != nil {
			s.log.Warn("restore failed: daemon not connected", zap.String("backup_id", backupID.String()))
			return
		}
		if rErr := client.RestoreBackup(bgCtx, serverID.String(), backupID.String()); rErr != nil {
			s.log.Warn("restore failed", zap.String("backup_id", backupID.String()), zap.Error(rErr))
		}
	}()

	return b, nil
}

// DownloadBackup streams a completed backup archive from its node.
func (s *BackupService) DownloadBackup(ctx context.Context, backupID uuid.UUID) (io.ReadCloser, string, error) {
	b, err := s.getBackup(ctx, backupID)
	if err != nil {
		return nil, "", err
	}
	if b.Status != "completed" {
		return nil, "", fmt.Errorf("backup is not downloadable (status: %s)", b.Status)
	}
	if b.ServerID == nil {
		return nil, "", fmt.Errorf("backup has no associated server")
	}
	serverID := *b.ServerID
	nodeID, err := s.serverNode(ctx, serverID)
	if err != nil {
		return nil, "", err
	}
	client, err := s.daemons.Get(nodeID)
	if err != nil {
		return nil, "", fmt.Errorf("daemon not connected")
	}
	body, err := client.DownloadBackup(ctx, serverID.String(), backupID.String())
	if err != nil {
		return nil, "", err
	}
	return body, fmt.Sprintf("%s.zip", backupID.String()), nil
}

// Schedules

func (s *BackupService) ListSchedules(ctx context.Context, serverID uuid.UUID) ([]*models.BackupSchedule, error) {
	rows, err := s.db.Query(ctx, `
		SELECT id, server_id, cron_expr, type, storage, retention_count,
		       retention_days, enabled, last_run, next_run, created_at
		FROM backup_schedules WHERE server_id = $1 ORDER BY created_at DESC
	`, serverID)
	if err != nil {
		return nil, fmt.Errorf("querying schedules: %w", err)
	}
	defer rows.Close()

	var schedules []*models.BackupSchedule
	for rows.Next() {
		bs := &models.BackupSchedule{}
		if err := rows.Scan(&bs.ID, &bs.ServerID, &bs.CronExpr, &bs.Type, &bs.Storage,
			&bs.RetentionCount, &bs.RetentionDays, &bs.Enabled, &bs.LastRun,
			&bs.NextRun, &bs.CreatedAt); err != nil {
			return nil, fmt.Errorf("scanning schedule row: %w", err)
		}
		schedules = append(schedules, bs)
	}
	return schedules, rows.Err()
}

func (s *BackupService) CreateSchedule(ctx context.Context, req CreateScheduleRequest) (*models.BackupSchedule, error) {
	if req.Type == "" {
		req.Type = "full"
	}
	if req.Storage == "" {
		req.Storage = "local"
	}

	// Validate the cron expression and seed the first next_run so the runner
	// fires it at the right time.
	next, err := nextCronTime(req.CronExpr, time.Now())
	if err != nil {
		return nil, fmt.Errorf("invalid cron expression: %w", err)
	}

	bs := &models.BackupSchedule{}
	err = s.db.QueryRow(ctx, `
		INSERT INTO backup_schedules (server_id, cron_expr, type, storage, retention_count, retention_days, next_run)
		VALUES ($1, $2, $3, $4, $5, $6, $7)
		RETURNING id, server_id, cron_expr, type, storage, retention_count,
		          retention_days, enabled, last_run, next_run, created_at
	`, req.ServerID, req.CronExpr, req.Type, req.Storage, req.RetentionCount, req.RetentionDays, next).Scan(
		&bs.ID, &bs.ServerID, &bs.CronExpr, &bs.Type, &bs.Storage,
		&bs.RetentionCount, &bs.RetentionDays, &bs.Enabled, &bs.LastRun,
		&bs.NextRun, &bs.CreatedAt,
	)
	if err != nil {
		return nil, fmt.Errorf("creating schedule: %w", err)
	}
	return bs, nil
}

func (s *BackupService) ToggleSchedule(ctx context.Context, scheduleID uuid.UUID, enabled bool) error {
	ct, err := s.db.Exec(ctx, `UPDATE backup_schedules SET enabled = $1 WHERE id = $2`, enabled, scheduleID)
	if err != nil {
		return fmt.Errorf("toggling schedule: %w", err)
	}
	if ct.RowsAffected() == 0 {
		return ErrScheduleNotFound
	}
	return nil
}

func (s *BackupService) DeleteSchedule(ctx context.Context, scheduleID uuid.UUID) error {
	ct, err := s.db.Exec(ctx, `DELETE FROM backup_schedules WHERE id = $1`, scheduleID)
	if err != nil {
		return fmt.Errorf("deleting schedule: %w", err)
	}
	if ct.RowsAffected() == 0 {
		return ErrScheduleNotFound
	}
	return nil
}

// ─── Schedule runner ──────────────────────────────────────────────────────────

// StartScheduler runs the backup schedule loop until ctx is cancelled. It ticks
// once a minute, firing any due schedules and rescheduling their next_run.
func (s *BackupService) StartScheduler(ctx context.Context) {
	ticker := time.NewTicker(time.Minute)
	defer ticker.Stop()
	s.log.Info("backup scheduler started")
	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
			s.runDueSchedules(ctx)
		}
	}
}

// runDueSchedules fires every enabled schedule whose next_run has passed.
func (s *BackupService) runDueSchedules(ctx context.Context) {
	rows, err := s.db.Query(ctx, `
		SELECT id, server_id, type, storage, cron_expr, retention_count, retention_days
		FROM backup_schedules
		WHERE enabled = true AND next_run IS NOT NULL AND next_run <= NOW()
	`)
	if err != nil {
		s.log.Warn("backup scheduler: query due schedules failed", zap.Error(err))
		return
	}

	type due struct {
		id             uuid.UUID
		serverID       uuid.UUID
		typ            string
		storage        string
		cronExpr       string
		retentionCount *int
		retentionDays  *int
	}
	var dues []due
	for rows.Next() {
		var d due
		if err := rows.Scan(&d.id, &d.serverID, &d.typ, &d.storage, &d.cronExpr, &d.retentionCount, &d.retentionDays); err != nil {
			s.log.Warn("backup scheduler: scan failed", zap.Error(err))
			continue
		}
		dues = append(dues, d)
	}
	rows.Close()

	now := time.Now()
	for _, d := range dues {
		// Reschedule first so a failing backup does not cause a tight retry loop.
		next, cErr := nextCronTime(d.cronExpr, now)
		if cErr != nil {
			s.log.Warn("backup scheduler: invalid cron, disabling schedule",
				zap.String("schedule_id", d.id.String()), zap.Error(cErr))
			_, _ = s.db.Exec(ctx, `UPDATE backup_schedules SET enabled = false WHERE id = $1`, d.id)
			continue
		}
		_, _ = s.db.Exec(ctx, `UPDATE backup_schedules SET last_run = NOW(), next_run = $2 WHERE id = $1`, d.id, next)

		if _, bErr := s.createBackup(ctx, CreateBackupRequest{
			ServerID: d.serverID,
			Type:     d.typ,
			Storage:  d.storage,
		}, "schedule"); bErr != nil {
			s.log.Warn("backup scheduler: create backup failed",
				zap.String("schedule_id", d.id.String()), zap.Error(bErr))
			continue
		}

		s.pruneBackups(ctx, d.serverID, d.retentionCount, d.retentionDays)
	}
}

// pruneBackups enforces a schedule's retention policy, removing old backups
// (and their on-node archives) beyond the retention count / age window.
func (s *BackupService) pruneBackups(ctx context.Context, serverID uuid.UUID, retentionCount, retentionDays *int) {
	var ids []uuid.UUID

	if retentionDays != nil && *retentionDays > 0 {
		rows, err := s.db.Query(ctx, `
			SELECT id FROM backups
			WHERE server_id = $1 AND created_at < NOW() - make_interval(days => $2)
		`, serverID, *retentionDays)
		if err == nil {
			for rows.Next() {
				var id uuid.UUID
				if rows.Scan(&id) == nil {
					ids = append(ids, id)
				}
			}
			rows.Close()
		}
	}

	if retentionCount != nil && *retentionCount > 0 {
		rows, err := s.db.Query(ctx, `
			SELECT id FROM backups
			WHERE server_id = $1 AND status = 'completed'
			ORDER BY created_at DESC OFFSET $2
		`, serverID, *retentionCount)
		if err == nil {
			for rows.Next() {
				var id uuid.UUID
				if rows.Scan(&id) == nil {
					ids = append(ids, id)
				}
			}
			rows.Close()
		}
	}

	for _, id := range ids {
		if err := s.DeleteBackup(ctx, id); err != nil {
			s.log.Warn("backup retention prune failed", zap.String("backup_id", id.String()), zap.Error(err))
		}
	}
}
