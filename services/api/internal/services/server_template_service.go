package services

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"regexp"
	"strings"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
)

var (
	ErrServerTemplateNotFound = errors.New("server template not found")
	// ErrServerTemplateBuiltin: built-in templates ship with the panel and
	// cannot be changed or deleted.
	ErrServerTemplateBuiltin = errors.New("built-in templates are read-only")
	// ErrInvalidServerTemplate wraps a rejected template field; handlers
	// report its message as a 400.
	ErrInvalidServerTemplate = errors.New("invalid server template")
)

// ServerTemplateSettings are the server settings a template presets. Every
// field is optional; nil means "use the normal default". Only fields the
// create path can apply belong here.
type ServerTemplateSettings struct {
	HytaleVersion *string   `json:"hytale_version,omitempty"`
	RAMLimitMB    *int      `json:"ram_limit_mb,omitempty"`
	CPULimit      *int      `json:"cpu_limit,omitempty"`
	DiskLimitMB   *int      `json:"disk_limit_mb,omitempty"`
	AutoRestart   *bool     `json:"auto_restart,omitempty"`
	CrashLimit    *int      `json:"crash_limit,omitempty"`
	CrashWindowS  *int      `json:"crash_window_s,omitempty"`
	LaunchFlags   *[]string `json:"launch_flags,omitempty"`
	NetworkRole   *string   `json:"network_role,omitempty"`
}

// ServerTemplate is a named preset for new servers.
type ServerTemplate struct {
	ID          uuid.UUID              `json:"id"`
	Name        string                 `json:"name"`
	Description string                 `json:"description"`
	Icon        string                 `json:"icon"`
	IsBuiltin   bool                   `json:"is_builtin"`
	CreatedBy   *uuid.UUID             `json:"created_by"`
	Settings    ServerTemplateSettings `json:"settings"`
	CreatedAt   time.Time              `json:"created_at"`
	UpdatedAt   time.Time              `json:"updated_at"`
}

// CreateServerTemplateRequest is the body of POST /admin/server-templates.
type CreateServerTemplateRequest struct {
	Name        string                 `json:"name"`
	Description string                 `json:"description"`
	Icon        string                 `json:"icon"`
	Settings    ServerTemplateSettings `json:"settings"`
}

// UpdateServerTemplateRequest is the body of PATCH /admin/server-templates/:id.
// A given settings object replaces the stored settings as a whole.
type UpdateServerTemplateRequest struct {
	Name        *string                 `json:"name"`
	Description *string                 `json:"description"`
	Icon        *string                 `json:"icon"`
	Settings    *ServerTemplateSettings `json:"settings"`
}

// TemplateFromServerRequest is the body of
// POST /admin/server-templates/from-server/:id.
type TemplateFromServerRequest struct {
	Name        string `json:"name"`
	Description string `json:"description"`
	Icon        string `json:"icon"`
}

const (
	maxTemplateNameLen        = 80
	maxTemplateDescriptionLen = 500
)

// Material Symbols ligature names: lowercase letters, digits, underscores.
var templateIconRe = regexp.MustCompile(`^[a-z0-9_]{0,40}$`)

// ServerTemplateService stores server templates.
type ServerTemplateService struct {
	db *pgxpool.Pool
}

// NewServerTemplateService constructs a ServerTemplateService.
func NewServerTemplateService(db *pgxpool.Pool) *ServerTemplateService {
	return &ServerTemplateService{db: db}
}

const templateColumns = `id, name, description, icon, is_builtin, created_by, settings, created_at, updated_at`

func scanTemplate(row pgx.Row) (*ServerTemplate, error) {
	t := &ServerTemplate{}
	var raw []byte
	if err := row.Scan(&t.ID, &t.Name, &t.Description, &t.Icon, &t.IsBuiltin,
		&t.CreatedBy, &raw, &t.CreatedAt, &t.UpdatedAt); err != nil {
		return nil, err
	}
	if err := json.Unmarshal(raw, &t.Settings); err != nil {
		return nil, fmt.Errorf("decoding template settings: %w", err)
	}
	return t, nil
}

// List returns every template, built-ins first, then by name.
func (s *ServerTemplateService) List(ctx context.Context) ([]*ServerTemplate, error) {
	rows, err := s.db.Query(ctx, `SELECT `+templateColumns+` FROM server_templates ORDER BY is_builtin DESC, name`)
	if err != nil {
		return nil, fmt.Errorf("listing server templates: %w", err)
	}
	defer rows.Close()

	out := []*ServerTemplate{}
	for rows.Next() {
		t, err := scanTemplate(rows)
		if err != nil {
			return nil, fmt.Errorf("scanning server template: %w", err)
		}
		out = append(out, t)
	}
	return out, rows.Err()
}

// Get returns one template.
func (s *ServerTemplateService) Get(ctx context.Context, id uuid.UUID) (*ServerTemplate, error) {
	return getServerTemplate(ctx, s.db, id)
}

func getServerTemplate(ctx context.Context, db *pgxpool.Pool, id uuid.UUID) (*ServerTemplate, error) {
	t, err := scanTemplate(db.QueryRow(ctx, `SELECT `+templateColumns+` FROM server_templates WHERE id = $1`, id))
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return nil, ErrServerTemplateNotFound
		}
		return nil, fmt.Errorf("fetching server template: %w", err)
	}
	return t, nil
}

// Create stores a new custom template.
func (s *ServerTemplateService) Create(ctx context.Context, createdBy uuid.UUID, req CreateServerTemplateRequest) (*ServerTemplate, error) {
	name, desc, icon, err := validateTemplateMeta(req.Name, req.Description, req.Icon)
	if err != nil {
		return nil, err
	}
	settings, err := normalizeTemplateSettings(req.Settings)
	if err != nil {
		return nil, err
	}
	raw, err := json.Marshal(settings)
	if err != nil {
		return nil, fmt.Errorf("encoding template settings: %w", err)
	}
	t, err := scanTemplate(s.db.QueryRow(ctx, `
		INSERT INTO server_templates (name, description, icon, is_builtin, created_by, settings)
		VALUES ($1, $2, $3, false, $4, $5)
		RETURNING `+templateColumns,
		name, desc, icon, createdBy, raw))
	if err != nil {
		return nil, fmt.Errorf("inserting server template: %w", err)
	}
	return t, nil
}

// Update changes a custom template. Built-ins are read-only.
func (s *ServerTemplateService) Update(ctx context.Context, id uuid.UUID, req UpdateServerTemplateRequest) (*ServerTemplate, error) {
	cur, err := s.Get(ctx, id)
	if err != nil {
		return nil, err
	}
	if cur.IsBuiltin {
		return nil, ErrServerTemplateBuiltin
	}
	name, desc, icon := cur.Name, cur.Description, cur.Icon
	if req.Name != nil {
		name = *req.Name
	}
	if req.Description != nil {
		desc = *req.Description
	}
	if req.Icon != nil {
		icon = *req.Icon
	}
	name, desc, icon, err = validateTemplateMeta(name, desc, icon)
	if err != nil {
		return nil, err
	}
	settings := cur.Settings
	if req.Settings != nil {
		settings = *req.Settings
	}
	settings, err = normalizeTemplateSettings(settings)
	if err != nil {
		return nil, err
	}
	raw, err := json.Marshal(settings)
	if err != nil {
		return nil, fmt.Errorf("encoding template settings: %w", err)
	}
	t, err := scanTemplate(s.db.QueryRow(ctx, `
		UPDATE server_templates
		SET name = $1, description = $2, icon = $3, settings = $4, updated_at = NOW()
		WHERE id = $5 AND NOT is_builtin
		RETURNING `+templateColumns,
		name, desc, icon, raw, id))
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return nil, ErrServerTemplateNotFound
		}
		return nil, fmt.Errorf("updating server template: %w", err)
	}
	return t, nil
}

// Delete removes a custom template. Built-ins cannot be deleted.
func (s *ServerTemplateService) Delete(ctx context.Context, id uuid.UUID) error {
	cur, err := s.Get(ctx, id)
	if err != nil {
		return err
	}
	if cur.IsBuiltin {
		return ErrServerTemplateBuiltin
	}
	if _, err := s.db.Exec(ctx, `DELETE FROM server_templates WHERE id = $1 AND NOT is_builtin`, id); err != nil {
		return fmt.Errorf("deleting server template: %w", err)
	}
	return nil
}

// CreateFromServer saves an existing server's current settings as a new
// custom template.
func (s *ServerTemplateService) CreateFromServer(ctx context.Context, createdBy, serverID uuid.UUID, req TemplateFromServerRequest) (*ServerTemplate, error) {
	var (
		version                               string
		cpu, ram, disk, crashLimit, crashWind int
		autoRestart                           bool
		flags                                 []string
		role                                  *string
	)
	err := s.db.QueryRow(ctx, `
		SELECT hytale_version, cpu_limit, ram_limit_mb, disk_limit_mb, auto_restart,
		       crash_limit, crash_window_s, launch_flags, network_role
		FROM servers WHERE id = $1
	`, serverID).Scan(&version, &cpu, &ram, &disk, &autoRestart, &crashLimit, &crashWind, &flags, &role)
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return nil, ErrServerNotFound
		}
		return nil, fmt.Errorf("loading server settings: %w", err)
	}
	if flags == nil {
		flags = []string{}
	}
	settings := ServerTemplateSettings{
		HytaleVersion: &version,
		RAMLimitMB:    &ram,
		CPULimit:      &cpu,
		DiskLimitMB:   &disk,
		AutoRestart:   &autoRestart,
		CrashLimit:    &crashLimit,
		CrashWindowS:  &crashWind,
		LaunchFlags:   &flags,
		NetworkRole:   role,
	}
	return s.Create(ctx, createdBy, CreateServerTemplateRequest{
		Name:        req.Name,
		Description: req.Description,
		Icon:        req.Icon,
		Settings:    settings,
	})
}

func validateTemplateMeta(name, desc, icon string) (string, string, string, error) {
	name = strings.TrimSpace(name)
	desc = strings.TrimSpace(desc)
	icon = strings.TrimSpace(icon)
	if name == "" {
		return "", "", "", fmt.Errorf("%w: name is required", ErrInvalidServerTemplate)
	}
	if len([]rune(name)) > maxTemplateNameLen {
		return "", "", "", fmt.Errorf("%w: name is longer than %d characters", ErrInvalidServerTemplate, maxTemplateNameLen)
	}
	if len([]rune(desc)) > maxTemplateDescriptionLen {
		return "", "", "", fmt.Errorf("%w: description is longer than %d characters", ErrInvalidServerTemplate, maxTemplateDescriptionLen)
	}
	if !templateIconRe.MatchString(icon) {
		return "", "", "", fmt.Errorf("%w: icon must be a Material Symbols name (a-z, 0-9, _)", ErrInvalidServerTemplate)
	}
	return name, desc, icon, nil
}

// normalizeTemplateSettings checks the values a template may hold. It checks
// shape only (allowlisted flags, known roles, sane numbers); whether the
// person creating a server may use them is decided when the template is
// applied, by the same checks as explicit request fields.
func normalizeTemplateSettings(in ServerTemplateSettings) (ServerTemplateSettings, error) {
	out := in
	bad := func(format string, args ...any) (ServerTemplateSettings, error) {
		return ServerTemplateSettings{}, fmt.Errorf("%w: "+format, append([]any{ErrInvalidServerTemplate}, args...)...)
	}
	if in.HytaleVersion != nil {
		v, err := NormalizeHytaleVersion(*in.HytaleVersion)
		if err != nil {
			return bad("%v", err)
		}
		out.HytaleVersion = &v
	}
	if in.RAMLimitMB != nil && *in.RAMLimitMB < MinServerRAMMB {
		return bad("ram_limit_mb must be at least %d", MinServerRAMMB)
	}
	if in.CPULimit != nil && *in.CPULimit < 0 {
		return bad("cpu_limit cannot be negative")
	}
	if in.DiskLimitMB != nil && *in.DiskLimitMB <= 0 {
		return bad("disk_limit_mb must be positive")
	}
	if in.CrashLimit != nil && *in.CrashLimit < 0 {
		return bad("crash_limit cannot be negative")
	}
	if in.CrashWindowS != nil && *in.CrashWindowS <= 0 {
		return bad("crash_window_s must be positive")
	}
	if in.LaunchFlags != nil {
		flags, err := normalizeLaunchFlags(*in.LaunchFlags)
		if err != nil {
			return bad("%s", strings.TrimPrefix(err.Error(), ErrInvalidServerSettings.Error()+": "))
		}
		out.LaunchFlags = &flags
	}
	if in.NetworkRole != nil {
		role, err := normalizeNetworkRole(*in.NetworkRole)
		if err != nil {
			return bad("network_role must be hub, gamemode or none")
		}
		out.NetworkRole = role
	}
	return out, nil
}

// normalizeNetworkRole maps a requested role to the stored value: nil for
// "none"/"" (not networked), or "hub"/"gamemode".
func normalizeNetworkRole(role string) (*string, error) {
	switch r := strings.ToLower(strings.TrimSpace(role)); r {
	case "hub", "gamemode":
		return &r, nil
	case "", "none":
		return nil, nil
	default:
		return nil, ErrInvalidNetworkRole
	}
}
