package services

import (
	"crypto/rand"
	"encoding/base64"
	"fmt"
	"strconv"
)

// Keyring holds the panel-managed backup encryption keys. The active key
// encrypts new backups; older keys are retained so existing backups still
// restore after a rotation. It is serialized as JSON into app_settings.
type Keyring struct {
	Active uint8             `json:"active"`
	Keys   map[string]string `json:"keys"` // key id -> base64 32-byte key
}

// generateKey returns a fresh base64-encoded 32-byte key.
func generateKey() (string, error) {
	var raw [32]byte
	if _, err := rand.Read(raw[:]); err != nil {
		return "", fmt.Errorf("generating backup key: %w", err)
	}
	return base64.StdEncoding.EncodeToString(raw[:]), nil
}

// newKeyring creates a keyring with a single active key, id 0.
func newKeyring() (Keyring, error) {
	k, err := generateKey()
	if err != nil {
		return Keyring{}, err
	}
	return Keyring{Active: 0, Keys: map[string]string{"0": k}}, nil
}

// ActiveKey returns the active key id, its base64 material, and whether it exists.
func (k *Keyring) ActiveKey() (uint8, string, bool) {
	v, ok := k.Keys[strconv.Itoa(int(k.Active))]
	return k.Active, v, ok
}

// KeyByID returns the base64 key for a specific id, if present.
func (k *Keyring) KeyByID(id uint8) (string, bool) {
	v, ok := k.Keys[strconv.Itoa(int(id))]
	return v, ok
}

// rotate adds a new key under the next id and makes it active, keeping the old
// keys so backups encrypted under them still restore.
func (k *Keyring) rotate() (uint8, error) {
	highest := -1
	for idStr := range k.Keys {
		id, err := strconv.Atoi(idStr)
		if err != nil || id < 0 || id > 255 {
			continue
		}
		if id > highest {
			highest = id
		}
	}
	if highest >= 255 {
		return 0, fmt.Errorf("backup keyring is full (255 keys)")
	}
	next := uint8(highest + 1)
	nk, err := generateKey()
	if err != nil {
		return 0, err
	}
	if k.Keys == nil {
		k.Keys = map[string]string{}
	}
	k.Keys[strconv.Itoa(int(next))] = nk
	k.Active = next
	return next, nil
}
