package services

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"
	"github.com/jackc/pgx/v5/pgxpool"
	"go.uber.org/zap"

	tpcrypto "github.com/BitaceS/talepanel/api/internal/crypto"
	"github.com/BitaceS/talepanel/api/internal/daemon"
)

var (
	ErrDestinationNotFound = errors.New("backup destination not found")
	ErrDestinationInUse    = errors.New("backup destination still holds backups that exist only off-site; delete those backups first")
	ErrNoNodeForTest       = errors.New("no online node is connected — a node is needed to run the connection test")
)

// destinationTestTimeout bounds a "Test connection" round trip (the daemon
// applies its own, shorter rclone timeout).
const destinationTestTimeout = 3 * time.Minute

// BackupDestination is the client view of a destination.  Secrets are never
// included; SecretsSet says which ones are stored.
type BackupDestination struct {
	ID             uuid.UUID         `json:"id"`
	Name           string            `json:"name"`
	Provider       string            `json:"provider"`
	BasePath       string            `json:"base_path"`
	Options        map[string]string `json:"options"`
	SecretsSet     map[string]bool   `json:"secrets_set"`
	KeepLocal      bool              `json:"keep_local"`
	RetentionCount *int              `json:"retention_count"`
	CreatedAt      time.Time         `json:"created_at"`
	UpdatedAt      time.Time         `json:"updated_at"`
}

// BackupDestinationSummary is what non-admin users see in the destination
// picker.
type BackupDestinationSummary struct {
	ID        uuid.UUID `json:"id"`
	Name      string    `json:"name"`
	Provider  string    `json:"provider"`
	KeepLocal bool      `json:"keep_local"`
}

// SaveDestinationRequest is the body of create/update.  Secrets left empty on
// update keep their stored value.
type SaveDestinationRequest struct {
	Name           string            `json:"name"`
	Provider       string            `json:"provider"`
	BasePath       string            `json:"base_path"`
	Options        map[string]string `json:"options"`
	Secrets        map[string]string `json:"secrets"`
	KeepLocal      *bool             `json:"keep_local"`
	RetentionCount *int              `json:"retention_count"`
}

// destinationRow is a destination with decrypted secrets, internal only.
type destinationRow struct {
	BackupDestination
	secrets map[string]string
}

type BackupDestinationService struct {
	db      *pgxpool.Pool
	encKey  []byte
	daemons *daemon.ClientPool
	log     *zap.Logger
}

func NewBackupDestinationService(db *pgxpool.Pool, encKey []byte, daemons *daemon.ClientPool, log *zap.Logger) *BackupDestinationService {
	if log == nil {
		log = zap.NewNop()
	}
	return &BackupDestinationService{db: db, encKey: encKey, daemons: daemons, log: log}
}

const destinationColumns = `id, name, provider, base_path, options, secrets, keep_local, retention_count, created_at, updated_at`

func (s *BackupDestinationService) scan(row pgx.Row) (*destinationRow, error) {
	d := &destinationRow{}
	var optsRaw []byte
	var encSecrets string
	if err := row.Scan(&d.ID, &d.Name, &d.Provider, &d.BasePath, &optsRaw, &encSecrets,
		&d.KeepLocal, &d.RetentionCount, &d.CreatedAt, &d.UpdatedAt); err != nil {
		return nil, err
	}
	d.Options = map[string]string{}
	if len(optsRaw) > 0 {
		if err := json.Unmarshal(optsRaw, &d.Options); err != nil {
			return nil, fmt.Errorf("decoding destination options: %w", err)
		}
	}
	d.secrets = map[string]string{}
	if encSecrets != "" {
		plain, err := tpcrypto.Decrypt(s.encKey, encSecrets)
		if err != nil {
			return nil, fmt.Errorf("decrypting destination secrets: %w", err)
		}
		if err := json.Unmarshal(plain, &d.secrets); err != nil {
			return nil, fmt.Errorf("decoding destination secrets: %w", err)
		}
	}
	d.SecretsSet = map[string]bool{}
	for k, v := range d.secrets {
		if v != "" {
			d.SecretsSet[k] = true
		}
	}
	return d, nil
}

func (s *BackupDestinationService) get(ctx context.Context, q pgx.Tx, id uuid.UUID, forUpdate bool) (*destinationRow, error) {
	sql := `SELECT ` + destinationColumns + ` FROM backup_destinations WHERE id = $1`
	var row pgx.Row
	if q != nil {
		if forUpdate {
			sql += ` FOR UPDATE`
		}
		row = q.QueryRow(ctx, sql, id)
	} else {
		row = s.db.QueryRow(ctx, sql, id)
	}
	d, err := s.scan(row)
	if errors.Is(err, pgx.ErrNoRows) {
		return nil, ErrDestinationNotFound
	}
	return d, err
}

// List returns all destinations (admin view, secrets masked).
func (s *BackupDestinationService) List(ctx context.Context) ([]*BackupDestination, error) {
	rows, err := s.db.Query(ctx, `SELECT `+destinationColumns+` FROM backup_destinations ORDER BY name`)
	if err != nil {
		return nil, fmt.Errorf("querying backup destinations: %w", err)
	}
	defer rows.Close()
	out := []*BackupDestination{}
	for rows.Next() {
		d, err := s.scan(rows)
		if err != nil {
			return nil, err
		}
		dest := d.BackupDestination
		out = append(out, &dest)
	}
	return out, rows.Err()
}

// ListSummaries returns id/name/provider of every destination for the picker.
func (s *BackupDestinationService) ListSummaries(ctx context.Context) ([]*BackupDestinationSummary, error) {
	rows, err := s.db.Query(ctx, `SELECT id, name, provider, keep_local FROM backup_destinations ORDER BY name`)
	if err != nil {
		return nil, fmt.Errorf("querying backup destinations: %w", err)
	}
	defer rows.Close()
	out := []*BackupDestinationSummary{}
	for rows.Next() {
		d := &BackupDestinationSummary{}
		if err := rows.Scan(&d.ID, &d.Name, &d.Provider, &d.KeepLocal); err != nil {
			return nil, err
		}
		out = append(out, d)
	}
	return out, rows.Err()
}

// Get returns one destination (admin view, secrets masked).
func (s *BackupDestinationService) Get(ctx context.Context, id uuid.UUID) (*BackupDestination, error) {
	d, err := s.get(ctx, nil, id, false)
	if err != nil {
		return nil, err
	}
	return &d.BackupDestination, nil
}

// Exists reports whether a destination id is valid (used when creating
// backups and schedules).
func (s *BackupDestinationService) Exists(ctx context.Context, id uuid.UUID) (bool, error) {
	var ok bool
	err := s.db.QueryRow(ctx, `SELECT EXISTS (SELECT 1 FROM backup_destinations WHERE id = $1)`, id).Scan(&ok)
	return ok, err
}

func (s *BackupDestinationService) encryptSecrets(sec map[string]string) (string, error) {
	if len(sec) == 0 {
		return "", nil
	}
	raw, err := json.Marshal(sec)
	if err != nil {
		return "", err
	}
	return tpcrypto.Encrypt(s.encKey, raw)
}

func validateDestinationMeta(req *SaveDestinationRequest) (*DestinationProvider, error) {
	req.Name = strings.TrimSpace(req.Name)
	if req.Name == "" || len(req.Name) > 64 {
		return nil, invalidf("name is required (at most 64 characters)")
	}
	if strings.ContainsAny(req.Name, "\r\n\x00") {
		return nil, invalidf("name must be a single line")
	}
	p, ok := findProvider(req.Provider)
	if !ok {
		return nil, invalidf("unknown provider %q", req.Provider)
	}
	req.BasePath = strings.TrimSpace(req.BasePath)
	if err := ValidateRemoteBasePath(req.BasePath); err != nil {
		return nil, err
	}
	if req.RetentionCount != nil && *req.RetentionCount <= 0 {
		req.RetentionCount = nil
	}
	if req.RetentionCount != nil && *req.RetentionCount > 10000 {
		return nil, invalidf("retention must be at most 10000 backups")
	}
	return p, nil
}

func mapUniqueViolation(err error) error {
	var pgErr *pgconn.PgError
	if errors.As(err, &pgErr) && pgErr.Code == "23505" {
		return invalidf("a destination with this name already exists")
	}
	return err
}

// Create validates and stores a new destination.
func (s *BackupDestinationService) Create(ctx context.Context, req SaveDestinationRequest, createdBy *uuid.UUID) (*BackupDestination, error) {
	p, err := validateDestinationMeta(&req)
	if err != nil {
		return nil, err
	}
	plain, sec, err := splitDestinationOptions(p, req.Options, req.Secrets, nil)
	if err != nil {
		return nil, err
	}
	optsRaw, _ := json.Marshal(plain)
	encSec, err := s.encryptSecrets(sec)
	if err != nil {
		return nil, fmt.Errorf("encrypting destination secrets: %w", err)
	}
	keepLocal := true
	if req.KeepLocal != nil {
		keepLocal = *req.KeepLocal
	}
	d, err := s.scan(s.db.QueryRow(ctx, `
		INSERT INTO backup_destinations (name, provider, base_path, options, secrets, keep_local, retention_count, created_by)
		VALUES ($1, $2, $3, $4, $5, $6, $7, $8)
		RETURNING `+destinationColumns,
		req.Name, p.ID, req.BasePath, optsRaw, encSec, keepLocal, req.RetentionCount, createdBy))
	if err != nil {
		return nil, mapUniqueViolation(err)
	}
	return &d.BackupDestination, nil
}

// Update replaces a destination's settings.  Secrets sent empty are kept.
// Changing the provider drops all stored secrets.
func (s *BackupDestinationService) Update(ctx context.Context, id uuid.UUID, req SaveDestinationRequest) (*BackupDestination, error) {
	p, err := validateDestinationMeta(&req)
	if err != nil {
		return nil, err
	}
	tx, err := s.db.Begin(ctx)
	if err != nil {
		return nil, err
	}
	defer func() { _ = tx.Rollback(ctx) }()

	cur, err := s.get(ctx, tx, id, true)
	if err != nil {
		return nil, err
	}
	existing := cur.secrets
	if cur.Provider != p.ID {
		existing = nil
	}
	plain, sec, err := splitDestinationOptions(p, req.Options, req.Secrets, existing)
	if err != nil {
		return nil, err
	}
	optsRaw, _ := json.Marshal(plain)
	encSec, err := s.encryptSecrets(sec)
	if err != nil {
		return nil, fmt.Errorf("encrypting destination secrets: %w", err)
	}
	keepLocal := cur.KeepLocal
	if req.KeepLocal != nil {
		keepLocal = *req.KeepLocal
	}
	d, err := s.scan(tx.QueryRow(ctx, `
		UPDATE backup_destinations
		   SET name = $2, provider = $3, base_path = $4, options = $5, secrets = $6,
		       keep_local = $7, retention_count = $8, updated_at = NOW()
		 WHERE id = $1
		RETURNING `+destinationColumns,
		id, req.Name, p.ID, req.BasePath, optsRaw, encSec, keepLocal, req.RetentionCount))
	if err != nil {
		return nil, mapUniqueViolation(err)
	}
	if err := tx.Commit(ctx); err != nil {
		return nil, err
	}
	return &d.BackupDestination, nil
}

// Delete removes a destination.  Refused while backups exist only on it:
// they would become unreachable from the panel.
func (s *BackupDestinationService) Delete(ctx context.Context, id uuid.UUID) error {
	var remoteOnly int
	if err := s.db.QueryRow(ctx, `
		SELECT COUNT(*) FROM backups
		 WHERE destination_id = $1 AND local_present = false AND upload_status = 'done'
	`, id).Scan(&remoteOnly); err != nil {
		return fmt.Errorf("checking destination usage: %w", err)
	}
	if remoteOnly > 0 {
		return ErrDestinationInUse
	}
	ct, err := s.db.Exec(ctx, `DELETE FROM backup_destinations WHERE id = $1`, id)
	if err != nil {
		return fmt.Errorf("deleting destination: %w", err)
	}
	if ct.RowsAffected() == 0 {
		return ErrDestinationNotFound
	}
	return nil
}

// RemoteSpec builds the payload the daemon turns into an rclone remote.
func (s *BackupDestinationService) RemoteSpec(ctx context.Context, id uuid.UUID) (*daemon.RemoteSpec, *BackupDestination, error) {
	d, err := s.get(ctx, nil, id, false)
	if err != nil {
		return nil, nil, err
	}
	p, ok := findProvider(d.Provider)
	if !ok {
		return nil, nil, invalidf("unknown provider %q", d.Provider)
	}
	opts, obscure := p.rcloneOptions(d.Options, d.secrets)
	return &daemon.RemoteSpec{
		Backend:  p.Backend,
		Options:  opts,
		Obscure:  obscure,
		BasePath: d.BasePath,
	}, &d.BackupDestination, nil
}

// ApplyConfigUpdates persists what rclone changed during a run on the daemon:
// a refreshed OAuth token, and the OneDrive drive id/type it resolved.  Only
// those keys are accepted, and only for providers that have such a field.
func (s *BackupDestinationService) ApplyConfigUpdates(ctx context.Context, id uuid.UUID, updates map[string]string) {
	if len(updates) == 0 {
		return
	}
	if err := s.applyConfigUpdates(ctx, id, updates); err != nil {
		s.log.Warn("could not save rclone config update for destination",
			zap.String("destination_id", id.String()), zap.Error(err))
	}
}

func (s *BackupDestinationService) applyConfigUpdates(ctx context.Context, id uuid.UUID, updates map[string]string) error {
	tx, err := s.db.Begin(ctx)
	if err != nil {
		return err
	}
	defer func() { _ = tx.Rollback(ctx) }()
	d, err := s.get(ctx, tx, id, true)
	if err != nil {
		return err
	}
	p, ok := findProvider(d.Provider)
	if !ok {
		return nil
	}
	changed := false
	for k, v := range updates {
		f, ok := p.field(k)
		if !writableConfigKeys[k] || !ok || v == "" || len(v) > maxOptionValueLen || strings.ContainsAny(v, "\r\n\x00") {
			continue
		}
		if f.token {
			nv, err := normalizeOAuthToken(v)
			if err != nil {
				continue
			}
			v = nv
		}
		if f.Secret {
			if d.secrets[k] != v {
				d.secrets[k] = v
				changed = true
			}
		} else if d.Options[k] != v {
			d.Options[k] = v
			changed = true
		}
	}
	if !changed {
		return nil
	}
	optsRaw, _ := json.Marshal(d.Options)
	encSec, err := s.encryptSecrets(d.secrets)
	if err != nil {
		return err
	}
	if _, err := tx.Exec(ctx, `UPDATE backup_destinations SET options = $2, secrets = $3, updated_at = NOW() WHERE id = $1`,
		id, optsRaw, encSec); err != nil {
		return err
	}
	return tx.Commit(ctx)
}

// saveUpdatesFromErr persists config updates carried by a failed daemon call.
func (s *BackupDestinationService) saveUpdatesFromErr(ctx context.Context, id uuid.UUID, err error) {
	var re *daemon.RemoteError
	if errors.As(err, &re) {
		s.ApplyConfigUpdates(ctx, id, re.ConfigUpdates)
	}
}

// pickTestClient returns a connected daemon client of any online node.
func (s *BackupDestinationService) pickTestClient(ctx context.Context, nodeID *uuid.UUID) (*daemon.Client, error) {
	if nodeID != nil {
		c, err := s.daemons.Get(nodeID.String())
		if err != nil {
			return nil, ErrNoNodeForTest
		}
		return c, nil
	}
	rows, err := s.db.Query(ctx, `SELECT id FROM nodes WHERE status = 'online' ORDER BY last_heartbeat DESC NULLS LAST`)
	if err != nil {
		return nil, fmt.Errorf("listing nodes: %w", err)
	}
	defer rows.Close()
	for rows.Next() {
		var id uuid.UUID
		if rows.Scan(&id) != nil {
			continue
		}
		if c, err := s.daemons.Get(id.String()); err == nil {
			return c, nil
		}
	}
	return nil, ErrNoNodeForTest
}

// Test asks a node to write and delete a small probe file on the remote.
// The returned error text is rclone's own message.
func (s *BackupDestinationService) Test(ctx context.Context, id uuid.UUID, nodeID *uuid.UUID) error {
	spec, _, err := s.RemoteSpec(ctx, id)
	if err != nil {
		return err
	}
	client, err := s.pickTestClient(ctx, nodeID)
	if err != nil {
		return err
	}
	tctx, cancel := context.WithTimeout(ctx, destinationTestTimeout)
	defer cancel()
	res, err := client.TestRemote(tctx, *spec)
	if err != nil {
		s.saveUpdatesFromErr(ctx, id, err)
		return err
	}
	s.ApplyConfigUpdates(ctx, id, res.ConfigUpdates)
	return nil
}
