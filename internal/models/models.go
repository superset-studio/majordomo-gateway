package models

import (
	"time"

	"github.com/google/uuid"
)

// APIKey represents a Majordomo API key stored in the database
type APIKey struct {
	ID           uuid.UUID  `json:"id" db:"id"`
	KeyHash      string     `json:"-" db:"key_hash"` // Never expose in JSON
	Name         string     `json:"name" db:"name"`
	Description  *string    `json:"description,omitempty" db:"description"`
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
}

// UpdateAPIKeyInput contains fields for updating an API key
type UpdateAPIKeyInput struct {
	Name        *string
	Description *string
}

// APIKeyInfo contains resolved API key information for request processing
type APIKeyInfo struct {
	ID    uuid.UUID // Database ID for FK reference
	Hash  string    // SHA256 hash of the key
	Alias *string   // Optional alias (key name)
}

type UsageMetrics struct {
	Provider     string
	Model        string
	InputTokens  int
	OutputTokens int
	CachedTokens int
	ResponseTime time.Duration
}

type Cost struct {
	InputCost       float64
	OutputCost      float64
	TotalCost       float64
	ModelAliasFound bool
}

type RequestLog struct {
	ID uuid.UUID `json:"id" db:"id"`

	// Majordomo API key (validated, for tracking)
	MajordomoAPIKeyID *uuid.UUID `json:"majordomo_api_key_id,omitempty" db:"majordomo_api_key_id"`

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

	InputTokens  int `json:"input_tokens" db:"input_tokens"`
	OutputTokens int `json:"output_tokens" db:"output_tokens"`
	CachedTokens int `json:"cached_tokens" db:"cached_tokens"`

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
}
