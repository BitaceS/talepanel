package services

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"strconv"
	"strings"
	"time"

	"github.com/google/uuid"
	"go.uber.org/zap"

	"github.com/BitaceS/talepanel/api/internal/discord"
	"github.com/BitaceS/talepanel/api/internal/models"
)

// ─── Texts ───────────────────────────────────────────────────────────────────

// discordTexts holds every reply in English and German. Replies follow the
// Discord user's language; the status message follows the panel setting.
var discordTexts = map[string]map[string]string{
	"en": {
		"disabled":      "Discord commands are turned off in TalePanel.",
		"wrong_guild":   "These commands only work in the Discord server TalePanel is set up for.",
		"not_linked":    "Link your TalePanel account first: in TalePanel open Settings → Security → Discord, create a code and run /link with it here.",
		"inactive":      "Your TalePanel account is disabled.",
		"need_2fa":      "Set up two-factor login in TalePanel first; it is required for admin accounts.",
		"no_server":     "No server called “%s” that you can see.",
		"no_perm":       "You do not have the right to do that on %s.",
		"already":       "%s is already running.",
		"start":         "%s is starting (by %s).",
		"stop":          "%s is stopping (by %s).",
		"restart":       "%s is restarting (by %s).",
		"failed":        "TalePanel could not do that. Look in the panel for details.",
		"linked":        "Linked to the TalePanel account %s. /server now works with its rights.",
		"code_invalid":  "This code is unknown or expired. Create a new one in TalePanel.",
		"too_many":      "Too many wrong codes. Wait 10 minutes and try again.",
		"taken":         "This Discord account is already linked to another TalePanel account. Run /unlink first.",
		"unlinked":      "Your Discord account is no longer linked to TalePanel.",
		"was_not":       "This Discord account is not linked to TalePanel.",
		"no_public":     "No servers are shared here yet.",
		"title":         "Server status",
		"players_1":     "1 player online",
		"players_n":     "%d players online",
		"more":          "and %d more",
		"st_running":    "Online",
		"st_starting":   "Starting",
		"st_stopping":   "Stopping",
		"st_stopped":    "Offline",
		"st_crashed":    "Crashed",
		"st_installing": "Installing",
	},
	"de": {
		"disabled":      "Discord-Befehle sind in TalePanel ausgeschaltet.",
		"wrong_guild":   "Diese Befehle funktionieren nur auf dem Discord-Server, für den TalePanel eingerichtet ist.",
		"not_linked":    "Verknüpfe zuerst dein TalePanel-Konto: Öffne in TalePanel Einstellungen → Sicherheit → Discord, erstelle einen Code und gib ihn hier mit /link ein.",
		"inactive":      "Dein TalePanel-Konto ist deaktiviert.",
		"need_2fa":      "Richte zuerst die Zwei-Faktor-Anmeldung in TalePanel ein; für Admin-Konten ist sie Pflicht.",
		"no_server":     "Kein Server namens „%s“, den du sehen kannst.",
		"no_perm":       "Dafür fehlt dir auf %s das Recht.",
		"already":       "%s läuft schon.",
		"start":         "%s startet (von %s).",
		"stop":          "%s wird gestoppt (von %s).",
		"restart":       "%s startet neu (von %s).",
		"failed":        "TalePanel konnte das nicht ausführen. Details siehst du im Panel.",
		"linked":        "Mit dem TalePanel-Konto %s verknüpft. /server arbeitet jetzt mit dessen Rechten.",
		"code_invalid":  "Dieser Code ist unbekannt oder abgelaufen. Erstelle in TalePanel einen neuen.",
		"too_many":      "Zu viele falsche Codes. Warte 10 Minuten und versuch es dann noch einmal.",
		"taken":         "Dieses Discord-Konto ist schon mit einem anderen TalePanel-Konto verknüpft. Führe zuerst /unlink aus.",
		"unlinked":      "Dein Discord-Konto ist nicht mehr mit TalePanel verknüpft.",
		"was_not":       "Dieses Discord-Konto ist nicht mit TalePanel verknüpft.",
		"no_public":     "Hier sind noch keine Server freigegeben.",
		"title":         "Serverstatus",
		"players_1":     "1 Spieler online",
		"players_n":     "%d Spieler online",
		"more":          "und %d weitere",
		"st_running":    "Online",
		"st_starting":   "Startet",
		"st_stopping":   "Stoppt",
		"st_stopped":    "Offline",
		"st_crashed":    "Abgestürzt",
		"st_installing": "Wird installiert",
	},
}

func discordLang(locale string) string {
	if strings.HasPrefix(strings.ToLower(locale), "de") {
		return "de"
	}
	return "en"
}

func dt(lang, key string, args ...any) string {
	s := discordTexts[lang][key]
	if s == "" {
		s = discordTexts["en"][key]
	}
	if len(args) > 0 {
		return fmt.Sprintf(s, args...)
	}
	return s
}

// mdEscape keeps names from turning into Discord formatting.
var mdReplacer = strings.NewReplacer(`\`, `\\`, "*", `\*`, "_", `\_`, "~", `\~`, "`", "\\`", "|", `\|`, ">", `\>`, "#", `\#`)

func mdEscape(s string) string { return mdReplacer.Replace(s) }

// ─── Commands ────────────────────────────────────────────────────────────────

func discordCommands() []discord.Command {
	serverOpt := discord.CommandOption{
		Type: discord.OptionString, Name: "server", Description: "Server name",
		DescriptionLocalizations: map[string]string{"de": "Name des Servers"},
		Required:                 true, Autocomplete: true, MaxLength: 100,
	}
	sub := func(name, en, de string) discord.CommandOption {
		return discord.CommandOption{
			Type: discord.OptionSubCommand, Name: name, Description: en,
			DescriptionLocalizations: map[string]string{"de": de},
			Options:                  []discord.CommandOption{serverOpt},
		}
	}
	guild := []int{0}
	return []discord.Command{
		{
			Name: "status", Description: "Which servers are online and how many players are on",
			DescriptionLocalizations: map[string]string{"de": "Welche Server online sind und wie viele Spieler drauf sind"},
			Contexts:                 guild,
		},
		{
			Name: "server", Description: "Start, stop or restart a server (needs a linked TalePanel account)",
			DescriptionLocalizations: map[string]string{"de": "Server starten, stoppen oder neu starten (braucht ein verknüpftes TalePanel-Konto)"},
			Options: []discord.CommandOption{
				sub("start", "Start a server", "Server starten"),
				sub("stop", "Stop a server", "Server stoppen"),
				sub("restart", "Restart a server", "Server neu starten"),
				sub("status", "Status of one server", "Status eines Servers"),
			},
			Contexts: guild,
		},
		{
			Name: "link", Description: "Link your TalePanel account with the code from TalePanel",
			DescriptionLocalizations: map[string]string{"de": "Dein TalePanel-Konto mit dem Code aus TalePanel verknüpfen"},
			Options: []discord.CommandOption{{
				Type: discord.OptionString, Name: "code", Description: "Code from TalePanel → Settings → Security → Discord",
				DescriptionLocalizations: map[string]string{"de": "Code aus TalePanel → Einstellungen → Sicherheit → Discord"},
				Required:                 true, MaxLength: 20,
			}},
			Contexts: guild,
		},
		{
			Name: "unlink", Description: "Remove the link to your TalePanel account",
			DescriptionLocalizations: map[string]string{"de": "Verknüpfung mit deinem TalePanel-Konto entfernen"},
			Contexts:                 guild,
		},
	}
}

func reply(text string, ephemeral bool) discord.Response {
	d := &discord.ResponseData{Content: text, AllowedMentions: discord.NoMentions()}
	if ephemeral {
		d.Flags = discord.FlagEphemeral
	}
	return discord.Response{Type: discord.ResponseMessage, Data: d}
}

// HandleInteraction answers one interaction. The caller has checked the
// signature. PINGs are answered without looking at the settings, so Discord
// can verify the endpoint before the bot is enabled.
func (s *DiscordService) HandleInteraction(ctx context.Context, in discord.Interaction) discord.Response {
	if in.Type == discord.InteractionPing {
		return discord.Response{Type: discord.ResponsePong}
	}
	lang := discordLang(in.Locale)
	cfg, err := s.Config(ctx)
	if err != nil {
		s.log.Error("discord: loading settings", zap.Error(err))
		return reply(dt(lang, "failed"), true)
	}
	if in.Type == discord.InteractionAutocomplete {
		if !cfg.Enabled || in.ApplicationID != cfg.ApplicationID || in.GuildID != cfg.GuildID {
			return discord.Response{Type: discord.ResponseAutocomplete, Data: &discord.ResponseData{Choices: []discord.Choice{}}}
		}
		return s.autocomplete(ctx, in)
	}
	if in.Type != discord.InteractionCommand || in.Data == nil {
		return reply(dt(lang, "failed"), true)
	}
	if !cfg.Enabled {
		return reply(dt(lang, "disabled"), true)
	}
	if in.ApplicationID != cfg.ApplicationID || in.GuildID == "" || in.GuildID != cfg.GuildID {
		return reply(dt(lang, "wrong_guild"), true)
	}
	caller := in.Caller()
	if caller.ID == "" {
		return reply(dt(lang, "failed"), true)
	}
	switch in.Data.Name {
	case "status":
		return s.statusReply(ctx, cfg, lang)
	case "link":
		code := ""
		for _, o := range in.Data.Options {
			if o.Name == "code" {
				code = o.StringValue()
			}
		}
		return s.linkReply(ctx, caller, code, lang)
	case "unlink":
		user, err := s.linkedUser(ctx, caller.ID)
		if err != nil {
			s.log.Error("discord: unlink lookup", zap.Error(err))
			return reply(dt(lang, "failed"), true)
		}
		if user == nil {
			return reply(dt(lang, "was_not"), true)
		}
		if _, err := s.Unlink(ctx, user.ID, "discord"); err != nil {
			s.log.Error("discord: unlink", zap.Error(err))
			return reply(dt(lang, "failed"), true)
		}
		return reply(dt(lang, "unlinked"), true)
	case "server":
		if len(in.Data.Options) != 1 {
			return reply(dt(lang, "failed"), true)
		}
		subCmd := in.Data.Options[0]
		target := ""
		for _, o := range subCmd.Options {
			if o.Name == "server" {
				target = o.StringValue()
			}
		}
		return s.serverReply(ctx, caller, subCmd.Name, target, lang)
	}
	return reply(dt(lang, "failed"), true)
}

func (s *DiscordService) linkReply(ctx context.Context, caller discord.User, code, lang string) discord.Response {
	user, err := s.Link(ctx, caller, code)
	switch {
	case err == nil:
		return reply(dt(lang, "linked", mdEscape(user.Username)), true)
	case errors.Is(err, ErrDiscordCodeInvalid):
		return reply(dt(lang, "code_invalid"), true)
	case errors.Is(err, ErrDiscordTooManyTry):
		return reply(dt(lang, "too_many"), true)
	case errors.Is(err, ErrDiscordTaken):
		return reply(dt(lang, "taken"), true)
	}
	s.log.Error("discord: link", zap.Error(err))
	return reply(dt(lang, "failed"), true)
}

// commandUser returns the linked TalePanel user allowed to run server
// commands, or the reply that explains why not.
func (s *DiscordService) commandUser(ctx context.Context, caller discord.User, lang string) (*models.User, *discord.Response) {
	user, err := s.linkedUser(ctx, caller.ID)
	if err != nil {
		s.log.Error("discord: user lookup", zap.Error(err))
		r := reply(dt(lang, "failed"), true)
		return nil, &r
	}
	if user == nil {
		r := reply(dt(lang, "not_linked"), true)
		return nil, &r
	}
	if !user.IsActive {
		r := reply(dt(lang, "inactive"), true)
		return nil, &r
	}
	// Same rule as the panel: staff without 2FA reach only their own account.
	if models.RoleWeight(user.Role) >= models.RoleWeight(models.RoleAdmin) && !user.TOTPEnabled &&
		s.policy != nil && s.policy.RequireStaff2FA(ctx) {
		r := reply(dt(lang, "need_2fa"), true)
		return nil, &r
	}
	return user, nil
}

// findServer resolves the option value: a server ID from autocomplete or a
// name typed by hand, among the servers the user can see.
func (s *DiscordService) findServer(ctx context.Context, user *models.User, target string) (*models.Server, error) {
	list, err := s.servers.ListServers(ctx, user.ID, user.Role)
	if err != nil {
		return nil, err
	}
	target = strings.TrimSpace(target)
	if id, err := uuid.Parse(target); err == nil {
		for _, sv := range list {
			if sv.ID == id {
				return sv, nil
			}
		}
		return nil, nil
	}
	for _, sv := range list {
		if strings.EqualFold(sv.Name, target) {
			return sv, nil
		}
	}
	return nil, nil
}

var discordActionPerm = map[string]string{
	"start":   "server.start",
	"restart": "server.start",
	"stop":    "server.stop",
	"status":  "server.view",
}

func (s *DiscordService) serverReply(ctx context.Context, caller discord.User, action, target, lang string) discord.Response {
	perm, ok := discordActionPerm[action]
	if !ok {
		return reply(dt(lang, "failed"), true)
	}
	user, refusal := s.commandUser(ctx, caller, lang)
	if refusal != nil {
		return *refusal
	}
	sv, err := s.findServer(ctx, user, target)
	if err != nil {
		s.log.Error("discord: server lookup", zap.Error(err))
		return reply(dt(lang, "failed"), true)
	}
	if sv == nil {
		return reply(dt(lang, "no_server", mdEscape(target)), true)
	}
	name := mdEscape(sv.Name)
	allowed, err := s.perms.HasServerPermission(ctx, user, sv.ID, perm)
	if err != nil {
		s.log.Error("discord: permission check", zap.Error(err))
		return reply(dt(lang, "failed"), true)
	}
	if !allowed {
		return reply(dt(lang, "no_perm", name), true)
	}
	if action == "status" {
		rows, err := s.statusRows(ctx, []uuid.UUID{sv.ID}, false)
		if err != nil || len(rows) == 0 {
			if err != nil {
				s.log.Error("discord: server status", zap.Error(err))
			}
			return reply(dt(lang, "failed"), true)
		}
		return discord.Response{Type: discord.ResponseMessage, Data: &discord.ResponseData{
			Embeds: []discord.Embed{statusEmbed(rows, lang, "")}, Flags: discord.FlagEphemeral,
			AllowedMentions: discord.NoMentions(),
		}}
	}
	if s.power == nil {
		return reply(dt(lang, "failed"), true)
	}
	if _, err := s.power.PowerAction(ctx, sv.ID, action); err != nil {
		if errors.Is(err, ErrServerAlreadyRunning) {
			return reply(dt(lang, "already", name), true)
		}
		s.log.Error("discord: power action", zap.String("action", action), zap.Error(err))
		return reply(dt(lang, "failed"), true)
	}
	id := sv.ID
	s.audit(ctx, &user.ID, &id, "post_servers_"+action, map[string]any{
		"via": "discord", "discord_user_id": caller.ID, "discord_username": caller.DisplayName(),
		"server_name": sv.Name,
	})
	s.Nudge()
	// Visible to the channel, so the team sees who did what.
	return reply(dt(lang, action, "**"+name+"**", mdEscape(caller.DisplayName())), false)
}

func (s *DiscordService) autocomplete(ctx context.Context, in discord.Interaction) discord.Response {
	empty := discord.Response{Type: discord.ResponseAutocomplete, Data: &discord.ResponseData{Choices: []discord.Choice{}}}
	if in.Data == nil || len(in.Data.Options) != 1 {
		return empty
	}
	typed := ""
	for _, o := range in.Data.Options[0].Options {
		if o.Focused {
			typed = strings.ToLower(o.StringValue())
		}
	}
	user, err := s.linkedUser(ctx, in.Caller().ID)
	if err != nil || user == nil || !user.IsActive {
		return empty
	}
	list, err := s.servers.ListServers(ctx, user.ID, user.Role)
	if err != nil {
		return empty
	}
	choices := []discord.Choice{}
	for _, sv := range list {
		if typed != "" && !strings.Contains(strings.ToLower(sv.Name), typed) {
			continue
		}
		name := sv.Name
		if len(name) > 100 {
			name = name[:100]
		}
		choices = append(choices, discord.Choice{Name: name, Value: sv.ID.String()})
		if len(choices) == 25 {
			break
		}
	}
	return discord.Response{Type: discord.ResponseAutocomplete, Data: &discord.ResponseData{Choices: choices}}
}

// ─── Status ──────────────────────────────────────────────────────────────────

type discordStatusRow struct {
	Name    string
	Status  string
	Address string
	Port    int
	Players int
	Names   []string
}

// statusRows loads the servers in ids (in that order of names) with their
// player count, and the player names when withNames is set.
func (s *DiscordService) statusRows(ctx context.Context, ids []uuid.UUID, withNames bool) ([]discordStatusRow, error) {
	if len(ids) == 0 {
		return nil, nil
	}
	rows, err := s.db.Query(ctx, `
		SELECT s.id, s.name, s.status, s.port, COALESCE(NULLIF(n.public_address, ''), n.fqdn, ''),
		       (SELECT COUNT(*) FROM player_sessions ps
		          JOIN players p ON p.id = ps.player_id
		         WHERE ps.left_at IS NULL AND p.server_id = s.id)
		  FROM servers s
		  LEFT JOIN nodes n ON n.id = s.node_id
		 WHERE s.id = ANY($1)
		 ORDER BY lower(s.name)`, ids)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []discordStatusRow
	index := map[uuid.UUID]int{}
	for rows.Next() {
		var id uuid.UUID
		var r discordStatusRow
		if err := rows.Scan(&id, &r.Name, &r.Status, &r.Port, &r.Address, &r.Players); err != nil {
			return nil, err
		}
		index[id] = len(out)
		out = append(out, r)
	}
	if err := rows.Err(); err != nil {
		return nil, err
	}
	if !withNames || len(out) == 0 {
		return out, nil
	}
	names, err := s.db.Query(ctx, `
		SELECT DISTINCT p.server_id, p.username
		  FROM player_sessions ps
		  JOIN players p ON p.id = ps.player_id
		 WHERE ps.left_at IS NULL AND p.server_id = ANY($1)
		 ORDER BY p.username`, ids)
	if err != nil {
		return nil, err
	}
	defer names.Close()
	for names.Next() {
		var id uuid.UUID
		var name string
		if err := names.Scan(&id, &name); err != nil {
			return nil, err
		}
		if i, ok := index[id]; ok {
			out[i].Names = append(out[i].Names, name)
		}
	}
	return out, names.Err()
}

var statusEmoji = map[string]string{
	models.StatusRunning:    "🟢",
	models.StatusStarting:   "🟡",
	models.StatusStopping:   "🟡",
	models.StatusInstalling: "🟡",
	models.StatusCrashed:    "🔴",
	models.StatusStopped:    "⚫",
}

const (
	colorOnline  = 0x3BA55C
	colorOffline = 0x747F8D
)

// statusEmbed renders the servers as one embed. timestamp ("" for none) is
// shown by Discord in each reader's own time zone.
func statusEmbed(rows []discordStatusRow, lang, timestamp string) discord.Embed {
	e := discord.Embed{Title: dt(lang, "title"), Color: colorOffline, Timestamp: timestamp,
		Footer: &discord.EmbedFooter{Text: "TalePanel"}}
	for _, r := range rows {
		emoji := statusEmoji[r.Status]
		if emoji == "" {
			emoji = "⚫"
		}
		label := dt(lang, "st_"+r.Status)
		if label == "" {
			label = r.Status
		}
		lines := []string{emoji + " " + label}
		if r.Status == models.StatusRunning {
			e.Color = colorOnline
			if r.Players == 1 {
				lines[0] += " · " + dt(lang, "players_1")
			} else {
				lines[0] += " · " + dt(lang, "players_n", r.Players)
			}
		}
		if r.Address != "" && r.Port > 0 {
			lines = append(lines, "`"+strings.ReplaceAll(r.Address, "`", "")+":"+strconv.Itoa(r.Port)+"`")
		}
		if len(r.Names) > 0 {
			shown := r.Names
			extra := 0
			if len(shown) > 20 {
				extra = len(shown) - 20
				shown = shown[:20]
			}
			esc := make([]string, len(shown))
			for i, n := range shown {
				esc[i] = mdEscape(n)
			}
			line := strings.Join(esc, ", ")
			if extra > 0 {
				line += " " + dt(lang, "more", extra)
			}
			lines = append(lines, line)
		}
		value := strings.Join(lines, "\n")
		if len(value) > 1024 {
			value = value[:1021] + "…"
		}
		name := r.Name
		if len(name) > 256 {
			name = name[:256]
		}
		e.Fields = append(e.Fields, discord.EmbedField{Name: name, Value: value})
	}
	return e
}

func (s *DiscordService) statusReply(ctx context.Context, cfg DiscordConfig, lang string) discord.Response {
	rows, err := s.statusRows(ctx, cfg.StatusServerIDs, cfg.ShowPlayerNames)
	if err != nil {
		s.log.Error("discord: status", zap.Error(err))
		return reply(dt(lang, "failed"), true)
	}
	if len(rows) == 0 {
		return reply(dt(lang, "no_public"), true)
	}
	return discord.Response{Type: discord.ResponseMessage, Data: &discord.ResponseData{
		Embeds: []discord.Embed{statusEmbed(rows, lang, "")}, Flags: discord.FlagEphemeral,
		AllowedMentions: discord.NoMentions(),
	}}
}

// Nudge asks the status loop to refresh soon, for example after a start.
func (s *DiscordService) Nudge() {
	select {
	case s.nudge <- struct{}{}:
	default:
	}
}

// Run keeps the status message up to date until ctx ends.
func (s *DiscordService) Run(ctx context.Context) {
	t := time.NewTicker(discordStatusEvery)
	defer t.Stop()
	s.UpdateStatus(ctx)
	for {
		select {
		case <-ctx.Done():
			return
		case <-t.C:
		case <-s.nudge:
			// Let a start or stop settle into its new status first.
			select {
			case <-ctx.Done():
				return
			case <-time.After(3 * time.Second):
			}
		}
		s.UpdateStatus(ctx)
	}
}

// UpdateStatus posts or edits the status message when it changed.
func (s *DiscordService) UpdateStatus(ctx context.Context) {
	ctx, cancel := context.WithTimeout(ctx, 30*time.Second)
	defer cancel()
	cfg, err := s.Config(ctx)
	if err != nil || !cfg.Enabled || cfg.StatusChannelID == "" || cfg.BotToken == "" {
		return
	}
	rows, err := s.statusRows(ctx, cfg.StatusServerIDs, cfg.ShowPlayerNames)
	if err != nil {
		s.statusError(err)
		return
	}
	var msg discord.Message
	msg.AllowedMentions = discord.NoMentions()
	if len(rows) == 0 {
		msg.Content = dt(cfg.Language, "no_public")
		msg.Embeds = []discord.Embed{}
	} else {
		msg.Embeds = []discord.Embed{statusEmbed(rows, cfg.Language, "")}
	}
	key, _ := json.Marshal(msg)
	s.statusMu.Lock()
	unchanged := string(key) == s.lastStatus
	s.statusMu.Unlock()
	if unchanged {
		return
	}
	if len(msg.Embeds) > 0 {
		msg.Embeds[0].Timestamp = s.now().UTC().Format(time.RFC3339)
	}

	stored, err := s.settings.Get(ctx, discordStatusMsgKey)
	if err != nil {
		s.statusError(err)
		return
	}
	channel, msgID, _ := strings.Cut(stored, ":")
	if channel == cfg.StatusChannelID && msgID != "" {
		err = s.client.EditMessage(ctx, cfg.BotToken, channel, msgID, msg)
		if err == nil {
			s.statusSent(string(key))
			return
		}
		if !errors.Is(err, discord.ErrNotFound) {
			s.statusError(err)
			return
		}
		// Someone deleted the message: post a new one.
	}
	id, err := s.client.CreateMessage(ctx, cfg.BotToken, cfg.StatusChannelID, msg)
	if err != nil {
		s.statusError(err)
		return
	}
	if err := s.settings.Set(ctx, discordStatusMsgKey, cfg.StatusChannelID+":"+id, nil); err != nil {
		s.statusError(err)
		return
	}
	s.statusSent(string(key))
}

func (s *DiscordService) statusSent(key string) {
	s.statusMu.Lock()
	s.lastStatus, s.lastErr = key, ""
	s.statusMu.Unlock()
}

// statusError logs an error once until it changes or an update works.
func (s *DiscordService) statusError(err error) {
	s.statusMu.Lock()
	defer s.statusMu.Unlock()
	if err.Error() == s.lastErr {
		return
	}
	s.lastErr = err.Error()
	s.log.Warn("discord: status message update failed", zap.Error(err))
}
