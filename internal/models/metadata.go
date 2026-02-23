package models

import (
	"time"

	"github.com/google/uuid"
)

// MetadataKey represents a discovered metadata key from llm_requests_metadata_keys.
type MetadataKey struct {
	MajordomoAPIKeyID uuid.UUID  `json:"majordomo_api_key_id" db:"majordomo_api_key_id"`
	KeyName           string     `json:"key_name" db:"key_name"`
	DisplayName       *string    `json:"display_name,omitempty" db:"display_name"`
	KeyType           string     `json:"key_type" db:"key_type"`
	IsRequired        bool       `json:"is_required" db:"is_required"`
	IsActive          bool       `json:"is_active" db:"is_active"`
	ActivatedAt       *time.Time `json:"activated_at,omitempty" db:"activated_at"`
	RequestCount      int64      `json:"request_count" db:"request_count"`
	LastSeenAt        *time.Time `json:"last_seen_at,omitempty" db:"last_seen_at"`
	ApproxCardinality int64      `json:"approx_cardinality" db:"approx_cardinality"`
	CreatedAt         time.Time  `json:"created_at" db:"created_at"`
}

// MetadataBreakdown holds usage metrics grouped by a metadata key's values.
type MetadataBreakdown struct {
	Value        string  `json:"value"`
	RequestCount int64   `json:"request_count"`
	InputTokens  int64   `json:"input_tokens"`
	OutputTokens int64   `json:"output_tokens"`
	TotalCost    float64 `json:"total_cost"`
}
