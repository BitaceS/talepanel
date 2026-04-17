package services

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgxpool"
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
	db *pgxpool.Pool
}

func NewAlertService(db *pgxpool.Pool) *AlertService {
	return &AlertService{db: db}
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
