package services

import (
	"context"
	"crypto/rand"
	"encoding/json"
	"errors"
	"fmt"
	"regexp"
	"strings"
	"sync"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/redis/go-redis/v9"
	"go.uber.org/zap"

	"github.com/BitaceS/talepanel/api/internal/discord"
	"github.com/BitaceS/talepanel/api/internal/models"
)

const (
	discordSettingsKey  = "discord_config"
	discordStatusMsgKey = "discord_status_message"
	discordLinkPrefix   = "discord_link:"
	discordTriesPrefix  = "discord_link_tries:"
	discordLinkTTL      = 10 * time.Minute
	discordMaxLinkTries = 5
	// discordStatusEvery is how often the status message is refreshed; it is
	// only edited when its content changed.
	discordStatusEvery = time.Minute
)

// DiscordConfig is the owner's Discord setup. BotToken is never sent back to
// the panel.
type DiscordConfig struct {
	Enabled         bool        `json:"enabled"`
	ApplicationID   string      `json:"application_id"`
	PublicKey       string      `json:"public_key"`
	BotToken        string      `json:"bot_token,omitempty"`
	GuildID         string      `json:"guild_id"`
	StatusChannelID string      `json:"status_channel_id"`
	StatusServerIDs []uuid.UUID `json:"status_server_ids"`
	ShowPlayerNames bool        `json:"show_player_names"`
	// Language of the status message: "en" or "de". Command replies follow
	// each Discord user's own language.
	Language string `json:"language"`
}

var (
	discordSnowflakeRe = regexp.MustCompile(`^[0-9]{15,21}$`)
	discordKeyRe       = regexp.MustCompile(`^[0-9a-f]{64}$`)
	discordTokenRe     = regexp.MustCompile(`^[A-Za-z0-9._-]{50,100}$`)
)

var (
	ErrDiscordConfig   = errors.New("invalid discord settings")
	ErrDiscordDisabled = errors.New("the discord bot is not enabled")
)

// Normalize trims the fields and fills defaults.
func (c DiscordConfig) Normalize() DiscordConfig {
	c.ApplicationID = strings.TrimSpace(c.ApplicationID)
	c.PublicKey = strings.ToLower(strings.TrimSpace(c.PublicKey))
	c.BotToken = strings.TrimSpace(c.BotToken)
	c.GuildID = strings.TrimSpace(c.GuildID)
	c.StatusChannelID = strings.TrimSpace(c.StatusChannelID)
	if c.Language != "de" {
		c.Language = "en"
	}
	seen := map[uuid.UUID]bool{}
	ids := make([]uuid.UUID, 0, len(c.StatusServerIDs))
	for _, id := range c.StatusServerIDs {
		if id != uuid.Nil && !seen[id] {
			seen[id] = true
			ids = append(ids, id)
		}
	}
	c.StatusServerIDs = ids
	return c
}

// Validate checks the format of every field that is set. Enabling needs the
// application, public key, bot token and guild.
func (c DiscordConfig) Validate() error {
	bad := func(msg string) error { return fmt.Errorf("%w: %s", ErrDiscordConfig, msg) }
	if c.ApplicationID != "" && !discordSnowflakeRe.MatchString(c.ApplicationID) {
		return bad("the application ID is a number of 15 to 21 digits")
	}
	if c.GuildID != "" && !discordSnowflakeRe.MatchString(c.GuildID) {
		return bad("the server ID is a number of 15 to 21 digits")
	}
	if c.StatusChannelID != "" && !discordSnowflakeRe.MatchString(c.StatusChannelID) {
		return bad("the channel ID is a number of 15 to 21 digits")
	}
	if c.PublicKey != "" && !discordKeyRe.MatchString(c.PublicKey) {
		return bad("the public key has 64 hex characters")
	}
	if c.BotToken != "" && !discordTokenRe.MatchString(c.BotToken) {
		return bad("that does not look like a bot token")
	}
	if len(c.StatusServerIDs) > 25 {
		return bad("at most 25 servers can be shown")
	}
	if c.Enabled {
		switch {
		case c.ApplicationID == "":
			return bad("the application ID is required")
		case c.PublicKey == "":
			return bad("the public key is required")
		case c.BotToken == "":
			return bad("the bot token is required")
		case c.GuildID == "":
			return bad("the server ID is required")
		}
	}
	return nil
}

// ServerPowerController starts and stops servers; the server handler
// implements it, so a command from Discord runs the same code as the panel.
type ServerPowerController interface {
	// PowerAction runs "start", "stop" or "restart" and returns the status
	// the server moves to. It does not check permissions.
	PowerAction(ctx context.Context, serverID uuid.UUID, action string) (string, error)
}

// ErrServerAlreadyRunning is returned by PowerAction for a start of a server
// that is running or starting.
var ErrServerAlreadyRunning = errors.New("server is already running or starting")

// DiscordService runs the bot: settings, account links, commands and the
// status message.
type DiscordService struct {
	db       *pgxpool.Pool
	rdb      *redis.Client
	settings *AppSettingsService
	servers  *ServerService
	perms    *PermissionService
	policy   *SecurityPolicyService
	client   *discord.Client
	log      *zap.Logger
	power    ServerPowerController
	now      func() time.Time

	nudge chan struct{}

	statusMu   sync.Mutex
	lastStatus string // JSON of the last status message sent
	lastErr    string // last status update error, to log each one once
}

// NewDiscordService wires the service. Call SetPower before serving
// commands.
func NewDiscordService(db *pgxpool.Pool, rdb *redis.Client, settings *AppSettingsService, servers *ServerService,
	perms *PermissionService, policy *SecurityPolicyService, log *zap.Logger) *DiscordService {
	if log == nil {
		log = zap.NewNop()
	}
	return &DiscordService{
		db: db, rdb: rdb, settings: settings, servers: servers, perms: perms, policy: policy,
		client: discord.NewClient(), log: log, now: time.Now,
		nudge: make(chan struct{}, 1),
	}
}

// SetPower sets the controller that starts and stops servers.
func (s *DiscordService) SetPower(p ServerPowerController) { s.power = p }

// SetClient replaces the Discord REST client (tests).
func (s *DiscordService) SetClient(c *discord.Client) { s.client = c }

// Config returns the stored settings (zero value when none).
func (s *DiscordService) Config(ctx context.Context) (DiscordConfig, error) {
	raw, err := s.settings.Get(ctx, discordSettingsKey)
	if err != nil || raw == "" {
		return DiscordConfig{}.Normalize(), err
	}
	var c DiscordConfig
	if err := json.Unmarshal([]byte(raw), &c); err != nil {
		return DiscordConfig{}, fmt.Errorf("decoding discord settings: %w", err)
	}
	return c.Normalize(), nil
}

// SetConfig validates and stores the settings. An empty bot token keeps the
// stored one for the same application. When enabled, the token is checked
// against Discord and the slash commands are registered in the guild, so a
// wrong token or a bot that is not in the server shows up here.
func (s *DiscordService) SetConfig(ctx context.Context, c DiscordConfig, updatedBy uuid.UUID) (DiscordConfig, error) {
	c = c.Normalize()
	old, err := s.Config(ctx)
	if err != nil {
		return DiscordConfig{}, err
	}
	if c.BotToken == "" && old.ApplicationID == c.ApplicationID {
		c.BotToken = old.BotToken
	}
	if err := c.Validate(); err != nil {
		return DiscordConfig{}, err
	}
	if c.Enabled {
		app, err := s.client.CurrentApplication(ctx, c.BotToken)
		if err != nil {
			return DiscordConfig{}, fmt.Errorf("%w: Discord refused the bot token (%v)", ErrDiscordConfig, err)
		}
		if app.ID != c.ApplicationID {
			return DiscordConfig{}, fmt.Errorf("%w: the bot token belongs to another application", ErrDiscordConfig)
		}
		if err := s.client.SetGuildCommands(ctx, c.BotToken, c.ApplicationID, c.GuildID, discordCommands()); err != nil {
			return DiscordConfig{}, fmt.Errorf("%w: could not register the commands in the Discord server; is the bot invited there? (%v)", ErrDiscordConfig, err)
		}
	}
	raw, err := json.Marshal(c)
	if err != nil {
		return DiscordConfig{}, err
	}
	if err := s.settings.Set(ctx, discordSettingsKey, string(raw), &updatedBy); err != nil {
		return DiscordConfig{}, err
	}
	if old.StatusChannelID != c.StatusChannelID {
		// A new channel gets a new message; the old one stays where it was.
		if err := s.settings.Set(ctx, discordStatusMsgKey, "", &updatedBy); err != nil {
			s.log.Warn("discord: clearing the status message id failed", zap.Error(err))
		}
	}
	s.statusMu.Lock()
	s.lastStatus = ""
	s.statusMu.Unlock()
	s.Nudge()
	s.audit(ctx, &updatedBy, nil, "admin.discord.settings_changed", map[string]any{
		"enabled": c.Enabled, "application_id": c.ApplicationID, "guild_id": c.GuildID,
		"status_channel_id": c.StatusChannelID, "status_servers": len(c.StatusServerIDs),
		"show_player_names": c.ShowPlayerNames,
	})
	return c, nil
}

// ─── Account links ───────────────────────────────────────────────────────────

// DiscordLink is a user's linked Discord account.
type DiscordLink struct {
	DiscordUserID   string    `json:"discord_user_id"`
	DiscordUsername string    `json:"discord_username"`
	LinkedAt        time.Time `json:"linked_at"`
}

var (
	ErrDiscordCodeInvalid = errors.New("this code is unknown or expired")
	ErrDiscordTooManyTry  = errors.New("too many wrong codes, wait a few minutes")
	ErrDiscordTaken       = errors.New("this discord account is linked to another TalePanel account")
	ErrDiscordNotLinked   = errors.New("no TalePanel account is linked to this discord account")
)

// codeAlphabet leaves out 0/O, 1/I/L so a code can be typed from a screen.
const codeAlphabet = "ABCDEFGHJKMNPQRSTUVWXYZ23456789"

func newLinkCode() (string, error) {
	b := make([]byte, 8)
	if _, err := rand.Read(b); err != nil {
		return "", err
	}
	out := make([]byte, 0, 9)
	for i, v := range b {
		if i == 4 {
			out = append(out, '-')
		}
		out = append(out, codeAlphabet[int(v)%len(codeAlphabet)])
	}
	return string(out), nil
}

func normalizeLinkCode(code string) string {
	code = strings.ToUpper(strings.ReplaceAll(strings.ReplaceAll(strings.TrimSpace(code), " ", ""), "-", ""))
	if len(code) != 8 {
		return ""
	}
	return code[:4] + "-" + code[4:]
}

// CreateLinkCode makes a single-use code the user types into /link in
// Discord. It is valid for 10 minutes.
func (s *DiscordService) CreateLinkCode(ctx context.Context, userID uuid.UUID) (string, time.Time, error) {
	c, err := s.Config(ctx)
	if err != nil {
		return "", time.Time{}, err
	}
	if !c.Enabled {
		return "", time.Time{}, ErrDiscordDisabled
	}
	code, err := newLinkCode()
	if err != nil {
		return "", time.Time{}, err
	}
	if err := s.rdb.Set(ctx, discordLinkPrefix+code, userID.String(), discordLinkTTL).Err(); err != nil {
		return "", time.Time{}, err
	}
	return code, s.now().Add(discordLinkTTL), nil
}

// Link connects the Discord user to the TalePanel account that created code.
// A Discord account can be linked to one TalePanel account; linking again
// from the same TalePanel account replaces its old Discord account.
func (s *DiscordService) Link(ctx context.Context, du discord.User, code string) (*models.User, error) {
	tries, err := s.rdb.Incr(ctx, discordTriesPrefix+du.ID).Result()
	if err != nil {
		return nil, err
	}
	if tries == 1 {
		s.rdb.Expire(ctx, discordTriesPrefix+du.ID, discordLinkTTL)
	}
	if tries > discordMaxLinkTries {
		return nil, ErrDiscordTooManyTry
	}
	code = normalizeLinkCode(code)
	if code == "" {
		return nil, ErrDiscordCodeInvalid
	}
	raw, err := s.rdb.GetDel(ctx, discordLinkPrefix+code).Result()
	if errors.Is(err, redis.Nil) {
		return nil, ErrDiscordCodeInvalid
	}
	if err != nil {
		return nil, err
	}
	userID, err := uuid.Parse(raw)
	if err != nil {
		return nil, ErrDiscordCodeInvalid
	}
	user, err := s.userByID(ctx, userID)
	if err != nil {
		return nil, err
	}
	if user == nil || !user.IsActive {
		return nil, ErrDiscordCodeInvalid
	}

	tx, err := s.db.Begin(ctx)
	if err != nil {
		return nil, err
	}
	defer func() { _ = tx.Rollback(ctx) }()
	var owner uuid.UUID
	err = tx.QueryRow(ctx, `SELECT user_id FROM user_discord_links WHERE discord_user_id = $1`, du.ID).Scan(&owner)
	if err == nil && owner != userID {
		return nil, ErrDiscordTaken
	}
	if err != nil && !errors.Is(err, pgx.ErrNoRows) {
		return nil, err
	}
	if _, err := tx.Exec(ctx, `
		INSERT INTO user_discord_links (user_id, discord_user_id, discord_username, linked_at)
		VALUES ($1, $2, $3, NOW())
		ON CONFLICT (user_id) DO UPDATE
		   SET discord_user_id = EXCLUDED.discord_user_id,
		       discord_username = EXCLUDED.discord_username,
		       linked_at = NOW()`, userID, du.ID, du.DisplayName()); err != nil {
		return nil, err
	}
	if err := tx.Commit(ctx); err != nil {
		return nil, err
	}
	s.rdb.Del(ctx, discordTriesPrefix+du.ID)
	s.audit(ctx, &userID, nil, "auth.discord.linked", map[string]any{
		"discord_user_id": du.ID, "discord_username": du.DisplayName(),
	})
	return user, nil
}

// Unlink removes the user's Discord link. It reports whether there was one.
func (s *DiscordService) Unlink(ctx context.Context, userID uuid.UUID, via string) (bool, error) {
	var discordID string
	err := s.db.QueryRow(ctx,
		`DELETE FROM user_discord_links WHERE user_id = $1 RETURNING discord_user_id`, userID).Scan(&discordID)
	if errors.Is(err, pgx.ErrNoRows) {
		return false, nil
	}
	if err != nil {
		return false, err
	}
	s.audit(ctx, &userID, nil, "auth.discord.unlinked", map[string]any{"discord_user_id": discordID, "via": via})
	return true, nil
}

// LinkOf returns the user's Discord link, or nil.
func (s *DiscordService) LinkOf(ctx context.Context, userID uuid.UUID) (*DiscordLink, error) {
	var l DiscordLink
	err := s.db.QueryRow(ctx,
		`SELECT discord_user_id, discord_username, linked_at FROM user_discord_links WHERE user_id = $1`, userID).
		Scan(&l.DiscordUserID, &l.DiscordUsername, &l.LinkedAt)
	if errors.Is(err, pgx.ErrNoRows) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	return &l, nil
}

// linkedUser returns the TalePanel user linked to a Discord user, or nil.
func (s *DiscordService) linkedUser(ctx context.Context, discordID string) (*models.User, error) {
	var id uuid.UUID
	err := s.db.QueryRow(ctx,
		`SELECT user_id FROM user_discord_links WHERE discord_user_id = $1`, discordID).Scan(&id)
	if errors.Is(err, pgx.ErrNoRows) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	return s.userByID(ctx, id)
}

func (s *DiscordService) userByID(ctx context.Context, id uuid.UUID) (*models.User, error) {
	u := &models.User{}
	err := s.db.QueryRow(ctx,
		`SELECT id, email, username, role, totp_enabled, is_active FROM users WHERE id = $1`, id).
		Scan(&u.ID, &u.Email, &u.Username, &u.Role, &u.TOTPEnabled, &u.IsActive)
	if errors.Is(err, pgx.ErrNoRows) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	return u, nil
}

// audit writes an activity log entry. serverID is set for server actions.
func (s *DiscordService) audit(ctx context.Context, userID, serverID *uuid.UUID, action string, payload map[string]any) {
	body, _ := json.Marshal(payload)
	targetType, targetID := "user", userID
	if serverID != nil {
		targetType, targetID = "servers", serverID
	}
	if _, err := s.db.Exec(ctx, `
		INSERT INTO activity_logs (id, user_id, server_id, action, target_type, target_id, payload, created_at)
		VALUES ($1, $2, $3, $4, $5, $6, $7::jsonb, NOW())
	`, uuid.New(), userID, serverID, action, targetType, targetID, string(body)); err != nil {
		s.log.Warn("discord audit insert failed", zap.String("action", action), zap.Error(err))
	}
}
