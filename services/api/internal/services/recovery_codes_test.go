package services

import (
	"regexp"
	"strings"
	"testing"
)

var recoveryCodeFormat = regexp.MustCompile(`^[0-9abcdefghjkmnpqrstvwxyz]{5}-[0-9abcdefghjkmnpqrstvwxyz]{5}$`)

func TestGenerateRecoveryCodes_FormatAndUniqueness(t *testing.T) {
	seen := map[string]struct{}{}
	for round := 0; round < 200; round++ {
		codes, err := generateRecoveryCodes(RecoveryCodeCount)
		if err != nil {
			t.Fatalf("generate: %v", err)
		}
		if len(codes) != RecoveryCodeCount {
			t.Fatalf("got %d codes, want %d", len(codes), RecoveryCodeCount)
		}
		for _, c := range codes {
			if !recoveryCodeFormat.MatchString(c) {
				t.Fatalf("code %q does not match xxxxx-xxxxx in the Crockford alphabet", c)
			}
			if _, dup := seen[c]; dup {
				t.Fatalf("duplicate code %q", c)
			}
			seen[c] = struct{}{}
			if normalizeRecoveryCode(c) != strings.ReplaceAll(c, "-", "") {
				t.Fatalf("generated code %q does not round-trip through normalisation", c)
			}
		}
	}
}

func TestRecoveryCodeAlphabet(t *testing.T) {
	if len(recoveryCodeAlphabet) != 32 {
		t.Fatalf("alphabet has %d symbols; masking to 5 bits needs exactly 32", len(recoveryCodeAlphabet))
	}
	for _, ambiguous := range "ilou" {
		if strings.ContainsRune(recoveryCodeAlphabet, ambiguous) {
			t.Fatalf("alphabet contains ambiguous %q", ambiguous)
		}
	}
}

func TestNormalizeRecoveryCode(t *testing.T) {
	cases := map[string]string{
		"abcde-fgh23":       "abcdefgh23",
		"ABCDE-FGH23":       "abcdefgh23",
		"abcdefgh23":        "abcdefgh23",
		"  abcde fgh23 \n":  "abcdefgh23",
		"abcde--fgh23":      "abcdefgh23",
		"oOiIl-LxyzX":       "00111" + "1xyzx",
		"abcde-fgh2":        "",
		"abcde-fgh234":      "",
		"abcde-fgh2u":       "",
		"abcde-fgh2!":       "",
		"":                  "",
		"123456":            "",
		"äbcde-fgh23":       "",
		"abcde-fgh23-extra": "",
	}
	for in, want := range cases {
		if got := normalizeRecoveryCode(in); got != want {
			t.Errorf("normalizeRecoveryCode(%q) = %q, want %q", in, got, want)
		}
	}
}

func TestIsTOTPCode(t *testing.T) {
	for in, want := range map[string]bool{
		"123456":      true,
		"12345":       false,
		"1234567":     false,
		"12345a":      false,
		"abcde-fgh23": false,
	} {
		if got := isTOTPCode(in); got != want {
			t.Errorf("isTOTPCode(%q) = %v, want %v", in, got, want)
		}
	}
}
