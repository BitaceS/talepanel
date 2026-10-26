package services

import (
	"encoding/base64"
	"testing"
)

func TestNewKeyringHasActiveKeyZeroOf32Bytes(t *testing.T) {
	kr, err := newKeyring()
	if err != nil {
		t.Fatalf("newKeyring: %v", err)
	}
	id, b64, ok := kr.ActiveKey()
	if !ok {
		t.Fatal("new keyring has no active key")
	}
	if id != 0 {
		t.Fatalf("active id = %d, want 0", id)
	}
	raw, err := base64.StdEncoding.DecodeString(b64)
	if err != nil {
		t.Fatalf("active key not base64: %v", err)
	}
	if len(raw) != 32 {
		t.Fatalf("active key is %d bytes, want 32", len(raw))
	}
}

func TestRotateAddsNewActiveKeyAndKeepsOld(t *testing.T) {
	kr, _ := newKeyring()
	oldID, oldKey, _ := kr.ActiveKey()

	newID, err := kr.rotate()
	if err != nil {
		t.Fatalf("rotate: %v", err)
	}
	if newID != oldID+1 {
		t.Fatalf("new id = %d, want %d", newID, oldID+1)
	}
	gotID, gotKey, _ := kr.ActiveKey()
	if gotID != newID {
		t.Fatalf("active id after rotate = %d, want %d", gotID, newID)
	}
	if gotKey == oldKey {
		t.Fatal("rotate reused the same key material")
	}
	// The old key is retained so older backups still restore.
	retained, ok := kr.KeyByID(oldID)
	if !ok || retained != oldKey {
		t.Fatalf("old key id %d not retained", oldID)
	}
}

func TestKeyByIDMissingReportsNotFound(t *testing.T) {
	kr, _ := newKeyring()
	if _, ok := kr.KeyByID(99); ok {
		t.Fatal("unknown key id reported as found")
	}
}
