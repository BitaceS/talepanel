package services

import (
	"encoding/json"
	"strings"
	"testing"
	"time"
)

// The web reads joined_at (apps/web/types/index.ts PlayerSession).
func TestPlayerSessionJSONFieldNames(t *testing.T) {
	b, err := json.Marshal(PlayerSession{JoinedAt: time.Unix(0, 0).UTC()})
	if err != nil {
		t.Fatal(err)
	}
	for _, k := range []string{`"joined_at"`, `"left_at"`, `"duration_s"`} {
		if !strings.Contains(string(b), k) {
			t.Errorf("%s missing from %s", k, b)
		}
	}
}
