package services

import (
	"context"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"

	"github.com/google/uuid"
	"go.uber.org/zap/zaptest"

	"github.com/BitaceS/talepanel/api/internal/discord"
	"github.com/BitaceS/talepanel/api/internal/models"
)

const (
	testAppID   = "123456789012345678"
	testGuildID = "223456789012345678"
	testChannel = "323456789012345678"
)

var testBotToken = strings.Repeat("t", 60)

// fakeDiscord records the REST calls the service makes.
type fakeDiscord struct {
	mu       sync.Mutex
	calls    []string
	bodies   []string
	editCode int // status for message edits (0 = 200)
	appID    string
}

func (f *fakeDiscord) handler(w http.ResponseWriter, r *http.Request) {
	body, _ := io.ReadAll(r.Body)
	f.mu.Lock()
	defer f.mu.Unlock()
	f.calls = append(f.calls, r.Method+" "+r.URL.Path)
	f.bodies = append(f.bodies, string(body))
	switch {
	case r.URL.Path == "/applications/@me":
		_, _ = w.Write([]byte(`{"id":"` + f.appID + `"}`))
	case r.Method == http.MethodPost && strings.HasSuffix(r.URL.Path, "/messages"):
		_, _ = w.Write([]byte(`{"id":"900000000000000001"}`))
	case r.Method == http.MethodPatch:
		if f.editCode != 0 {
			w.WriteHeader(f.editCode)
			return
		}
		_, _ = w.Write([]byte(`{}`))
	default:
		_, _ = w.Write([]byte(`[]`))
	}
}

func (f *fakeDiscord) take() []string {
	f.mu.Lock()
	defer f.mu.Unlock()
	out := f.calls
	f.calls, f.bodies = nil, nil
	return out
}

type fakePower struct {
	mu    sync.Mutex
	calls []string
}

func (p *fakePower) PowerAction(_ context.Context, id uuid.UUID, action string) (string, error) {
	p.mu.Lock()
	defer p.mu.Unlock()
	p.calls = append(p.calls, action+" "+id.String())
	return models.StatusStarting, nil
}

type discordTestEnv struct {
	svc      *DiscordService
	fake     *fakeDiscord
	power    *fakePower
	policy   *SecurityPolicyService
	settings *AppSettingsService
	owner    uuid.UUID
}

func newDiscordTestEnv(t *testing.T) *discordTestEnv {
	t.Helper()
	pool := openTestDB(t)
	_, rdb := newMiniRedis(t)
	settings := NewAppSettingsService(pool, make([]byte, 32))
	policy := NewSecurityPolicyService(settings, nil)
	svc := NewDiscordService(pool, rdb, settings, NewServerService(pool), NewPermissionService(pool), policy, zaptest.NewLogger(t))
	fake := &fakeDiscord{appID: testAppID}
	srv := httptest.NewServer(http.HandlerFunc(fake.handler))
	t.Cleanup(srv.Close)
	svc.SetClient(&discord.Client{BaseURL: srv.URL, HTTP: srv.Client()})
	power := &fakePower{}
	svc.SetPower(power)
	owner := seedTestUser(t, pool, models.RoleOwner)
	t.Cleanup(func() {
		ctx := context.Background()
		_ = settings.Set(ctx, discordSettingsKey, "", nil)
		_ = settings.Set(ctx, discordStatusMsgKey, "", nil)
		_ = settings.Set(ctx, settingRequireStaff2FA, "", nil)
	})
	return &discordTestEnv{svc: svc, fake: fake, power: power, policy: policy, settings: settings, owner: owner}
}

func (e *discordTestEnv) configure(t *testing.T, mutate func(*DiscordConfig)) {
	t.Helper()
	c := DiscordConfig{Enabled: true, ApplicationID: testAppID, GuildID: testGuildID,
		PublicKey: strings.Repeat("ab", 32), BotToken: testBotToken}
	if mutate != nil {
		mutate(&c)
	}
	if _, err := e.svc.SetConfig(context.Background(), c, e.owner); err != nil {
		t.Fatalf("SetConfig: %v", err)
	}
	e.fake.take()
}

func command(discordUser, name string, opts ...discord.Option) discord.Interaction {
	return discord.Interaction{
		Type: discord.InteractionCommand, ApplicationID: testAppID, GuildID: testGuildID,
		Member: &discord.Member{User: discord.User{ID: discordUser, Username: "user" + discordUser}},
		Data:   &discord.CommandData{Name: name, Options: opts},
	}
}

func strOpt(name, value string) discord.Option {
	v, _ := json.Marshal(value)
	return discord.Option{Name: name, Type: discord.OptionString, Value: v}
}

func serverCmd(discordUser, action, server string) discord.Interaction {
	return command(discordUser, "server", discord.Option{Name: action, Type: discord.OptionSubCommand,
		Options: []discord.Option{strOpt("server", server)}})
}

func text(r discord.Response) string {
	if r.Data == nil {
		return ""
	}
	return r.Data.Content
}

// link creates a code for userID and runs /link as discordUser.
func (e *discordTestEnv) link(t *testing.T, userID uuid.UUID, discordUser string) discord.Response {
	t.Helper()
	code, _, err := e.svc.CreateLinkCode(context.Background(), userID)
	if err != nil {
		t.Fatalf("CreateLinkCode: %v", err)
	}
	return e.svc.HandleInteraction(context.Background(), command(discordUser, "link", strOpt("code", strings.ToLower(code))))
}

func TestDiscordSetConfigRegistersCommands(t *testing.T) {
	e := newDiscordTestEnv(t)
	ctx := context.Background()
	c := DiscordConfig{Enabled: true, ApplicationID: testAppID, GuildID: testGuildID,
		PublicKey: strings.Repeat("ab", 32), BotToken: testBotToken}
	if _, err := e.svc.SetConfig(ctx, c, e.owner); err != nil {
		t.Fatalf("SetConfig: %v", err)
	}
	calls := e.fake.take()
	want := "PUT /applications/" + testAppID + "/guilds/" + testGuildID + "/commands"
	if len(calls) != 2 || calls[1] != want {
		t.Fatalf("calls = %v", calls)
	}

	// An empty token keeps the stored one.
	c.BotToken = ""
	stored, err := e.svc.SetConfig(ctx, c, e.owner)
	if err != nil || stored.BotToken != testBotToken {
		t.Fatalf("token not kept: %v %q", err, stored.BotToken)
	}

	// A token of another application is refused.
	e.fake.appID = "999999999999999999"
	c.BotToken = testBotToken
	if _, err := e.svc.SetConfig(ctx, c, e.owner); err == nil {
		t.Fatal("token of another application accepted")
	}
}

func TestDiscordLinkAndPower(t *testing.T) {
	e := newDiscordTestEnv(t)
	e.configure(t, nil)
	ctx := context.Background()
	pool := e.svc.db
	user := seedTestUser(t, pool, models.RoleUser)
	own := seedServerWithLimits(t, pool, user)
	other := seedServerWithLimits(t, pool, e.owner)
	var ownName, otherName string
	_ = pool.QueryRow(ctx, `SELECT name FROM servers WHERE id = $1`, own).Scan(&ownName)
	_ = pool.QueryRow(ctx, `SELECT name FROM servers WHERE id = $1`, other).Scan(&otherName)

	// Not linked yet: refused, nothing started.
	r := e.svc.HandleInteraction(ctx, serverCmd("111111111111111111", "start", own.String()))
	if !strings.Contains(text(r), "Link your TalePanel account") || r.Data.Flags != discord.FlagEphemeral {
		t.Fatalf("unlinked start: %+v", r.Data)
	}

	if r := e.link(t, user, "111111111111111111"); !strings.Contains(text(r), "Linked to the TalePanel account") {
		t.Fatalf("link: %q", text(r))
	}
	if l, _ := e.svc.LinkOf(ctx, user); l == nil || l.DiscordUserID != "111111111111111111" {
		t.Fatalf("LinkOf = %+v", l)
	}

	// Own server by ID (autocomplete) and by name: started, visible reply.
	r = e.svc.HandleInteraction(ctx, serverCmd("111111111111111111", "start", own.String()))
	if !strings.Contains(text(r), "is starting") || r.Data.Flags == discord.FlagEphemeral {
		t.Fatalf("start: %+v", r.Data)
	}
	r = e.svc.HandleInteraction(ctx, serverCmd("111111111111111111", "restart", strings.ToUpper(ownName)))
	if !strings.Contains(text(r), "is restarting") {
		t.Fatalf("restart by name: %q", text(r))
	}
	if len(e.power.calls) != 2 || e.power.calls[0] != "start "+own.String() {
		t.Fatalf("power calls = %v", e.power.calls)
	}
	var audited int
	_ = pool.QueryRow(ctx, `SELECT COUNT(*) FROM activity_logs WHERE server_id = $1 AND user_id = $2
		AND payload->>'via' = 'discord'`, own, user).Scan(&audited)
	if audited != 2 {
		t.Fatalf("audit entries = %d", audited)
	}

	// Someone else's server is not even found.
	r = e.svc.HandleInteraction(ctx, serverCmd("111111111111111111", "stop", other.String()))
	if !strings.Contains(text(r), "No server called") || len(e.power.calls) != 2 {
		t.Fatalf("other's server: %q, calls %v", text(r), e.power.calls)
	}

	// Autocomplete offers only the user's own server.
	ac := e.svc.HandleInteraction(ctx, discord.Interaction{
		Type: discord.InteractionAutocomplete, ApplicationID: testAppID, GuildID: testGuildID,
		Member: &discord.Member{User: discord.User{ID: "111111111111111111"}},
		Data: &discord.CommandData{Name: "server", Options: []discord.Option{{Name: "start", Type: 1,
			Options: []discord.Option{{Name: "server", Type: 3, Value: json.RawMessage(`""`), Focused: true}}}}},
	})
	if len(ac.Data.Choices) != 1 || ac.Data.Choices[0].Value != own.String() {
		t.Fatalf("autocomplete = %+v", ac.Data.Choices)
	}

	// Another guild: refused before anything else.
	in := serverCmd("111111111111111111", "start", own.String())
	in.GuildID = "999999999999999999"
	if r := e.svc.HandleInteraction(ctx, in); !strings.Contains(text(r), "only work in the Discord server") {
		t.Fatalf("other guild: %q", text(r))
	}

	// Unlink from Discord: commands refused again.
	if r := e.svc.HandleInteraction(ctx, command("111111111111111111", "unlink")); !strings.Contains(text(r), "no longer linked") {
		t.Fatalf("unlink: %q", text(r))
	}
	r = e.svc.HandleInteraction(ctx, serverCmd("111111111111111111", "start", own.String()))
	if !strings.Contains(text(r), "Link your TalePanel account") {
		t.Fatalf("start after unlink: %q", text(r))
	}
}

func TestDiscordLinkCodeRules(t *testing.T) {
	e := newDiscordTestEnv(t)
	e.configure(t, nil)
	ctx := context.Background()
	pool := e.svc.db
	alice := seedTestUser(t, pool, models.RoleUser)
	bob := seedTestUser(t, pool, models.RoleUser)

	code, _, err := e.svc.CreateLinkCode(ctx, alice)
	if err != nil {
		t.Fatal(err)
	}
	if r := e.svc.HandleInteraction(ctx, command("222222222222222222", "link", strOpt("code", code))); !strings.Contains(text(r), "Linked") {
		t.Fatalf("link: %q", text(r))
	}
	// Single use.
	if r := e.svc.HandleInteraction(ctx, command("333333333333333333", "link", strOpt("code", code))); !strings.Contains(text(r), "unknown or expired") {
		t.Fatalf("reused code: %q", text(r))
	}
	// A Discord account linked to Alice cannot be linked to Bob.
	if r := e.link(t, bob, "222222222222222222"); !strings.Contains(text(r), "already linked to another") {
		t.Fatalf("taken: %q", text(r))
	}
	// Linking Alice again with another Discord account replaces the old one.
	if r := e.link(t, alice, "444444444444444444"); !strings.Contains(text(r), "Linked") {
		t.Fatalf("relink: %q", text(r))
	}
	if l, _ := e.svc.LinkOf(ctx, alice); l == nil || l.DiscordUserID != "444444444444444444" {
		t.Fatalf("relink = %+v", l)
	}
	// Five wrong codes, then even a right one is refused.
	for i := 0; i < discordMaxLinkTries; i++ {
		e.svc.HandleInteraction(ctx, command("555555555555555555", "link", strOpt("code", "AAAA-AAAA")))
	}
	if r := e.link(t, bob, "555555555555555555"); !strings.Contains(text(r), "Too many wrong codes") {
		t.Fatalf("after wrong codes: %q", text(r))
	}
	// German replies for a German Discord client.
	in := command("666666666666666666", "link", strOpt("code", "BBBB-BBBB"))
	in.Locale = "de"
	if r := e.svc.HandleInteraction(ctx, in); !strings.Contains(text(r), "unbekannt oder abgelaufen") {
		t.Fatalf("german: %q", text(r))
	}
}

func TestDiscordStaffWithout2FA(t *testing.T) {
	e := newDiscordTestEnv(t)
	e.configure(t, nil)
	ctx := context.Background()
	pool := e.svc.db
	admin := seedTestUser(t, pool, models.RoleAdmin)
	srv := seedServerWithLimits(t, pool, e.owner)
	if r := e.link(t, admin, "777777777777777777"); !strings.Contains(text(r), "Linked") {
		t.Fatalf("link: %q", text(r))
	}
	if r := e.svc.HandleInteraction(ctx, serverCmd("777777777777777777", "start", srv.String())); !strings.Contains(text(r), "is starting") {
		t.Fatalf("admin start without policy: %q", text(r))
	}
	if err := e.settings.Set(ctx, settingRequireStaff2FA, "1", nil); err != nil {
		t.Fatal(err)
	}
	// The policy service caches its value; a fresh one reads the new setting.
	e.svc.policy = NewSecurityPolicyService(e.settings, nil)
	if r := e.svc.HandleInteraction(ctx, serverCmd("777777777777777777", "start", srv.String())); !strings.Contains(text(r), "two-factor") {
		t.Fatalf("admin start with policy: %q", text(r))
	}
}

func TestDiscordStatus(t *testing.T) {
	e := newDiscordTestEnv(t)
	pool := e.svc.db
	ctx := context.Background()
	shown := seedServerWithLimits(t, pool, e.owner)
	hidden := seedServerWithLimits(t, pool, e.owner)
	var shownName, hiddenName string
	_ = pool.QueryRow(ctx, `SELECT name FROM servers WHERE id = $1`, shown).Scan(&shownName)
	_ = pool.QueryRow(ctx, `SELECT name FROM servers WHERE id = $1`, hidden).Scan(&hiddenName)
	e.configure(t, func(c *DiscordConfig) {
		c.StatusChannelID = testChannel
		c.StatusServerIDs = []uuid.UUID{shown}
	})

	r := e.svc.HandleInteraction(ctx, command("888888888888888888", "status"))
	if len(r.Data.Embeds) != 1 || len(r.Data.Embeds[0].Fields) != 1 || r.Data.Embeds[0].Fields[0].Name != shownName {
		t.Fatalf("/status = %+v", r.Data)
	}

	// First update posts, the same content again sends nothing, a change edits.
	e.svc.UpdateStatus(ctx)
	if calls := e.fake.take(); len(calls) != 1 || calls[0] != "POST /channels/"+testChannel+"/messages" {
		t.Fatalf("first update = %v", calls)
	}
	e.svc.UpdateStatus(ctx)
	if calls := e.fake.take(); len(calls) != 0 {
		t.Fatalf("unchanged update = %v", calls)
	}
	_, _ = pool.Exec(ctx, `UPDATE servers SET status = 'running' WHERE id = $1`, shown)
	e.svc.UpdateStatus(ctx)
	if calls := e.fake.take(); len(calls) != 1 || !strings.HasPrefix(calls[0], "PATCH /channels/"+testChannel+"/messages/900000000000000001") {
		t.Fatalf("changed update = %v", calls)
	}
	// The message was deleted in Discord: a new one is posted.
	e.fake.editCode = http.StatusNotFound
	_, _ = pool.Exec(ctx, `UPDATE servers SET status = 'stopped' WHERE id = $1`, shown)
	e.svc.UpdateStatus(ctx)
	if calls := e.fake.take(); len(calls) != 2 || !strings.HasPrefix(calls[1], "POST ") {
		t.Fatalf("after delete = %v", calls)
	}
}
