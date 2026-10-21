package services

import (
	"testing"

	"github.com/google/uuid"
)

func TestMySQLIdentForServer_FitsLimitAndPassesCheck(t *testing.T) {
	a := uuid.MustParse("11111111-2222-3333-4444-aaaaaaaaaaaa")
	b := uuid.MustParse("99999999-2222-3333-4444-aaaaaaaaaaaa") // same last 8 hex digits

	name := mysqlIdentForServer(a)
	if len(name) != 32 {
		t.Fatalf("len(%q) = %d, want 32 (MariaDB user-name limit)", name, len(name))
	}
	if err := checkMySQLIdents(name); err != nil {
		t.Fatal(err)
	}
	if mysqlIdentForServer(b) == name {
		t.Fatal("servers sharing a UUID suffix must get different names")
	}
}

func TestCheckMySQL_RejectsInjection(t *testing.T) {
	for _, id := range []string{"tp_x`; DROP DATABASE mysql; --", "tp_1234abcd'@'%", "root", ""} {
		if checkMySQLIdents(id) == nil {
			t.Errorf("identifier %q accepted", id)
		}
	}
	if checkMySQLIdents("tp_1234abcd") != nil {
		t.Error("legacy 8-digit name rejected")
	}
	for _, pw := range []string{"x' OR '1'='1", "", "0123456789abcdef0123456789abcdeG"} {
		if checkMySQLPassword(pw) == nil {
			t.Errorf("password %q accepted", pw)
		}
	}
	if pw, err := generateDBPassword(); err != nil || checkMySQLPassword(pw) != nil {
		t.Fatalf("generated password %q rejected: %v", pw, err)
	}
}
