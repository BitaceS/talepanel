package daemon

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
)

// ─────────────────────────────────────────────────────────────────────────────
// Off-site backups — the daemon runs rclone against a remote built from the
// RemoteSpec.  The spec carries secrets, so it only ever travels in request
// bodies, never in URLs.
// ─────────────────────────────────────────────────────────────────────────────

// RemoteSpec mirrors the daemon's rclone::RemoteSpec.
type RemoteSpec struct {
	// Backend is the rclone backend type (onedrive, drive, s3, sftp, ...).
	Backend string `json:"backend"`
	// Options are rclone options in clear text, secrets included.
	Options map[string]string `json:"options"`
	// Obscure lists options rclone expects obscured (passwords).
	Obscure []string `json:"obscure"`
	// BasePath is the folder on the remote; backups land in
	// <base_path>/<server_id>/<backup_id>.zip.
	BasePath string `json:"base_path"`
}

// RemoteResult is the daemon's answer to every rclone operation.
// ConfigUpdates holds options rclone changed while running (a refreshed OAuth
// token, the resolved OneDrive drive) that the API must persist.
type RemoteResult struct {
	Success       bool              `json:"success"`
	Error         string            `json:"error,omitempty"`
	RemotePath    string            `json:"remote_path,omitempty"`
	ConfigUpdates map[string]string `json:"config_updates,omitempty"`
}

// RemoteError is returned when the daemon reports a failed rclone run.  It
// still carries config updates that must be saved.
type RemoteError struct {
	Status        int
	Message       string
	ConfigUpdates map[string]string
}

func (e *RemoteError) Error() string { return e.Message }

type remoteBackupRequest struct {
	BackupID string      `json:"backup_id"`
	Remote   *RemoteSpec `json:"remote,omitempty"`
}

// postRemote sends body to path and decodes a RemoteResult.  A non-2xx answer
// becomes a *RemoteError carrying the daemon's error text.
func (c *Client) postRemote(ctx context.Context, path string, body any) (*RemoteResult, error) {
	data, err := json.Marshal(body)
	if err != nil {
		return nil, fmt.Errorf("daemon: marshal body: %w", err)
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, c.endpoint(path, nil), bytes.NewReader(data))
	if err != nil {
		return nil, fmt.Errorf("daemon: build request: %w", err)
	}
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Authorization", "Bearer "+c.nodeToken)

	resp, err := c.longClient().Do(req)
	if err != nil {
		return nil, fmt.Errorf("daemon: POST %s: %w", path, err)
	}
	defer resp.Body.Close()

	raw, _ := io.ReadAll(io.LimitReader(resp.Body, 1<<20))
	var out RemoteResult
	decodeErr := json.Unmarshal(raw, &out)
	if resp.StatusCode == http.StatusNotFound && decodeErr != nil {
		return nil, &RemoteError{Status: resp.StatusCode,
			Message: "this node's daemon does not support off-site backups yet — update the daemon on the node"}
	}
	if resp.StatusCode >= 400 || !out.Success {
		msg := out.Error
		if msg == "" {
			msg = fmt.Sprintf("daemon returned %d: %s", resp.StatusCode, bytes.TrimSpace(raw))
		}
		return nil, &RemoteError{Status: resp.StatusCode, Message: msg, ConfigUpdates: out.ConfigUpdates}
	}
	if decodeErr != nil {
		return nil, fmt.Errorf("daemon: decode %s response: %w", path, decodeErr)
	}
	return &out, nil
}

// UploadBackup uploads an existing local backup archive to the remote.
func (c *Client) UploadBackup(ctx context.Context, serverID, backupID string, remote RemoteSpec) (*RemoteResult, error) {
	return c.postRemote(ctx, "/servers/"+url.PathEscape(serverID)+"/backup/upload",
		remoteBackupRequest{BackupID: backupID, Remote: &remote})
}

// DeleteRemoteBackup deletes a backup archive from the remote (idempotent).
func (c *Client) DeleteRemoteBackup(ctx context.Context, serverID, backupID string, remote RemoteSpec) (*RemoteResult, error) {
	return c.postRemote(ctx, "/servers/"+url.PathEscape(serverID)+"/backup/remote-delete",
		remoteBackupRequest{BackupID: backupID, Remote: &remote})
}

// TestRemote checks that the daemon can write to and delete from the remote.
func (c *Client) TestRemote(ctx context.Context, remote RemoteSpec) (*RemoteResult, error) {
	return c.postRemote(ctx, "/rclone/test", struct {
		Remote RemoteSpec `json:"remote"`
	}{remote})
}

// RestoreBackupFrom restores a backup; when the local archive is gone the
// daemon first fetches it from remote.
func (c *Client) RestoreBackupFrom(ctx context.Context, serverID, backupID string, remote *RemoteSpec) error {
	_, err := c.postRemote(ctx, "/servers/"+url.PathEscape(serverID)+"/backup/restore",
		remoteBackupRequest{BackupID: backupID, Remote: remote})
	return err
}

// DownloadBackupFrom streams a backup archive; when the local archive is gone
// the daemon fetches it from remote first.
func (c *Client) DownloadBackupFrom(ctx context.Context, serverID, backupID string, remote *RemoteSpec) (io.ReadCloser, error) {
	data, err := json.Marshal(remoteBackupRequest{BackupID: backupID, Remote: remote})
	if err != nil {
		return nil, fmt.Errorf("daemon: marshal body: %w", err)
	}
	downloadURL := c.endpoint("/servers/"+url.PathEscape(serverID)+"/backup/download", nil)
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, downloadURL, bytes.NewReader(data))
	if err != nil {
		return nil, fmt.Errorf("daemon: build backup download request: %w", err)
	}
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Authorization", "Bearer "+c.nodeToken)

	resp, err := c.longClient().Do(req)
	if err != nil {
		return nil, fmt.Errorf("daemon: download backup: %w", err)
	}
	if resp.StatusCode >= 400 {
		b, _ := io.ReadAll(io.LimitReader(resp.Body, 64*1024))
		resp.Body.Close()
		return nil, fmt.Errorf("daemon: download backup failed (%d): %s", resp.StatusCode, b)
	}
	return resp.Body, nil
}
