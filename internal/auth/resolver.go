package auth

import (
	"crypto/sha256"
	"encoding/hex"
	"errors"

	"github.com/superset-studio/majordomo-gateway/internal/models"
)

var ErrInvalidAPIKey = errors.New("invalid API key")

type Resolver struct{}

func NewResolver() *Resolver {
	return &Resolver{}
}

func (r *Resolver) ResolveAPIKey(apiKey string) (*models.APIKeyInfo, error) {
	if apiKey == "" {
		return nil, ErrInvalidAPIKey
	}

	return &models.APIKeyInfo{
		Hash:  hashAPIKey(apiKey),
		Alias: nil,
	}, nil
}

func hashAPIKey(key string) string {
	h := sha256.Sum256([]byte(key))
	return hex.EncodeToString(h[:])
}

func HashAPIKey(key string) string {
	return hashAPIKey(key)
}
