// Package crypto provides AES-256-GCM helpers for encrypting small secrets
// (TOTP shared secrets, recovery codes, etc.) at rest in the database.
//
// The output format is `base64(nonce || ciphertext || tag)` so the whole
// envelope fits in a single TEXT column.
package crypto

import (
	"crypto/aes"
	"crypto/cipher"
	"crypto/rand"
	"encoding/base64"
	"errors"
	"fmt"
	"io"
)

// ErrInvalidKey is returned when the key is not exactly 32 bytes.
var ErrInvalidKey = errors.New("key must be exactly 32 bytes for AES-256-GCM")

// Encrypt encrypts plaintext with AES-256-GCM and returns a base64-encoded
// envelope `nonce || ciphertext || tag`.
func Encrypt(key, plaintext []byte) (string, error) {
	if len(key) != 32 {
		return "", ErrInvalidKey
	}

	block, err := aes.NewCipher(key)
	if err != nil {
		return "", fmt.Errorf("aes.NewCipher: %w", err)
	}
	gcm, err := cipher.NewGCM(block)
	if err != nil {
		return "", fmt.Errorf("cipher.NewGCM: %w", err)
	}

	nonce := make([]byte, gcm.NonceSize())
	if _, err := io.ReadFull(rand.Reader, nonce); err != nil {
		return "", fmt.Errorf("random nonce: %w", err)
	}

	sealed := gcm.Seal(nonce, nonce, plaintext, nil)
	return base64.StdEncoding.EncodeToString(sealed), nil
}

// Decrypt reverses Encrypt. Returns the plaintext or an error if the
// envelope is malformed, the key is wrong, or the ciphertext was tampered.
func Decrypt(key []byte, envelope string) ([]byte, error) {
	if len(key) != 32 {
		return nil, ErrInvalidKey
	}

	raw, err := base64.StdEncoding.DecodeString(envelope)
	if err != nil {
		return nil, fmt.Errorf("base64: %w", err)
	}

	block, err := aes.NewCipher(key)
	if err != nil {
		return nil, fmt.Errorf("aes.NewCipher: %w", err)
	}
	gcm, err := cipher.NewGCM(block)
	if err != nil {
		return nil, fmt.Errorf("cipher.NewGCM: %w", err)
	}

	if len(raw) < gcm.NonceSize() {
		return nil, errors.New("ciphertext too short")
	}

	nonce, ciphertext := raw[:gcm.NonceSize()], raw[gcm.NonceSize():]
	plaintext, err := gcm.Open(nil, nonce, ciphertext, nil)
	if err != nil {
		return nil, fmt.Errorf("gcm.Open: %w", err)
	}
	return plaintext, nil
}
