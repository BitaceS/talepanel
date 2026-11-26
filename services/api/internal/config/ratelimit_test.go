package config

import "testing"

func TestParseRateLimit(t *testing.T) {
	for _, tc := range []struct {
		raw     string
		want    int
		wantErr bool
	}{
		{"", DefaultRateLimitPerMin, false},
		{"  ", DefaultRateLimitPerMin, false},
		{"600", 600, false},
		{" 2000 ", 2000, false},
		{"0", 0, true},
		{"-5", 0, true},
		{"lots", 0, true},
		{"1.5", 0, true},
	} {
		got, err := parseRateLimit(tc.raw)
		if (err != nil) != tc.wantErr {
			t.Errorf("parseRateLimit(%q) error = %v, want error %v", tc.raw, err, tc.wantErr)
			continue
		}
		if got != tc.want {
			t.Errorf("parseRateLimit(%q) = %d, want %d", tc.raw, got, tc.want)
		}
	}
}
