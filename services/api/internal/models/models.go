package models

import (
	"encoding/json"
	"time"

	"github.com/google/uuid"
)

// Role constants define the user privilege hierarchy.
// Higher numeric value = more privilege.
const (
	RoleUser      = "user"
	RoleModerator = "moderator"
	RoleAdmin     = "admin"
	RoleOwner     = "owner"
)

// RoleWeight returns the numeric weight of a role for comparison.
func RoleWeight(role string) int {
	switch role {
	case RoleOwner:
		return 4
	case RoleAdmin:
		return 3
	case RoleModerator:
		return 2
	case RoleUser:
		return 1
	default:
		return 0
	}
}

// Server status constants.
const (
	StatusInstalling = "installing"
	StatusStopped    = "stopped"
	StatusStarting   = "starting"
	StatusRunning    = "running"
	StatusStopping   = "stopping"
	StatusCrashed    = "crashed"
)

// User represents an authenticated principal in the system.
type User struct {
	ID           uuid.UUID  `json:"id"`
	Email        string     `json:"email"`
	Username     string     `json:"username"`
	PasswordHash string     `json:"-"`
	Role         string     `json:"role"`
	TOTPSecret   string     `json:"-"`
	TOTPEnabled  bool       `json:"totp_enabled"`
	CreatedAt    time.Time  `json:"created_at"`
	LastLoginAt  *time.Time `json:"last_login_at,omitempty"`
	IsActive     bool       `json:"is_active"`
	DisplayName  *string    `json:"display_name,omitempty"`
	AvatarURL    *string    `json:"avatar_url,omitempty"`
	Language     string     `json:"language,omitempty"`
	Timezone     string     `json:"timezone,omitempty"`
}

// Session represents an active refresh-token session stored in the database.
type Session struct {
	ID         uuid.UUID `json:"id"`
	UserID     uuid.UUID `json:"user_id"`
	TokenHash  string    `json:"-"`
	IPAddress  string    `json:"ip_address"`
	UserAgent  string    `json:"user_agent"`
	CreatedAt  time.Time `json:"created_at"`
	ExpiresAt  time.Time `json:"expires_at"`
	Revoked    bool      `json:"revoked"`
}

// Node represents a physical or virtual daemon host that runs game servers.
type Node struct {
	ID              uuid.UUID       `json:"id"`
	Name            string          `json:"name"`
	FQDN            string          `json:"fqdn"`
	Port            int             `json:"port"`
	Location        *string         `json:"location,omitempty"`
	CertThumbprint  *string         `json:"cert_thumbprint,omitempty"`
	TotalCPU        int             `json:"total_cpu"`
	TotalRAMMB      int             `json:"total_ram_mb"`
	TotalDiskMB     int             `json:"total_disk_mb"`
	MaxServers      int             `json:"max_servers"`
	Status          string          `json:"status"`
	LastHeartbeat   *time.Time      `json:"last_heartbeat,omitempty"`
	CreatedAt       time.Time       `json:"created_at"`
	Metadata        json.RawMessage `json:"metadata,omitempty"`
}

// Server represents a managed Hytale game server instance.
type Server struct {
	ID             uuid.UUID       `json:"id"`
	Name           string          `json:"name"`
	NodeID         uuid.UUID       `json:"node_id"`
	OwnerID        uuid.UUID       `json:"owner_id"`
	Status         string          `json:"status"`
	HytaleVersion  string          `json:"hytale_version"`
	CPULimit       int             `json:"cpu_limit"`
	RAMLimitMB     int             `json:"ram_limit_mb"`
	DiskLimitMB    int             `json:"disk_limit_mb"`
	Port           int             `json:"port"`
	DataPath       string          `json:"data_path"`
	AutoRestart    bool            `json:"auto_restart"`
	CrashLimit     int             `json:"crash_limit"`
	CrashWindowS   int             `json:"crash_window_s"`
	ActiveWorld    string          `json:"active_world"`
	CreatedAt      time.Time       `json:"created_at"`
	UpdatedAt      time.Time       `json:"updated_at"`
	Metadata       json.RawMessage `json:"metadata,omitempty"`

	// Joined from the nodes table — populated by GetServer so the web UI
	// can render the public connect address (<node_fqdn>:<port>) without
	// a second round trip.  Empty when the server has no node assigned.
	NodeFQDN string `json:"node_fqdn,omitempty"`
}

// ServerLog is a single log line pushed by a TaleDaemon node.
type ServerLog struct {
	ID       int64     `json:"id"`
	ServerID uuid.UUID `json:"server_id"`
	LoggedAt time.Time `json:"logged_at"`
	Level    string    `json:"level"`
	Message  string    `json:"message"`
}

// ServerMod tracks a .jar plugin installed on a server via CurseForge or manual upload.
type ServerMod struct {
	ID               uuid.UUID       `json:"id"`
	ServerID         uuid.UUID       `json:"server_id"`
	Filename         string          `json:"filename"`
	DisplayName      string          `json:"display_name"`
	Version          string          `json:"version"`
	DownloadURL      string          `json:"download_url"`
	CFModID          *int            `json:"cf_mod_id,omitempty"`
	CFFileID         *int            `json:"cf_file_id,omitempty"`
	InstalledAt      time.Time       `json:"installed_at"`
	Source           string          `json:"source"`
	PluginName       *string         `json:"plugin_name,omitempty"`
	Author           *string         `json:"author,omitempty"`
	Description      *string         `json:"description,omitempty"`
	DetectedCommands json.RawMessage `json:"detected_commands,omitempty"`
	ConfigFiles      json.RawMessage `json:"config_files,omitempty"`
	FileHash         *string         `json:"file_hash,omitempty"`
	LastScannedAt    *time.Time      `json:"last_scanned_at,omitempty"`
	IsPresent        bool            `json:"is_present"`
}

// GameCommand is a predefined command template shown in the Game Control panel.
type GameCommand struct {
	ID              uuid.UUID       `json:"id"`
	ServerID        *uuid.UUID      `json:"server_id,omitempty"`
	Category        string          `json:"category"`
	Name            string          `json:"name"`
	Description     string          `json:"description"`
	CommandTemplate string          `json:"command_template"`
	Icon            string          `json:"icon"`
	Params          json.RawMessage `json:"params"`
	SortOrder       int             `json:"sort_order"`
	IsDefault       bool            `json:"is_default"`
	MinRole         string          `json:"min_role"`
	CreatedAt       time.Time       `json:"created_at"`
	Source          string          `json:"source"`
	SourcePlugin    *string         `json:"source_plugin,omitempty"`
}

// CommandParam describes a parameter placeholder in a command template.
type CommandParam struct {
	Name        string `json:"name"`
	Type        string `json:"type"`
	Required    bool   `json:"required"`
	Placeholder string `json:"placeholder"`
	Default     string `json:"default,omitempty"`
}

// ActivityLog records a mutating action performed by a user or system.
type ActivityLog struct {
	ID         uuid.UUID       `json:"id"`
	UserID     *uuid.UUID      `json:"user_id,omitempty"`
	ServerID   *uuid.UUID      `json:"server_id,omitempty"`
	Action     string          `json:"action"`
	TargetType string          `json:"target_type"`
	TargetID   *uuid.UUID      `json:"target_id,omitempty"`
	IPAddress  string          `json:"ip_address"`
	Payload    json.RawMessage `json:"payload,omitempty"`
	CreatedAt  time.Time       `json:"created_at"`
}

// World represents a Hytale world tracked per server.
type World struct {
	ID        uuid.UUID       `json:"id"`
	ServerID  uuid.UUID       `json:"server_id"`
	Name      string          `json:"name"`
	Seed      *int64          `json:"seed,omitempty"`
	Generator *string         `json:"generator,omitempty"`
	IsActive  bool            `json:"is_active"`
	SizeBytes *int64          `json:"size_bytes,omitempty"`
	Thumbnail *string         `json:"thumbnail,omitempty"`
	Metadata  json.RawMessage `json:"metadata,omitempty"`
	CreatedAt time.Time       `json:"created_at"`
	UpdatedAt time.Time       `json:"updated_at"`
}

// Player represents a known player on a server.
type Player struct {
	ID            uuid.UUID  `json:"id"`
	ServerID      uuid.UUID  `json:"server_id"`
	HytaleUUID    uuid.UUID  `json:"hytale_uuid"`
	Username      string     `json:"username"`
	FirstSeen     time.Time  `json:"first_seen"`
	LastSeen      *time.Time `json:"last_seen,omitempty"`
	PlaytimeS     int64      `json:"playtime_s"`
	IsWhitelisted bool       `json:"is_whitelisted"`
	IsBanned      bool       `json:"is_banned"`
	BanReason     *string    `json:"ban_reason,omitempty"`
	BannedAt      *time.Time `json:"banned_at,omitempty"`
	BannedBy      *uuid.UUID `json:"banned_by,omitempty"`
	IsOp          bool       `json:"is_op"`
	IsMuted       bool       `json:"is_muted"`
}

// Backup represents a server backup record.
type Backup struct {
	ID          uuid.UUID  `json:"id"`
	ServerID    *uuid.UUID `json:"server_id,omitempty"`
	WorldName   *string    `json:"world_name,omitempty"`
	Type        string     `json:"type"`
	Storage     string     `json:"storage"`
	StoragePath string     `json:"storage_path"`
	SizeBytes   *int64     `json:"size_bytes,omitempty"`
	Checksum    *string    `json:"checksum,omitempty"`
	Status      string     `json:"status"`
	TriggeredBy string     `json:"triggered_by"`
	CreatedAt   time.Time  `json:"created_at"`
	CompletedAt *time.Time `json:"completed_at,omitempty"`
	ExpiresAt   *time.Time `json:"expires_at,omitempty"`
	Error       *string    `json:"error,omitempty"`
}

// BackupSchedule represents a cron-based backup schedule.
type BackupSchedule struct {
	ID             uuid.UUID  `json:"id"`
	ServerID       uuid.UUID  `json:"server_id"`
	CronExpr       string     `json:"cron_expr"`
	Type           string     `json:"type"`
	Storage        string     `json:"storage"`
	RetentionCount *int       `json:"retention_count,omitempty"`
	RetentionDays  *int       `json:"retention_days,omitempty"`
	Enabled        bool       `json:"enabled"`
	LastRun        *time.Time `json:"last_run,omitempty"`
	NextRun        *time.Time `json:"next_run,omitempty"`
	CreatedAt      time.Time  `json:"created_at"`
}

// AlertRule represents an alert trigger rule.
type AlertRule struct {
	ID        uuid.UUID       `json:"id"`
	ServerID  *uuid.UUID      `json:"server_id,omitempty"`
	UserID    uuid.UUID       `json:"user_id"`
	Type      string          `json:"type"`
	Threshold *float64        `json:"threshold,omitempty"`
	Channels  json.RawMessage `json:"channels"`
	Enabled   bool            `json:"enabled"`
	CreatedAt time.Time       `json:"created_at"`
}

// Permission represents a granular permission key in the system catalogue.
type Permission struct {
	Key         string `json:"key"`
	Description string `json:"description"`
	Category    string `json:"category"`
}

// UserPermission represents a per-user permission override.
type UserPermission struct {
	ID      uuid.UUID `json:"id"`
	UserID  uuid.UUID `json:"user_id"`
	PermKey string    `json:"perm_key"`
	Granted bool      `json:"granted"`
}

// NotificationPref represents a user's notification preference for an alert type.
type NotificationPref struct {
	ID        uuid.UUID `json:"id"`
	UserID    uuid.UUID `json:"user_id"`
	AlertType string    `json:"alert_type"`
	Email     bool      `json:"email"`
	Discord   bool      `json:"discord"`
	Telegram  bool      `json:"telegram"`
}

// ServerInvitation represents a pending invitation to join a server.
type ServerInvitation struct {
	ID           uuid.UUID `json:"id"`
	ServerID     uuid.UUID `json:"server_id"`
	InviterID    uuid.UUID `json:"inviter_id"`
	InviteeEmail string    `json:"invitee_email"`
	Token        string    `json:"token,omitempty"`
	Role         string    `json:"role"`
	Status       string    `json:"status"`
	CreatedAt    time.Time `json:"created_at"`
	ExpiresAt    time.Time `json:"expires_at"`
}

// ServerDatabase represents a per-server MariaDB database.
type ServerDatabase struct {
	ID         uuid.UUID `json:"id"`
	ServerID   uuid.UUID `json:"server_id"`
	DBName     string    `json:"db_name"`
	DBUser     string    `json:"db_user"`
	DBPassword string    `json:"db_password"`
	Host       string    `json:"host"`
	Port       int       `json:"port"`
	CreatedAt  time.Time `json:"created_at"`
}

// AlertEvent represents a fired alert instance.
type AlertEvent struct {
	ID         uuid.UUID       `json:"id"`
	RuleID     *uuid.UUID      `json:"rule_id,omitempty"`
	ServerID   *uuid.UUID      `json:"server_id,omitempty"`
	NodeID     *uuid.UUID      `json:"node_id,omitempty"`
	Type       string          `json:"type"`
	Severity   string          `json:"severity"`
	Title      string          `json:"title"`
	Body       *string         `json:"body,omitempty"`
	Metadata   json.RawMessage `json:"metadata,omitempty"`
	Resolved   bool            `json:"resolved"`
	ResolvedAt *time.Time      `json:"resolved_at,omitempty"`
	CreatedAt  time.Time       `json:"created_at"`
}
