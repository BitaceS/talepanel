package crypto

import (
	"bytes"
	"encoding/hex"
	"strings"
	"testing"
)

func testKey(t *testing.T) []byte {
	t.Helper()
	k, err := hex.DecodeString("00112233445566778899aabbccddeeff00112233445566778899aabbccddeeff")
	if err != nil {
		t.Fatalf("decoding test key: %v", err)
	}
	return k
}

func TestEncryptDecryptRoundTrip(t *testing.T) {
	key := testKey(t)
	plaintext := []byte("JBSWY3DPEHPK3PXP") // example TOTP secret

	ciphertext, err := Encrypt(key, plaintext)
	if err != nil {
		t.Fatalf("encrypt: %v", err)
	}
	if len(ciphertext) == 0 {
		t.Fatal("ciphertext is empty")
	}
	if bytes.Contains([]byte(ciphertext), plaintext) {
		t.Fatal("ciphertext leaks plaintext")
	}

	decrypted, err := Decrypt(key, ciphertext)
	if err != nil {
		t.Fatalf("decrypt: %v", err)
	}
	if !bytes.Equal(decrypted, plaintext) {
		t.Fatalf("decrypted mismatch: got %q want %q", decrypted, plaintext)
	}
}

func TestEncryptProducesUniqueCiphertexts(t *testing.T) {
	key := testKey(t)
	plaintext := []byte("same-input")

	c1, err := Encrypt(key, plaintext)
	if err != nil {
		t.Fatal(err)
	}
	c2, err := Encrypt(key, plaintext)
	if err != nil {
		t.Fatal(err)
	}
	if c1 == c2 {
		t.Fatal("two encryptions of the same plaintext produced identical ciphertext — nonce is not random")
	}
}

func TestDecryptRejectsWrongKey(t *testing.T) {
	key1 := testKey(t)
	key2 := make([]byte, 32)
	for i := range key2 {
		key2[i] = 0xAA
	}
	ciphertext, err := Encrypt(key1, []byte("secret"))
	if err != nil {
		t.Fatal(err)
	}
	if _, err := Decrypt(key2, ciphertext); err == nil {
		t.Fatal("expected decrypt to fail with wrong key")
	}
}

func TestDecryptRejectsTamperedCiphertext(t *testing.T) {
	key := testKey(t)
	ciphertext, err := Encrypt(key, []byte("secret"))
	if err != nil {
		t.Fatal(err)
	}
	tampered := strings.Replace(ciphertext, ciphertext[len(ciphertext)-2:], "00", 1)
	if _, err := Decrypt(key, tampered); err == nil {
		t.Fatal("expected decrypt to fail with tampered ciphertext")
	}
}

func TestEncryptRejectsInvalidKeyLength(t *testing.T) {
	shortKey := []byte("too-short")
	if _, err := Encrypt(shortKey, []byte("data")); err == nil {
		t.Fatal("expected encrypt to fail with short key")
	}
}
