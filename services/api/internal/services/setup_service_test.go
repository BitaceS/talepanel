package services

import (
	"crypto/sha256"
	"encoding/hex"
	"testing"

	"github.com/BitaceS/talepanel/api/internal/config"
)

func setupCodeHash(code string) string {
	sum := sha256.Sum256([]byte(code))
	return hex.EncodeToString(sum[:])
}

func TestNormalizeSetupCode(t *testing.T) {
	cases := []struct {
		in   string
		want string
		ok   bool
	}{
		{"ABCD-EFGH-JKLM", "ABCD-EFGH-JKLM", true},
		{" abcd-efgh-jklm \n", "ABCD-EFGH-JKLM", true},
		{"AB CD-EF GH-JK LM", "ABCD-EFGH-JKLM", true},
		{"ABCDEFGHJKLM", "ABCDEFGHJKLM", false}, // dashes are part of the format
		{"ABCD_EFGH_JKLM", "ABCD_EFGH_JKLM", false},
		{"ABC0-EFGH-JKLM", "ABC0-EFGH-JKLM", false}, // 0, O, 1 and I are never issued
		{"ABCO-EFGH-JKLM", "ABCO-EFGH-JKLM", false},
		{"ABC1-EFGH-JKLM", "ABC1-EFGH-JKLM", false},
		{"ABCI-EFGH-JKLM", "ABCI-EFGH-JKLM", false},
		{"ABCD-EFGH-JKLM-", "ABCD-EFGH-JKLM-", false},
		{"", "", false},
	}
	for _, tc := range cases {
		got, ok := NormalizeSetupCode(tc.in)
		if got != tc.want || ok != tc.ok {
			t.Errorf("NormalizeSetupCode(%q) = %q, %v; want %q, %v", tc.in, got, ok, tc.want, tc.ok)
		}
	}
}

func TestCheckSetupCode(t *testing.T) {
	svc := NewAuthService(nil, nil, &config.Config{SetupCodeHash: setupCodeHash("ABCD-EFGH-JKLM")})
	if !svc.SetupCodeEnabled() {
		t.Fatal("setup should be enabled when a hash is configured")
	}
	for _, good := range []string{"ABCD-EFGH-JKLM", "abcd-efgh-jklm", " ABCD - EFGH - JKLM "} {
		if !svc.checkSetupCode(good) {
			t.Errorf("checkSetupCode(%q) = false, want true", good)
		}
	}
	for _, bad := range []string{"ABCD-EFGH-JKLN", "ABCDEFGHJKLM", "", "ABCD-EFGH-JKLM-X"} {
		if svc.checkSetupCode(bad) {
			t.Errorf("checkSetupCode(%q) = true, want false", bad)
		}
	}

	if NewAuthService(nil, nil, &config.Config{}).SetupCodeEnabled() {
		t.Fatal("setup must be disabled without a hash")
	}
}
