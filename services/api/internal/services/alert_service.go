package services

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgxpool"
	"go.uber.org/zap"
	"github.com/Bitaces/talepanel/api/internal/alerting"
	"github.com/Bitaces/talepanel/api/internal/models"
)

var (
	ErrAlertRuleNotFound  = errors.New("alert rule not found")
	ErrAlertEventNotFound = errors.New("alert event not found")
)

type CreateAlertRuleRequest struct {
	ServerID  *uuid.UUID `json:"server_id"`
	Type      string     `json:"type" binding:"required"`
	Threshold *float64   `json:"threshold"`
	Channels  []string   `json:"channels"`
}

type AlertService struct {
	db       *pgxpool.Pool
	notifier alerting.Notifier
	log      *zap.Logger
}

func NewAlertService(db *pgxpool.Pool, notifier alerting.Notifier, log *zap.Logger) *AlertService {
	return &AlertService{db: db, notifier: notifier, log: log}
}

func (s *AlertService) ListRules(ctx context.Context, userID uuid.UUID) ([]*models.AlertRule, error) {
	rows, err := s.db.Query(ctx, `
		SELECT id, server_id, user_id, type, threshold, channels, enabled, created_at
		FROM alert_rules WHERE user_id = $1 ORDER BY created_at DESC
	`, userID)
	if err != nil {
		return nil, fmt.Errorf("querying alert rules: %w", err)
	}
	defer rows.Close()

	var rules []*models.AlertRule
	for rows.Next() {
		r := &models.AlertRule{}
		if err := rows.Scan(&r.ID, &r.ServerID, &r.UserID, &r.Type, &r.Threshold,
			&r.Channels, &r.Enabled, &r.CreatedAt); err != nil {
			return nil, fmt.Errorf("scanning alert rule row: %w", err)
		}
		rules = append(rules, r)
	}
	return rules, rows.Err()
}

func (s *AlertService) CreateRule(ctx context.Context, userID uuid.UUID, req CreateAlertRuleRequest) (*models.AlertRule, error) {
	channels, _ := json.Marshal(req.Channels)
	if req.Channels == nil {
		channels = []byte("[]")
	}

	r := &models.AlertRule{}
	err := s.db.QueryRow(ctx, `
		INSERT INTO alert_rules (server_id, user_id, type, threshold, channels)
		VALUES ($1, $2, $3, $4, $5)
		RETURNING id, server_id, user_id, type, threshold, channels, enabled, created_at
	`, req.ServerID, userID, req.Type, req.Threshold, channels).Scan(
		&r.ID, &r.ServerID, &r.UserID, &r.Type, &r.Threshold,
		&r.Channels, &r.Enabled, &r.CreatedAt,
	)
	if err != nil {
		return nil, fmt.Errorf("creating alert rule: %w", err)
	}
	return r, nil
}

func (s *AlertService) ToggleRule(ctx context.Context, ruleID, userID uuid.UUID, enabled bool) error {
	ct, err := s.db.Exec(ctx, `UPDATE alert_rules SET enabled = $1 WHERE id = $2 AND user_id = $3`, enabled, ruleID, userID)
	if err != nil {
		return fmt.Errorf("toggling rule: %w", err)
	}
	if ct.RowsAffected() == 0 {
		return ErrAlertRuleNotFound
	}
	return nil
}

func (s *AlertService) DeleteRule(ctx context.Context, ruleID, userID uuid.UUID) error {
	ct, err := s.db.Exec(ctx, `DELETE FROM alert_rules WHERE id = $1 AND user_id = $2`, ruleID, userID)
	if err != nil {
		return fmt.Errorf("deleting rule: %w", err)
	}
	if ct.RowsAffected() == 0 {
		return ErrAlertRuleNotFound
	}
	return nil
}

func (s *AlertService) ListEvents(ctx context.Context, userID uuid.UUID, limit int) ([]*models.AlertEvent, error) {
	if limit <= 0 || limit > 200 {
		limit = 50
	}
	rows, err := s.db.Query(ctx, `
		SELECT ae.id, ae.rule_id, ae.server_id, ae.node_id, ae.type, ae.severity,
		       ae.title, ae.body, ae.metadata, ae.resolved, ae.resolved_at, ae.created_at
		FROM alert_events ae
		LEFT JOIN alert_rules ar ON ar.id = ae.rule_id
		WHERE ar.user_id = $1 OR ae.rule_id IS NULL
		ORDER BY ae.created_at DESC LIMIT $2
	`, userID, limit)
	if err != nil {
		return nil, fmt.Errorf("querying alert events: %w", err)
	}
	defer rows.Close()

	var events []*models.AlertEvent
	for rows.Next() {
		e := &models.AlertEvent{}
		if err := rows.Scan(&e.ID, &e.RuleID, &e.ServerID, &e.NodeID, &e.Type,
			&e.Severity, &e.Title, &e.Body, &e.Metadata, &e.Resolved,
			&e.ResolvedAt, &e.CreatedAt); err != nil {
			return nil, fmt.Errorf("scanning alert event row: %w", err)
		}
		events = append(events, e)
	}
	return events, rows.Err()
}

func (s *AlertService) ResolveEvent(ctx context.Context, eventID uuid.UUID) error {
	ct, err := s.db.Exec(ctx, `UPDATE alert_events SET resolved = true, resolved_at = NOW() WHERE id = $1`, eventID)
	if err != nil {
		return fmt.Errorf("resolving event: %w", err)
	}
	if ct.RowsAffected() == 0 {
		return ErrAlertEventNotFound
	}
	return nil
}

// EvaluateAndFire checks active alert rules of ruleType, deduplicates within 15 minutes,
// inserts an alert_event row, and dispatches notifications.
// value is the metric value for threshold-based rules (0 for binary rules like server_crashed).
func (s *AlertService) EvaluateAndFire(ctx context.Context, ruleType string, subjectID uuid.UUID, value float64) error {
	// 1. Query active rules for this rule_type.
	// alert_rules uses "type" (not rule_type) and "enabled" (not is_active).
	rows, err := s.db.Query(ctx, `
		SELECT id, user_id, server_id, threshold, channels
		FROM alert_rules
		WHERE type = $1 AND enabled = true
		  AND (server_id IS NULL OR server_id = $2)
	`, ruleType, subjectID)
	if err != nil {
		return fmt.Errorf("querying alert rules for type %s: %w", ruleType, err)
	}
	defer rows.Close()

	type ruleRow struct {
		id        uuid.UUID
		userID    uuid.UUID
		serverID  *uuid.UUID
		threshold *float64
		channels  json.RawMessage
	}

	var rules []ruleRow
	for rows.Next() {
		var r ruleRow
		if err := rows.Scan(&r.id, &r.userID, &r.serverID, &r.threshold, &r.channels); err != nil {
			return fmt.Errorf("scanning alert rule: %w", err)
		}
		rules = append(rules, r)
	}
	if err := rows.Err(); err != nil {
		return fmt.Errorf("iterating alert rules: %w", err)
	}

	for _, rule := range rules {
		// 2. For threshold rules: skip if value is below threshold.
		if rule.threshold != nil && value < *rule.threshold {
			continue
		}

		// 3. Dedup: skip if unresolved event for same (rule_id, subject_id-matched server/node) in last 15 min.
		// subject_id maps to either server_id or node_id depending on rule type.
		var dedupCount int
		dedupErr := s.db.QueryRow(ctx, `
			SELECT COUNT(*) FROM alert_events
			WHERE rule_id = $1
			  AND resolved = false
			  AND created_at > NOW() - INTERVAL '15 minutes'
		`, rule.id).Scan(&dedupCount)
		if dedupErr != nil {
			if s.log != nil {
				s.log.Warn("alert dedup check failed", zap.String("rule_id", rule.id.String()), zap.Error(dedupErr))
			}
			continue
		}
		if dedupCount > 0 {
			continue
		}

		// 4. Determine subject assignment — server_id or node_id based on rule type.
		var eventServerID *uuid.UUID
		var eventNodeID *uuid.UUID
		switch ruleType {
		case "node_offline":
			eventNodeID = &subjectID
		default:
			eventServerID = &subjectID
		}

		// Build title and severity.
		title := fmt.Sprintf("Alert: %s", ruleType)
		severity := "warning"
		switch ruleType {
		case "node_offline", "server_crashed":
			severity = "critical"
		case "high_cpu", "high_ram", "high_disk":
			severity = "warning"
		}

		// 5. Insert alert_event row.
		// alert_events columns: id, rule_id, server_id, node_id, type, severity, title, body, metadata, resolved, resolved_at, created_at
		var eventID uuid.UUID
		insertErr := s.db.QueryRow(ctx, `
			INSERT INTO alert_events (rule_id, server_id, node_id, type, severity, title)
			VALUES ($1, $2, $3, $4, $5, $6)
			RETURNING id
		`, rule.id, eventServerID, eventNodeID, ruleType, severity, title).Scan(&eventID)
		if insertErr != nil {
			if s.log != nil {
				s.log.Error("failed to insert alert event", zap.String("rule_id", rule.id.String()), zap.Error(insertErr))
			}
			continue
		}

		if s.notifier == nil {
			continue
		}

		// 6. Fetch user email and notification prefs.
		var userEmail string
		emailErr := s.db.QueryRow(ctx, `SELECT email FROM users WHERE id = $1`, rule.userID).Scan(&userEmail)
		if emailErr != nil {
			if s.log != nil {
				s.log.Warn("alert: failed to fetch user email", zap.String("user_id", rule.userID.String()), zap.Error(emailErr))
			}
			// proceed without email
		}

		// Determine active channels from rule.channels JSON array.
		var channels []string
		if len(rule.channels) > 0 {
			_ = json.Unmarshal(rule.channels, &channels)
		}

		// Dispatch notification fire-and-forget.
		payload := alerting.NotificationPayload{
			RuleType:  ruleType,
			Severity:  severity,
			Message:   title,
			FiredAt:   time.Now(),
			SubjectID: subjectID.String(),
		}
		// webhookURL is not stored in the current schema — pass empty string.
		s.notifier.Dispatch(ctx, payload, channels, userEmail, "")
	}

	return nil
}

// EvaluateNodeOffline checks for nodes that were online but have not sent a heartbeat
// in the last 5 minutes and fires node_offline alerts for each one.
func (s *AlertService) EvaluateNodeOffline(ctx context.Context) error {
	rows, err := s.db.Query(ctx, `
		SELECT id FROM nodes
		WHERE status = 'online'
		  AND last_heartbeat < NOW() - INTERVAL '5 minutes'
	`)
	if err != nil {
		return fmt.Errorf("querying stale nodes: %w", err)
	}
	defer rows.Close()

	for rows.Next() {
		var nodeID uuid.UUID
		if err := rows.Scan(&nodeID); err != nil {
			return fmt.Errorf("scanning stale node id: %w", err)
		}
		if err := s.EvaluateAndFire(ctx, "node_offline", nodeID, 0); err != nil {
			if s.log != nil {
				s.log.Error("EvaluateAndFire failed for node_offline",
					zap.String("node_id", nodeID.String()), zap.Error(err))
			}
		}
	}
	return rows.Err()
}
