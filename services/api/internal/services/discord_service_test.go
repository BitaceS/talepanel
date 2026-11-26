package services

import (
	"context"
	"regexp"
	"strings"
	"testing"

	"github.com/google/uuid"

	"github.com/BitaceS/talepanel/api/internal/discord"
	"github.com/BitaceS/talepanel/api/internal/models"
)

func TestDiscordConfigValidate(t *testing.T) {
	good := DiscordConfig{
		Enabled: true, ApplicationID: "123456789012345678", GuildID: "223456789012345678",
		PublicKey: strings.Repeat("ab", 32), BotToken: strings.Repeat("x", 59) + ".abc",
	}
	if err := good.Normalize().Validate(); err != nil {
		t.Fatalf("valid config refused: %v", err)
	}
	if err := (DiscordConfig{}).Normalize().Validate(); err != nil {
		t.Fatalf("empty disabled config refused: %v", err)
	}
	for name, mutate := range map[string]func(*DiscordConfig){
		"app id letters":  func(c *DiscordConfig) { c.ApplicationID = "abc" },
		"short key":       func(c *DiscordConfig) { c.PublicKey = "abcd" },
		"token spaces":    func(c *DiscordConfig) { c.BotToken = "Bot " + strings.Repeat("x", 60) },
		"channel letters": func(c *DiscordConfig) { c.StatusChannelID = "general" },
		"no guild":        func(c *DiscordConfig) { c.GuildID = "" },
		"no token":        func(c *DiscordConfig) { c.BotToken = "" },
	} {
		c := good
		mutate(&c)
		if err := c.Normalize().Validate(); err == nil {
			t.Errorf("%s: accepted", name)
		}
	}
	n := DiscordConfig{PublicKey: " " + strings.Repeat("AB", 32) + " ", Language: "fr",
		StatusServerIDs: []uuid.UUID{uuid.Nil, uuid.MustParse("11111111-1111-1111-1111-111111111111"),
			uuid.MustParse("11111111-1111-1111-1111-111111111111")}}.Normalize()
	if n.PublicKey != strings.Repeat("ab", 32) || n.Language != "en" || len(n.StatusServerIDs) != 1 {
		t.Fatalf("Normalize = %+v", n)
	}
}

func TestDiscordLinkCodes(t *testing.T) {
	re := regexp.MustCompile(`^[A-HJKMNP-Z2-9]{4}-[A-HJKMNP-Z2-9]{4}$`)
	for i := 0; i < 50; i++ {
		c, err := newLinkCode()
		if err != nil || !re.MatchString(c) {
			t.Fatalf("code %q, %v", c, err)
		}
	}
	for in, want := range map[string]string{
		"abcd-efgh": "ABCD-EFGH", " ABCDEFGH ": "ABCD-EFGH", "ab cd ef gh": "ABCD-EFGH",
		"abc": "", "ABCD-EFGH-I": "",
	} {
		if got := normalizeLinkCode(in); got != want {
			t.Errorf("normalizeLinkCode(%q) = %q, want %q", in, got, want)
		}
	}
}

func TestDiscordStatusEmbed(t *testing.T) {
	rows := []discordStatusRow{
		{Name: "Emberwood", Status: models.StatusRunning, Address: "play.example.com", Port: 5520, Players: 1,
			Names: []string{"_Alex_", "Sam"}},
		{Name: "Lobby", Status: models.StatusStopped, Players: 3},
	}
	e := statusEmbed(rows, "de", "")
	if e.Title != "Serverstatus" || e.Color != colorOnline || len(e.Fields) != 2 {
		t.Fatalf("embed = %+v", e)
	}
	v := e.Fields[0].Value
	for _, want := range []string{"🟢 Online · 1 Spieler online", "`play.example.com:5520`", `\_Alex\_, Sam`} {
		if !strings.Contains(v, want) {
			t.Errorf("field %q lacks %q", v, want)
		}
	}
	if strings.Contains(e.Fields[1].Value, "Spieler") {
		t.Errorf("stopped server shows players: %q", e.Fields[1].Value)
	}
	if got := statusEmbed(rows[1:], "en", "").Color; got != colorOffline {
		t.Errorf("all offline color = %x", got)
	}
}

func TestDiscordPingNeedsNoSettings(t *testing.T) {
	s := &DiscordService{}
	if r := s.HandleInteraction(context.Background(), discord.Interaction{Type: discord.InteractionPing}); r.Type != discord.ResponsePong {
		t.Fatalf("ping answered with %+v", r)
	}
}

func TestDiscordLang(t *testing.T) {
	for in, want := range map[string]string{"de": "de", "de-AT": "de", "en-US": "en", "": "en", "fr": "en"} {
		if got := discordLang(in); got != want {
			t.Errorf("discordLang(%q) = %q", in, got)
		}
	}
	if dt("de", "no_such_key_xyz") != "" || dt("fr", "title") != "Server status" {
		t.Error("text fallback wrong")
	}
}
