package auth

import (
	"crypto/rand"
	"encoding/base64"
	"fmt"
	"strings"
)

const (
	// KeyPrefix is the prefix for all Majordomo API keys
	KeyPrefix = "mdm_sk_"
	// KeyByteLength is the number of random bytes to generate (256 bits)
	KeyByteLength = 32
)

// GenerateAPIKey creates a new API key with the mdm_sk_ prefix.
// Returns the plaintext key (show once to user) and its SHA256 hash (store in DB).
func GenerateAPIKey() (plaintext string, hash string, err error) {
	bytes := make([]byte, KeyByteLength)
	if _, err := rand.Read(bytes); err != nil {
		return "", "", fmt.Errorf("failed to generate random bytes: %w", err)
	}

	// URL-safe base64 encoding, no padding
	encoded := base64.RawURLEncoding.EncodeToString(bytes)
	plaintext = KeyPrefix + encoded
	hash = HashAPIKey(plaintext)

	return plaintext, hash, nil
}

// ValidateKeyFormat checks if a key has the expected mdm_sk_ prefix and minimum length.
func ValidateKeyFormat(key string) bool {
	if len(key) <= len(KeyPrefix) {
		return false
	}
	return strings.HasPrefix(key, KeyPrefix)
}
