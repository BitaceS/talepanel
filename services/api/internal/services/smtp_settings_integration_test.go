package services

import (
	"bytes"
	"context"
	"strings"
	"testing"

	"github.com/BitaceS/talepanel/api/internal/alerting"
)

func TestSMTPSettingsRoundTripEncrypted(t *testing.T) {
	pool := openTestDB(t)
	ctx := context.Background()
	svc := NewAppSettingsService(pool, bytes.Repeat([]byte{7}, 32))
	admin := seedTestUser(t, pool, "admin")
	t.Cleanup(func() { _ = svc.SetSMTP(context.Background(), nil, nil) })

	want := alerting.SMTPConfig{
		Host: "smtp.example.com", Port: 465, Username: "mailer",
		Password: "s3cret-pass", From: "TalePanel <noreply@example.com>",
	}.Normalize()
	if err := svc.SetSMTP(ctx, &want, &admin); err != nil {
		t.Fatalf("SetSMTP: %v", err)
	}

	var stored string
	if err := pool.QueryRow(ctx, `SELECT value FROM app_settings WHERE key = 'smtp_config'`).Scan(&stored); err != nil {
		t.Fatalf("reading row: %v", err)
	}
	if strings.Contains(stored, "s3cret-pass") || strings.Contains(stored, "smtp.example.com") {
		t.Fatal("smtp settings stored in plain text")
	}

	got, err := svc.GetSMTP(ctx)
	if err != nil || got == nil {
		t.Fatalf("GetSMTP: %v %v", got, err)
	}
	if *got != want {
		t.Errorf("round trip mismatch: got %+v want %+v", *got, want)
	}

	if err := svc.SetSMTP(ctx, nil, &admin); err != nil {
		t.Fatalf("clearing: %v", err)
	}
	if got, err := svc.GetSMTP(ctx); err != nil || got != nil {
		t.Errorf("after clear: got %+v err %v, want nil", got, err)
	}
}
