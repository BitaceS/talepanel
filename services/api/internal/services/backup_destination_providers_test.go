package services

import (
	"errors"
	"strings"
	"testing"
)

func TestValidateRemoteBasePath(t *testing.T) {
	ok := []string{"talepanel", "TalePanel/backups", "/srv/backups", "my-bucket/tale panel", "a.b/c_d"}
	for _, p := range ok {
		if err := ValidateRemoteBasePath(p); err != nil {
			t.Errorf("ValidateRemoteBasePath(%q) = %v, want nil", p, err)
		}
	}
	bad := []string{
		"", "/", "..", "a/../b", "./a", "a//b", "a/", "-a", "a/--config=/etc/x",
		"a\nb", "a\\b", "remote:x", "a/ b", "a/b ", strings.Repeat("a", 513),
	}
	for _, p := range bad {
		err := ValidateRemoteBasePath(p)
		if err == nil {
			t.Errorf("ValidateRemoteBasePath(%q) = nil, want error", p)
		} else if !errors.Is(err, ErrInvalidDestination) {
			t.Errorf("ValidateRemoteBasePath(%q) error %v does not wrap ErrInvalidDestination", p, err)
		}
	}
}

func TestNormalizeOAuthToken(t *testing.T) {
	pasted := "Paste the following into your remote machine --->\n" +
		`{"access_token":"abc","token_type":"Bearer","refresh_token":"r","expiry":"2026-10-23T10:00:00Z"}` +
		"\n<---End paste\n"
	got, err := normalizeOAuthToken(pasted)
	if err != nil {
		t.Fatalf("normalizeOAuthToken: %v", err)
	}
	if strings.ContainsAny(got, "\r\n") || !strings.HasPrefix(got, "{") || !strings.Contains(got, `"access_token":"abc"`) {
		t.Fatalf("unexpected token %q", got)
	}
	for _, bad := range []string{"", "abc", `{"refresh_token":"r"}`, `{"access_token":`} {
		if _, err := normalizeOAuthToken(bad); err == nil {
			t.Errorf("normalizeOAuthToken(%q) = nil error", bad)
		}
	}
}

func TestSplitDestinationOptionsRejectsUnknownAndDangerousKeys(t *testing.T) {
	p, _ := findProvider("sftp")
	cases := []map[string]string{
		{"ssh": "sh -c 'id'"},
		{"key_file": "/etc/shadow"},
		{"known_hosts_file": "/x"},
		{"type": "local"},
	}
	for _, opts := range cases {
		opts["host"] = "h"
		opts["user"] = "u"
		if _, _, err := splitDestinationOptions(p, opts, map[string]string{"pass": "x"}, nil); err == nil {
			t.Errorf("options %v accepted, want error", opts)
		}
	}
	// A secret sent as a plain option is refused (it would be stored unencrypted).
	if _, _, err := splitDestinationOptions(p, map[string]string{"host": "h", "user": "u", "pass": "x"}, nil, nil); err == nil {
		t.Error("secret accepted as plain option")
	}
	// Newlines would break the rclone config file.
	if _, _, err := splitDestinationOptions(p, map[string]string{"host": "h\n[evil]", "user": "u"}, map[string]string{"pass": "x"}, nil); err == nil {
		t.Error("multi-line host accepted")
	}
}

func TestSplitDestinationOptionsSFTP(t *testing.T) {
	p, _ := findProvider("sftp")
	if _, _, err := splitDestinationOptions(p, map[string]string{"host": "h", "user": "u"}, nil, nil); err == nil {
		t.Error("sftp without password or key accepted")
	}
	pem := "-----BEGIN OPENSSH PRIVATE KEY-----\r\nAAA\r\nBBB\r\n-----END OPENSSH PRIVATE KEY-----\r\n"
	plain, sec, err := splitDestinationOptions(p, map[string]string{"host": "h", "user": "u", "port": "2222"},
		map[string]string{"key_pem": pem}, nil)
	if err != nil {
		t.Fatalf("split: %v", err)
	}
	if want := `-----BEGIN OPENSSH PRIVATE KEY-----\nAAA\nBBB\n-----END OPENSSH PRIVATE KEY-----`; sec["key_pem"] != want {
		t.Errorf("key_pem = %q, want %q", sec["key_pem"], want)
	}
	if plain["port"] != "2222" {
		t.Errorf("port = %q", plain["port"])
	}
	for _, port := range []string{"0", "70000", "22a", "-1"} {
		if _, _, err := splitDestinationOptions(p, map[string]string{"host": "h", "user": "u", "port": port},
			map[string]string{"pass": "x"}, nil); err == nil {
			t.Errorf("port %q accepted", port)
		}
	}
}

func TestSplitDestinationOptionsKeepsExistingSecrets(t *testing.T) {
	p, _ := findProvider("webdav")
	existing := map[string]string{"pass": "old"}
	_, sec, err := splitDestinationOptions(p, map[string]string{"url": "https://dav.example.com/", "user": "u"},
		map[string]string{"pass": ""}, existing)
	if err != nil {
		t.Fatalf("split: %v", err)
	}
	if sec["pass"] != "old" {
		t.Errorf("pass = %q, want kept value", sec["pass"])
	}
	_, sec, _ = splitDestinationOptions(p, map[string]string{"url": "https://dav.example.com/", "user": "u"},
		map[string]string{"pass": "new"}, existing)
	if sec["pass"] != "new" {
		t.Errorf("pass = %q, want new value", sec["pass"])
	}
	if _, _, err := splitDestinationOptions(p, map[string]string{"url": "file:///etc", "user": "u"},
		map[string]string{"pass": "x"}, nil); err == nil {
		t.Error("non-http url accepted")
	}
}

func TestSplitDestinationOptionsChoicesAndRequirements(t *testing.T) {
	s3, _ := findProvider("s3")
	sec := map[string]string{"access_key_id": "a", "secret_access_key": "b"}
	if _, _, err := splitDestinationOptions(s3, map[string]string{"provider": "Wasabi"}, sec, nil); err == nil {
		t.Error("Wasabi without endpoint accepted")
	}
	if _, _, err := splitDestinationOptions(s3, map[string]string{"provider": "Bogus"}, sec, nil); err == nil {
		t.Error("unknown s3 provider accepted")
	}
	if _, _, err := splitDestinationOptions(s3, map[string]string{"provider": "AWS"}, sec, nil); err != nil {
		t.Errorf("AWS s3: %v", err)
	}

	od, _ := findProvider("onedrive")
	tok := map[string]string{"token": `{"access_token":"x"}`}
	if _, _, err := splitDestinationOptions(od, map[string]string{"drive_id": "abc"}, tok, nil); err == nil {
		t.Error("drive_id without drive_type accepted")
	}
	if _, _, err := splitDestinationOptions(od, nil, tok, nil); err != nil {
		t.Errorf("onedrive with only a token: %v", err)
	}
	if _, _, err := splitDestinationOptions(od, nil, nil, nil); err == nil {
		t.Error("onedrive without token accepted")
	}
}

func TestRcloneOptionsMarksObscureAndFixed(t *testing.T) {
	p, _ := findProvider("webdav")
	opts, obscure := p.rcloneOptions(map[string]string{"url": "https://x/", "user": "u"}, map[string]string{"pass": "pw"})
	if opts["pass"] != "pw" || opts["vendor"] != "nextcloud" || opts["url"] != "https://x/" {
		t.Errorf("unexpected options %v", opts)
	}
	if len(obscure) != 1 || obscure[0] != "pass" {
		t.Errorf("obscure = %v, want [pass]", obscure)
	}
	g, _ := findProvider("gdrive")
	opts, _ = g.rcloneOptions(nil, map[string]string{"token": "{}"})
	if opts["scope"] != "drive" {
		t.Errorf("gdrive scope = %q", opts["scope"])
	}
}

// Every provider must stay free of rclone options that run commands or read
// local files on the node.
func TestProvidersHaveNoDangerousFields(t *testing.T) {
	for _, p := range DestinationProviders() {
		for _, f := range p.Fields {
			k := f.Key
			if k == "ssh" || strings.HasSuffix(k, "_command") || strings.HasSuffix(k, "_file") || k == "type" {
				t.Errorf("provider %s exposes dangerous option %q", p.ID, k)
			}
		}
	}
}
