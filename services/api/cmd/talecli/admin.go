package main

import (
	"context"
	"errors"
	"fmt"
	"os"
	"strings"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/spf13/cobra"
	"golang.org/x/crypto/bcrypt"
	"golang.org/x/term"
)

func adminCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "admin",
		Short: "User administration",
	}
	cmd.AddCommand(adminCreateCmd())
	return cmd
}

func adminCreateCmd() *cobra.Command {
	var email, username, password string
	var nonInteractive bool

	cmd := &cobra.Command{
		Use:   "create",
		Short: "Create an owner account",
		RunE: func(cmd *cobra.Command, args []string) error {
			email = strings.ToLower(strings.TrimSpace(email))
			username = strings.TrimSpace(username)

			if !nonInteractive {
				if email == "" {
					email = promptLine("Admin email: ")
				}
				if username == "" {
					username = promptLine("Admin username: ")
				}
				if password == "" {
					p, err := promptPassword("Admin password (min 12 chars, 1 digit, 1 symbol): ")
					if err != nil {
						return err
					}
					password = p
				}
			}

			if email == "" || username == "" || password == "" {
				return errors.New("--email, --username, and --password are all required in non-interactive mode")
			}
			if err := validateOwnerPassword(password); err != nil {
				return err
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

			hash, err := bcrypt.GenerateFromPassword([]byte(password), 12)
			if err != nil {
				return fmt.Errorf("hash: %w", err)
			}

			id := uuid.New()
			_, err = pool.Exec(ctx, `
				INSERT INTO users (id, email, username, password_hash, role, is_active)
				VALUES ($1, $2, $3, $4, 'owner', true)
				ON CONFLICT (email) DO NOTHING
			`, id, email, username, string(hash))
			if err != nil {
				return fmt.Errorf("insert user: %w", err)
			}

			// Verify the row actually got inserted — ON CONFLICT DO NOTHING
			// silently skips duplicates, so we re-check by email.
			var existingID uuid.UUID
			err = pool.QueryRow(ctx,
				`SELECT id FROM users WHERE email = $1`, email,
			).Scan(&existingID)
			if err != nil {
				return fmt.Errorf("verify: %w", err)
			}
			if existingID != id {
				return fmt.Errorf("a user with email %s already exists", email)
			}

			fmt.Printf("Admin user created: %s (%s)\n", username, email)
			return nil
		},
	}

	cmd.Flags().StringVar(&email, "email", "", "admin email")
	cmd.Flags().StringVar(&username, "username", "", "admin username")
	cmd.Flags().StringVar(&password, "password", "", "admin password")
	cmd.Flags().BoolVar(&nonInteractive, "non-interactive", false, "fail instead of prompting for missing fields")
	return cmd
}

// promptLine reads a single line from stdin, stripping the trailing newline.
func promptLine(label string) string {
	fmt.Print(label)
	var s string
	_, _ = fmt.Scanln(&s)
	return s
}

// promptPassword reads a password from a TTY without echoing it back.
func promptPassword(label string) (string, error) {
	fmt.Print(label)
	b, err := term.ReadPassword(int(os.Stdin.Fd()))
	fmt.Println()
	if err != nil {
		return "", err
	}
	return string(b), nil
}

// validateOwnerPassword mirrors validatePasswordForRole("owner", ...) from
// the api package.  Duplicated intentionally — tale-cli does not import
// server-internal packages.
func validateOwnerPassword(password string) error {
	if len([]rune(password)) < 12 {
		return errors.New("password must be at least 12 characters long")
	}
	hasDigit, hasSymbol := false, false
	for _, r := range password {
		switch {
		case r >= '0' && r <= '9':
			hasDigit = true
		case (r >= 'a' && r <= 'z') || (r >= 'A' && r <= 'Z'):
		default:
			hasSymbol = true
		}
	}
	if !hasDigit || !hasSymbol {
		return errors.New("password must contain at least one digit and one non-alphanumeric symbol")
	}
	return nil
}
