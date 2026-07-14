package services

import "testing"

// The API must never enqueue a world name that could make the daemon delete
// outside universe/worlds. The daemon re-validates, but this is the first gate.
func TestIsSafeWorldName(t *testing.T) {
	safe := []string{"overworld", "my-world", "world_2", "Welt 1", "world.backup"}
	for _, name := range safe {
		if !isSafeWorldName(name) {
			t.Errorf("isSafeWorldName(%q) = false, want true", name)
		}
	}

	unsafe := []string{
		"",
		".",
		"..",
		"../etc",
		"../../srv/taledaemon",
		"worlds/../../..",
		"a/b",
		`a\b`,
		"/etc/passwd",
		`C:\Windows`,
		"world\x00evil",
	}
	for _, name := range unsafe {
		if isSafeWorldName(name) {
			t.Errorf("isSafeWorldName(%q) = true, want false", name)
		}
	}
}

func TestNormalizeModDir(t *testing.T) {
	if got := normalizeModDir("plugins"); got != "plugins" {
		t.Errorf(`normalizeModDir("plugins") = %q, want "plugins"`, got)
	}
	if got := normalizeModDir("mods"); got != "mods" {
		t.Errorf(`normalizeModDir("mods") = %q, want "mods"`, got)
	}
	// Anything unexpected falls back to the historic default rather than being
	// forwarded to a daemon that renames files.
	for _, dir := range []string{"", "..", "../mods", "universe", "/etc"} {
		if got := normalizeModDir(dir); got != "mods" {
			t.Errorf("normalizeModDir(%q) = %q, want \"mods\"", dir, got)
		}
	}
}
