package api

import (
	"context"
	"fmt"
	"log/slog"

	"github.com/google/uuid"
	"github.com/superset-studio/majordomo-gateway/internal/auth"
	"github.com/superset-studio/majordomo-gateway/internal/models"
	"github.com/superset-studio/majordomo-gateway/internal/secrets"
	"github.com/superset-studio/majordomo-gateway/internal/storage"
)

// ProxyKeyService encapsulates proxy key CRUD and provider mapping logic
// shared between Handler (API key auth) and AdminHandler (JWT auth).
type ProxyKeyService struct {
	store   storage.ProxyKeyStorage
	secrets secrets.SecretStore
}

// NewProxyKeyService creates a new ProxyKeyService.
func NewProxyKeyService(store storage.ProxyKeyStorage, secretStore secrets.SecretStore) *ProxyKeyService {
	return &ProxyKeyService{
		store:   store,
		secrets: secretStore,
	}
}

type setProviderMappingRequest struct {
	APIKey string `json:"api_key"`
}

type providerMappingResponse struct {
	ID         uuid.UUID `json:"id"`
	ProxyKeyID uuid.UUID `json:"proxy_key_id"`
	Provider   string    `json:"provider"`
	CreatedAt  string    `json:"created_at"`
	UpdatedAt  string    `json:"updated_at"`
}

// CreateProxyKey generates a new proxy key, stores it, and returns the model + plaintext key.
func (s *ProxyKeyService) CreateProxyKey(ctx context.Context, majordomoKeyID uuid.UUID, name string, description *string) (*models.ProxyKey, string, error) {
	plaintext, hash, err := auth.GenerateProxyKey()
	if err != nil {
		return nil, "", fmt.Errorf("generate proxy key: %w", err)
	}

	input := &models.CreateProxyKeyInput{
		Name:        name,
		Description: description,
	}

	pk, err := s.store.CreateProxyKey(ctx, hash, majordomoKeyID, input)
	if err != nil {
		return nil, "", fmt.Errorf("create proxy key: %w", err)
	}

	return pk, plaintext, nil
}

// ListProxyKeys returns all proxy keys for the given majordomo API key.
func (s *ProxyKeyService) ListProxyKeys(ctx context.Context, majordomoKeyID uuid.UUID) ([]*models.ProxyKey, error) {
	return s.store.ListProxyKeys(ctx, majordomoKeyID)
}

// GetProxyKey fetches a proxy key and verifies it belongs to the given majordomo API key.
func (s *ProxyKeyService) GetProxyKey(ctx context.Context, id uuid.UUID, majordomoKeyID uuid.UUID) (*models.ProxyKey, error) {
	pk, err := s.store.GetProxyKeyByID(ctx, id)
	if err != nil {
		return nil, err
	}

	if pk.MajordomoAPIKeyID != majordomoKeyID {
		return nil, storage.ErrProxyKeyNotFound
	}

	return pk, nil
}

// RevokeProxyKey fetches, verifies ownership, and revokes a proxy key.
func (s *ProxyKeyService) RevokeProxyKey(ctx context.Context, id uuid.UUID, majordomoKeyID uuid.UUID) error {
	pk, err := s.store.GetProxyKeyByID(ctx, id)
	if err != nil {
		return err
	}

	if pk.MajordomoAPIKeyID != majordomoKeyID {
		return storage.ErrProxyKeyNotFound
	}

	return s.store.RevokeProxyKey(ctx, id)
}

// SetProviderMapping verifies ownership, encrypts the API key, and stores the mapping.
func (s *ProxyKeyService) SetProviderMapping(ctx context.Context, proxyKeyID uuid.UUID, majordomoKeyID uuid.UUID, provider string, apiKey string) error {
	pk, err := s.store.GetProxyKeyByID(ctx, proxyKeyID)
	if err != nil {
		return err
	}

	if pk.MajordomoAPIKeyID != majordomoKeyID {
		return storage.ErrProxyKeyNotFound
	}

	encrypted, err := s.secrets.Encrypt(apiKey)
	if err != nil {
		slog.Error("failed to encrypt provider key", "error", err)
		return fmt.Errorf("encrypt provider key: %w", err)
	}

	return s.store.SetProviderMapping(ctx, proxyKeyID, provider, encrypted)
}

// DeleteProviderMapping verifies ownership and deletes the mapping.
func (s *ProxyKeyService) DeleteProviderMapping(ctx context.Context, proxyKeyID uuid.UUID, majordomoKeyID uuid.UUID, provider string) error {
	pk, err := s.store.GetProxyKeyByID(ctx, proxyKeyID)
	if err != nil {
		return err
	}

	if pk.MajordomoAPIKeyID != majordomoKeyID {
		return storage.ErrProxyKeyNotFound
	}

	return s.store.DeleteProviderMapping(ctx, proxyKeyID, provider)
}

// ListProviderMappings verifies ownership and returns mappings without encrypted keys.
func (s *ProxyKeyService) ListProviderMappings(ctx context.Context, proxyKeyID uuid.UUID, majordomoKeyID uuid.UUID) ([]providerMappingResponse, error) {
	pk, err := s.store.GetProxyKeyByID(ctx, proxyKeyID)
	if err != nil {
		return nil, err
	}

	if pk.MajordomoAPIKeyID != majordomoKeyID {
		return nil, storage.ErrProxyKeyNotFound
	}

	mappings, err := s.store.ListProviderMappings(ctx, proxyKeyID)
	if err != nil {
		return nil, err
	}

	var resp []providerMappingResponse
	for _, m := range mappings {
		resp = append(resp, providerMappingResponse{
			ID:         m.ID,
			ProxyKeyID: m.ProxyKeyID,
			Provider:   m.Provider,
			CreatedAt:  m.CreatedAt.Format("2006-01-02T15:04:05Z07:00"),
			UpdatedAt:  m.UpdatedAt.Format("2006-01-02T15:04:05Z07:00"),
		})
	}

	return resp, nil
}
