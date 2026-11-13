package middleware

import (
	"strings"
	"testing"
)

func TestRedactQuery(t *testing.T) {
	cases := map[string]string{
		"":                      "",
		"after=42":              "after=42",
		"ticket=abc123":         "ticket=REDACTED",
		"after=7&ticket=s3cr3t": "after=7&ticket=REDACTED",
		"access_token=x&page=2": "access_token=REDACTED&page=2",
	}
	for in, want := range cases {
		if got := redactQuery(in); got != want {
			t.Errorf("redactQuery(%q) = %q, want %q", in, got, want)
		}
	}
	if got := redactQuery("ticket=%zz"); strings.Contains(got, "zz") {
		t.Errorf("unparseable query leaked: %q", got)
	}
}
