package version

import "testing"

func TestNewer(t *testing.T) {
	cases := []struct {
		a, b string
		want bool
	}{
		{"v0.13.0", "v0.12.0-beta", true},
		{"v0.13.0", "v0.13.0-beta", true},
		{"v0.13.0-beta", "v0.13.0", false},
		{"v0.13.0", "v0.13.0", false},
		{"v0.12.0-beta", "v0.13.0", false},
		{"v1.0.0", "v0.99.9", true},
		{"v0.13.1", "v0.13.0", true},
		{"v0.13.0", "dev", false},
		{"garbage", "v0.1.0", false},
	}
	for _, c := range cases {
		if got := Newer(c.a, c.b); got != c.want {
			t.Errorf("Newer(%q, %q) = %v, want %v", c.a, c.b, got, c.want)
		}
	}
}
