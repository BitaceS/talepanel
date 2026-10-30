// Package daemon provides an HTTP client for communicating with TaleDaemon
// nodes.  Each node runs an Axum HTTP server; the API calls it to execute
// power actions (start/stop/restart/kill) and to retrieve real-time metrics.
//
// Authentication: every request carries the node's registration token as a
// Bearer token.  The daemon validates this against its local config.  Nodes
// that have finished mTLS enrollment are additionally reached over https with
// mutual TLS: the panel presents its CA-issued client certificate and pins the
// daemon's certificate to the panel CA and the node's UUID (see
// BuildPanelTLSConfig).  The bearer token is sent in both modes.
package daemon

import (
	"bytes"
	"context"
	"crypto/tls"
	"crypto/x509"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"mime/multipart"
	"net/http"
	"net/url"
	"sync"
	"time"

	"go.uber.org/zap"
)

// ─────────────────────────────────────────────────────────────────────────────
// Request / response types (mirror daemon's Rust structs)
// ─────────────────────────────────────────────────────────────────────────────

// ServerConfig is the payload sent to the daemon when starting a server.
// Must match hytale::ServerConfig in services/daemon/src/process/hytale.rs
type ServerConfig struct {
	ServerID   string  `json:"server_id"`
	Name       string  `json:"name"`
	DataPath   string  `json:"data_path"`
	Port       uint16  `json:"port"`
	RAMLimitMB uint32  `json:"ram_limit_mb"`
	// CPU limit in percent of one core (100 = one full core, 250 = two and a
	// half cores). 0 = unlimited. Enforced by the daemon via cgroup v2 cpu.max.
	CPULimit float32 `json:"cpu_limit"`
	// Auto restart on unexpected exits: at most CrashLimit restarts within
	// CrashWindowS seconds, then the daemon gives up and leaves it crashed.
	AutoRestart  bool   `json:"auto_restart"`
	CrashLimit   uint32 `json:"crash_limit"`
	CrashWindowS uint32 `json:"crash_window_s"`
	// Allowlisted per-server Hytale launch flags; the daemon re-checks them.
	LaunchFlags []string `json:"launch_flags"`
}

// StartRequest is the body of POST /servers/:id/start
type StartRequest struct {
	Config ServerConfig `json:"config"`
}

// CommandRequest is the body of POST /servers/:id/command
type CommandRequest struct {
	Command string `json:"command"`
}

// ProvisionRequest is the body of POST /servers/:id/provision
type ProvisionRequest struct {
	Version  string `json:"version"`
	DataPath string `json:"data_path"`
	// Reinstall replaces the server binaries even if they already exist.
	Reinstall bool `json:"reinstall,omitempty"`
}

// ActionResponse is the daemon's standard success/error envelope.
type ActionResponse struct {
	Success bool   `json:"success"`
	Message string `json:"message"`
}

// ServerStatus is returned by GET /servers/:id/status
type ServerStatus struct {
	ServerID string `json:"server_id"`
	Status   string `json:"status"`
}

// ServerMetrics is returned by GET /servers/:id/metrics
type ServerMetrics struct {
	ServerID   string  `json:"server_id"`
	CPUPercent float32 `json:"cpu_percent"`
	RAMMB      uint64  `json:"ram_mb"`
	UptimeS    uint64  `json:"uptime_s"`
}

// DaemonHealth is returned by GET /health
type DaemonHealth struct {
	Status  string `json:"status"`
	Version string `json:"version"`
	NodeID  string `json:"node_id"`
}

// ─────────────────────────────────────────────────────────────────────────────
// Client
// ─────────────────────────────────────────────────────────────────────────────

// Client communicates with a single TaleDaemon node.
type Client struct {
	baseURL    string // e.g. "http://192.168.1.10:8444" or "https://..." for mTLS
	nodeToken  string
	httpClient *http.Client
	// transport is the mTLS transport of a TLS client, nil for legacy clients
	// (which use http.DefaultTransport). Shared by httpClient and longClient.
	transport *http.Transport
	tls       bool
	log       *zap.Logger
}

// NewClient constructs a legacy (plain http + bearer) daemon client.
//
//	baseURL   — "http://{fqdn}:{port}" of the daemon's local HTTP server
//	nodeToken — the registration token issued to this node
func NewClient(baseURL, nodeToken string, log *zap.Logger) *Client {
	return &Client{
		baseURL:   baseURL,
		nodeToken: nodeToken,
		log:       log,
		httpClient: &http.Client{
			Timeout: 30 * time.Second,
		},
	}
}

// NewClientTLS constructs a daemon client that talks https with the given
// mutual-TLS config (from BuildPanelTLSConfig). baseURL must be
// "https://{fqdn}:{port}". The bearer token is still sent on every request.
func NewClientTLS(baseURL, nodeToken string, tlsCfg *tls.Config, log *zap.Logger) *Client {
	tr := http.DefaultTransport.(*http.Transport).Clone()
	tr.TLSClientConfig = tlsCfg
	return &Client{
		baseURL:   baseURL,
		nodeToken: nodeToken,
		log:       log,
		transport: tr,
		tls:       true,
		httpClient: &http.Client{
			Timeout:   30 * time.Second,
			Transport: tr,
		},
	}
}

// roundTripper returns the transport for this client; nil means
// http.DefaultTransport.
func (c *Client) roundTripper() http.RoundTripper {
	if c.transport == nil {
		return nil
	}
	return c.transport
}

// closeIdle drops the pooled connections of a client that is being replaced.
func (c *Client) closeIdle() {
	if c.transport != nil {
		c.transport.CloseIdleConnections()
	}
}

// BuildPanelTLSConfig builds the panel's client-side TLS config for one node.
//
// The daemon's certificate is pinned to the panel CA and to expectedCN (the
// node's UUID) instead of the dialed hostname: nodes are addressed by Docker
// service names, private IPs or changing FQDNs, while their identity is the
// node UUID the panel's CA wrote into the CN. Go's built-in verification is
// therefore disabled (InsecureSkipVerify) and fully replaced by
// VerifyConnection, which checks the chain to the panel CA, the validity
// window, the serverAuth usage and the CN. VerifyConnection (unlike
// VerifyPeerCertificate) also runs on resumed sessions, so enabling a session
// cache later cannot bypass the pin. The panel's client certificate is
// presented for the daemon's side of the mutual authentication.
func BuildPanelTLSConfig(caCertPEM, panelCertPEM, panelKeyPEM []byte, expectedCN string) (*tls.Config, error) {
	if expectedCN == "" {
		return nil, errors.New("daemon tls: empty expected node CN")
	}
	roots := x509.NewCertPool()
	if !roots.AppendCertsFromPEM(caCertPEM) {
		return nil, errors.New("daemon tls: no valid CA certificate")
	}
	clientCert, err := tls.X509KeyPair(panelCertPEM, panelKeyPEM)
	if err != nil {
		return nil, fmt.Errorf("daemon tls: panel client certificate: %w", err)
	}
	return &tls.Config{
		MinVersion:   tls.VersionTLS12,
		Certificates: []tls.Certificate{clientCert},
		// Disables only Go's hostname-based verification; VerifyConnection
		// below re-does the chain check and pins the CN instead.
		InsecureSkipVerify: true,
		VerifyConnection: func(cs tls.ConnectionState) error {
			return verifyNodeCert(cs.PeerCertificates, roots, expectedCN)
		},
	}, nil
}

// verifyNodeCert checks that certs[0] chains to roots (with the remaining
// certs as intermediates), is valid right now, allows serverAuth and has
// Subject CN == expectedCN.
func verifyNodeCert(certs []*x509.Certificate, roots *x509.CertPool, expectedCN string) error {
	if len(certs) == 0 {
		return errors.New("daemon tls: no peer certificate")
	}
	intermediates := x509.NewCertPool()
	for _, cert := range certs[1:] {
		intermediates.AddCert(cert)
	}
	leaf := certs[0]
	if _, err := leaf.Verify(x509.VerifyOptions{
		Roots:         roots,
		Intermediates: intermediates,
		CurrentTime:   time.Now(),
		KeyUsages:     []x509.ExtKeyUsage{x509.ExtKeyUsageServerAuth},
	}); err != nil {
		return fmt.Errorf("daemon tls: peer certificate rejected: %w", err)
	}
	if leaf.Subject.CommonName != expectedCN {
		return fmt.Errorf("daemon tls: peer certificate CN %q does not match node %q",
			leaf.Subject.CommonName, expectedCN)
	}
	return nil
}

// ─────────────────────────────────────────────────────────────────────────────
// Power actions
// ─────────────────────────────────────────────────────────────────────────────

// StartServer instructs the daemon to spawn the Hytale server process.
func (c *Client) StartServer(ctx context.Context, cfg ServerConfig) error {
	body := StartRequest{Config: cfg}
	return c.postAction(ctx, fmt.Sprintf("/servers/%s/start", cfg.ServerID), body)
}

// StopServer instructs the daemon to gracefully stop a server.
func (c *Client) StopServer(ctx context.Context, serverID string) error {
	return c.postAction(ctx, fmt.Sprintf("/servers/%s/stop", serverID), nil)
}

// RestartServer instructs the daemon to restart a running server.
func (c *Client) RestartServer(ctx context.Context, serverID string) error {
	return c.postAction(ctx, fmt.Sprintf("/servers/%s/restart", serverID), nil)
}

// KillServer instructs the daemon to immediately SIGKILL a server process.
func (c *Client) KillServer(ctx context.Context, serverID string) error {
	return c.postAction(ctx, fmt.Sprintf("/servers/%s/kill", serverID), nil)
}

// DeleteServerData tells the daemon to kill the process (if any) and
// rm -rf the server's data directory.  Called by the API before removing
// the server DB row.
func (c *Client) DeleteServerData(ctx context.Context, serverID string) error {
	return c.doRequest(ctx, http.MethodDelete, fmt.Sprintf("/servers/%s", serverID), nil, nil)
}

// SendCommand sends a console command to a running server via its stdin.
func (c *Client) SendCommand(ctx context.Context, serverID, command string) error {
	return c.postAction(ctx, fmt.Sprintf("/servers/%s/command", serverID), CommandRequest{Command: command})
}

// ProvisionServer instructs the daemon to download Hytale server files for the given server.
// The daemon runs the Hytale Downloader CLI in the background and reports status
// back to the API when provisioning completes.
func (c *Client) ProvisionServer(ctx context.Context, serverID, version, dataPath string) error {
	return c.postAction(ctx, fmt.Sprintf("/servers/%s/provision", serverID), ProvisionRequest{
		Version:  version,
		DataPath: dataPath,
	})
}

// ReinstallServer instructs the daemon to fetch the Hytale server files again,
// replacing the existing binaries (world data and configs are kept). Like
// ProvisionServer it runs in the background and reports status when done.
func (c *Client) ReinstallServer(ctx context.Context, serverID, version, dataPath string) error {
	return c.postAction(ctx, fmt.Sprintf("/servers/%s/provision", serverID), ProvisionRequest{
		Version:   version,
		DataPath:  dataPath,
		Reinstall: true,
	})
}

// ─────────────────────────────────────────────────────────────────────────────
// Observability
// ─────────────────────────────────────────────────────────────────────────────

// GetServerStatus returns the current lifecycle status of a server.
func (c *Client) GetServerStatus(ctx context.Context, serverID string) (*ServerStatus, error) {
	var result ServerStatus
	if err := c.get(ctx, fmt.Sprintf("/servers/%s/status", serverID), &result); err != nil {
		return nil, err
	}
	return &result, nil
}

// GetServerMetrics returns real-time resource metrics for a server.
func (c *Client) GetServerMetrics(ctx context.Context, serverID string) (*ServerMetrics, error) {
	var result ServerMetrics
	if err := c.get(ctx, fmt.Sprintf("/servers/%s/metrics", serverID), &result); err != nil {
		return nil, err
	}
	return &result, nil
}

// GetNetworkStats retrieves network traffic stats from the daemon.
// Returns the raw daemon JSON so all fields (current, averages, threats,
// history, mitigation, etc.) pass through to the panel without a struct
// definition that has to be kept in sync with the daemon.
func (c *Client) GetNetworkStats(ctx context.Context) (json.RawMessage, error) {
	var result json.RawMessage
	if err := c.get(ctx, "/network-stats", &result); err != nil {
		return nil, err
	}
	return result, nil
}

// Health checks whether the daemon is reachable and returns its version info.
func (c *Client) Health(ctx context.Context) (*DaemonHealth, error) {
	var result DaemonHealth
	if err := c.get(ctx, "/health", &result); err != nil {
		return nil, err
	}
	return &result, nil
}

// ─────────────────────────────────────────────────────────────────────────────
// File browser types
// ─────────────────────────────────────────────────────────────────────────────

// FileEntry represents a file or directory in the server's data directory.
type FileEntry struct {
	Name     string `json:"name"`
	Size     int64  `json:"size"`
	IsDir    bool   `json:"is_dir"`
	Modified string `json:"modified"`
}

// FileListResponse is the response from listing files in a directory.
type FileListResponse struct {
	Entries []FileEntry `json:"entries"`
	Path    string      `json:"path"`
}

// FileContentResponse is the response from reading a file's content.
type FileContentResponse struct {
	Content string `json:"content"`
	Path    string `json:"path"`
}

// ─────────────────────────────────────────────────────────────────────────────
// File browser operations
// ─────────────────────────────────────────────────────────────────────────────

// ListFiles returns the directory listing for a path inside a server's data dir.
func (c *Client) ListFiles(ctx context.Context, serverID, path string) (*FileListResponse, error) {
	var result FileListResponse
	if err := c.getWithQuery(ctx, fmt.Sprintf("/servers/%s/files", serverID), map[string]string{"path": path}, &result); err != nil {
		return nil, err
	}
	return &result, nil
}

// GetFileContent returns the text content of a file inside a server's data dir.
func (c *Client) GetFileContent(ctx context.Context, serverID, path string) (*FileContentResponse, error) {
	var result FileContentResponse
	if err := c.getWithQuery(ctx, fmt.Sprintf("/servers/%s/files/content", serverID), map[string]string{"path": path}, &result); err != nil {
		return nil, err
	}
	return &result, nil
}

// WriteFileContent writes text content to a file inside a server's data dir.
func (c *Client) WriteFileContent(ctx context.Context, serverID, path, content string) error {
	body := map[string]string{"path": path, "content": content}
	return c.doRequest(ctx, http.MethodPut, fmt.Sprintf("/servers/%s/files/content", serverID), nil, body)
}

// DeleteFile removes a file or directory inside a server's data dir.
func (c *Client) DeleteFile(ctx context.Context, serverID, path string) error {
	return c.doRequest(ctx, http.MethodDelete, fmt.Sprintf("/servers/%s/files", serverID), map[string]string{"path": path}, nil)
}

// CreateDirectory creates a directory inside a server's data dir.
func (c *Client) CreateDirectory(ctx context.Context, serverID, path string) error {
	body := map[string]string{"path": path}
	return c.doRequest(ctx, http.MethodPost, fmt.Sprintf("/servers/%s/files/directory", serverID), nil, body)
}

// RenameFile renames a file or directory inside a server's data dir.
func (c *Client) RenameFile(ctx context.Context, serverID, path, newName string) error {
	body := map[string]string{"path": path, "new_name": newName}
	return c.doRequest(ctx, http.MethodPost, fmt.Sprintf("/servers/%s/files/rename", serverID), nil, body)
}

// UploadFile forwards a multipart upload to the daemon.
func (c *Client) UploadFile(ctx context.Context, serverID, dir string, fileName string, data io.Reader) error {
	var buf bytes.Buffer
	writer := multipart.NewWriter(&buf)
	part, err := writer.CreateFormFile("file", fileName)
	if err != nil {
		return fmt.Errorf("daemon: create form file: %w", err)
	}
	if _, err := io.Copy(part, data); err != nil {
		return fmt.Errorf("daemon: copy file data: %w", err)
	}
	writer.Close()

	uploadURL := c.endpoint("/servers/"+url.PathEscape(serverID)+"/files/upload", url.Values{"path": {dir}})
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, uploadURL, &buf)
	if err != nil {
		return fmt.Errorf("daemon: build upload request: %w", err)
	}
	req.Header.Set("Content-Type", writer.FormDataContentType())
	req.Header.Set("Authorization", "Bearer "+c.nodeToken)

	resp, err := c.httpClient.Do(req)
	if err != nil {
		return fmt.Errorf("daemon: upload: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode >= 400 {
		body, _ := io.ReadAll(resp.Body)
		return fmt.Errorf("daemon: upload failed (%d): %s", resp.StatusCode, body)
	}
	return nil
}

// UploadStream forwards a raw multipart upload body to the daemon without
// buffering it, so multi-gigabyte uploads (and whole folders) stream straight
// through with flat memory usage. contentType must be the original client
// Content-Type header — it carries the multipart boundary. Uses a client with
// no timeout since large uploads can take a while.
func (c *Client) UploadStream(ctx context.Context, serverID, dir, contentType string, body io.Reader) error {
	uploadURL := c.endpoint("/servers/"+url.PathEscape(serverID)+"/files/upload", url.Values{"path": {dir}})
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, uploadURL, body)
	if err != nil {
		return fmt.Errorf("daemon: build upload request: %w", err)
	}
	req.Header.Set("Content-Type", contentType)
	req.Header.Set("Authorization", "Bearer "+c.nodeToken)

	resp, err := c.longClient().Do(req)
	if err != nil {
		return fmt.Errorf("daemon: upload: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode >= 400 {
		b, _ := io.ReadAll(resp.Body)
		return fmt.Errorf("daemon: upload failed (%d): %s", resp.StatusCode, b)
	}
	return nil
}

// DownloadFile streams a file from the daemon as raw bytes.
func (c *Client) DownloadFile(ctx context.Context, serverID, path string) (io.ReadCloser, string, error) {
	downloadURL := c.endpoint("/servers/"+url.PathEscape(serverID)+"/files/download", url.Values{"path": {path}})
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, downloadURL, nil)
	if err != nil {
		return nil, "", fmt.Errorf("daemon: build download request: %w", err)
	}
	req.Header.Set("Authorization", "Bearer "+c.nodeToken)

	resp, err := c.httpClient.Do(req)
	if err != nil {
		return nil, "", fmt.Errorf("daemon: download: %w", err)
	}

	if resp.StatusCode >= 400 {
		body, _ := io.ReadAll(resp.Body)
		resp.Body.Close()
		return nil, "", fmt.Errorf("daemon: download failed (%d): %s", resp.StatusCode, body)
	}

	// Take the filename from the daemon's Content-Disposition, parsed
	// properly rather than by string splitting.
	fileName := filenameFromDisposition(resp.Header.Get("Content-Disposition"), "download")

	return resp.Body, fileName, nil
}

// ExtractArchive tells the daemon to extract a zip archive.
func (c *Client) ExtractArchive(ctx context.Context, serverID, path string) error {
	body := map[string]string{"path": path}
	return c.doRequest(ctx, http.MethodPost, fmt.Sprintf("/servers/%s/files/extract", serverID), nil, body)
}

// CreateArchive tells the daemon to create a zip archive from a path.
func (c *Client) CreateArchive(ctx context.Context, serverID, path string) error {
	body := map[string]string{"path": path}
	return c.doRequest(ctx, http.MethodPost, fmt.Sprintf("/servers/%s/files/archive", serverID), nil, body)
}

// ─────────────────────────────────────────────────────────────────────────────
// Backups — archiving a whole server can take minutes, so these bypass the
// client's default 30s timeout and rely on the caller's context deadline.
// ─────────────────────────────────────────────────────────────────────────────

// BackupCreatedResponse mirrors the daemon's create_backup response.
type BackupCreatedResponse struct {
	Success     bool   `json:"success"`
	SizeBytes   int64  `json:"size_bytes"`
	StoragePath string `json:"storage_path"`
}

// longClient returns an HTTP client with no fixed timeout for long-running
// backup operations. The caller's context governs cancellation.
func (c *Client) longClient() *http.Client {
	return &http.Client{Timeout: 0, Transport: c.roundTripper()}
}

// EncKey carries a panel-managed backup key to the daemon for one operation.
// The daemon encrypts with it on create and decrypts with it on restore/
// download; it is never stored on the node.
type EncKey struct {
	Key   string // base64 32-byte key
	KeyID uint8
}

// backupBody builds a backup request body, adding the encryption key when set.
func backupBody(backupID string, enc *EncKey) []byte {
	m := map[string]any{"backup_id": backupID}
	if enc != nil {
		m["encryption_key"] = enc.Key
		m["key_id"] = enc.KeyID
	}
	b, _ := json.Marshal(m)
	return b
}

// CreateBackup asks the daemon to archive the whole server data dir into the
// backups directory and returns the archive size and on-node path. When enc is
// set the archive is encrypted.
func (c *Client) CreateBackup(ctx context.Context, serverID, backupID string, enc *EncKey) (*BackupCreatedResponse, error) {
	body := backupBody(backupID, enc)
	backupURL := c.endpoint("/servers/"+url.PathEscape(serverID)+"/backup", nil)
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, backupURL, bytes.NewReader(body))
	if err != nil {
		return nil, fmt.Errorf("daemon: build backup request: %w", err)
	}
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Authorization", "Bearer "+c.nodeToken)

	resp, err := c.longClient().Do(req)
	if err != nil {
		return nil, fmt.Errorf("daemon: create backup: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode >= 400 {
		b, _ := io.ReadAll(resp.Body)
		return nil, fmt.Errorf("daemon: create backup failed (%d): %s", resp.StatusCode, b)
	}

	var out BackupCreatedResponse
	if err := json.NewDecoder(resp.Body).Decode(&out); err != nil {
		return nil, fmt.Errorf("daemon: decode backup response: %w", err)
	}
	return &out, nil
}

// RestoreBackup asks the daemon to extract a backup archive back into the
// server. When enc is set the archive is decrypted first.
func (c *Client) RestoreBackup(ctx context.Context, serverID, backupID string, enc *EncKey) error {
	body := backupBody(backupID, enc)
	restoreURL := c.endpoint("/servers/"+url.PathEscape(serverID)+"/backup/restore", nil)
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, restoreURL, bytes.NewReader(body))
	if err != nil {
		return fmt.Errorf("daemon: build restore request: %w", err)
	}
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Authorization", "Bearer "+c.nodeToken)

	resp, err := c.longClient().Do(req)
	if err != nil {
		return fmt.Errorf("daemon: restore backup: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode >= 400 {
		b, _ := io.ReadAll(resp.Body)
		return fmt.Errorf("daemon: restore backup failed (%d): %s", resp.StatusCode, b)
	}
	return nil
}

// DownloadBackup streams a backup archive from the daemon.
func (c *Client) DownloadBackup(ctx context.Context, serverID, backupID string) (io.ReadCloser, error) {
	downloadURL := c.endpoint("/servers/"+url.PathEscape(serverID)+"/backup/download", url.Values{"backup_id": {backupID}})
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, downloadURL, nil)
	if err != nil {
		return nil, fmt.Errorf("daemon: build backup download request: %w", err)
	}
	req.Header.Set("Authorization", "Bearer "+c.nodeToken)

	resp, err := c.longClient().Do(req)
	if err != nil {
		return nil, fmt.Errorf("daemon: download backup: %w", err)
	}
	if resp.StatusCode >= 400 {
		b, _ := io.ReadAll(resp.Body)
		resp.Body.Close()
		return nil, fmt.Errorf("daemon: download backup failed (%d): %s", resp.StatusCode, b)
	}
	return resp.Body, nil
}

// DeleteBackupArchive removes a backup archive from the node (idempotent).
func (c *Client) DeleteBackupArchive(ctx context.Context, serverID, backupID string) error {
	return c.doRequest(ctx, http.MethodDelete, fmt.Sprintf("/servers/%s/backup", serverID),
		map[string]string{"backup_id": backupID}, nil)
}

// ImportServer streams a server archive to the daemon, which reconstructs the
// server's data directory. Used as the receiving half of a cross-node migration.
// The archive is streamed from `data` without buffering the whole thing.
func (c *Client) ImportServer(ctx context.Context, serverID string, data io.Reader) error {
	pr, pw := io.Pipe()
	writer := multipart.NewWriter(pw)

	go func() {
		part, err := writer.CreateFormFile("file", serverID+".zip")
		if err != nil {
			pw.CloseWithError(err)
			return
		}
		if _, err := io.Copy(part, data); err != nil {
			pw.CloseWithError(err)
			return
		}
		pw.CloseWithError(writer.Close())
	}()

	importURL := c.endpoint("/servers/"+url.PathEscape(serverID)+"/import", nil)
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, importURL, pr)
	if err != nil {
		return fmt.Errorf("daemon: build import request: %w", err)
	}
	req.Header.Set("Content-Type", writer.FormDataContentType())
	req.Header.Set("Authorization", "Bearer "+c.nodeToken)

	resp, err := c.longClient().Do(req)
	if err != nil {
		return fmt.Errorf("daemon: import server: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode >= 400 {
		b, _ := io.ReadAll(resp.Body)
		return fmt.Errorf("daemon: import server failed (%d): %s", resp.StatusCode, b)
	}
	return nil
}

// ─────────────────────────────────────────────────────────────────────────────
// Internal HTTP helpers
// ─────────────────────────────────────────────────────────────────────────────

func (c *Client) postAction(ctx context.Context, path string, body any) error {
	var bodyReader io.Reader
	if body != nil {
		data, err := json.Marshal(body)
		if err != nil {
			return fmt.Errorf("daemon: marshal body: %w", err)
		}
		bodyReader = bytes.NewReader(data)
	} else {
		bodyReader = http.NoBody
	}

	req, err := http.NewRequestWithContext(ctx, http.MethodPost, c.baseURL+path, bodyReader)
	if err != nil {
		return fmt.Errorf("daemon: build request: %w", err)
	}
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Authorization", "Bearer "+c.nodeToken)

	resp, err := c.httpClient.Do(req)
	if err != nil {
		return fmt.Errorf("daemon: POST %s: %w", path, err)
	}
	defer resp.Body.Close()

	var result ActionResponse
	if err := json.NewDecoder(resp.Body).Decode(&result); err != nil {
		return fmt.Errorf("daemon: decode response: %w", err)
	}

	if !result.Success || (resp.StatusCode >= 400) {
		return fmt.Errorf("daemon: %s failed: %s", path, result.Message)
	}

	c.log.Debug("daemon action succeeded",
		zap.String("path", path),
		zap.String("message", result.Message),
	)
	return nil
}

func (c *Client) get(ctx context.Context, path string, out any) error {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, c.baseURL+path, nil)
	if err != nil {
		return fmt.Errorf("daemon: build request: %w", err)
	}
	req.Header.Set("Authorization", "Bearer "+c.nodeToken)

	resp, err := c.httpClient.Do(req)
	if err != nil {
		return fmt.Errorf("daemon: GET %s: %w", path, err)
	}
	defer resp.Body.Close()

	if resp.StatusCode >= 400 {
		body, _ := io.ReadAll(resp.Body)
		return fmt.Errorf("daemon: GET %s returned %d: %s", path, resp.StatusCode, body)
	}

	return json.NewDecoder(resp.Body).Decode(out)
}

func (c *Client) getWithQuery(ctx context.Context, path string, query map[string]string, out any) error {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, c.baseURL+path, nil)
	if err != nil {
		return fmt.Errorf("daemon: build request: %w", err)
	}
	req.Header.Set("Authorization", "Bearer "+c.nodeToken)

	q := req.URL.Query()
	for k, v := range query {
		q.Set(k, v)
	}
	req.URL.RawQuery = q.Encode()

	resp, err := c.httpClient.Do(req)
	if err != nil {
		return fmt.Errorf("daemon: GET %s: %w", path, err)
	}
	defer resp.Body.Close()

	if resp.StatusCode >= 400 {
		body, _ := io.ReadAll(resp.Body)
		return fmt.Errorf("daemon: GET %s returned %d: %s", path, resp.StatusCode, body)
	}

	return json.NewDecoder(resp.Body).Decode(out)
}

// doRequest is a general-purpose helper that supports any HTTP method, optional
// query parameters, and an optional JSON body.  It expects the daemon's standard
// ActionResponse envelope.
func (c *Client) doRequest(ctx context.Context, method, path string, query map[string]string, body any) error {
	var bodyReader io.Reader
	if body != nil {
		data, err := json.Marshal(body)
		if err != nil {
			return fmt.Errorf("daemon: marshal body: %w", err)
		}
		bodyReader = bytes.NewReader(data)
	} else {
		bodyReader = http.NoBody
	}

	req, err := http.NewRequestWithContext(ctx, method, c.baseURL+path, bodyReader)
	if err != nil {
		return fmt.Errorf("daemon: build request: %w", err)
	}
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Authorization", "Bearer "+c.nodeToken)

	if query != nil {
		q := req.URL.Query()
		for k, v := range query {
			q.Set(k, v)
		}
		req.URL.RawQuery = q.Encode()
	}

	resp, err := c.httpClient.Do(req)
	if err != nil {
		return fmt.Errorf("daemon: %s %s: %w", method, path, err)
	}
	defer resp.Body.Close()

	var result ActionResponse
	if err := json.NewDecoder(resp.Body).Decode(&result); err != nil {
		return fmt.Errorf("daemon: decode response: %w", err)
	}

	if !result.Success || (resp.StatusCode >= 400) {
		return fmt.Errorf("daemon: %s failed: %s", path, result.Message)
	}

	c.log.Debug("daemon action succeeded",
		zap.String("method", method),
		zap.String("path", path),
		zap.String("message", result.Message),
	)
	return nil
}

// ─────────────────────────────────────────────────────────────────────────────
// ClientPool — manages one client per node
// ─────────────────────────────────────────────────────────────────────────────

// ClientPool holds a daemon client per registered node, keyed by node UUID.
// Heartbeats register concurrently with request handlers reading, so the map
// is guarded.
type ClientPool struct {
	mu      sync.RWMutex
	clients map[string]*Client
	log     *zap.Logger

	// Panel mTLS material (PEM). Nil until SetTLSMaterial succeeds; until then
	// every node is reached over legacy http.
	caPEM, certPEM, keyPEM []byte
}

// NewClientPool creates an empty pool.
func NewClientPool(log *zap.Logger) *ClientPool {
	return &ClientPool{
		clients: make(map[string]*Client),
		log:     log,
	}
}

// SetTLSMaterial gives the pool the panel CA certificate and the panel's
// client certificate/key, enabling https+mTLS for nodes registered with mtls.
// The material is validated up front; on error the pool stays legacy-only.
func (p *ClientPool) SetTLSMaterial(caPEM, certPEM, keyPEM []byte) error {
	if _, err := BuildPanelTLSConfig(caPEM, certPEM, keyPEM, "validate"); err != nil {
		return err
	}
	p.mu.Lock()
	p.caPEM, p.certPEM, p.keyPEM = caPEM, certPEM, keyPEM
	p.mu.Unlock()
	return nil
}

// Register adds or replaces a daemon client for the given node. The fqdn and
// port are validated so a stored node address cannot smuggle a path, query or
// userinfo into every daemon URL.
//
// With mtls set (the node reported a ready certificate) the client uses https
// with mutual TLS, pinning the daemon certificate to the node UUID. If the
// pool has no TLS material it falls back to legacy http and logs a warning
// instead of failing registration. An unchanged registration keeps the
// existing client, so its pooled connections survive heartbeats.
func (p *ClientPool) Register(nodeID, fqdn string, port int, token string, mtls bool) error {
	p.mu.RLock()
	caPEM, certPEM, keyPEM := p.caPEM, p.certPEM, p.keyPEM
	p.mu.RUnlock()

	var tlsCfg *tls.Config
	if mtls {
		if caPEM == nil {
			p.log.Warn("node is mTLS-ready but the panel has no TLS material; using legacy http",
				zap.String("node_id", nodeID))
		} else if cfg, err := BuildPanelTLSConfig(caPEM, certPEM, keyPEM, nodeID); err != nil {
			p.log.Warn("building node TLS config failed; using legacy http",
				zap.String("node_id", nodeID), zap.Error(err))
		} else {
			tlsCfg = cfg
		}
	}

	scheme := "http"
	if tlsCfg != nil {
		scheme = "https"
	}
	baseURL, err := baseURLWithScheme(scheme, fqdn, port)
	if err != nil {
		return err
	}

	p.mu.Lock()
	defer p.mu.Unlock()
	if old, ok := p.clients[nodeID]; ok {
		if old.baseURL == baseURL && old.nodeToken == token && old.tls == (tlsCfg != nil) {
			return nil
		}
		old.closeIdle()
	}
	if tlsCfg != nil {
		p.clients[nodeID] = NewClientTLS(baseURL, token, tlsCfg, p.log)
	} else {
		p.clients[nodeID] = NewClient(baseURL, token, p.log)
	}
	return nil
}

// Get returns the client for a node, or an error if the node is not registered.
func (p *ClientPool) Get(nodeID string) (*Client, error) {
	p.mu.RLock()
	c, ok := p.clients[nodeID]
	p.mu.RUnlock()
	if !ok {
		return nil, fmt.Errorf("no daemon client registered for node %s", nodeID)
	}
	return c, nil
}

// Remove deregisters a node's client.
func (p *ClientPool) Remove(nodeID string) {
	p.mu.Lock()
	delete(p.clients, nodeID)
	p.mu.Unlock()
}
