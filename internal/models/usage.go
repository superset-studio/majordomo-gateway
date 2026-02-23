package models

import (
	"time"

	"github.com/google/uuid"
)

// UsageSummary holds aggregate usage metrics for a time range.
type UsageSummary struct {
	TotalRequests    int64   `json:"total_requests"`
	TotalInputTokens int64   `json:"total_input_tokens"`
	TotalOutputTokens int64  `json:"total_output_tokens"`
	TotalCost        float64 `json:"total_cost"`
}

// DailyUsage holds per-day usage metrics.
type DailyUsage struct {
	Date         string  `json:"date"`
	RequestCount int64   `json:"request_count"`
	InputTokens  int64   `json:"input_tokens"`
	OutputTokens int64   `json:"output_tokens"`
	TotalCost    float64 `json:"total_cost"`
}

// ModelUsage holds usage metrics grouped by provider and model.
type ModelUsage struct {
	Provider     string  `json:"provider"`
	Model        string  `json:"model"`
	RequestCount int64   `json:"request_count"`
	InputTokens  int64   `json:"input_tokens"`
	OutputTokens int64   `json:"output_tokens"`
	TotalCost    float64 `json:"total_cost"`
}

// APIKeyUsage holds usage metrics grouped by API key.
type APIKeyUsage struct {
	APIKeyID     uuid.UUID `json:"api_key_id"`
	APIKeyName   string    `json:"api_key_name"`
	RequestCount int64     `json:"request_count"`
	InputTokens  int64     `json:"input_tokens"`
	OutputTokens int64     `json:"output_tokens"`
	TotalCost    float64   `json:"total_cost"`
}

// RequestListItem is a lightweight row for the request log table (no bodies).
type RequestListItem struct {
	ID                uuid.UUID  `json:"id" db:"id"`
	MajordomoAPIKeyID *uuid.UUID `json:"majordomo_api_key_id,omitempty" db:"majordomo_api_key_id"`
	Provider          string     `json:"provider" db:"provider"`
	Model             string     `json:"model" db:"model"`
	RequestedAt       time.Time  `json:"requested_at" db:"requested_at"`
	ResponseTimeMs    int64      `json:"response_time_ms" db:"response_time_ms"`
	InputTokens       int        `json:"input_tokens" db:"input_tokens"`
	OutputTokens      int        `json:"output_tokens" db:"output_tokens"`
	TotalCost         float64    `json:"total_cost" db:"total_cost"`
	StatusCode        int        `json:"status_code" db:"status_code"`
	ErrorMessage      *string    `json:"error_message,omitempty" db:"error_message"`
}
