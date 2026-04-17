// Package daemon provides an HTTP client for communicating with TaleDaemon
// nodes.  Each node runs an Axum HTTP server; the API calls it to execute
// power actions (start/stop/restart/kill) and to retrieve real-time metrics.
//
// Authentication: every request carries the node's registration token as a
// Bearer token.  The daemon validates this against its local config.
package daemon

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"mime/multipart"
	"net/http"
	"strings"
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
	CPULimit   float32 `json:"cpu_limit"`
	CrashLimit uint32  `json:"crash_limit"`
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
	baseURL    string // e.g. "http://192.168.1.10:8444"
	nodeToken  string
	httpClient *http.Client
	log        *zap.Logger
}

// NewClient constructs a daemon client for the given node.
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

// NetworkStats is returned by GET /network-stats
type NetworkStats struct {
	Interfaces      json.RawMessage `json:"interfaces"`
	Connections     json.RawMessage `json:"connections"`
	GamePortSummary json.RawMessage `json:"game_port_summary"`
}

// GetNetworkStats retrieves network traffic stats from the daemon.
func (c *Client) GetNetworkStats(ctx context.Context) (*NetworkStats, error) {
	var result NetworkStats
	if err := c.get(ctx, "/network-stats", &result); err != nil {
		return nil, err
	}
	return &result, nil
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

	url := fmt.Sprintf("%s/servers/%s/files/upload?path=%s", c.baseURL, serverID, dir)
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, url, &buf)
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

// DownloadFile streams a file from the daemon as raw bytes.
func (c *Client) DownloadFile(ctx context.Context, serverID, path string) (io.ReadCloser, string, error) {
	url := fmt.Sprintf("%s/servers/%s/files/download?path=%s", c.baseURL, serverID, path)
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, url, nil)
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

	// Extract filename from Content-Disposition header
	fileName := "download"
	if cd := resp.Header.Get("Content-Disposition"); cd != "" {
		if parts := strings.SplitN(cd, "filename=\"", 2); len(parts) == 2 {
			fileName = strings.TrimSuffix(parts[1], "\"")
		}
	}

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
type ClientPool struct {
	clients map[string]*Client
	log     *zap.Logger
}

// NewClientPool creates an empty pool.
func NewClientPool(log *zap.Logger) *ClientPool {
	return &ClientPool{
		clients: make(map[string]*Client),
		log:     log,
	}
}

// Register adds or replaces a daemon client for the given node.
func (p *ClientPool) Register(nodeID, fqdn string, port int, token string) {
	baseURL := fmt.Sprintf("http://%s:%d", fqdn, port)
	p.clients[nodeID] = NewClient(baseURL, token, p.log)
}

// Get returns the client for a node, or an error if the node is not registered.
func (p *ClientPool) Get(nodeID string) (*Client, error) {
	c, ok := p.clients[nodeID]
	if !ok {
		return nil, fmt.Errorf("no daemon client registered for node %s", nodeID)
	}
	return c, nil
}

// Remove deregisters a node's client.
func (p *ClientPool) Remove(nodeID string) {
	delete(p.clients, nodeID)
}
