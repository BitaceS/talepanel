package config

import (
	"encoding/hex"
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
	AppVersion string

	// DeploymentProfile is the operator archetype seeded by the installer.
	// Values: "solo" (single-host hobbyist) or "hoster" (multi-tenant provider).
	// Used by the web frontend to seed default module visibility on first load.
	DeploymentProfile string

	// Database
	DatabaseURL string

	// Redis
	RedisURL string

	// JWT
	JWTSecret        string
	JWTRefreshSecret string

	// Encryption
	// TOTPEncKey is a 32-byte key (hex-encoded in the env as 64 hex chars)
	// used for AES-256-GCM encryption of TOTP secrets at rest.
	TOTPEncKey []byte

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

	// SMTP (for alert notifications)
	SMTPHost     string
	SMTPPort     int
	SMTPUser     string
	SMTPPassword string
	SMTPFrom     string
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

	// APP_VERSION — optional, default "dev"
	cfg.AppVersion = os.Getenv("APP_VERSION")
	if cfg.AppVersion == "" {
		cfg.AppVersion = "dev"
	}

	// DEPLOYMENT_PROFILE — optional, default "solo".
	// Only "solo" and "hoster" are recognised; anything else falls back to "solo".
	cfg.DeploymentProfile = strings.ToLower(strings.TrimSpace(os.Getenv("DEPLOYMENT_PROFILE")))
	if cfg.DeploymentProfile != "solo" && cfg.DeploymentProfile != "hoster" {
		cfg.DeploymentProfile = "solo"
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

	// JWT_SECRET — required, min 32 chars, reject placeholder.
	cfg.JWTSecret = os.Getenv("JWT_SECRET")
	if cfg.JWTSecret == "" {
		return nil, fmt.Errorf("JWT_SECRET is required")
	}
	if isPlaceholderSecret(cfg.JWTSecret) {
		return nil, fmt.Errorf("JWT_SECRET is still set to a placeholder value; run the installer or regenerate with `openssl rand -hex 32`")
	}
	if len(cfg.JWTSecret) < 32 {
		return nil, fmt.Errorf("JWT_SECRET must be at least 32 characters long, got %d", len(cfg.JWTSecret))
	}

	// JWT_REFRESH_SECRET — required, min 32 chars, reject placeholder.
	cfg.JWTRefreshSecret = os.Getenv("JWT_REFRESH_SECRET")
	if cfg.JWTRefreshSecret == "" {
		return nil, fmt.Errorf("JWT_REFRESH_SECRET is required")
	}
	if isPlaceholderSecret(cfg.JWTRefreshSecret) {
		return nil, fmt.Errorf("JWT_REFRESH_SECRET is still set to a placeholder value; run the installer or regenerate with `openssl rand -hex 32`")
	}
	if len(cfg.JWTRefreshSecret) < 32 {
		return nil, fmt.Errorf("JWT_REFRESH_SECRET must be at least 32 characters long, got %d", len(cfg.JWTRefreshSecret))
	}

	// TOTP_ENC_KEY — required, 32 bytes hex-encoded (64 hex chars) for AES-256-GCM.
	totpKeyHex := os.Getenv("TOTP_ENC_KEY")
	if totpKeyHex == "" {
		return nil, fmt.Errorf("TOTP_ENC_KEY is required (generate with `openssl rand -hex 32`)")
	}
	if isPlaceholderSecret(totpKeyHex) {
		return nil, fmt.Errorf("TOTP_ENC_KEY is still set to a placeholder value; run the installer or regenerate with `openssl rand -hex 32`")
	}
	totpKey, err := hex.DecodeString(totpKeyHex)
	if err != nil {
		return nil, fmt.Errorf("TOTP_ENC_KEY must be hex-encoded: %w", err)
	}
	if len(totpKey) != 32 {
		return nil, fmt.Errorf("TOTP_ENC_KEY must decode to exactly 32 bytes, got %d", len(totpKey))
	}
	cfg.TOTPEncKey = totpKey

	// CORS_ORIGINS — optional, comma-separated
	corsRaw := os.Getenv("CORS_ORIGINS")
	if corsRaw != "" {
		for p := range strings.SplitSeq(corsRaw, ",") {
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

	// SMTP — optional (leave SMTP_HOST empty to disable email notifications)
	cfg.SMTPHost = os.Getenv("SMTP_HOST")
	cfg.SMTPUser = os.Getenv("SMTP_USER")
	cfg.SMTPPassword = os.Getenv("SMTP_PASSWORD")
	cfg.SMTPFrom = os.Getenv("SMTP_FROM")
	if portStr := os.Getenv("SMTP_PORT"); portStr != "" {
		port, err := strconv.Atoi(portStr)
		if err != nil {
			return nil, fmt.Errorf("SMTP_PORT must be an integer: %w", err)
		}
		cfg.SMTPPort = port
	} else {
		cfg.SMTPPort = 587
	}

	return cfg, nil
}

// IsDevelopment returns true when the ENV is set to "development".
func (c *Config) IsDevelopment() bool {
	return c.Env == "development"
}

// isPlaceholderSecret catches the two common "you forgot to set this" shapes:
// the installer's CHANGEME sentinel, and .env.example's verbose
// "replace-with-..." placeholders. Both are long enough to pass the 32-char
// check so we need an explicit reject.
func isPlaceholderSecret(v string) bool {
	if v == "CHANGEME_GENERATED_BY_INSTALLER" {
		return true
	}
	if strings.HasPrefix(v, "replace-with-") {
		return true
	}
	return false
}
