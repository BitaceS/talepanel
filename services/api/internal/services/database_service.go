package services

import (
	"context"
	"crypto/rand"
	"database/sql"
	"encoding/hex"
	"errors"
	"fmt"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/BitaceS/talepanel/api/internal/models"

	_ "github.com/go-sql-driver/mysql"
)

var (
	ErrDatabaseExists   = errors.New("database already exists for this server")
	ErrDatabaseNotFound = errors.New("database not found for this server")
)

// DatabaseService manages per-server MariaDB databases.
type DatabaseService struct {
	db       *pgxpool.Pool
	mysqlDSN string // root DSN for MariaDB, e.g. "root:password@tcp(mariadb:3306)/"
	dbHost   string
	dbPort   int
}

func NewDatabaseService(db *pgxpool.Pool, mysqlDSN, dbHost string, dbPort int) *DatabaseService {
	return &DatabaseService{db: db, mysqlDSN: mysqlDSN, dbHost: dbHost, dbPort: dbPort}
}

// CreateDatabase provisions a new MariaDB database for a server.
func (s *DatabaseService) CreateDatabase(ctx context.Context, serverID uuid.UUID) (*models.ServerDatabase, error) {
	// Check if database already exists.
	var count int
	err := s.db.QueryRow(ctx,
		`SELECT COUNT(*) FROM server_databases WHERE server_id = $1`, serverID,
	).Scan(&count)
	if err != nil {
		return nil, fmt.Errorf("checking existing database: %w", err)
	}
	if count > 0 {
		return nil, ErrDatabaseExists
	}

	// Generate credentials.
	dbName := "tp_" + shortUUID(serverID)
	dbUser := "tp_" + shortUUID(serverID)
	dbPassword, err := generateDBPassword()
	if err != nil {
		return nil, fmt.Errorf("generating password: %w", err)
	}

	// Create database and user in MariaDB.
	if err := s.provisionMySQL(dbName, dbUser, dbPassword); err != nil {
		return nil, fmt.Errorf("provisioning MySQL database: %w", err)
	}

	// Record in PostgreSQL.
	sdb := &models.ServerDatabase{}
	err = s.db.QueryRow(ctx, `
		INSERT INTO server_databases (server_id, db_name, db_user, db_password, host, port)
		VALUES ($1, $2, $3, $4, $5, $6)
		RETURNING id, server_id, db_name, db_user, db_password, host, port, created_at
	`, serverID, dbName, dbUser, dbPassword, s.dbHost, s.dbPort,
	).Scan(&sdb.ID, &sdb.ServerID, &sdb.DBName, &sdb.DBUser, &sdb.DBPassword,
		&sdb.Host, &sdb.Port, &sdb.CreatedAt)
	if err != nil {
		return nil, fmt.Errorf("recording database: %w", err)
	}

	return sdb, nil
}

// GetCredentials returns the database credentials for a server.
func (s *DatabaseService) GetCredentials(ctx context.Context, serverID uuid.UUID) (*models.ServerDatabase, error) {
	sdb := &models.ServerDatabase{}
	err := s.db.QueryRow(ctx, `
		SELECT id, server_id, db_name, db_user, db_password, host, port, created_at
		FROM server_databases WHERE server_id = $1
	`, serverID).Scan(&sdb.ID, &sdb.ServerID, &sdb.DBName, &sdb.DBUser, &sdb.DBPassword,
		&sdb.Host, &sdb.Port, &sdb.CreatedAt)
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return nil, ErrDatabaseNotFound
		}
		return nil, fmt.Errorf("fetching database credentials: %w", err)
	}
	return sdb, nil
}

// ResetPassword generates a new password for the server's database user.
func (s *DatabaseService) ResetPassword(ctx context.Context, serverID uuid.UUID) (*models.ServerDatabase, error) {
	sdb, err := s.GetCredentials(ctx, serverID)
	if err != nil {
		return nil, err
	}

	newPassword, err := generateDBPassword()
	if err != nil {
		return nil, fmt.Errorf("generating new password: %w", err)
	}

	// Update in MariaDB.
	if err := s.resetMySQLPassword(sdb.DBUser, newPassword); err != nil {
		return nil, fmt.Errorf("resetting MySQL password: %w", err)
	}

	// Update in PostgreSQL.
	_, err = s.db.Exec(ctx,
		`UPDATE server_databases SET db_password = $1 WHERE server_id = $2`,
		newPassword, serverID,
	)
	if err != nil {
		return nil, fmt.Errorf("updating password in DB: %w", err)
	}

	sdb.DBPassword = newPassword
	return sdb, nil
}

// DeleteDatabase drops the MariaDB database and user, removes the record.
func (s *DatabaseService) DeleteDatabase(ctx context.Context, serverID uuid.UUID) error {
	sdb, err := s.GetCredentials(ctx, serverID)
	if err != nil {
		return err
	}

	// Drop from MariaDB (best-effort).
	_ = s.dropMySQL(sdb.DBName, sdb.DBUser)

	// Remove from PostgreSQL.
	_, err = s.db.Exec(ctx,
		`DELETE FROM server_databases WHERE server_id = $1`, serverID,
	)
	if err != nil {
		return fmt.Errorf("deleting database record: %w", err)
	}
	return nil
}

// ── MySQL helpers ──────────────────────────────────────────────────────────────

func (s *DatabaseService) provisionMySQL(dbName, dbUser, dbPassword string) error {
	if s.mysqlDSN == "" {
		return fmt.Errorf("MariaDB not configured")
	}

	conn, err := sql.Open("mysql", s.mysqlDSN)
	if err != nil {
		return fmt.Errorf("connecting to MariaDB: %w", err)
	}
	defer conn.Close()

	statements := []string{
		fmt.Sprintf("CREATE DATABASE IF NOT EXISTS `%s`", dbName),
		fmt.Sprintf("CREATE USER IF NOT EXISTS '%s'@'%%' IDENTIFIED BY '%s'", dbUser, dbPassword),
		fmt.Sprintf("GRANT ALL PRIVILEGES ON `%s`.* TO '%s'@'%%'", dbName, dbUser),
		"FLUSH PRIVILEGES",
	}

	for _, stmt := range statements {
		if _, err := conn.Exec(stmt); err != nil {
			return fmt.Errorf("executing %q: %w", stmt, err)
		}
	}
	return nil
}

func (s *DatabaseService) resetMySQLPassword(dbUser, newPassword string) error {
	if s.mysqlDSN == "" {
		return fmt.Errorf("MariaDB not configured")
	}

	conn, err := sql.Open("mysql", s.mysqlDSN)
	if err != nil {
		return fmt.Errorf("connecting to MariaDB: %w", err)
	}
	defer conn.Close()

	_, err = conn.Exec(fmt.Sprintf("ALTER USER '%s'@'%%' IDENTIFIED BY '%s'", dbUser, newPassword))
	if err != nil {
		return fmt.Errorf("altering user password: %w", err)
	}
	_, _ = conn.Exec("FLUSH PRIVILEGES")
	return nil
}

func (s *DatabaseService) dropMySQL(dbName, dbUser string) error {
	if s.mysqlDSN == "" {
		return nil
	}

	conn, err := sql.Open("mysql", s.mysqlDSN)
	if err != nil {
		return err
	}
	defer conn.Close()

	_, _ = conn.Exec(fmt.Sprintf("DROP DATABASE IF EXISTS `%s`", dbName))
	_, _ = conn.Exec(fmt.Sprintf("DROP USER IF EXISTS '%s'@'%%'", dbUser))
	return nil
}

func shortUUID(id uuid.UUID) string {
	s := id.String()
	// Use last 8 chars of UUID for short identifier.
	return s[len(s)-8:]
}

func generateDBPassword() (string, error) {
	b := make([]byte, 16)
	if _, err := rand.Read(b); err != nil {
		return "", err
	}
	return hex.EncodeToString(b), nil
}
