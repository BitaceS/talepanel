package services

import (
	"context"
	"crypto/rand"
	"database/sql"
	"encoding/hex"
	"errors"
	"fmt"
	"regexp"
	"strings"

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
	dbName := mysqlIdentForServer(serverID)
	dbUser := dbName
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

	if err := checkMySQLIdents(dbName, dbUser); err != nil {
		return err
	}
	if err := checkMySQLPassword(dbPassword); err != nil {
		return err
	}

	conn, err := sql.Open("mysql", s.mysqlDSN)
	if err != nil {
		return fmt.Errorf("connecting to MariaDB: %w", err)
	}
	defer conn.Close()

	// No IF NOT EXISTS: an existing database or user must fail loudly rather
	// than be silently shared with another server.
	statements := []struct{ desc, sql string }{
		{"creating database", fmt.Sprintf("CREATE DATABASE `%s`", dbName)},
		{"creating user", fmt.Sprintf("CREATE USER '%s'@'%%' IDENTIFIED BY '%s'", dbUser, dbPassword)},
		{"granting privileges", fmt.Sprintf("GRANT ALL PRIVILEGES ON `%s`.* TO '%s'@'%%'", dbName, dbUser)},
		{"flushing privileges", "FLUSH PRIVILEGES"},
	}

	for _, stmt := range statements {
		// The statement text holds the password, so it never goes into the error.
		if _, err := conn.Exec(stmt.sql); err != nil {
			return fmt.Errorf("%s: %w", stmt.desc, err)
		}
	}
	return nil
}

func (s *DatabaseService) resetMySQLPassword(dbUser, newPassword string) error {
	if s.mysqlDSN == "" {
		return fmt.Errorf("MariaDB not configured")
	}

	if err := checkMySQLIdents(dbUser); err != nil {
		return err
	}
	if err := checkMySQLPassword(newPassword); err != nil {
		return err
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

	if err := checkMySQLIdents(dbName, dbUser); err != nil {
		return err
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

// mysqlIdentForServer is the database and user name for serverID: "tp_" and
// the first 29 hex digits of the UUID, which fills MariaDB's 32-character
// user-name limit. The former 8-digit suffix collided at scale, and with
// IF NOT EXISTS a collision silently shared one server's database with another.
func mysqlIdentForServer(id uuid.UUID) string {
	return "tp_" + strings.ReplaceAll(id.String(), "-", "")[:29]
}

// Identifiers and passwords are interpolated into DDL (MySQL cannot bind
// them), so both are checked against a strict charset first. Older rows hold
// 11-character names, which also pass.
var (
	mysqlIdentRe    = regexp.MustCompile(`^tp_[0-9a-f]{8,29}$`)
	mysqlPasswordRe = regexp.MustCompile(`^[0-9a-f]{32}$`)
)

func checkMySQLIdents(idents ...string) error {
	for _, id := range idents {
		if !mysqlIdentRe.MatchString(id) {
			return fmt.Errorf("refusing unsafe MariaDB identifier %q", id)
		}
	}
	return nil
}

func checkMySQLPassword(pw string) error {
	if !mysqlPasswordRe.MatchString(pw) {
		return errors.New("refusing MariaDB password outside the generated charset")
	}
	return nil
}

func generateDBPassword() (string, error) {
	b := make([]byte, 16)
	if _, err := rand.Read(b); err != nil {
		return "", err
	}
	return hex.EncodeToString(b), nil
}
