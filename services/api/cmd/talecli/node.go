package main

import (
	"context"
	"errors"
	"fmt"
	"os"
	"strings"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/spf13/cobra"
	"go.uber.org/zap"

	"github.com/BitaceS/talepanel/api/internal/db"
	"github.com/BitaceS/talepanel/api/internal/services"
)

func nodeCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "node",
		Short: "Node administration",
	}
	cmd.AddCommand(nodeEnrollCmd())
	return cmd
}

// nodeEnrollCmd mints a one-shot enrollment token, like POST
// /admin/nodes/enroll, without needing a user account. The installer uses it
// to register the local daemon before the first owner exists.
func nodeEnrollCmd() *cobra.Command {
	var name string
	var maxServers int
	var ttl time.Duration

	cmd := &cobra.Command{
		Use:   "enroll",
		Short: "Create a node enrollment token and print it",
		RunE: func(cmd *cobra.Command, args []string) error {
			name = strings.TrimSpace(name)
			if name == "" {
				return errors.New("--name is required")
			}
			if maxServers < 1 {
				return errors.New("--max-servers must be at least 1")
			}
			if ttl < time.Minute || ttl > 24*time.Hour {
				return errors.New("--ttl must be between 1m and 24h")
			}

			url := os.Getenv("DATABASE_URL")
			if url == "" {
				return errors.New("DATABASE_URL is not set")
			}
			ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
			defer cancel()

			pool, err := pgxpool.New(ctx, url)
			if err != nil {
				return fmt.Errorf("connect: %w", err)
			}
			defer pool.Close()

			// Logs go to stderr so stdout carries only the token.
			migLogger, _ := zap.NewProduction()
			defer func() { _ = migLogger.Sync() }()
			if err := db.RunMigrations(ctx, pool, migLogger); err != nil {
				return fmt.Errorf("run migrations: %w", err)
			}

			_, token, err := services.NewEnrollmentService(pool).Create(ctx, services.CreateEnrollmentRequest{
				NodeName:   name,
				MaxServers: maxServers,
				TTL:        ttl,
			})
			if err != nil {
				return err
			}
			fmt.Println(token)
			return nil
		},
	}

	cmd.Flags().StringVar(&name, "name", "", "node name shown in the panel")
	cmd.Flags().IntVar(&maxServers, "max-servers", 10, "maximum number of servers on the node")
	cmd.Flags().DurationVar(&ttl, "ttl", 15*time.Minute, "how long the token stays valid")
	return cmd
}
