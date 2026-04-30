package models

import (
	"time"

	"github.com/google/uuid"
)

// Experiment is the API-facing representation of an A/B test.
type Experiment struct {
	ID              uuid.UUID          `json:"id" db:"id"`
	UserID          *uuid.UUID         `json:"userId,omitempty" db:"user_id"`
	OrgID           *uuid.UUID         `json:"orgId,omitempty" db:"org_id"`
	APIKeyID        *uuid.UUID         `json:"apiKeyId,omitempty" db:"api_key_id"`
	Name            string             `json:"name" db:"name"`
	Description     *string            `json:"description,omitempty" db:"description"`
	Status          string             `json:"status" db:"status"`
	Sticky          bool               `json:"sticky" db:"sticky"`
	StickyKeyHeader *string            `json:"stickyKeyHeader,omitempty" db:"sticky_key_header"`
	CreatedAt       time.Time          `json:"createdAt" db:"created_at"`
	UpdatedAt       time.Time          `json:"updatedAt" db:"updated_at"`
	Variants        []ExperimentVariant `json:"variants,omitempty" db:"-"`
}

// ExperimentVariant is the API-facing representation of a variant arm.
type ExperimentVariant struct {
	ID           uuid.UUID `json:"id" db:"id"`
	ExperimentID uuid.UUID `json:"experimentId" db:"experiment_id"`
	Name         string    `json:"name" db:"name"`
	Provider     string    `json:"provider" db:"provider"`
	Model        string    `json:"model" db:"model"`
	Weight       int       `json:"weight" db:"weight"`
	IsControl    bool      `json:"isControl" db:"is_control"`
	CreatedAt    time.Time `json:"createdAt" db:"created_at"`
}

// ExperimentListItem is a lightweight view for listing experiments.
type ExperimentListItem struct {
	ID           uuid.UUID  `json:"id" db:"id"`
	APIKeyID     *uuid.UUID `json:"apiKeyId,omitempty" db:"api_key_id"`
	Name         string     `json:"name" db:"name"`
	Status       string     `json:"status" db:"status"`
	Sticky       bool       `json:"sticky" db:"sticky"`
	VariantCount int        `json:"variantCount" db:"variant_count"`
	CreatedAt    time.Time  `json:"createdAt" db:"created_at"`
	UpdatedAt    time.Time  `json:"updatedAt" db:"updated_at"`
}

// ExperimentResults holds aggregated per-variant metrics for an experiment.
type ExperimentResults struct {
	ExperimentID   uuid.UUID                `json:"experimentId"`
	ExperimentName string                   `json:"experimentName"`
	TotalRequests  int                      `json:"totalRequests"`
	Variants       []ExperimentVariantResult `json:"variants"`
}

// ExperimentVariantResult holds aggregated metrics for a single variant.
type ExperimentVariantResult struct {
	VariantName    string  `json:"variantName" db:"variant_name"`
	RequestCount   int     `json:"requestCount" db:"request_count"`
	AvgLatencyMs   float64 `json:"avgLatencyMs" db:"avg_latency_ms"`
	TotalCost      float64 `json:"totalCost" db:"total_cost"`
	AvgInputTokens float64 `json:"avgInputTokens" db:"avg_input_tokens"`
	AvgOutputTokens float64 `json:"avgOutputTokens" db:"avg_output_tokens"`
	ErrorCount     int     `json:"errorCount" db:"error_count"`
	ErrorRate      float64 `json:"errorRate" db:"error_rate"`
}
