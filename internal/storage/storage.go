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
	ListAPIKeysByOrgID(ctx context.Context, orgID uuid.UUID) ([]*models.APIKey, error)
}

// UserStorage defines the interface for user CRUD operations
type UserStorage interface {
    CreateUser(ctx context.Context, input *models.CreateUserInput) (*models.User, error)
    GetUserByID(ctx context.Context, id uuid.UUID) (*models.User, error)
    GetUserByUsername(ctx context.Context, username string) (*models.User, error)
    GetUserByEmail(ctx context.Context, email string) (*models.User, error)
    GetUserByAuthProvider(ctx context.Context, provider, providerID string) (*models.User, error)
    CreateOAuthUser(ctx context.Context, input *models.CreateOAuthUserInput) (*models.User, error)
    ListUsers(ctx context.Context) ([]*models.User, error)
    UpdateUserPassword(ctx context.Context, id uuid.UUID, passwordHash string) error
    UpdateUserS3Config(ctx context.Context, userID uuid.UUID, bucket, region, endpoint, encAccessKeyID, encSecretAccessKey string) error
    ClearUserS3Config(ctx context.Context, userID uuid.UUID) error
    GetUserS3Config(ctx context.Context, userID uuid.UUID) (*models.User, error)
    UpdateUserCloudStorageConfig(ctx context.Context, userID uuid.UUID, provider, s3Bucket, s3Region, s3Endpoint, encS3AccessKeyID, encS3SecretKey, gcsBucket, gcsProjectID, encGCSCredJSON string) error
    ClearUserCloudStorageConfig(ctx context.Context, userID uuid.UUID) error
    GetUserCloudStorageConfig(ctx context.Context, userID uuid.UUID) (*models.User, error)
    MarkUserEmailVerified(ctx context.Context, id uuid.UUID) error
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
	OrgID           *uuid.UUID
	Provider        *string
	Model           *string
	MetadataFilters []MetadataFilter // AND of up to 2 key=value pairs
}

// OrganizationStorage defines the interface for organization CRUD operations.
type OrganizationStorage interface {
	// Orgs
	CreateOrganizationWithUser(ctx context.Context, orgInput *models.CreateOrganizationInput, userInput *models.CreateUserInput) (*models.User, *models.Organization, error)
	CreateOrganization(ctx context.Context, input *models.CreateOrganizationInput, creatorUserID uuid.UUID) (*models.Organization, error)
	GetOrganizationByID(ctx context.Context, id uuid.UUID) (*models.Organization, error)
	GetOrganizationBySlug(ctx context.Context, slug string) (*models.Organization, error)
	UpdateOrganization(ctx context.Context, id uuid.UUID, input *models.UpdateOrganizationInput) (*models.Organization, error)

	// Members
	AddMember(ctx context.Context, orgID, userID uuid.UUID, role string) error
	RemoveMember(ctx context.Context, orgID, userID uuid.UUID) error
	GetMember(ctx context.Context, orgID, userID uuid.UUID) (*models.OrganizationMember, error)
	ListMembers(ctx context.Context, orgID uuid.UUID) ([]*models.OrganizationMember, error)
	GetUserOrganization(ctx context.Context, userID uuid.UUID) (*models.Organization, *models.OrganizationMember, error)
	UpdateMemberRole(ctx context.Context, orgID, userID uuid.UUID, role string) error

	// S3 Config
	UpdateOrgS3Config(ctx context.Context, orgID uuid.UUID, bucket, region, endpoint, encAccessKeyID, encSecretAccessKey string) error
	ClearOrgS3Config(ctx context.Context, orgID uuid.UUID) error
	GetOrgS3Config(ctx context.Context, orgID uuid.UUID) (*models.Organization, error)

	// Cloud Storage Config (S3 or GCS)
	UpdateOrgCloudStorageConfig(ctx context.Context, orgID uuid.UUID, provider, s3Bucket, s3Region, s3Endpoint, encS3AccessKeyID, encS3SecretKey, gcsBucket, gcsProjectID, encGCSCredJSON string) error
	ClearOrgCloudStorageConfig(ctx context.Context, orgID uuid.UUID) error
	GetOrgCloudStorageConfig(ctx context.Context, orgID uuid.UUID) (*models.Organization, error)

	// Invites
	CreateInvite(ctx context.Context, orgID uuid.UUID, input *models.CreateInviteInput, invitedBy uuid.UUID, token string, expiresAt time.Time) (*models.OrganizationInvite, error)
	GetInviteByToken(ctx context.Context, token string) (*models.OrganizationInvite, error)
	GetInviteByID(ctx context.Context, id uuid.UUID) (*models.OrganizationInvite, error)
	ListPendingInvites(ctx context.Context, orgID uuid.UUID) ([]*models.OrganizationInvite, error)
	AcceptInvite(ctx context.Context, inviteID uuid.UUID) error
	DeleteInvite(ctx context.Context, id uuid.UUID) error
	ListInvitesByEmail(ctx context.Context, email string) ([]*models.OrganizationInvite, error)
}

// ProviderKeyStorage defines the interface for provider API key CRUD operations (for replay).
type ProviderKeyStorage interface {
	SetProviderKey(ctx context.Context, userID *uuid.UUID, orgID *uuid.UUID, provider string, encryptedKey string) error
	ListProviderKeys(ctx context.Context, userID *uuid.UUID, orgID *uuid.UUID) ([]*models.ProviderKeyInfo, error)
	GetProviderKey(ctx context.Context, userID *uuid.UUID, orgID *uuid.UUID, provider string) (*models.ProviderAPIKey, error)
	DeleteProviderKey(ctx context.Context, userID *uuid.UUID, orgID *uuid.UUID, provider string) error
}

// ReplayStorage defines the interface for replay run and result CRUD operations.
type ReplayStorage interface {
	CreateReplayRun(ctx context.Context, run *models.ReplayRun) error
	GetReplayRun(ctx context.Context, id uuid.UUID) (*models.ReplayRun, error)
	ListReplayRuns(ctx context.Context, userID uuid.UUID, orgID *uuid.UUID, limit, offset int) ([]*models.ReplayRunListItem, int, error)
	UpdateReplayRunStatus(ctx context.Context, id uuid.UUID, status string, errorMessage *string) error
	CancelReplayRun(ctx context.Context, id uuid.UUID, userID uuid.UUID, orgID *uuid.UUID) error
	ListReplayResults(ctx context.Context, runID uuid.UUID, limit, offset int) ([]*models.ReplayResult, int, error)
	GetReplayResult(ctx context.Context, id uuid.UUID) (*models.ReplayResult, error)
	ListLLMProviders(ctx context.Context) ([]*models.LLMProvider, error)
}

// EvalSetSourceFilters holds filters for populating an eval set from logged requests.
type EvalSetSourceFilters struct {
	APIKeyID *uuid.UUID
	Provider *string
	Model    *string
	Start    *time.Time
	End      *time.Time
	Metadata map[string]string
	Limit    int
}

// EvalStorage defines the interface for eval set, run, and result operations.
type EvalStorage interface {
	// Eval Sets
	CreateEvalSet(ctx context.Context, set *models.EvalSet) error
	GetEvalSet(ctx context.Context, id uuid.UUID) (*models.EvalSet, error)
	ListEvalSets(ctx context.Context, userID uuid.UUID, orgID *uuid.UUID, limit, offset int) ([]*models.EvalSet, int, error)
	UpdateEvalSet(ctx context.Context, id uuid.UUID, name string, description *string) (*models.EvalSet, error)
	DeleteEvalSet(ctx context.Context, id uuid.UUID) error

	// Eval Set Items
	AddEvalSetItems(ctx context.Context, evalSetID uuid.UUID, requestIDs []uuid.UUID) (int, error)
	AddEvalSetItemsFromFilters(ctx context.Context, evalSetID uuid.UUID, userID uuid.UUID, orgID *uuid.UUID, filters *EvalSetSourceFilters) (int, error)
	RemoveEvalSetItem(ctx context.Context, evalSetID uuid.UUID, requestID uuid.UUID) error
	ListEvalSetItems(ctx context.Context, evalSetID uuid.UUID, limit, offset int) ([]*models.EvalSetItem, int, error)

	// Eval Runs
	CreateEvalRun(ctx context.Context, run *models.EvalRun) error
	GetEvalRun(ctx context.Context, id uuid.UUID) (*models.EvalRun, error)
	ListEvalRuns(ctx context.Context, userID uuid.UUID, orgID *uuid.UUID, limit, offset int) ([]*models.EvalRunListItem, int, error)
	CancelEvalRun(ctx context.Context, id uuid.UUID, userID uuid.UUID, orgID *uuid.UUID) error

	// Eval Results
	ListEvalResults(ctx context.Context, runID uuid.UUID, limit, offset int) ([]*models.EvalResult, int, error)
	GetEvalResult(ctx context.Context, id uuid.UUID) (*models.EvalResult, error)
}

// ClaudeAnalyticsStorage defines the interface for Claude Code analytics queries.
type ClaudeAnalyticsStorage interface {
	GetClaudeSummary(ctx context.Context, filter *UsageFilter) (*models.ClaudeSummary, error)
	GetClaudeDailyStats(ctx context.Context, filter *UsageFilter) ([]*models.ClaudeDailyStats, error)
	ListClaudeSessionsAdmin(ctx context.Context, filter *UsageFilter, limit, offset int) ([]*models.ClaudeSessionListItem, int, error)
	GetClaudeToolUsage(ctx context.Context, filter *UsageFilter, topN int) ([]*models.ClaudeToolUsage, error)
	GetClaudePerformance(ctx context.Context, filter *UsageFilter) (*models.ClaudePerformance, error)
	GetClaudeSessionDetail(ctx context.Context, sessionID uuid.UUID, userID uuid.UUID, orgID *uuid.UUID) (*models.ClaudeSessionDetail, error)
	GetClaudeModelUsage(ctx context.Context, filter *UsageFilter) ([]*models.ClaudeModelUsage, error)
	GetClaudeAPIKeyBreakdown(ctx context.Context, filter *UsageFilter) ([]*models.ClaudeAPIKeyUsage, error)
}

// UsageStorage defines the interface for usage reporting queries.
type UsageStorage interface {
    GetUsageSummary(ctx context.Context, filter *UsageFilter) (*models.UsageSummary, error)
    GetDailyUsage(ctx context.Context, filter *UsageFilter) ([]*models.DailyUsage, error)
    GetModelBreakdown(ctx context.Context, filter *UsageFilter) ([]*models.ModelUsage, error)
    GetAPIKeyBreakdown(ctx context.Context, filter *UsageFilter) ([]*models.APIKeyUsage, error)
    ListUsageRequests(ctx context.Context, filter *UsageFilter, limit, offset int) ([]*models.RequestListItem, int, error)
    GetMetadataBreakdown(ctx context.Context, filter *UsageFilter, keyName string) ([]*models.MetadataBreakdown, error)
    GetRequestDetail(ctx context.Context, requestID uuid.UUID, userID uuid.UUID, orgID *uuid.UUID) (*models.RequestLog, error)
}

// PasswordResetStorage defines operations for password reset tokens.
type PasswordResetStorage interface {
    CreatePasswordResetToken(ctx context.Context, userID uuid.UUID, token string, expiresAt time.Time) (*models.PasswordResetToken, error)
    GetPasswordResetByToken(ctx context.Context, token string) (*models.PasswordResetToken, error)
    MarkPasswordResetUsed(ctx context.Context, id uuid.UUID) error
}

// EmailVerificationStorage defines operations for email verification tokens.
type EmailVerificationStorage interface {
    CreateEmailVerificationToken(ctx context.Context, userID uuid.UUID, token string, expiresAt time.Time) (*models.EmailVerificationToken, error)
    GetEmailVerificationByToken(ctx context.Context, token string) (*models.EmailVerificationToken, error)
    MarkEmailVerificationUsed(ctx context.Context, id uuid.UUID) error
}
