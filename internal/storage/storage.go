package storage

import (
	"context"

	"github.com/google/uuid"
	"github.com/superset-studio/majordomo-gateway/internal/models"
)

// Storage defines the interface for request log storage
type Storage interface {
	WriteRequestLog(ctx context.Context, log *models.RequestLog)
	Ping(ctx context.Context) error
	Close() error
}

// APIKeyStorage defines the interface for API key CRUD operations
type APIKeyStorage interface {
	CreateAPIKey(ctx context.Context, keyHash string, input *models.CreateAPIKeyInput) (*models.APIKey, error)
	GetAPIKeyByHash(ctx context.Context, keyHash string) (*models.APIKey, error)
	GetAPIKeyByID(ctx context.Context, id uuid.UUID) (*models.APIKey, error)
	ListAPIKeys(ctx context.Context) ([]*models.APIKey, error)
	UpdateAPIKey(ctx context.Context, id uuid.UUID, input *models.UpdateAPIKeyInput) (*models.APIKey, error)
	RevokeAPIKey(ctx context.Context, id uuid.UUID) error
	UpdateAPIKeyLastUsed(ctx context.Context, id uuid.UUID) error
}

// ProxyKeyStorage defines the interface for proxy key CRUD operations
type ProxyKeyStorage interface {
	CreateProxyKey(ctx context.Context, keyHash string, majordomoKeyID uuid.UUID, input *models.CreateProxyKeyInput) (*models.ProxyKey, error)
	GetProxyKeyByHash(ctx context.Context, keyHash string) (*models.ProxyKey, error)
	GetProxyKeyByID(ctx context.Context, id uuid.UUID) (*models.ProxyKey, error)
	ListProxyKeys(ctx context.Context, majordomoKeyID uuid.UUID) ([]*models.ProxyKey, error)
	RevokeProxyKey(ctx context.Context, id uuid.UUID) error
	UpdateProxyKeyLastUsed(ctx context.Context, id uuid.UUID) error

	SetProviderMapping(ctx context.Context, proxyKeyID uuid.UUID, provider string, encryptedKey string) error
	GetProviderMapping(ctx context.Context, proxyKeyID uuid.UUID, provider string) (*models.ProviderMapping, error)
	ListProviderMappings(ctx context.Context, proxyKeyID uuid.UUID) ([]*models.ProviderMapping, error)
	DeleteProviderMapping(ctx context.Context, proxyKeyID uuid.UUID, provider string) error
}
