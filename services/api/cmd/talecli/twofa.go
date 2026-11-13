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
)

// errUserNotFound is returned by disableUser2FA when no account matches.
var errUserNotFound = errors.New("no user with that email")

// adminDisable2FACmd turns off TOTP 2FA for one account. It is the recovery
// path for a user who lost their authenticator device: TalePanel has no
// backup codes yet, so an operator with shell access to the panel host
// resets 2FA here and the user enrols a new device after signing in.
func adminDisable2FACmd() *cobra.Command {
	var email string
	var revokeSessions bool

	cmd := &cobra.Command{
		Use:   "disable-2fa",
		Short: "Turn off two-factor authentication for a user (lost authenticator)",
		RunE: func(cmd *cobra.Command, args []string) error {
			email = strings.ToLower(strings.TrimSpace(email))
			if email == "" {
				return errors.New("--email is required")
			}

			url := os.Getenv("DATABASE_URL")
			if url == "" {
				return errors.New("DATABASE_URL is not set")
			}
			ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
			defer cancel()

			pool, err := pgxpool.New(ctx, url)
			if err != nil {
				return fmt.Errorf("connect: %w", err)
			}
			defer pool.Close()

			res, err := disableUser2FA(ctx, pool, email, revokeSessions)
			if err != nil {
				return err
			}
			if !res.WasEnabled {
				fmt.Printf("2FA was not enabled for %s; nothing changed.\n", email)
				return nil
			}
			fmt.Printf("2FA disabled for %s.", email)
			if revokeSessions {
				fmt.Printf(" %d session(s) revoked.", res.SessionsRevoked)
			}
			fmt.Println(" The user can enable 2FA again under Profile → Security.")
			return nil
		},
	}

	cmd.Flags().StringVar(&email, "email", "", "email of the account")
	cmd.Flags().BoolVar(&revokeSessions, "revoke-sessions", false, "also sign the user out everywhere")
	return cmd
}

type disable2FAResult struct {
	UserID          uuid.UUID
	WasEnabled      bool
	SessionsRevoked int64
}

// disableUser2FA clears the TOTP secret of the account with the given
// (already lower-cased) email and records the change in the audit log.
// Optionally it revokes all of the user's sessions as well.
func disableUser2FA(ctx context.Context, pool *pgxpool.Pool, email string, revokeSessions bool) (*disable2FAResult, error) {
	tx, err := pool.Begin(ctx)
	if err != nil {
		return nil, fmt.Errorf("begin: %w", err)
	}
	defer func() { _ = tx.Rollback(ctx) }()

	res := &disable2FAResult{}
	err = tx.QueryRow(ctx,
		`SELECT id, totp_enabled OR totp_secret IS NOT NULL FROM users WHERE email = $1 FOR UPDATE`,
		email,
	).Scan(&res.UserID, &res.WasEnabled)
	if errors.Is(err, pgx.ErrNoRows) {
		return nil, errUserNotFound
	}
	if err != nil {
		return nil, fmt.Errorf("lookup user: %w", err)
	}
	if !res.WasEnabled && !revokeSessions {
		return res, nil
	}

	if res.WasEnabled {
		if _, err := tx.Exec(ctx,
			`UPDATE users SET totp_enabled = false, totp_secret = NULL WHERE id = $1`,
			res.UserID,
		); err != nil {
			return nil, fmt.Errorf("update user: %w", err)
		}
	}

	if revokeSessions {
		ct, err := tx.Exec(ctx,
			`UPDATE sessions SET revoked = true WHERE user_id = $1 AND revoked = false`,
			res.UserID,
		)
		if err != nil {
			return nil, fmt.Errorf("revoke sessions: %w", err)
		}
		res.SessionsRevoked = ct.RowsAffected()
	}

	if _, err := tx.Exec(ctx, `
		INSERT INTO activity_logs (id, user_id, action, target_type, target_id, payload, created_at)
		VALUES ($1, NULL, 'cli.user.disable_2fa', 'user', $2,
		        jsonb_build_object('source', 'tale-cli', 'revoke_sessions', $3::boolean), NOW())
	`, uuid.New(), res.UserID, revokeSessions); err != nil {
		return nil, fmt.Errorf("audit log: %w", err)
	}

	if err := tx.Commit(ctx); err != nil {
		return nil, fmt.Errorf("commit: %w", err)
	}
	return res, nil
}
