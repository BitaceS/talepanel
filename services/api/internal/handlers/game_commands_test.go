package handlers

import "testing"

func TestResolveCommandTemplate(t *testing.T) {
	cases := []struct {
		name     string
		template string
		params   map[string]string
		world    string
		want     string
	}{
		{"positional params", "teleport {player} {x} {y} {z}",
			map[string]string{"player": "Eve", "x": "1", "y": "2", "z": "3"}, "", "teleport Eve 1 2 3"},
		{"optional flag with value", "give {player} {item} --quantity {count}",
			map[string]string{"player": "Eve", "item": "Rock", "count": "5"}, "", "give Eve Rock --quantity 5"},
		{"optional flag omitted entirely when its value is missing", "give {player} {item} --quantity {count}",
			map[string]string{"player": "Eve", "item": "Rock"}, "", "give Eve Rock"},
		{"empty value counts as missing", "give {player} {item} --quantity {count}",
			map[string]string{"player": "Eve", "item": "Rock", "count": ""}, "", "give Eve Rock"},
		{"world is injected", "time set day --world {world}", nil, "default", "time set day --world default"},
		{"unknown world drops the flag instead of leaving it dangling", "time set day --world {world}", nil, "", "time set day"},
		{"bare flags are kept", "world save --confirm --all", nil, "", "world save --confirm --all"},
		{"negative coordinates are not flags", "teleport {player} {x} {y} {z}",
			map[string]string{"player": "Eve", "x": "-10", "y": "64", "z": "-3"}, "", "teleport Eve -10 64 -3"},
		{"multi-word values survive", "say {message}", map[string]string{"message": "hello there"}, "", "say hello there"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			got, err := resolveCommandTemplate(tc.template, tc.params, tc.world)
			if err != nil {
				t.Fatalf("unexpected error: %v", err)
			}
			if got != tc.want {
				t.Fatalf("got %q, want %q", got, tc.want)
			}
		})
	}
}

// A newline in a param would make the daemon write a second console line,
// running an arbitrary command past the per-command min_role gate.
func TestResolveCommandTemplate_RejectsLineBreaksInParams(t *testing.T) {
	for _, v := range []string{"Eve\nop add Mallory", "Eve\rstop", "Eve\x00"} {
		if _, err := resolveCommandTemplate("kick {player}", map[string]string{"player": v}, ""); err == nil {
			t.Errorf("param %q accepted, want error", v)
		}
	}
}

// Non-text params must stay one token, or a value like "day --world other"
// would append arguments the stored template never had.
func TestResolveCommandTemplate_RejectsExtraArgsInParams(t *testing.T) {
	for _, v := range []string{"Eve Mallory", "--world", "-1 --all"} {
		if _, err := resolveCommandTemplate("op add {player}", map[string]string{"player": v}, ""); err == nil {
			t.Errorf("param %q accepted, want error", v)
		}
	}
}
