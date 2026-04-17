package config

import (
	"fmt"
	"os"
	"strconv"
	"strings"
)

// Config holds all application configuration loaded from environment variables.
type Config struct {
	// Server
	ServerPort int
	Env        string
	LogLevel   string

	// Database
	DatabaseURL string

	// Redis
	RedisURL string

	// JWT
	JWTSecret        string
	JWTRefreshSecret string

	// CORS
	CORSOrigins []string

	// MinIO
	MinIOEndpoint  string
	MinIOAccessKey string
	MinIOSecretKey string
	MinioBucket    string

	// Daemon
	// DaemonServersDir is the base path on daemon nodes where server data lives.
	// Each server gets a subdirectory: {DaemonServersDir}/{server_id}
	DaemonServersDir string

	// CurseForge
	CurseForgeAPIKey string
	CurseForgeGameID int

	// MariaDB (for per-server databases)
	MariaDBDSN  string // e.g. "root:password@tcp(mariadb:3306)/"
	MariaDBHost string // hostname for server connections, e.g. "mariadb"
	MariaDBPort int    // port for server connections, e.g. 3306
}

// Load reads configuration from environment variables, validates required fields,
// and returns a populated Config or an error describing the first failure.
func Load() (*Config, error) {
	cfg := &Config{}

	// SERVER_PORT — optional, default 8080
	portStr := os.Getenv("SERVER_PORT")
	if portStr == "" {
		cfg.ServerPort = 8080
	} else {
		port, err := strconv.Atoi(portStr)
		if err != nil {
			return nil, fmt.Errorf("SERVER_PORT must be a valid integer: %w", err)
		}
		if port < 1 || port > 65535 {
			return nil, fmt.Errorf("SERVER_PORT must be between 1 and 65535, got %d", port)
		}
		cfg.ServerPort = port
	}

	// ENV — optional, default "production"
	cfg.Env = os.Getenv("ENV")
	if cfg.Env == "" {
		cfg.Env = "production"
	}
	if cfg.Env != "development" && cfg.Env != "production" {
		return nil, fmt.Errorf("ENV must be 'development' or 'production', got %q", cfg.Env)
	}

	// LOG_LEVEL — optional, default "info"
	cfg.LogLevel = os.Getenv("LOG_LEVEL")
	if cfg.LogLevel == "" {
		cfg.LogLevel = "info"
	}

	// DATABASE_URL — required
	cfg.DatabaseURL = os.Getenv("DATABASE_URL")
	if cfg.DatabaseURL == "" {
		return nil, fmt.Errorf("DATABASE_URL is required")
	}

	// REDIS_URL — required
	cfg.RedisURL = os.Getenv("REDIS_URL")
	if cfg.RedisURL == "" {
		return nil, fmt.Errorf("REDIS_URL is required")
	}

	// JWT_SECRET — required, min 32 chars
	cfg.JWTSecret = os.Getenv("JWT_SECRET")
	if cfg.JWTSecret == "" {
		return nil, fmt.Errorf("JWT_SECRET is required")
	}
	if len(cfg.JWTSecret) < 32 {
		return nil, fmt.Errorf("JWT_SECRET must be at least 32 characters long, got %d", len(cfg.JWTSecret))
	}

	// JWT_REFRESH_SECRET — required
	cfg.JWTRefreshSecret = os.Getenv("JWT_REFRESH_SECRET")
	if cfg.JWTRefreshSecret == "" {
		return nil, fmt.Errorf("JWT_REFRESH_SECRET is required")
	}

	// CORS_ORIGINS — optional, comma-separated
	corsRaw := os.Getenv("CORS_ORIGINS")
	if corsRaw != "" {
		parts := strings.Split(corsRaw, ",")
		for _, p := range parts {
			trimmed := strings.TrimSpace(p)
			if trimmed != "" {
				cfg.CORSOrigins = append(cfg.CORSOrigins, trimmed)
			}
		}
	}

	// MinIO — optional (features degrade gracefully when absent)
	cfg.MinIOEndpoint = os.Getenv("MINIO_ENDPOINT")
	cfg.MinIOAccessKey = os.Getenv("MINIO_ACCESS_KEY")
	cfg.MinIOSecretKey = os.Getenv("MINIO_SECRET_KEY")
	cfg.MinioBucket = os.Getenv("MINIO_BUCKET")

	// DAEMON_SERVERS_DIR — base directory path used by daemon nodes for server data.
	// The API appends /{server_id} to construct each server's data_path.
	cfg.DaemonServersDir = os.Getenv("DAEMON_SERVERS_DIR")
	if cfg.DaemonServersDir == "" {
		cfg.DaemonServersDir = "/srv/taledaemon/servers"
	}

	// CurseForge — optional; features degrade gracefully when absent.
	cfg.CurseForgeAPIKey = os.Getenv("CURSEFORGE_API_KEY")
	if gameIDStr := os.Getenv("CURSEFORGE_GAME_ID"); gameIDStr != "" {
		id, err := strconv.Atoi(gameIDStr)
		if err != nil {
			return nil, fmt.Errorf("CURSEFORGE_GAME_ID must be an integer: %w", err)
		}
		cfg.CurseForgeGameID = id
	}

	// MariaDB — optional; per-server database feature degrades gracefully.
	cfg.MariaDBDSN = os.Getenv("MARIADB_DSN")
	cfg.MariaDBHost = os.Getenv("MARIADB_HOST")
	if cfg.MariaDBHost == "" {
		cfg.MariaDBHost = "mariadb"
	}
	if portStr := os.Getenv("MARIADB_PORT"); portStr != "" {
		p, err := strconv.Atoi(portStr)
		if err != nil {
			return nil, fmt.Errorf("MARIADB_PORT must be an integer: %w", err)
		}
		cfg.MariaDBPort = p
	} else {
		cfg.MariaDBPort = 3306
	}

	return cfg, nil
}

// IsDevelopment returns true when the ENV is set to "development".
func (c *Config) IsDevelopment() bool {
	return c.Env == "development"
}
