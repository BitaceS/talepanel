package discord

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"time"
)

// APIBase is Discord's REST API.
const APIBase = "https://discord.com/api/v10"

// ErrNotFound is returned when Discord answers 404, for example for a
// status message someone deleted.
var ErrNotFound = errors.New("discord: not found")

// APIError is a non-2xx answer from Discord. Message is Discord's own text,
// which names the problem (unknown channel, missing access, invalid token).
type APIError struct {
	Status  int
	Message string
}

func (e *APIError) Error() string {
	if e.Message != "" {
		return fmt.Sprintf("discord: %d %s", e.Status, e.Message)
	}
	return fmt.Sprintf("discord: HTTP %d", e.Status)
}

// Client calls Discord's REST API with a bot token. It only ever talks to
// BaseURL (discord.com outside tests) and never follows redirects.
type Client struct {
	BaseURL string
	HTTP    *http.Client
}

// NewClient returns a client for discord.com.
func NewClient() *Client {
	return &Client{
		BaseURL: APIBase,
		HTTP: &http.Client{
			Timeout: 10 * time.Second,
			CheckRedirect: func(*http.Request, []*http.Request) error {
				return http.ErrUseLastResponse
			},
		},
	}
}

func (c *Client) do(ctx context.Context, token, method, path string, body, out any) error {
	var rd io.Reader
	if body != nil {
		b, err := json.Marshal(body)
		if err != nil {
			return err
		}
		rd = bytes.NewReader(b)
	}
	req, err := http.NewRequestWithContext(ctx, method, c.BaseURL+path, rd)
	if err != nil {
		return err
	}
	req.Header.Set("Authorization", "Bot "+token)
	req.Header.Set("User-Agent", "DiscordBot (https://github.com/BitaceS/talepanel, 1)")
	if body != nil {
		req.Header.Set("Content-Type", "application/json")
	}
	resp, err := c.HTTP.Do(req)
	if err != nil {
		// The error names the URL only, never the token header.
		return err
	}
	defer resp.Body.Close()
	data, _ := io.ReadAll(io.LimitReader(resp.Body, 1<<20))
	if resp.StatusCode == http.StatusNotFound {
		return ErrNotFound
	}
	if resp.StatusCode < 200 || resp.StatusCode > 299 {
		var e struct {
			Message string `json:"message"`
		}
		_ = json.Unmarshal(data, &e)
		return &APIError{Status: resp.StatusCode, Message: e.Message}
	}
	if out != nil && len(data) > 0 {
		return json.Unmarshal(data, out)
	}
	return nil
}

// Application is the bot's application as Discord reports it.
type Application struct {
	ID string `json:"id"`
}

// CurrentApplication returns the application the token belongs to; it
// checks the token and that it matches the configured application ID.
func (c *Client) CurrentApplication(ctx context.Context, token string) (*Application, error) {
	var app Application
	if err := c.do(ctx, token, http.MethodGet, "/applications/@me", nil, &app); err != nil {
		return nil, err
	}
	return &app, nil
}

// SetGuildCommands replaces the application's commands in one guild. Guild
// commands show up at once; global ones can take an hour.
func (c *Client) SetGuildCommands(ctx context.Context, token, appID, guildID string, cmds []Command) error {
	path := "/applications/" + url.PathEscape(appID) + "/guilds/" + url.PathEscape(guildID) + "/commands"
	return c.do(ctx, token, http.MethodPut, path, cmds, nil)
}

// CreateMessage posts msg in a channel and returns the new message's ID.
func (c *Client) CreateMessage(ctx context.Context, token, channelID string, msg Message) (string, error) {
	var out struct {
		ID string `json:"id"`
	}
	path := "/channels/" + url.PathEscape(channelID) + "/messages"
	if err := c.do(ctx, token, http.MethodPost, path, msg, &out); err != nil {
		return "", err
	}
	return out.ID, nil
}

// EditMessage replaces a message the bot posted earlier.
func (c *Client) EditMessage(ctx context.Context, token, channelID, messageID string, msg Message) error {
	path := "/channels/" + url.PathEscape(channelID) + "/messages/" + url.PathEscape(messageID)
	return c.do(ctx, token, http.MethodPatch, path, msg, nil)
}
