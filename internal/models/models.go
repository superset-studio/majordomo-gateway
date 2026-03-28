package models

import (
	"time"

	"github.com/google/uuid"
)

// User represents a web UI user stored in the database
type User struct {
	ID             uuid.UUID `json:"id" db:"id"`
	Username       string    `json:"username" db:"username"`
	PasswordHash   *string   `json:"-" db:"password_hash"`
	Email          *string   `json:"email,omitempty" db:"email"`
	AuthProvider   *string   `json:"auth_provider,omitempty" db:"auth_provider"`
	AuthProviderID *string   `json:"-" db:"auth_provider_id"`
	IsActive       bool      `json:"is_active" db:"is_active"`
	EmailVerified  bool      `json:"email_verified" db:"email_verified"`
	CreatedAt      time.Time `json:"created_at" db:"created_at"`

	// Per-user cloud body storage configuration
	CloudStorageProvider        *string `json:"cloud_storage_provider,omitempty" db:"cloud_storage_provider"`
	S3Bucket                    *string `json:"s3_bucket,omitempty" db:"s3_bucket"`
	S3Region                    *string `json:"s3_region,omitempty" db:"s3_region"`
	S3Endpoint                  *string `json:"s3_endpoint,omitempty" db:"s3_endpoint"`
	S3AccessKeyIDEncrypted      *string `json:"-" db:"s3_access_key_id_encrypted"`
	S3SecretAccessKeyEncrypted  *string `json:"-" db:"s3_secret_access_key_encrypted"`
	GCSBucket                   *string `json:"gcs_bucket,omitempty" db:"gcs_bucket"`
	GCSProjectID                *string `json:"gcs_project_id,omitempty" db:"gcs_project_id"`
	GCSCredentialsJSONEncrypted *string `json:"-" db:"gcs_credentials_json_encrypted"`
}

// UserS3Config holds decrypted S3 configuration for per-user body storage.
type UserS3Config struct {
	Bucket         string
	Region         string
	Endpoint       string
	AccessKeyID    string
	SecretAccessKey string
}

// CloudStorageProviderType identifies the cloud storage provider.
type CloudStorageProviderType string

const (
	CloudStorageProviderS3  CloudStorageProviderType = "s3"
	CloudStorageProviderGCS CloudStorageProviderType = "gcs"
)

// UserCloudStorageConfig holds decrypted cloud storage configuration (S3 or GCS).
type UserCloudStorageConfig struct {
	Provider CloudStorageProviderType
	// S3 fields
	Bucket         string
	Region         string
	Endpoint       string
	AccessKeyID    string
	SecretAccessKey string
	// GCS fields
	GCSBucket          string
	GCSProjectID       string
	GCSCredentialsJSON string
}

// CreateUserInput contains fields for creating a new user
type CreateUserInput struct {
	Username string
	Email    string
	Password string
}

// CreateOAuthUserInput contains fields for creating a new OAuth user
type CreateOAuthUserInput struct {
    Username       string
    Email          *string
    AuthProvider   string
    AuthProviderID string
}

// PasswordResetToken represents a single-use token to reset a password
type PasswordResetToken struct {
    ID        uuid.UUID `json:"id" db:"id"`
    UserID    uuid.UUID `json:"user_id" db:"user_id"`
    Token     string    `json:"token" db:"token"`
    ExpiresAt time.Time `json:"expires_at" db:"expires_at"`
    UsedAt    *time.Time `json:"used_at" db:"used_at"`
    CreatedAt time.Time `json:"created_at" db:"created_at"`
}

// EmailVerificationToken represents a single-use token to verify a user's email
type EmailVerificationToken struct {
    ID        uuid.UUID  `json:"id" db:"id"`
    UserID    uuid.UUID  `json:"user_id" db:"user_id"`
    Token     string     `json:"token" db:"token"`
    ExpiresAt time.Time  `json:"expires_at" db:"expires_at"`
    UsedAt    *time.Time `json:"used_at" db:"used_at"`
    CreatedAt time.Time  `json:"created_at" db:"created_at"`
}

// APIKey represents a Majordomo API key stored in the database
type APIKey struct {
	ID           uuid.UUID  `json:"id" db:"id"`
	KeyHash      string     `json:"-" db:"key_hash"` // Never expose in JSON
	Name         string     `json:"name" db:"name"`
	Description  *string    `json:"description,omitempty" db:"description"`
	UserID       *uuid.UUID `json:"user_id,omitempty" db:"user_id"`
	OrgID        *uuid.UUID `json:"org_id,omitempty" db:"org_id"`
	IsActive     bool       `json:"is_active" db:"is_active"`
	CreatedAt    time.Time  `json:"created_at" db:"created_at"`
	RevokedAt    *time.Time `json:"revoked_at,omitempty" db:"revoked_at"`
	LastUsedAt   *time.Time `json:"last_used_at,omitempty" db:"last_used_at"`
	RequestCount int64      `json:"request_count" db:"request_count"`
}

// CreateAPIKeyInput contains fields for creating a new API key
type CreateAPIKeyInput struct {
	Name        string
	Description *string
	UserID      *uuid.UUID
	OrgID       *uuid.UUID
}

// UpdateAPIKeyInput contains fields for updating an API key
type UpdateAPIKeyInput struct {
	Name        *string
	Description *string
}

// APIKeyInfo contains resolved API key information for request processing
type APIKeyInfo struct {
	ID     uuid.UUID  // Database ID for FK reference
	Hash   string     // SHA256 hash of the key
	Alias  *string    // Optional alias (key name)
	UserID *uuid.UUID // Owning user (if key belongs to a user)
	OrgID  *uuid.UUID // Owning organization (if key belongs to an org)
}

type UsageMetrics struct {
	Provider            string
	Model               string
	InputTokens         int
	OutputTokens        int
	CachedTokens        int
	CacheCreationTokens int
	ResponseTime        time.Duration
}

type Cost struct {
	InputCost       float64
	OutputCost      float64
	TotalCost       float64
	ModelAliasFound bool
}

// ProxyKey represents a customer-facing proxy key that maps to real provider keys
type ProxyKey struct {
	ID                uuid.UUID  `json:"id" db:"id"`
	KeyHash           string     `json:"-" db:"key_hash"`
	Name              string     `json:"name" db:"name"`
	Description       *string    `json:"description,omitempty" db:"description"`
	MajordomoAPIKeyID uuid.UUID  `json:"majordomo_api_key_id" db:"majordomo_api_key_id"`
	IsActive          bool       `json:"is_active" db:"is_active"`
	CreatedAt         time.Time  `json:"created_at" db:"created_at"`
	RevokedAt         *time.Time `json:"revoked_at,omitempty" db:"revoked_at"`
	LastUsedAt        *time.Time `json:"last_used_at,omitempty" db:"last_used_at"`
	RequestCount      int64      `json:"request_count" db:"request_count"`
}

// CreateProxyKeyInput contains fields for creating a new proxy key
type CreateProxyKeyInput struct {
	Name        string
	Description *string
}

// ProviderMapping maps a proxy key to an encrypted provider API key for a specific provider
type ProviderMapping struct {
	ID           uuid.UUID `json:"id" db:"id"`
	ProxyKeyID   uuid.UUID `json:"proxy_key_id" db:"proxy_key_id"`
	Provider     string    `json:"provider" db:"provider"`
	EncryptedKey string    `json:"-" db:"encrypted_key"`
	CreatedAt    time.Time `json:"created_at" db:"created_at"`
	UpdatedAt    time.Time `json:"updated_at" db:"updated_at"`
}

type RequestLog struct {
	ID uuid.UUID `json:"id" db:"id"`

	// Majordomo API key (validated, for tracking)
	MajordomoAPIKeyID *uuid.UUID `json:"majordomo_api_key_id,omitempty" db:"majordomo_api_key_id"`

	// User who owns the API key
	UserID *uuid.UUID `json:"user_id,omitempty" db:"user_id"`

	// Organization that owns the API key
	OrgID *uuid.UUID `json:"org_id,omitempty" db:"org_id"`

	// Proxy key (if request used a proxy key)
	ProxyKeyID *uuid.UUID `json:"proxy_key_id,omitempty" db:"proxy_key_id"`

	// Provider API key (hashed Authorization header)
	ProviderAPIKeyHash  *string `json:"provider_api_key_hash,omitempty" db:"provider_api_key_hash"`
	ProviderAPIKeyAlias *string `json:"provider_api_key_alias,omitempty" db:"provider_api_key_alias"`

	Provider      string `json:"provider" db:"provider"`
	Model         string `json:"model" db:"model"`
	RequestPath   string `json:"request_path" db:"request_path"`
	RequestMethod string `json:"request_method" db:"request_method"`

	RequestedAt    time.Time `json:"requested_at" db:"requested_at"`
	RespondedAt    time.Time `json:"responded_at" db:"responded_at"`
	ResponseTimeMs int64     `json:"response_time_ms" db:"response_time_ms"`

	InputTokens         int `json:"input_tokens" db:"input_tokens"`
	OutputTokens        int `json:"output_tokens" db:"output_tokens"`
	CachedTokens        int `json:"cached_tokens" db:"cached_tokens"`
	CacheCreationTokens int `json:"cache_creation_tokens" db:"cache_creation_tokens"`

	InputCost  float64 `json:"input_cost" db:"input_cost"`
	OutputCost float64 `json:"output_cost" db:"output_cost"`
	TotalCost  float64 `json:"total_cost" db:"total_cost"`

	StatusCode   int     `json:"status_code" db:"status_code"`
	ErrorMessage *string `json:"error_message,omitempty" db:"error_message"`

	RawMetadata     map[string]string `json:"raw_metadata,omitempty" db:"raw_metadata"`
	IndexedMetadata map[string]string `json:"indexed_metadata,omitempty" db:"indexed_metadata"`
	RequestBody     *string           `json:"request_body,omitempty" db:"request_body"`
	ResponseBody    *string           `json:"response_body,omitempty" db:"response_body"`
	BodyS3Key       *string           `json:"body_s3_key,omitempty" db:"body_s3_key"`
	ModelAliasFound bool              `json:"model_alias_found" db:"model_alias_found"`

	CreatedAt time.Time `json:"created_at" db:"created_at"`

	// Transient — not persisted in llm_requests, carried through the async
	// write channel so claude_request_details is written after the FK target exists.
	ClaudeMetadata *ClaudeRequestMetadata `json:"-" db:"-"`
}

// ClaudeRequestMetadata holds parsed Claude Code metadata attached to a RequestLog
// for deferred writing after the llm_requests row is committed.
type ClaudeRequestMetadata struct {
	SessionID             *uuid.UUID
	MessageCount          int
	UserMessageCount      int
	AssistantMessageCount int
	ToolNames             []string
	ToolUseCount          int
	HasThinking           bool
	IsPlanMode            bool
	StopReason            string
	SystemPromptHash      string
}

// Organization represents a team/organization for shared API key and reporting management.
type Organization struct {
	ID                          uuid.UUID `json:"id" db:"id"`
	Name                        string    `json:"name" db:"name"`
	Slug                        string    `json:"slug" db:"slug"`
	CloudStorageProvider        *string   `json:"cloud_storage_provider,omitempty" db:"cloud_storage_provider"`
	S3Bucket                    *string   `json:"s3_bucket,omitempty" db:"s3_bucket"`
	S3Region                    *string   `json:"s3_region,omitempty" db:"s3_region"`
	S3Endpoint                  *string   `json:"s3_endpoint,omitempty" db:"s3_endpoint"`
	S3AccessKeyIDEncrypted      *string   `json:"-" db:"s3_access_key_id_encrypted"`
	S3SecretAccessKeyEncrypted  *string   `json:"-" db:"s3_secret_access_key_encrypted"`
	GCSBucket                   *string   `json:"gcs_bucket,omitempty" db:"gcs_bucket"`
	GCSProjectID                *string   `json:"gcs_project_id,omitempty" db:"gcs_project_id"`
	GCSCredentialsJSONEncrypted *string   `json:"-" db:"gcs_credentials_json_encrypted"`
	CreatedAt                   time.Time `json:"created_at" db:"created_at"`
}

// OrganizationMember represents a user's membership in an organization.
type OrganizationMember struct {
	OrgID     uuid.UUID `json:"org_id" db:"org_id"`
	UserID    uuid.UUID `json:"user_id" db:"user_id"`
	Role      string    `json:"role" db:"role"`
	CreatedAt time.Time `json:"created_at" db:"created_at"`
	Username  string    `json:"username,omitempty" db:"username"`
	Email     *string   `json:"email,omitempty" db:"email"`
}

// OrganizationInvite represents a pending invitation to join an organization.
type OrganizationInvite struct {
	ID         uuid.UUID  `json:"id" db:"id"`
	OrgID      uuid.UUID  `json:"org_id" db:"org_id"`
	Email      string     `json:"email" db:"email"`
	Role       string     `json:"role" db:"role"`
	Token      string     `json:"-" db:"token"`
	InvitedBy  uuid.UUID  `json:"invited_by" db:"invited_by"`
	ExpiresAt  time.Time  `json:"expires_at" db:"expires_at"`
	AcceptedAt *time.Time `json:"accepted_at,omitempty" db:"accepted_at"`
	CreatedAt  time.Time  `json:"created_at" db:"created_at"`
}

// CreateOrganizationInput contains fields for creating a new organization.
type CreateOrganizationInput struct {
	Name string
	Slug string
}

// UpdateOrganizationInput contains fields for updating an organization.
type UpdateOrganizationInput struct {
	Name *string
	Slug *string
}

// CreateInviteInput contains fields for creating an organization invite.
type CreateInviteInput struct {
	Email string
	Role  string
}

// WaitlistEntry represents a waitlist signup stored in the database.
type WaitlistEntry struct {
	ID        uuid.UUID  `json:"id" db:"id"`
	Email     string     `json:"email" db:"email"`
	Source    *string    `json:"source,omitempty" db:"source"`
	CreatedAt time.Time  `json:"created_at" db:"created_at"`
}
