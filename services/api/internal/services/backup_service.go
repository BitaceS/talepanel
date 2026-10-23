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
	// Storage is kept for API compatibility and always stored as "local":
	// off-site copies are chosen with DestinationID.
	Storage       string     `json:"storage"`
	DestinationID *uuid.UUID `json:"destination_id"`
}

type CreateScheduleRequest struct {
	ServerID       uuid.UUID  `json:"server_id" binding:"required"`
	CronExpr       string     `json:"cron_expr" binding:"required"`
	Type           string     `json:"type"`
	Storage        string     `json:"storage"`
	RetentionCount *int       `json:"retention_count"`
	RetentionDays  *int       `json:"retention_days"`
	DestinationID  *uuid.UUID `json:"destination_id"`
}

// remoteTransferTimeout bounds one upload to / fetch from an off-site
// destination. Multi-GB archives over a home-grade uplink take a while.
const remoteTransferTimeout = 6 * time.Hour

// maxUploadErrorLen caps the rclone error text stored on a backup row.
const maxUploadErrorLen = 2000

type BackupService struct {
	db      *pgxpool.Pool
	daemons *daemon.ClientPool
	dests   *BackupDestinationService
	log     *zap.Logger
}

func NewBackupService(db *pgxpool.Pool, daemons *daemon.ClientPool, dests *BackupDestinationService, log *zap.Logger) *BackupService {
	if log == nil {
		log = zap.NewNop()
	}
	return &BackupService{db: db, daemons: daemons, dests: dests, log: log}
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

const backupSelect = `
	SELECT b.id, b.server_id, b.world_name, b.type, b.storage, b.storage_path,
	       b.size_bytes, b.checksum, b.status, b.triggered_by, b.created_at,
	       b.completed_at, b.expires_at, b.error,
	       b.destination_id, d.name, b.remote_path, b.upload_status, b.upload_error, b.local_present
	FROM backups b
	LEFT JOIN backup_destinations d ON d.id = b.destination_id`

func scanBackup(row pgx.Row) (*models.Backup, error) {
	b := &models.Backup{}
	err := row.Scan(&b.ID, &b.ServerID, &b.WorldName, &b.Type, &b.Storage,
		&b.StoragePath, &b.SizeBytes, &b.Checksum, &b.Status, &b.TriggeredBy,
		&b.CreatedAt, &b.CompletedAt, &b.ExpiresAt, &b.Error,
		&b.DestinationID, &b.DestinationName, &b.RemotePath, &b.UploadStatus, &b.UploadError, &b.LocalPresent)
	return b, err
}

func (s *BackupService) ListBackups(ctx context.Context, serverID *uuid.UUID) ([]*models.Backup, error) {
	var q string
	var args []any
	if serverID != nil {
		q = backupSelect + ` WHERE b.server_id = $1 ORDER BY b.created_at DESC LIMIT 100`
		args = []any{*serverID}
	} else {
		q = backupSelect + ` ORDER BY b.created_at DESC LIMIT 100`
	}

	rows, err := s.db.Query(ctx, q, args...)
	if err != nil {
		return nil, fmt.Errorf("querying backups: %w", err)
	}
	defer rows.Close()

	var backups []*models.Backup
	for rows.Next() {
		b, err := scanBackup(rows)
		if err != nil {
			return nil, fmt.Errorf("scanning backup row: %w", err)
		}
		backups = append(backups, b)
	}
	return backups, rows.Err()
}

func (s *BackupService) CreateBackup(ctx context.Context, req CreateBackupRequest) (*models.Backup, error) {
	return s.createBackup(ctx, req, "manual")
}

// checkDestination verifies an optional destination id exists.
func (s *BackupService) checkDestination(ctx context.Context, id *uuid.UUID) error {
	if id == nil {
		return nil
	}
	if s.dests == nil {
		return ErrDestinationNotFound
	}
	ok, err := s.dests.Exists(ctx, *id)
	if err != nil {
		return fmt.Errorf("checking destination: %w", err)
	}
	if !ok {
		return ErrDestinationNotFound
	}
	return nil
}

// createBackup inserts a backup row and dispatches the archive job to the node.
// triggeredBy distinguishes manual from scheduled backups.
func (s *BackupService) createBackup(ctx context.Context, req CreateBackupRequest, triggeredBy string) (*models.Backup, error) {
	if req.Type == "" {
		req.Type = "full"
	}
	// The archive always lands on the node first; an off-site copy is tracked
	// through destination_id/upload_status instead of the legacy enum.
	req.Storage = "local"
	if err := s.checkDestination(ctx, req.DestinationID); err != nil {
		return nil, err
	}

	// Resolve the node up front so an unreachable/unknown server fails fast
	// rather than leaving a permanently-pending row.
	nodeID, err := s.serverNode(ctx, req.ServerID)
	if err != nil {
		return nil, err
	}

	storagePath := fmt.Sprintf("backups/%s", req.ServerID)
	var uploadStatus *string
	if req.DestinationID != nil {
		pending := "pending"
		uploadStatus = &pending
	}

	var id uuid.UUID
	err = s.db.QueryRow(ctx, `
		INSERT INTO backups (server_id, world_name, type, storage, storage_path, status, triggered_by,
		                     destination_id, upload_status)
		VALUES ($1, $2, $3, $4, $5, 'pending', $6, $7, $8)
		RETURNING id
	`, req.ServerID, req.WorldName, req.Type, req.Storage, storagePath, triggeredBy, req.DestinationID, uploadStatus).Scan(&id)
	if err != nil {
		return nil, fmt.Errorf("creating backup: %w", err)
	}
	b, err := s.getBackup(ctx, id)
	if err != nil {
		return nil, err
	}

	// Archive on the node asynchronously; the row starts 'pending' and the
	// worker transitions it to running -> completed/failed.
	go s.runBackup(b.ID, req.ServerID, nodeID, req.DestinationID)

	return b, nil
}

// runBackup performs the archive on the node and updates the backup row's status.
// Runs in its own goroutine with a fresh context (the request context is gone).
func (s *BackupService) runBackup(backupID, serverID uuid.UUID, nodeID string, destinationID *uuid.UUID) {
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
		return
	}

	if destinationID != nil {
		s.uploadBackup(backupID, serverID, nodeID, *destinationID)
	}
}

func (s *BackupService) failBackup(ctx context.Context, backupID uuid.UUID, msg string) {
	// A backup that never got archived has nothing to upload either.
	_, err := s.db.Exec(ctx, `
		UPDATE backups SET status = 'failed', error = $2,
		       upload_error  = CASE WHEN upload_status IS NULL THEN NULL ELSE 'backup archive failed' END,
		       upload_status = CASE WHEN upload_status IS NULL THEN NULL ELSE 'failed' END
		WHERE id = $1`, backupID, msg)
	if err != nil {
		s.log.Error("failed to mark backup failed", zap.String("backup_id", backupID.String()), zap.Error(err))
	}
}

func truncateError(msg string) string {
	if len(msg) > maxUploadErrorLen {
		return msg[:maxUploadErrorLen] + "..."
	}
	return msg
}

func (s *BackupService) failUpload(ctx context.Context, backupID uuid.UUID, msg string) {
	s.log.Warn("backup upload failed", zap.String("backup_id", backupID.String()), zap.String("error", msg))
	if _, err := s.db.Exec(ctx, `UPDATE backups SET upload_status = 'failed', upload_error = $2 WHERE id = $1`,
		backupID, truncateError(msg)); err != nil {
		s.log.Error("failed to mark upload failed", zap.String("backup_id", backupID.String()), zap.Error(err))
	}
}

// uploadBackup copies a finished archive to its off-site destination, then
// applies the destination's keep-local and retention settings.  A failed
// upload never touches the local archive.
func (s *BackupService) uploadBackup(backupID, serverID uuid.UUID, nodeID string, destinationID uuid.UUID) {
	ctx, cancel := context.WithTimeout(context.Background(), remoteTransferTimeout)
	defer cancel()

	if s.dests == nil {
		s.failUpload(ctx, backupID, "off-site backups are not configured")
		return
	}
	spec, dest, err := s.dests.RemoteSpec(ctx, destinationID)
	if err != nil {
		s.failUpload(ctx, backupID, "backup destination is no longer available: "+err.Error())
		return
	}
	client, err := s.daemons.Get(nodeID)
	if err != nil {
		s.failUpload(ctx, backupID, "daemon not connected")
		return
	}

	_, _ = s.db.Exec(ctx, `UPDATE backups SET upload_status = 'uploading', upload_error = NULL WHERE id = $1`, backupID)

	res, err := client.UploadBackup(ctx, serverID.String(), backupID.String(), *spec)
	if err != nil {
		s.dests.saveUpdatesFromErr(ctx, destinationID, err)
		s.failUpload(ctx, backupID, err.Error())
		return
	}
	s.dests.ApplyConfigUpdates(ctx, destinationID, res.ConfigUpdates)

	if _, err := s.db.Exec(ctx, `
		UPDATE backups SET upload_status = 'done', upload_error = NULL, remote_path = $2 WHERE id = $1
	`, backupID, res.RemotePath); err != nil {
		s.log.Error("failed to mark upload done", zap.String("backup_id", backupID.String()), zap.Error(err))
		return
	}

	if !dest.KeepLocal {
		if err := client.DeleteBackupArchive(ctx, serverID.String(), backupID.String()); err != nil {
			s.log.Warn("could not remove local archive after upload",
				zap.String("backup_id", backupID.String()), zap.Error(err))
		} else {
			_, _ = s.db.Exec(ctx, `UPDATE backups SET local_present = false WHERE id = $1`, backupID)
		}
	}

	if dest.RetentionCount != nil && *dest.RetentionCount > 0 {
		s.pruneRemote(ctx, client, spec, serverID, destinationID, *dest.RetentionCount)
	}
}

// pruneRemote keeps the newest `keep` uploaded backups of a server on a
// destination and deletes older remote copies (rclone deletefile on the
// node). Rows whose local archive is also gone are removed entirely.
func (s *BackupService) pruneRemote(ctx context.Context, client *daemon.Client, spec *daemon.RemoteSpec,
	serverID, destinationID uuid.UUID, keep int) {
	rows, err := s.db.Query(ctx, `
		SELECT id, local_present FROM backups
		 WHERE server_id = $1 AND destination_id = $2 AND upload_status = 'done'
		 ORDER BY created_at DESC OFFSET $3
	`, serverID, destinationID, keep)
	if err != nil {
		s.log.Warn("remote retention: query failed", zap.Error(err))
		return
	}
	type old struct {
		id    uuid.UUID
		local bool
	}
	var olds []old
	for rows.Next() {
		var o old
		if rows.Scan(&o.id, &o.local) == nil {
			olds = append(olds, o)
		}
	}
	rows.Close()

	for _, o := range olds {
		res, err := client.DeleteRemoteBackup(ctx, serverID.String(), o.id.String(), *spec)
		if err != nil {
			s.dests.saveUpdatesFromErr(ctx, destinationID, err)
			s.log.Warn("remote retention: delete failed", zap.String("backup_id", o.id.String()), zap.Error(err))
			continue
		}
		s.dests.ApplyConfigUpdates(ctx, destinationID, res.ConfigUpdates)
		if o.local {
			_, _ = s.db.Exec(ctx, `
				UPDATE backups SET upload_status = NULL, upload_error = NULL, remote_path = NULL WHERE id = $1
			`, o.id)
		} else {
			_, _ = s.db.Exec(ctx, `DELETE FROM backups WHERE id = $1`, o.id)
		}
	}
}

// RetryUpload re-runs a failed (or stuck) upload of a completed backup.
func (s *BackupService) RetryUpload(ctx context.Context, backupID uuid.UUID) (*models.Backup, error) {
	b, err := s.getBackup(ctx, backupID)
	if err != nil {
		return nil, err
	}
	if b.Status != "completed" || b.DestinationID == nil || b.ServerID == nil {
		return nil, fmt.Errorf("backup has no off-site copy to upload")
	}
	if b.UploadStatus != nil && (*b.UploadStatus == "done" || *b.UploadStatus == "uploading") {
		return nil, fmt.Errorf("upload is already %s", *b.UploadStatus)
	}
	if !b.LocalPresent {
		return nil, fmt.Errorf("the local archive is gone; nothing to upload")
	}
	nodeID, err := s.serverNode(ctx, *b.ServerID)
	if err != nil {
		return nil, err
	}
	_, _ = s.db.Exec(ctx, `UPDATE backups SET upload_status = 'pending', upload_error = NULL WHERE id = $1`, backupID)
	go s.uploadBackup(backupID, *b.ServerID, nodeID, *b.DestinationID)
	pending := "pending"
	b.UploadStatus = &pending
	b.UploadError = nil
	return b, nil
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
	b, err := s.getBackup(ctx, backupID)
	if err != nil {
		return err
	}

	if b.ServerID != nil {
		serverID := *b.ServerID
		var client *daemon.Client
		if nodeID, nErr := s.serverNode(ctx, serverID); nErr == nil {
			client, _ = s.daemons.Get(nodeID)
		}

		// Remove the off-site copy first: if that fails the record stays, so
		// the user can retry instead of leaving an orphan in their cloud.
		if b.DestinationID != nil && b.UploadStatus != nil && *b.UploadStatus == "done" && s.dests != nil {
			spec, _, sErr := s.dests.RemoteSpec(ctx, *b.DestinationID)
			switch {
			case errors.Is(sErr, ErrDestinationNotFound):
				// Destination gone: nothing the panel can reach any more.
			case sErr != nil:
				return sErr
			case client == nil:
				return fmt.Errorf("daemon not connected; cannot delete the off-site copy")
			default:
				res, dErr := client.DeleteRemoteBackup(ctx, serverID.String(), backupID.String(), *spec)
				if dErr != nil {
					s.dests.saveUpdatesFromErr(ctx, *b.DestinationID, dErr)
					return fmt.Errorf("could not delete the off-site copy: %w", dErr)
				}
				s.dests.ApplyConfigUpdates(ctx, *b.DestinationID, res.ConfigUpdates)
			}
		}

		// Best-effort removal of the on-node archive before dropping the record.
		if client != nil && b.LocalPresent {
			if dErr := client.DeleteBackupArchive(ctx, serverID.String(), backupID.String()); dErr != nil {
				s.log.Warn("failed to delete backup archive on node",
					zap.String("backup_id", backupID.String()), zap.Error(dErr))
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
	b, err := scanBackup(s.db.QueryRow(ctx, backupSelect+` WHERE b.id = $1`, backupID))
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return nil, ErrBackupNotFound
		}
		return nil, fmt.Errorf("fetching backup: %w", err)
	}
	return b, nil
}

// remoteSourceFor returns the remote a backup must be fetched from, or nil
// when the local archive is still on the node.  Only remote-only backups get
// a spec, so nodes with an older daemon keep working for local backups.
func (s *BackupService) remoteSourceFor(ctx context.Context, b *models.Backup) (*daemon.RemoteSpec, error) {
	if b.LocalPresent {
		return nil, nil
	}
	if b.DestinationID == nil || b.UploadStatus == nil || *b.UploadStatus != "done" || s.dests == nil {
		return nil, fmt.Errorf("the local archive was removed and no off-site copy is available")
	}
	spec, _, err := s.dests.RemoteSpec(ctx, *b.DestinationID)
	if err != nil {
		return nil, fmt.Errorf("off-site destination unavailable: %w", err)
	}
	return spec, nil
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
	remote, err := s.remoteSourceFor(ctx, b)
	if err != nil {
		return nil, err
	}

	// Extract on the node asynchronously; the backup record itself is unchanged.
	go func() {
		timeout := backupDispatchTimeout
		if remote != nil {
			timeout = remoteTransferTimeout
		}
		bgCtx, cancel := context.WithTimeout(context.Background(), timeout)
		defer cancel()
		client, cErr := s.daemons.Get(nodeID)
		if cErr != nil {
			s.log.Warn("restore failed: daemon not connected", zap.String("backup_id", backupID.String()))
			return
		}
		var rErr error
		if remote != nil {
			rErr = client.RestoreBackupFrom(bgCtx, serverID.String(), backupID.String(), remote)
			s.dests.saveUpdatesFromErr(bgCtx, *b.DestinationID, rErr)
		} else {
			rErr = client.RestoreBackup(bgCtx, serverID.String(), backupID.String())
		}
		if rErr != nil {
			s.log.Warn("restore failed", zap.String("backup_id", backupID.String()), zap.Error(rErr))
		}
	}()

	return b, nil
}

// DownloadBackup streams a completed backup archive from its node.  For a
// backup that only exists off-site the node fetches it with rclone first.
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
	remote, err := s.remoteSourceFor(ctx, b)
	if err != nil {
		return nil, "", err
	}
	client, err := s.daemons.Get(nodeID)
	if err != nil {
		return nil, "", fmt.Errorf("daemon not connected")
	}
	var body io.ReadCloser
	if remote != nil {
		body, err = client.DownloadBackupFrom(ctx, serverID.String(), backupID.String(), remote)
	} else {
		body, err = client.DownloadBackup(ctx, serverID.String(), backupID.String())
	}
	if err != nil {
		return nil, "", err
	}
	return body, fmt.Sprintf("%s.zip", backupID.String()), nil
}

// Schedules

func (s *BackupService) ListSchedules(ctx context.Context, serverID uuid.UUID) ([]*models.BackupSchedule, error) {
	rows, err := s.db.Query(ctx, `
		SELECT id, server_id, cron_expr, type, storage, retention_count,
		       retention_days, enabled, last_run, next_run, created_at, destination_id
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
			&bs.NextRun, &bs.CreatedAt, &bs.DestinationID); err != nil {
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
	req.Storage = "local"
	if err := s.checkDestination(ctx, req.DestinationID); err != nil {
		return nil, err
	}

	// Validate the cron expression and seed the first next_run so the runner
	// fires it at the right time.
	next, err := nextCronTime(req.CronExpr, time.Now())
	if err != nil {
		return nil, fmt.Errorf("invalid cron expression: %w", err)
	}

	bs := &models.BackupSchedule{}
	err = s.db.QueryRow(ctx, `
		INSERT INTO backup_schedules (server_id, cron_expr, type, storage, retention_count, retention_days, next_run, destination_id)
		VALUES ($1, $2, $3, $4, $5, $6, $7, $8)
		RETURNING id, server_id, cron_expr, type, storage, retention_count,
		          retention_days, enabled, last_run, next_run, created_at, destination_id
	`, req.ServerID, req.CronExpr, req.Type, req.Storage, req.RetentionCount, req.RetentionDays, next, req.DestinationID).Scan(
		&bs.ID, &bs.ServerID, &bs.CronExpr, &bs.Type, &bs.Storage,
		&bs.RetentionCount, &bs.RetentionDays, &bs.Enabled, &bs.LastRun,
		&bs.NextRun, &bs.CreatedAt, &bs.DestinationID,
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
	// Uploads run in API goroutines; a restart kills them. Mark them failed so
	// the UI offers a retry instead of showing "uploading" forever.
	if _, err := s.db.Exec(ctx, `
		UPDATE backups SET upload_status = 'failed',
		       upload_error = 'interrupted: the panel restarted during the upload — retry it'
		 WHERE upload_status IN ('pending', 'uploading') AND status::text IN ('completed', 'failed')
	`); err != nil {
		s.log.Warn("could not reset interrupted backup uploads", zap.Error(err))
	}

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
		SELECT id, server_id, type, storage, cron_expr, retention_count, retention_days, destination_id
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
		destinationID  *uuid.UUID
	}
	var dues []due
	for rows.Next() {
		var d due
		if err := rows.Scan(&d.id, &d.serverID, &d.typ, &d.storage, &d.cronExpr, &d.retentionCount, &d.retentionDays, &d.destinationID); err != nil {
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
			ServerID:      d.serverID,
			Type:          d.typ,
			Storage:       d.storage,
			DestinationID: d.destinationID,
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
