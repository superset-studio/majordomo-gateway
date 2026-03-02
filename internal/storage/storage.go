package storage

import (
	"context"
	"time"

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
	ListAPIKeysByUserID(ctx context.Context, userID uuid.UUID) ([]*models.APIKey, error)
}

// UserStorage defines the interface for user CRUD operations
type UserStorage interface {
	CreateUser(ctx context.Context, input *models.CreateUserInput) (*models.User, error)
	GetUserByID(ctx context.Context, id uuid.UUID) (*models.User, error)
	GetUserByUsername(ctx context.Context, username string) (*models.User, error)
	ListUsers(ctx context.Context) ([]*models.User, error)
	UpdateUserPassword(ctx context.Context, id uuid.UUID, passwordHash string) error
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

// ClaudeSessionStorage defines the interface for Claude Code session and request detail operations.
type ClaudeSessionStorage interface {
	CreateClaudeSession(ctx context.Context, session *models.ClaudeSession) error
	EndClaudeSession(ctx context.Context, sessionID uuid.UUID) error
	GetClaudeSession(ctx context.Context, sessionID uuid.UUID) (*models.ClaudeSession, error)
	ListClaudeSessions(ctx context.Context, apiKeyID uuid.UUID, limit, offset int) ([]*models.ClaudeSession, int, error)
	UpdateClaudeSessionStats(ctx context.Context, sessionID uuid.UUID, inputTokens, outputTokens int, cost float64) error
	CreateClaudeRequestDetail(ctx context.Context, detail *models.ClaudeRequestDetail) error
	ListClaudeSessionRequests(ctx context.Context, sessionID uuid.UUID, limit, offset int) ([]*models.ClaudeRequestDetail, int, error)
}

// MetadataKeyStorage defines the interface for metadata key management.
type MetadataKeyStorage interface {
	ListMetadataKeys(ctx context.Context, userID uuid.UUID) ([]*models.MetadataKey, error)
	ActivateMetadataKey(ctx context.Context, apiKeyID uuid.UUID, keyName string) error
	DeactivateMetadataKey(ctx context.Context, apiKeyID uuid.UUID, keyName string) error
	UpdateMetadataKeyDisplayName(ctx context.Context, apiKeyID uuid.UUID, keyName string, displayName *string) error
}

// MetadataFilter represents a single key=value filter on indexed_metadata.
type MetadataFilter struct {
	Key   string
	Value string
}

// UsageFilter holds common filters for usage reporting queries.
type UsageFilter struct {
	UserID          uuid.UUID
	Start           time.Time
	End             time.Time
	APIKeyID        *uuid.UUID
	MetadataFilters []MetadataFilter // AND of up to 2 key=value pairs
}

// ClaudeAnalyticsStorage defines the interface for Claude Code analytics queries.
type ClaudeAnalyticsStorage interface {
	GetClaudeSummary(ctx context.Context, filter *UsageFilter) (*models.ClaudeSummary, error)
	GetClaudeDailyStats(ctx context.Context, filter *UsageFilter) ([]*models.ClaudeDailyStats, error)
	ListClaudeSessionsAdmin(ctx context.Context, filter *UsageFilter, limit, offset int) ([]*models.ClaudeSessionListItem, int, error)
	GetClaudeToolUsage(ctx context.Context, filter *UsageFilter, topN int) ([]*models.ClaudeToolUsage, error)
	GetClaudePerformance(ctx context.Context, filter *UsageFilter) (*models.ClaudePerformance, error)
	GetClaudeSessionDetail(ctx context.Context, sessionID uuid.UUID, userID uuid.UUID) (*models.ClaudeSessionDetail, error)
	GetClaudeModelUsage(ctx context.Context, filter *UsageFilter) ([]*models.ClaudeModelUsage, error)
}

// UsageStorage defines the interface for usage reporting queries.
type UsageStorage interface {
	GetUsageSummary(ctx context.Context, filter *UsageFilter) (*models.UsageSummary, error)
	GetDailyUsage(ctx context.Context, filter *UsageFilter) ([]*models.DailyUsage, error)
	GetModelBreakdown(ctx context.Context, filter *UsageFilter) ([]*models.ModelUsage, error)
	GetAPIKeyBreakdown(ctx context.Context, filter *UsageFilter) ([]*models.APIKeyUsage, error)
	ListUsageRequests(ctx context.Context, filter *UsageFilter, limit, offset int) ([]*models.RequestListItem, int, error)
	GetMetadataBreakdown(ctx context.Context, filter *UsageFilter, keyName string) ([]*models.MetadataBreakdown, error)
	GetRequestDetail(ctx context.Context, requestID uuid.UUID, userID uuid.UUID) (*models.RequestLog, error)
}
