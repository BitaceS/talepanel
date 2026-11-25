// Package version holds the TalePanel release this code belongs to.
package version

import (
	"strconv"
	"strings"
)

// Version is the release tag of this code. Bump it in the release commit
// (with the CHANGELOG heading); APP_VERSION or
// -ldflags "-X github.com/BitaceS/talepanel/api/internal/version.Version=…"
// override it. Installs build from a git checkout, so this constant is how a
// panel knows its own version.
var Version = "v0.17.0"

// Newer reports whether release a is newer than release b. Both are tags
// like "v0.13.0" or "v0.12.0-beta"; a release without a suffix is newer than
// the same version with one. Anything that does not parse is never newer.
func Newer(a, b string) bool {
	av, apre, aok := parse(a)
	bv, bpre, bok := parse(b)
	if !aok || !bok {
		return false
	}
	for i := range av {
		if av[i] != bv[i] {
			return av[i] > bv[i]
		}
	}
	switch {
	case apre == bpre:
		return false
	case apre == "":
		return true // 0.13.0 > 0.13.0-beta
	case bpre == "":
		return false
	default:
		return apre > bpre
	}
}

func parse(tag string) ([3]int, string, bool) {
	var v [3]int
	s := strings.TrimPrefix(strings.TrimSpace(tag), "v")
	core, pre, _ := strings.Cut(s, "-")
	parts := strings.Split(core, ".")
	if len(parts) != 3 {
		return v, "", false
	}
	for i, p := range parts {
		n, err := strconv.Atoi(p)
		if err != nil || n < 0 {
			return v, "", false
		}
		v[i] = n
	}
	return v, pre, true
}
