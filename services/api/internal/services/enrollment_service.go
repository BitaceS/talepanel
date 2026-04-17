package services

import (
	"context"
	"crypto/rand"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/Bitaces/talepanel/api/internal/models"
)

// ErrEnrollmentNotFound is returned when a token does not resolve to an
// outstanding enrollment record.
var ErrEnrollmentNotFound = errors.New("enrollment not found or already redeemed")

// ErrEnrollmentExpired is returned when the token is still present but past
// its expires_at timestamp.
var ErrEnrollmentExpired = errors.New("enrollment token expired")

// EnrollmentService owns node-enrollment tokens.
type EnrollmentService struct {
	db *pgxpool.Pool
}

// NewEnrollmentService constructs the service.
func NewEnrollmentService(db *pgxpool.Pool) *EnrollmentService {
	return &EnrollmentService{db: db}
}

// CreateEnrollmentRequest captures the admin-supplied parameters for a new
// enrollment token.
type CreateEnrollmentRequest struct {
	NodeName    string
	TotalCPU    int
	TotalRAMMB  int
	TotalDiskMB int
	MaxServers  int
	CreatedBy   uuid.UUID
	TTL         time.Duration
}

// Enrollment mirrors a node_enrollments row.
type Enrollment struct {
	ID          uuid.UUID
	TokenHash   string
	NodeName    string
	TotalCPU    *int
	TotalRAMMB  *int
	TotalDiskMB *int
	MaxServers  int
	CreatedBy   uuid.UUID
	CreatedAt   time.Time
	ExpiresAt   time.Time
	UsedAt      *time.Time
	NodeID      *uuid.UUID
}

// RedeemPayload is what the daemon sends when redeeming a token.
type RedeemPayload struct {
	FQDN string
	Port int
}

// Create inserts a new enrollment and returns the plaintext token.  The caller
// must surface that token to the admin exactly once — it is never stored
// reversibly.
func (s *EnrollmentService) Create(ctx context.Context, req CreateEnrollmentRequest) (*Enrollment, string, error) {
	if req.NodeName == "" {
		return nil, "", fmt.Errorf("node_name is required")
	}
	if req.MaxServers <= 0 {
		req.MaxServers = 10
	}
	if req.TTL == 0 {
		req.TTL = 15 * time.Minute
	}

	buf := make([]byte, 32)
	if _, err := rand.Read(buf); err != nil {
		return nil, "", fmt.Errorf("random token: %w", err)
	}
	plain := hex.EncodeToString(buf)
	sum := sha256.Sum256([]byte(plain))
	tokenHash := hex.EncodeToString(sum[:])

	enr := &Enrollment{
		ID:         uuid.New(),
		TokenHash:  tokenHash,
		NodeName:   req.NodeName,
		MaxServers: req.MaxServers,
		CreatedBy:  req.CreatedBy,
		CreatedAt:  time.Now().UTC(),
		ExpiresAt:  time.Now().UTC().Add(req.TTL),
	}

	_, err := s.db.Exec(ctx, `
		INSERT INTO node_enrollments
			(id, token_hash, node_name, total_cpu, total_ram_mb, total_disk_mb,
			 max_servers, created_by, expires_at)
		VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9)
	`, enr.ID, enr.TokenHash, enr.NodeName,
		nullableInt(req.TotalCPU), nullableInt(req.TotalRAMMB), nullableInt(req.TotalDiskMB),
		enr.MaxServers, enr.CreatedBy, enr.ExpiresAt)
	if err != nil {
		return nil, "", fmt.Errorf("insert enrollment: %w", err)
	}
	return enr, plain, nil
}

// Redeem consumes a plaintext token, inserts a new node row, and returns the
// new node together with its permanent token.  Atomic: either both rows land
// or neither.
func (s *EnrollmentService) Redeem(ctx context.Context, plainToken string, payload RedeemPayload) (*models.Node, string, error) {
	if payload.FQDN == "" || payload.Port == 0 {
		return nil, "", fmt.Errorf("fqdn and port are required")
	}

	sum := sha256.Sum256([]byte(plainToken))
	tokenHash := hex.EncodeToString(sum[:])

	tx, err := s.db.Begin(ctx)
	if err != nil {
		return nil, "", fmt.Errorf("begin: %w", err)
	}
	defer func() { _ = tx.Rollback(ctx) }()

	var enr Enrollment
	err = tx.QueryRow(ctx, `
		SELECT id, token_hash, node_name, total_cpu, total_ram_mb, total_disk_mb,
		       max_servers, created_by, created_at, expires_at, used_at
		  FROM node_enrollments
		 WHERE token_hash = $1
		   FOR UPDATE
	`, tokenHash).Scan(
		&enr.ID, &enr.TokenHash, &enr.NodeName, &enr.TotalCPU, &enr.TotalRAMMB,
		&enr.TotalDiskMB, &enr.MaxServers, &enr.CreatedBy, &enr.CreatedAt,
		&enr.ExpiresAt, &enr.UsedAt,
	)
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return nil, "", ErrEnrollmentNotFound
		}
		return nil, "", fmt.Errorf("select enrollment: %w", err)
	}
	if enr.UsedAt != nil {
		return nil, "", ErrEnrollmentNotFound
	}
	if time.Now().UTC().After(enr.ExpiresAt) {
		return nil, "", ErrEnrollmentExpired
	}

	tokenBuf := make([]byte, 32)
	if _, err := rand.Read(tokenBuf); err != nil {
		return nil, "", fmt.Errorf("random node token: %w", err)
	}
	plainNodeToken := hex.EncodeToString(tokenBuf)
	nodeSum := sha256.Sum256([]byte(plainNodeToken))
	nodeTokenHash := hex.EncodeToString(nodeSum[:])

	node := &models.Node{
		ID:         uuid.New(),
		Name:       enr.NodeName,
		FQDN:       payload.FQDN,
		Port:       payload.Port,
		MaxServers: enr.MaxServers,
		Status:     "offline",
	}
	if enr.TotalCPU != nil {
		node.TotalCPU = *enr.TotalCPU
	}
	if enr.TotalRAMMB != nil {
		node.TotalRAMMB = *enr.TotalRAMMB
	}
	if enr.TotalDiskMB != nil {
		node.TotalDiskMB = *enr.TotalDiskMB
	}

	_, err = tx.Exec(ctx, `
		INSERT INTO nodes (id, name, fqdn, port, total_cpu, total_ram_mb,
		                   total_disk_mb, max_servers, token_hash, status)
		VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9, $10)
	`, node.ID, node.Name, node.FQDN, node.Port, node.TotalCPU, node.TotalRAMMB,
		node.TotalDiskMB, node.MaxServers, nodeTokenHash, node.Status)
	if err != nil {
		return nil, "", fmt.Errorf("insert node: %w", err)
	}

	_, err = tx.Exec(ctx, `
		UPDATE node_enrollments SET used_at = NOW(), node_id = $1 WHERE id = $2
	`, node.ID, enr.ID)
	if err != nil {
		return nil, "", fmt.Errorf("mark enrollment used: %w", err)
	}

	if err := tx.Commit(ctx); err != nil {
		return nil, "", fmt.Errorf("commit: %w", err)
	}
	return node, plainNodeToken, nil
}

// nullableInt returns nil for zero values so the DB stores NULL instead of 0.
func nullableInt(v int) any {
	if v == 0 {
		return nil
	}
	return v
}
