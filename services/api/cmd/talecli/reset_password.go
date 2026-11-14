package main

import (
	"context"
	"errors"
	"fmt"
	"os"
	"strings"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/spf13/cobra"
	"golang.org/x/crypto/bcrypt"
)

// adminResetPasswordCmd sets a new password for one account. It is the
// recovery path for an owner who forgot their password: an operator with
// shell access to the panel host sets a new one here. All of the user's
// sessions are revoked, so a stolen session cannot outlive the reset.
func adminResetPasswordCmd() *cobra.Command {
	var email, password string
	var nonInteractive bool

	cmd := &cobra.Command{
		Use:   "reset-password",
		Short: "Set a new password for a user and sign them out everywhere",
		RunE: func(cmd *cobra.Command, args []string) error {
			email = strings.ToLower(strings.TrimSpace(email))
			if email == "" {
				return errors.New("--email is required")
			}
			if password == "" && !nonInteractive {
				p, err := promptPassword("New password (min 12 chars, 1 digit, 1 symbol): ")
				if err != nil {
					return err
				}
				password = p
			}
			if password == "" {
				return errors.New("--password is required in non-interactive mode")
			}

			url := os.Getenv("DATABASE_URL")
			if url == "" {
				return errors.New("DATABASE_URL is not set")
			}
			ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
			defer cancel()

			pool, err := pgxpool.New(ctx, url)
			if err != nil {
				return fmt.Errorf("connect: %w", err)
			}
			defer pool.Close()

			revoked, err := resetUserPassword(ctx, pool, email, password)
			if err != nil {
				return err
			}
			fmt.Printf("Password reset for %s; %d session(s) revoked.\n", email, revoked)
			return nil
		},
	}

	cmd.Flags().StringVar(&email, "email", "", "email of the account")
	cmd.Flags().StringVar(&password, "password", "", "new password (prompted when omitted)")
	cmd.Flags().BoolVar(&nonInteractive, "non-interactive", false, "fail instead of prompting for the password")
	return cmd
}

// resetUserPassword sets a new password for the account with the given
// (already lower-cased) email, revokes all of its sessions and records the
// change in the audit log, all in one transaction. The password must meet
// the owner/admin policy, the strictest one the panel enforces. It returns
// the number of revoked sessions.
func resetUserPassword(ctx context.Context, pool *pgxpool.Pool, email, password string) (int64, error) {
	if err := validateOwnerPassword(password); err != nil {
		return 0, err
	}
	hash, err := bcrypt.GenerateFromPassword([]byte(password), 12)
	if err != nil {
		return 0, fmt.Errorf("hash: %w", err)
	}

	tx, err := pool.Begin(ctx)
	if err != nil {
		return 0, fmt.Errorf("begin: %w", err)
	}
	defer func() { _ = tx.Rollback(ctx) }()

	var userID uuid.UUID
	err = tx.QueryRow(ctx,
		`UPDATE users SET password_hash = $2 WHERE email = $1 RETURNING id`,
		email, string(hash),
	).Scan(&userID)
	if errors.Is(err, pgx.ErrNoRows) {
		return 0, errUserNotFound
	}
	if err != nil {
		return 0, fmt.Errorf("update user: %w", err)
	}

	ct, err := tx.Exec(ctx,
		`UPDATE sessions SET revoked = true WHERE user_id = $1 AND revoked = false`,
		userID,
	)
	if err != nil {
		return 0, fmt.Errorf("revoke sessions: %w", err)
	}

	if _, err := tx.Exec(ctx, `
		INSERT INTO activity_logs (id, user_id, action, target_type, target_id, payload, created_at)
		VALUES ($1, NULL, 'cli.user.reset_password', 'user', $2,
		        jsonb_build_object('source', 'tale-cli'), NOW())
	`, uuid.New(), userID); err != nil {
		return 0, fmt.Errorf("audit log: %w", err)
	}

	if err := tx.Commit(ctx); err != nil {
		return 0, fmt.Errorf("commit: %w", err)
	}
	return ct.RowsAffected(), nil
}
