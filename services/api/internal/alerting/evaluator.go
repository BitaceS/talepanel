package alerting

import (
	"context"
	"time"

	"go.uber.org/zap"
)

// AlertEvaluatorService is the subset of AlertService needed by the evaluator.
// Using an interface keeps the alerting package free of a circular import on services.
type AlertEvaluatorService interface {
	EvaluateNodeOffline(ctx context.Context) error
}

// AlertEvaluator runs periodic time-based alert condition checks.
// Event-driven conditions (server crash, high CPU) are handled inline
// in their respective handlers via AlertService.EvaluateAndFire.
type AlertEvaluator struct {
	alertSvc AlertEvaluatorService
	log      *zap.Logger
	interval time.Duration
}

func NewAlertEvaluator(svc AlertEvaluatorService, log *zap.Logger) *AlertEvaluator {
	return &AlertEvaluator{
		alertSvc: svc,
		log:      log,
		interval: 60 * time.Second,
	}
}

// Start blocks until ctx is cancelled. Call as a goroutine.
func (e *AlertEvaluator) Start(ctx context.Context) {
	ticker := time.NewTicker(e.interval)
	defer ticker.Stop()
	e.log.Info("alert evaluator started", zap.Duration("interval", e.interval))
	for {
		select {
		case <-ctx.Done():
			e.log.Info("alert evaluator stopped")
			return
		case <-ticker.C:
			if err := e.alertSvc.EvaluateNodeOffline(ctx); err != nil {
				e.log.Warn("alert evaluator: node offline check failed", zap.Error(err))
			}
		}
	}
}
