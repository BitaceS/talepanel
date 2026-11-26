package discord

import (
	"context"
	"crypto/ed25519"
	"encoding/hex"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"strconv"
	"testing"
	"time"
)

func TestVerify(t *testing.T) {
	pub, priv, err := ed25519.GenerateKey(nil)
	if err != nil {
		t.Fatal(err)
	}
	now := time.Unix(1_800_000_000, 0)
	ts := strconv.FormatInt(now.Unix(), 10)
	body := []byte(`{"type":1}`)
	sig := hex.EncodeToString(ed25519.Sign(priv, append([]byte(ts), body...)))
	key := hex.EncodeToString(pub)

	if !Verify(key, sig, ts, body, now) {
		t.Fatal("valid signature refused")
	}
	cases := map[string]func() bool{
		"other body":    func() bool { return Verify(key, sig, ts, []byte(`{"type":2}`), now) },
		"other time":    func() bool { return Verify(key, sig, strconv.FormatInt(now.Unix()+1, 10), body, now) },
		"too old":       func() bool { return Verify(key, sig, ts, body, now.Add(MaxSkew+time.Second)) },
		"from future":   func() bool { return Verify(key, sig, ts, body, now.Add(-MaxSkew-time.Second)) },
		"no signature":  func() bool { return Verify(key, "", ts, body, now) },
		"bad hex":       func() bool { return Verify(key, "zz", ts, body, now) },
		"bad key":       func() bool { return Verify("abcd", sig, ts, body, now) },
		"bad timestamp": func() bool { return Verify(key, sig, "soon", body, now) },
		"other key": func() bool {
			p, _, _ := ed25519.GenerateKey(nil)
			return Verify(hex.EncodeToString(p), sig, ts, body, now)
		},
	}
	for name, f := range cases {
		if f() {
			t.Errorf("%s: accepted", name)
		}
	}
}

func TestCallerAndOptions(t *testing.T) {
	var in Interaction
	raw := `{"type":2,"member":{"user":{"id":"42","username":"lukas","global_name":"Lukas"}},
	  "data":{"name":"server","options":[{"name":"start","type":1,"options":[{"name":"server","type":3,"value":"abc","focused":true}]}]}}`
	if err := json.Unmarshal([]byte(raw), &in); err != nil {
		t.Fatal(err)
	}
	if c := in.Caller(); c.ID != "42" || c.DisplayName() != "Lukas" {
		t.Fatalf("caller = %+v", c)
	}
	opt := in.Data.Options[0].Options[0]
	if opt.StringValue() != "abc" || !opt.Focused {
		t.Fatalf("option = %+v", opt)
	}
}

func TestClientErrors(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Header.Get("Authorization") != "Bot tok" {
			t.Errorf("auth header = %q", r.Header.Get("Authorization"))
		}
		switch r.URL.Path {
		case "/channels/1/messages":
			_, _ = w.Write([]byte(`{"id":"99"}`))
		case "/channels/1/messages/5":
			w.WriteHeader(http.StatusNotFound)
		default:
			w.WriteHeader(http.StatusForbidden)
			_, _ = w.Write([]byte(`{"message":"Missing Access","code":50001}`))
		}
	}))
	defer srv.Close()
	c := &Client{BaseURL: srv.URL, HTTP: srv.Client()}
	ctx := context.Background()

	id, err := c.CreateMessage(ctx, "tok", "1", Message{Content: "hi"})
	if err != nil || id != "99" {
		t.Fatalf("CreateMessage = %q, %v", id, err)
	}
	if err := c.EditMessage(ctx, "tok", "1", "5", Message{}); !errors.Is(err, ErrNotFound) {
		t.Fatalf("EditMessage 404 = %v", err)
	}
	err = c.SetGuildCommands(ctx, "tok", "app", "guild", nil)
	var apiErr *APIError
	if !errors.As(err, &apiErr) || apiErr.Status != 403 || apiErr.Message != "Missing Access" {
		t.Fatalf("SetGuildCommands = %v", err)
	}
}
