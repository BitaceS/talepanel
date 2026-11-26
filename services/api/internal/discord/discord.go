// Package discord holds the parts of the Discord API TalePanel uses: the
// signature check and types of HTTP interactions (slash commands), and a
// small REST client for registering commands and posting the status message.
// There is no gateway connection; Discord sends each command to the panel's
// interactions endpoint.
package discord

import (
	"crypto/ed25519"
	"encoding/hex"
	"encoding/json"
	"strconv"
	"time"
)

// MaxSkew is how old an interaction's timestamp may be. Discord sends it
// right away; an old one is a replayed request.
const MaxSkew = 5 * time.Minute

// Verify checks the X-Signature-Ed25519 / X-Signature-Timestamp headers of
// an interaction against the application's public key (hex). Discord sends
// deliberately broken signatures when the endpoint is saved and expects 401.
func Verify(publicKeyHex, signatureHex, timestamp string, body []byte, now time.Time) bool {
	key, err := hex.DecodeString(publicKeyHex)
	if err != nil || len(key) != ed25519.PublicKeySize {
		return false
	}
	sig, err := hex.DecodeString(signatureHex)
	if err != nil || len(sig) != ed25519.SignatureSize {
		return false
	}
	ts, err := strconv.ParseInt(timestamp, 10, 64)
	if err != nil {
		return false
	}
	if d := now.Sub(time.Unix(ts, 0)); d > MaxSkew || d < -MaxSkew {
		return false
	}
	msg := make([]byte, 0, len(timestamp)+len(body))
	msg = append(msg, timestamp...)
	msg = append(msg, body...)
	return ed25519.Verify(ed25519.PublicKey(key), msg, sig)
}

// Interaction types.
const (
	InteractionPing         = 1
	InteractionCommand      = 2
	InteractionAutocomplete = 4
)

// Response types.
const (
	ResponsePong         = 1
	ResponseMessage      = 4
	ResponseAutocomplete = 8
)

// FlagEphemeral makes a reply visible only to the user who ran the command.
const FlagEphemeral = 1 << 6

// Option types used by TalePanel's commands.
const (
	OptionSubCommand = 1
	OptionString     = 3
)

// User is a Discord user.
type User struct {
	ID         string `json:"id"`
	Username   string `json:"username"`
	GlobalName string `json:"global_name"`
}

// DisplayName is the name to show for u.
func (u User) DisplayName() string {
	if u.GlobalName != "" {
		return u.GlobalName
	}
	return u.Username
}

// Member is a guild member; it carries the user in guild interactions.
type Member struct {
	User User `json:"user"`
}

// Option is a command option as Discord sends it. Focused marks the option
// the user is typing in during autocomplete.
type Option struct {
	Name    string          `json:"name"`
	Type    int             `json:"type"`
	Value   json.RawMessage `json:"value,omitempty"`
	Focused bool            `json:"focused,omitempty"`
	Options []Option        `json:"options,omitempty"`
}

// StringValue returns the option's value when it is a string.
func (o Option) StringValue() string {
	var s string
	_ = json.Unmarshal(o.Value, &s)
	return s
}

// CommandData is the invoked command.
type CommandData struct {
	Name    string   `json:"name"`
	Options []Option `json:"options,omitempty"`
}

// Interaction is the request Discord sends to the endpoint.
type Interaction struct {
	ID            string       `json:"id"`
	ApplicationID string       `json:"application_id"`
	Type          int          `json:"type"`
	Data          *CommandData `json:"data,omitempty"`
	GuildID       string       `json:"guild_id,omitempty"`
	ChannelID     string       `json:"channel_id,omitempty"`
	Member        *Member      `json:"member,omitempty"`
	User          *User        `json:"user,omitempty"` // set in DMs only
	Locale        string       `json:"locale,omitempty"`
}

// Caller returns the user who sent the interaction.
func (i Interaction) Caller() User {
	if i.Member != nil {
		return i.Member.User
	}
	if i.User != nil {
		return *i.User
	}
	return User{}
}

// EmbedField is one name/value pair of an embed.
type EmbedField struct {
	Name   string `json:"name"`
	Value  string `json:"value"`
	Inline bool   `json:"inline,omitempty"`
}

// EmbedFooter is the small text under an embed.
type EmbedFooter struct {
	Text string `json:"text"`
}

// Embed is a rich message block.
type Embed struct {
	Title       string       `json:"title,omitempty"`
	Description string       `json:"description,omitempty"`
	Color       int          `json:"color,omitempty"`
	Fields      []EmbedField `json:"fields,omitempty"`
	Footer      *EmbedFooter `json:"footer,omitempty"`
	Timestamp   string       `json:"timestamp,omitempty"`
}

// AllowedMentions with no parse entries stops a reply from pinging anyone,
// whatever a server or player name contains.
type AllowedMentions struct {
	Parse []string `json:"parse"`
}

// Message is the body of a reply or channel message.
type Message struct {
	Content         string           `json:"content,omitempty"`
	Embeds          []Embed          `json:"embeds,omitempty"`
	Flags           int              `json:"flags,omitempty"`
	AllowedMentions *AllowedMentions `json:"allowed_mentions,omitempty"`
}

// NoMentions is the AllowedMentions value for every message TalePanel sends.
func NoMentions() *AllowedMentions { return &AllowedMentions{Parse: []string{}} }

// Choice is one autocomplete suggestion.
type Choice struct {
	Name  string `json:"name"`
	Value string `json:"value"`
}

// ResponseData is the data of an interaction response: a message or, for
// autocomplete, the choices.
type ResponseData struct {
	Content         string           `json:"content,omitempty"`
	Embeds          []Embed          `json:"embeds,omitempty"`
	Flags           int              `json:"flags,omitempty"`
	AllowedMentions *AllowedMentions `json:"allowed_mentions,omitempty"`
	Choices         []Choice         `json:"choices,omitempty"`
}

// Response is what the endpoint answers.
type Response struct {
	Type int           `json:"type"`
	Data *ResponseData `json:"data,omitempty"`
}

// CommandOption describes an option when registering a command.
type CommandOption struct {
	Type                     int               `json:"type"`
	Name                     string            `json:"name"`
	Description              string            `json:"description"`
	DescriptionLocalizations map[string]string `json:"description_localizations,omitempty"`
	Required                 bool              `json:"required,omitempty"`
	Autocomplete             bool              `json:"autocomplete,omitempty"`
	MaxLength                int               `json:"max_length,omitempty"`
	Options                  []CommandOption   `json:"options,omitempty"`
}

// Command describes a slash command when registering it.
type Command struct {
	Name                     string            `json:"name"`
	Description              string            `json:"description"`
	DescriptionLocalizations map[string]string `json:"description_localizations,omitempty"`
	Options                  []CommandOption   `json:"options,omitempty"`
	// Contexts: 0 = guild only, so the commands never show up in DMs.
	Contexts []int `json:"contexts,omitempty"`
}
