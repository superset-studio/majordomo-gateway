package models

import (
	"time"

	"github.com/google/uuid"
)

type APIKeyInfo struct {
	Hash  string
	Alias *string
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
	ID          uuid.UUID `json:"id" db:"id"`
	APIKeyHash  string    `json:"api_key_hash" db:"api_key_hash"`
	APIKeyAlias *string   `json:"api_key_alias,omitempty" db:"api_key_alias"`

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
