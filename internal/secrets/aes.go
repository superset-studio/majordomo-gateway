package secrets

import (
	"crypto/aes"
	"crypto/cipher"
	"crypto/rand"
	"encoding/base64"
	"encoding/hex"
	"errors"
	"fmt"
	"io"
)

// AESStore implements SecretStore using AES-256-GCM authenticated encryption.
type AESStore struct {
	key []byte // 32-byte AES-256 key
}

// NewAESStore creates a new AESStore from a hex-encoded or base64-encoded 32-byte key.
func NewAESStore(key string) (*AESStore, error) {
	if key == "" {
		return nil, errors.New("encryption key is required")
	}

	// Try hex first (64 hex chars = 32 bytes)
	keyBytes, err := hex.DecodeString(key)
	if err != nil || len(keyBytes) != 32 {
		// Try base64
		keyBytes, err = base64.StdEncoding.DecodeString(key)
		if err != nil || len(keyBytes) != 32 {
			// Try base64 raw/URL variants
			keyBytes, err = base64.RawStdEncoding.DecodeString(key)
			if err != nil || len(keyBytes) != 32 {
				return nil, fmt.Errorf("encryption key must be 32 bytes (got %d), provide as 64-char hex or base64", len(keyBytes))
			}
		}
	}

	return &AESStore{key: keyBytes}, nil
}

// Encrypt encrypts plaintext using AES-256-GCM and returns base64(nonce + ciphertext).
func (s *AESStore) Encrypt(plaintext string) (string, error) {
	block, err := aes.NewCipher(s.key)
	if err != nil {
		return "", fmt.Errorf("failed to create cipher: %w", err)
	}

	gcm, err := cipher.NewGCM(block)
	if err != nil {
		return "", fmt.Errorf("failed to create GCM: %w", err)
	}

	nonce := make([]byte, gcm.NonceSize())
	if _, err := io.ReadFull(rand.Reader, nonce); err != nil {
		return "", fmt.Errorf("failed to generate nonce: %w", err)
	}

	ciphertext := gcm.Seal(nonce, nonce, []byte(plaintext), nil)
	return base64.StdEncoding.EncodeToString(ciphertext), nil
}

// Decrypt decrypts a base64(nonce + ciphertext) string using AES-256-GCM.
func (s *AESStore) Decrypt(ciphertext string) (string, error) {
	data, err := base64.StdEncoding.DecodeString(ciphertext)
	if err != nil {
		return "", fmt.Errorf("failed to decode ciphertext: %w", err)
	}

	block, err := aes.NewCipher(s.key)
	if err != nil {
		return "", fmt.Errorf("failed to create cipher: %w", err)
	}

	gcm, err := cipher.NewGCM(block)
	if err != nil {
		return "", fmt.Errorf("failed to create GCM: %w", err)
	}

	nonceSize := gcm.NonceSize()
	if len(data) < nonceSize {
		return "", errors.New("ciphertext too short")
	}

	nonce, encrypted := data[:nonceSize], data[nonceSize:]
	plaintext, err := gcm.Open(nil, nonce, encrypted, nil)
	if err != nil {
		return "", fmt.Errorf("failed to decrypt: %w", err)
	}

	return string(plaintext), nil
}
