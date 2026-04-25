package db

import (
	"context"
	"errors"
	"fmt"
	"io/fs"
	"sort"
	"strings"

	"github.com/jackc/pgx/v5/pgconn"
	"github.com/jackc/pgx/v5/pgxpool"
	"go.uber.org/zap"

	"github.com/BitaceS/talepanel/api/migrations"
)

// PostgreSQL SQLSTATE codes that mean "the object this migration creates
// already exists" — almost always because the database was provisioned
// out-of-band.  We treat these as "already applied" so the file is recorded
// in schema_migrations and never tried again.
var alreadyExistsCodes = map[string]bool{
	"42710": true, // duplicate_object
	"42P07": true, // duplicate_table
	"42701": true, // duplicate_column
	"42723": true, // duplicate_function
	"42P06": true, // duplicate_schema
}

// RunMigrations applies any embedded *.sql migrations that are not yet
// recorded in schema_migrations.  Each file is executed in its own
// transaction so a failure cannot leave a half-applied schema behind.
//
// All current migrations use IF NOT EXISTS / ADD COLUMN IF NOT EXISTS, so
// re-running an existing database that was first provisioned via the old
// docker-entrypoint-initdb mount is safe — every statement is a no-op and
// the file is then recorded as applied.
func RunMigrations(ctx context.Context, pool *pgxpool.Pool, log *zap.Logger) error {
	if _, err := pool.Exec(ctx, `
		CREATE TABLE IF NOT EXISTS schema_migrations (
			version    TEXT        PRIMARY KEY,
			applied_at TIMESTAMPTZ NOT NULL DEFAULT NOW()
		)
	`); err != nil {
		return fmt.Errorf("creating schema_migrations: %w", err)
	}

	applied, err := loadAppliedVersions(ctx, pool)
	if err != nil {
		return err
	}

	files, err := listMigrationFiles()
	if err != nil {
		return err
	}

	pending := 0
	for _, name := range files {
		if applied[name] {
			continue
		}
		body, err := migrations.FS.ReadFile(name)
		if err != nil {
			return fmt.Errorf("reading migration %s: %w", name, err)
		}
		applied, err := applyOne(ctx, pool, name, string(body))
		if err != nil {
			return err
		}
		if applied {
			log.Info("migration applied", zap.String("file", name))
		} else {
			log.Info("migration recorded (already present)", zap.String("file", name))
		}
		pending++
	}

	if pending == 0 {
		log.Info("schema up-to-date", zap.Int("migrations", len(files)))
	} else {
		log.Info("migrations applied", zap.Int("count", pending))
	}
	return nil
}

func loadAppliedVersions(ctx context.Context, pool *pgxpool.Pool) (map[string]bool, error) {
	rows, err := pool.Query(ctx, `SELECT version FROM schema_migrations`)
	if err != nil {
		return nil, fmt.Errorf("loading applied versions: %w", err)
	}
	defer rows.Close()

	out := make(map[string]bool)
	for rows.Next() {
		var v string
		if err := rows.Scan(&v); err != nil {
			return nil, fmt.Errorf("scanning version: %w", err)
		}
		out[v] = true
	}
	return out, rows.Err()
}

func listMigrationFiles() ([]string, error) {
	var files []string
	err := fs.WalkDir(migrations.FS, ".", func(path string, d fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if d.IsDir() {
			return nil
		}
		if strings.HasSuffix(path, ".sql") {
			files = append(files, path)
		}
		return nil
	})
	if err != nil {
		return nil, fmt.Errorf("walking migrations: %w", err)
	}
	sort.Strings(files)
	return files, nil
}

// applyOne runs a single migration and records it in schema_migrations.
// Returns (true, nil) when the SQL ran successfully and (false, nil) when it
// failed with an "object already exists" error — meaning the schema change
// is already in place and we should just record the file as applied.
func applyOne(ctx context.Context, pool *pgxpool.Pool, name, body string) (bool, error) {
	tx, err := pool.Begin(ctx)
	if err != nil {
		return false, fmt.Errorf("begin tx for %s: %w", name, err)
	}
	defer func() { _ = tx.Rollback(ctx) }()

	executed := true
	if _, err := tx.Exec(ctx, body); err != nil {
		var pgErr *pgconn.PgError
		if errors.As(err, &pgErr) && alreadyExistsCodes[pgErr.Code] {
			// Roll back the failed transaction and start a fresh one to
			// record the migration — Postgres marks the original aborted.
			_ = tx.Rollback(ctx)
			tx, err = pool.Begin(ctx)
			if err != nil {
				return false, fmt.Errorf("begin tx for %s after recovery: %w", name, err)
			}
			executed = false
		} else {
			return false, fmt.Errorf("executing %s: %w", name, err)
		}
	}

	if _, err := tx.Exec(ctx, `INSERT INTO schema_migrations (version) VALUES ($1)`, name); err != nil {
		return false, fmt.Errorf("recording %s: %w", name, err)
	}
	if err := tx.Commit(ctx); err != nil {
		return false, fmt.Errorf("commit %s: %w", name, err)
	}
	return executed, nil
}
