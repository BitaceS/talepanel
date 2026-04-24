package services

import (
	"context"
	"errors"
	"fmt"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/BitaceS/talepanel/api/internal/models"
)

var ErrWorldNotFound = errors.New("world not found")

type CreateWorldRequest struct {
	Name      string `json:"name" binding:"required,min=1,max=100"`
	Seed      *int64 `json:"seed"`
	Generator string `json:"generator"`
}

type WorldService struct {
	db *pgxpool.Pool
}

func NewWorldService(db *pgxpool.Pool) *WorldService {
	return &WorldService{db: db}
}

func (s *WorldService) ListWorlds(ctx context.Context, serverID uuid.UUID) ([]*models.World, error) {
	rows, err := s.db.Query(ctx, `
		SELECT id, server_id, name, seed, generator, is_active, size_bytes,
		       thumbnail, metadata, created_at, updated_at
		FROM worlds WHERE server_id = $1 ORDER BY created_at DESC
	`, serverID)
	if err != nil {
		return nil, fmt.Errorf("querying worlds: %w", err)
	}
	defer rows.Close()

	var worlds []*models.World
	for rows.Next() {
		w := &models.World{}
		if err := rows.Scan(&w.ID, &w.ServerID, &w.Name, &w.Seed, &w.Generator,
			&w.IsActive, &w.SizeBytes, &w.Thumbnail, &w.Metadata,
			&w.CreatedAt, &w.UpdatedAt); err != nil {
			return nil, fmt.Errorf("scanning world row: %w", err)
		}
		worlds = append(worlds, w)
	}
	return worlds, rows.Err()
}

func (s *WorldService) CreateWorld(ctx context.Context, serverID uuid.UUID, req CreateWorldRequest) (*models.World, error) {
	w := &models.World{}
	err := s.db.QueryRow(ctx, `
		INSERT INTO worlds (server_id, name, seed, generator)
		VALUES ($1, $2, $3, $4)
		RETURNING id, server_id, name, seed, generator, is_active, size_bytes,
		          thumbnail, metadata, created_at, updated_at
	`, serverID, req.Name, req.Seed, req.Generator).Scan(
		&w.ID, &w.ServerID, &w.Name, &w.Seed, &w.Generator,
		&w.IsActive, &w.SizeBytes, &w.Thumbnail, &w.Metadata,
		&w.CreatedAt, &w.UpdatedAt,
	)
	if err != nil {
		return nil, fmt.Errorf("creating world: %w", err)
	}
	return w, nil
}

func (s *WorldService) SetActiveWorld(ctx context.Context, serverID, worldID uuid.UUID) error {
	tx, err := s.db.Begin(ctx)
	if err != nil {
		return fmt.Errorf("begin tx: %w", err)
	}
	defer tx.Rollback(ctx)

	// Deactivate all worlds for this server.
	if _, err := tx.Exec(ctx, `UPDATE worlds SET is_active = false WHERE server_id = $1`, serverID); err != nil {
		return fmt.Errorf("deactivating worlds: %w", err)
	}

	// Activate the target world.
	ct, err := tx.Exec(ctx, `UPDATE worlds SET is_active = true WHERE id = $1 AND server_id = $2`, worldID, serverID)
	if err != nil {
		return fmt.Errorf("activating world: %w", err)
	}
	if ct.RowsAffected() == 0 {
		return ErrWorldNotFound
	}

	// Update active_world on the server record.
	var worldName string
	if err := tx.QueryRow(ctx, `SELECT name FROM worlds WHERE id = $1`, worldID).Scan(&worldName); err != nil {
		return fmt.Errorf("fetching world name: %w", err)
	}
	if _, err := tx.Exec(ctx, `UPDATE servers SET active_world = $1 WHERE id = $2`, worldName, serverID); err != nil {
		return fmt.Errorf("updating server active_world: %w", err)
	}

	return tx.Commit(ctx)
}

func (s *WorldService) DeleteWorld(ctx context.Context, serverID, worldID uuid.UUID) error {
	ct, err := s.db.Exec(ctx, `DELETE FROM worlds WHERE id = $1 AND server_id = $2`, worldID, serverID)
	if err != nil {
		return fmt.Errorf("deleting world: %w", err)
	}
	if ct.RowsAffected() == 0 {
		return ErrWorldNotFound
	}
	return nil
}
