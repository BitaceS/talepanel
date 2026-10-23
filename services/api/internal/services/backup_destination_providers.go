package services

import (
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"unicode"
)

// Off-site backup destinations are rclone remotes.  The daemon runs rclone;
// the API only stores and validates the options.  Every option a destination
// may carry is listed here: anything else is rejected, so a destination can
// never smuggle in rclone options that run commands or read local files on
// the node (sftp "ssh", webdav "bearer_token_command", "*_file", ...).

// DestinationField describes one rclone option the panel asks for.
type DestinationField struct {
	Key         string   `json:"key"`
	Label       string   `json:"label"`
	Help        string   `json:"help,omitempty"`
	Placeholder string   `json:"placeholder,omitempty"`
	Secret      bool     `json:"secret"`
	Required    bool     `json:"required"`
	Multiline   bool     `json:"multiline,omitempty"`
	Choices     []string `json:"choices,omitempty"`
	Default     string   `json:"default,omitempty"`
	// Advanced fields are hidden behind a toggle in the UI.
	Advanced bool `json:"advanced,omitempty"`
	// obscure marks options rclone stores obscured (IsPassword in rclone).
	// The daemon obscures them with `rclone obscure -` before writing the
	// config file.
	obscure bool
	// token marks an OAuth token field: the value must be the JSON printed by
	// `rclone authorize`.
	token bool
	// pem marks a private key; newlines are turned into the literal \n
	// rclone expects for key_pem.
	pem bool
}

// DestinationProvider is one supported destination type.
type DestinationProvider struct {
	ID      string `json:"id"`
	Label   string `json:"label"`
	Backend string `json:"backend"`
	// AuthorizeCommand is set for OAuth providers: the command the user runs
	// on their own computer to obtain the token JSON.
	AuthorizeCommand string             `json:"authorize_command,omitempty"`
	BasePathHelp     string             `json:"base_path_help"`
	Fields           []DestinationField `json:"fields"`
	// fixed options are always sent to rclone and cannot be overridden.
	fixed map[string]string
}

const oauthTokenHelp = "Run the command above on your own computer (install rclone from rclone.org first). " +
	"A browser window opens; sign in and allow access. rclone then prints a line starting with {\"access_token\": " +
	"— copy that whole line (or everything between the ---> and <--- markers) and paste it here."

var destinationProviders = []DestinationProvider{
	{
		ID:               "onedrive",
		Label:            "Microsoft OneDrive",
		Backend:          "onedrive",
		AuthorizeCommand: `rclone authorize "onedrive"`,
		BasePathHelp:     "Folder in your OneDrive, e.g. TalePanel/backups. It is created if missing.",
		Fields: []DestinationField{
			{Key: "token", Label: "Token from rclone authorize", Help: oauthTokenHelp, Secret: true, Required: true, Multiline: true, token: true},
			{Key: "drive_id", Label: "Drive ID", Advanced: true,
				Help: "Leave empty: TalePanel looks up your personal or work OneDrive automatically the first time it connects. Only needed for a SharePoint document library."},
			{Key: "drive_type", Label: "Drive type", Advanced: true, Choices: []string{"personal", "business", "documentLibrary"},
				Help: "Leave empty unless you entered a Drive ID yourself."},
			{Key: "region", Label: "Region", Advanced: true, Choices: []string{"global", "us", "de", "cn"}, Default: "global",
				Help: "Only change this for national clouds (US government, Germany, China)."},
		},
	},
	{
		ID:               "gdrive",
		Label:            "Google Drive",
		Backend:          "drive",
		AuthorizeCommand: `rclone authorize "drive"`,
		BasePathHelp:     "Folder in your Google Drive, e.g. TalePanel/backups. It is created if missing.",
		Fields: []DestinationField{
			{Key: "token", Label: "Token from rclone authorize", Help: oauthTokenHelp, Secret: true, Required: true, Multiline: true, token: true},
			{Key: "root_folder_id", Label: "Root folder ID", Advanced: true,
				Help: "Optional. The ID at the end of a Drive folder URL; the folder above becomes relative to it."},
			{Key: "team_drive", Label: "Shared drive ID", Advanced: true,
				Help: "Optional. ID of a shared drive (Google Workspace) to store backups in."},
		},
		fixed: map[string]string{"scope": "drive"},
	},
	{
		ID:               "dropbox",
		Label:            "Dropbox",
		Backend:          "dropbox",
		AuthorizeCommand: `rclone authorize "dropbox"`,
		BasePathHelp:     "Folder in your Dropbox, e.g. TalePanel/backups. It is created if missing.",
		Fields: []DestinationField{
			{Key: "token", Label: "Token from rclone authorize", Help: oauthTokenHelp, Secret: true, Required: true, Multiline: true, token: true},
		},
	},
	{
		ID:           "s3",
		Label:        "S3-compatible (AWS, Wasabi, Cloudflare R2, ...)",
		Backend:      "s3",
		BasePathHelp: "Bucket name, optionally followed by a folder: my-bucket/talepanel. The bucket must already exist.",
		Fields: []DestinationField{
			{Key: "provider", Label: "Provider", Required: true, Choices: []string{"AWS", "Wasabi", "Cloudflare", "Other"}, Default: "AWS",
				Help: "Pick Other for any other S3-compatible service and fill in the endpoint."},
			{Key: "access_key_id", Label: "Access key ID", Secret: true, Required: true},
			{Key: "secret_access_key", Label: "Secret access key", Secret: true, Required: true},
			{Key: "region", Label: "Region", Placeholder: "us-east-1", Help: "Leave empty for the provider default."},
			{Key: "endpoint", Label: "Endpoint", Placeholder: "https://s3.eu-central-1.wasabisys.com",
				Help: "Required for Wasabi, Cloudflare R2 and Other; leave empty for AWS."},
		},
	},
	{
		ID:           "b2",
		Label:        "Backblaze B2",
		Backend:      "b2",
		BasePathHelp: "Bucket name, optionally followed by a folder: my-bucket/talepanel. The bucket must already exist.",
		Fields: []DestinationField{
			{Key: "account", Label: "Application key ID", Secret: true, Required: true},
			{Key: "key", Label: "Application key", Secret: true, Required: true},
		},
	},
	{
		ID:           "sftp",
		Label:        "SFTP server",
		Backend:      "sftp",
		BasePathHelp: "Folder on the server. Relative paths start in the user's home directory; /srv/backups is absolute.",
		Fields: []DestinationField{
			{Key: "host", Label: "Host", Required: true, Placeholder: "backup.example.com"},
			{Key: "port", Label: "Port", Default: "22", Placeholder: "22"},
			{Key: "user", Label: "User", Required: true},
			{Key: "pass", Label: "Password", Secret: true, obscure: true, Help: "Password or private key — one of the two is required."},
			{Key: "key_pem", Label: "Private key (PEM)", Secret: true, Multiline: true, pem: true,
				Help: "Paste an unencrypted private key, including the BEGIN/END lines."},
		},
	},
	{
		ID:           "webdav",
		Label:        "WebDAV / Nextcloud / ownCloud",
		Backend:      "webdav",
		BasePathHelp: "Folder below the WebDAV URL, e.g. TalePanel/backups.",
		Fields: []DestinationField{
			{Key: "url", Label: "WebDAV URL", Required: true, Placeholder: "https://cloud.example.com/remote.php/dav/files/USERNAME/",
				Help: "Nextcloud: Files > Settings (bottom left) shows the WebDAV address."},
			{Key: "vendor", Label: "Vendor", Choices: []string{"nextcloud", "owncloud", "sharepoint", "other"}, Default: "nextcloud"},
			{Key: "user", Label: "User", Required: true},
			{Key: "pass", Label: "Password", Secret: true, Required: true, obscure: true,
				Help: "Nextcloud: create an app password under Settings > Security instead of using your login password."},
		},
	},
}

// DestinationProviders returns the supported destination types (for the UI).
func DestinationProviders() []DestinationProvider { return destinationProviders }

func findProvider(id string) (*DestinationProvider, bool) {
	for i := range destinationProviders {
		if destinationProviders[i].ID == id {
			return &destinationProviders[i], true
		}
	}
	return nil, false
}

func (p *DestinationProvider) field(key string) (*DestinationField, bool) {
	for i := range p.Fields {
		if p.Fields[i].Key == key {
			return &p.Fields[i], true
		}
	}
	return nil, false
}

// ErrInvalidDestination wraps every validation failure so handlers can map it
// to 400.
var ErrInvalidDestination = errors.New("invalid backup destination")

func invalidf(format string, args ...any) error {
	return fmt.Errorf("%w: %s", ErrInvalidDestination, fmt.Sprintf(format, args...))
}

const maxOptionValueLen = 16 * 1024

// ValidateRemoteBasePath checks a destination base path.  It ends up inside
// an rclone remote path ("tp:<base>/<server>/<file>"), so it must not climb
// out with "..", must not look like an option, and must stay on one line.
// A single leading "/" is allowed (absolute path on SFTP).
func ValidateRemoteBasePath(p string) error {
	if p == "" {
		return invalidf("base path is required")
	}
	if len(p) > 512 {
		return invalidf("base path is too long")
	}
	rest := strings.TrimPrefix(p, "/")
	if rest == "" {
		return invalidf("base path must name a folder")
	}
	for _, seg := range strings.Split(rest, "/") {
		if seg == "" {
			return invalidf("base path must not contain empty segments or a trailing /")
		}
		if seg == "." || seg == ".." {
			return invalidf("base path must not contain . or ..")
		}
		if strings.HasPrefix(seg, "-") {
			return invalidf("base path segments must not start with -")
		}
		if strings.TrimSpace(seg) != seg {
			return invalidf("base path segments must not start or end with spaces")
		}
		for _, r := range seg {
			if unicode.IsControl(r) || strings.ContainsRune(`\:*?"<>|`, r) {
				return invalidf("base path contains a forbidden character %q", r)
			}
		}
	}
	return nil
}

// normalizeOAuthToken accepts what `rclone authorize` prints (optionally with
// the surrounding "--->"/"<---End paste" lines) and returns compact single
// line JSON.
func normalizeOAuthToken(raw string) (string, error) {
	start := strings.Index(raw, "{")
	end := strings.LastIndex(raw, "}")
	if start < 0 || end < start {
		return "", invalidf("token must be the JSON printed by rclone authorize (it starts with {\"access_token\")")
	}
	var tok map[string]any
	if err := json.Unmarshal([]byte(raw[start:end+1]), &tok); err != nil {
		return "", invalidf("token is not valid JSON: %v", err)
	}
	at, _ := tok["access_token"].(string)
	if at == "" {
		return "", invalidf("token JSON has no access_token")
	}
	compact, err := json.Marshal(tok)
	if err != nil {
		return "", invalidf("token: %v", err)
	}
	return string(compact), nil
}

// normalizeOption validates one option value for a field and returns the
// form stored in the DB.
func normalizeOption(f *DestinationField, value string) (string, error) {
	if f.token {
		return normalizeOAuthToken(value)
	}
	if f.pem {
		value = strings.TrimSpace(strings.ReplaceAll(value, "\r\n", "\n"))
		value = strings.ReplaceAll(value, "\n", `\n`)
	} else {
		value = strings.TrimSpace(value)
	}
	if len(value) > maxOptionValueLen {
		return "", invalidf("%s is too long", f.Label)
	}
	if strings.ContainsAny(value, "\r\n\x00") {
		return "", invalidf("%s must be a single line", f.Label)
	}
	if len(f.Choices) > 0 && value != "" {
		ok := false
		for _, c := range f.Choices {
			if c == value {
				ok = true
				break
			}
		}
		if !ok {
			return "", invalidf("%s must be one of %s", f.Label, strings.Join(f.Choices, ", "))
		}
	}
	if f.Key == "port" && value != "" {
		var port int
		if _, err := fmt.Sscanf(value, "%d", &port); err != nil || port < 1 || port > 65535 || fmt.Sprint(port) != value {
			return "", invalidf("port must be a number between 1 and 65535")
		}
	}
	if (f.Key == "url" || f.Key == "endpoint") && value != "" &&
		!strings.HasPrefix(value, "https://") && !strings.HasPrefix(value, "http://") {
		return "", invalidf("%s must start with https:// or http://", f.Label)
	}
	return value, nil
}

// splitDestinationOptions validates user input against the provider schema
// and splits it into plain options and secrets.  existingSecrets are kept for
// secret fields the user left empty (the UI never sees stored secrets).
func splitDestinationOptions(p *DestinationProvider, options, secrets, existingSecrets map[string]string) (map[string]string, map[string]string, error) {
	plain := map[string]string{}
	sec := map[string]string{}
	for k, v := range existingSecrets {
		if f, ok := p.field(k); ok && f.Secret {
			sec[k] = v
		}
	}
	for k, v := range options {
		f, ok := p.field(k)
		if !ok {
			return nil, nil, invalidf("unknown option %q for %s", k, p.Label)
		}
		if f.Secret {
			return nil, nil, invalidf("%s must be sent as a secret", f.Label)
		}
		nv, err := normalizeOption(f, v)
		if err != nil {
			return nil, nil, err
		}
		if nv != "" {
			plain[k] = nv
		}
	}
	for k, v := range secrets {
		f, ok := p.field(k)
		if !ok || !f.Secret {
			return nil, nil, invalidf("unknown secret %q for %s", k, p.Label)
		}
		if strings.TrimSpace(v) == "" {
			continue // keep the stored value
		}
		nv, err := normalizeOption(f, v)
		if err != nil {
			return nil, nil, err
		}
		sec[k] = nv
	}
	for _, f := range p.Fields {
		if !f.Required {
			continue
		}
		if (f.Secret && sec[f.Key] == "") || (!f.Secret && plain[f.Key] == "" && f.Default == "") {
			return nil, nil, invalidf("%s is required", f.Label)
		}
	}
	if p.ID == "sftp" && sec["pass"] == "" && sec["key_pem"] == "" {
		return nil, nil, invalidf("SFTP needs a password or a private key")
	}
	if p.ID == "s3" {
		prov := plain["provider"]
		if prov == "" {
			prov = "AWS"
		}
		if prov != "AWS" && plain["endpoint"] == "" {
			return nil, nil, invalidf("Endpoint is required for %s", prov)
		}
	}
	if p.ID == "onedrive" && (plain["drive_id"] == "") != (plain["drive_type"] == "") {
		return nil, nil, invalidf("Drive ID and Drive type must be set together (or both left empty)")
	}
	return plain, sec, nil
}

// rcloneOptions merges defaults, plain options, secrets and fixed options into
// the map sent to the daemon, plus the list of keys rclone wants obscured.
func (p *DestinationProvider) rcloneOptions(plain, secrets map[string]string) (map[string]string, []string) {
	out := map[string]string{}
	var obscure []string
	for _, f := range p.Fields {
		v := plain[f.Key]
		if f.Secret {
			v = secrets[f.Key]
		}
		if v == "" {
			v = f.Default
		}
		if v == "" {
			continue
		}
		out[f.Key] = v
		if f.obscure {
			obscure = append(obscure, f.Key)
		}
	}
	for k, v := range p.fixed {
		out[k] = v
	}
	return out, obscure
}

// writableConfigKeys are the only keys a daemon may write back after an rclone
// run: a refreshed OAuth token and the OneDrive drive it resolved.
var writableConfigKeys = map[string]bool{"token": true, "drive_id": true, "drive_type": true}
