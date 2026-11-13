package alerting

import (
	"context"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"go.uber.org/zap"
)

// Dispatch is called with the request context, which is cancelled when the
// handler returns. Webhooks must still be delivered.
func TestDispatchSurvivesCancelledContext(t *testing.T) {
	got := make(chan struct{}, 2)
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		got <- struct{}{}
	}))
	defer srv.Close()

	log := zap.NewNop()
	m := NewMultiNotifier(nil,
		&WebhookNotifier{client: srv.Client(), log: log},
		&DiscordNotifier{client: srv.Client(), log: log},
		log)

	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	m.Dispatch(ctx, NotificationPayload{RuleType: "cpu_high", FiredAt: time.Now()},
		[]string{"webhook", "discord"}, "", srv.URL)

	for i := 0; i < 2; i++ {
		select {
		case <-got:
		case <-time.After(5 * time.Second):
			t.Fatalf("only %d of 2 notifications delivered", i)
		}
	}
}
