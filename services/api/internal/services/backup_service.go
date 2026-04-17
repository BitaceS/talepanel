package services

import (
	"context"
	"errors"
	"fmt"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/Bitaces/talepanel/api/internal/models"
)

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
	db *pgxpool.Pool
}

func NewBackupService(db *pgxpool.Pool) *BackupService {
	return &BackupService{db: db}
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
	if req.Type == "" {
		req.Type = "full"
	}
	if req.Storage == "" {
		req.Storage = "local"
	}

	storagePath := fmt.Sprintf("backups/%s/%s", req.ServerID, uuid.New().String())

	b := &models.Backup{}
	err := s.db.QueryRow(ctx, `
		INSERT INTO backups (server_id, world_name, type, storage, storage_path, status, triggered_by)
		VALUES ($1, $2, $3, $4, $5, 'pending', 'manual')
		RETURNING id, server_id, world_name, type, storage, storage_path,
		          size_bytes, checksum, status, triggered_by, created_at,
		          completed_at, expires_at, error
	`, req.ServerID, req.WorldName, req.Type, req.Storage, storagePath).Scan(
		&b.ID, &b.ServerID, &b.WorldName, &b.Type, &b.Storage,
		&b.StoragePath, &b.SizeBytes, &b.Checksum, &b.Status, &b.TriggeredBy,
		&b.CreatedAt, &b.CompletedAt, &b.ExpiresAt, &b.Error,
	)
	if err != nil {
		return nil, fmt.Errorf("creating backup: %w", err)
	}
	return b, nil
}

func (s *BackupService) DeleteBackup(ctx context.Context, backupID uuid.UUID) error {
	ct, err := s.db.Exec(ctx, `DELETE FROM backups WHERE id = $1`, backupID)
	if err != nil {
		return fmt.Errorf("deleting backup: %w", err)
	}
	if ct.RowsAffected() == 0 {
		return ErrBackupNotFound
	}
	return nil
}

func (s *BackupService) RestoreBackup(ctx context.Context, backupID uuid.UUID) (*models.Backup, error) {
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
	// Mark as running (actual restore would be dispatched to daemon)
	_, _ = s.db.Exec(ctx, `UPDATE backups SET status = 'running' WHERE id = $1`, backupID)
	return b, nil
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

	bs := &models.BackupSchedule{}
	err := s.db.QueryRow(ctx, `
		INSERT INTO backup_schedules (server_id, cron_expr, type, storage, retention_count, retention_days)
		VALUES ($1, $2, $3, $4, $5, $6)
		RETURNING id, server_id, cron_expr, type, storage, retention_count,
		          retention_days, enabled, last_run, next_run, created_at
	`, req.ServerID, req.CronExpr, req.Type, req.Storage, req.RetentionCount, req.RetentionDays).Scan(
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
