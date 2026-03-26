package models

import (
	"time"

	"github.com/google/uuid"
)

// ProviderAPIKey stores an encrypted API key for a specific LLM provider.
type ProviderAPIKey struct {
	ID           uuid.UUID  `json:"id" db:"id"`
	UserID       *uuid.UUID `json:"userId,omitempty" db:"user_id"`
	OrgID        *uuid.UUID `json:"orgId,omitempty" db:"org_id"`
	Provider     string     `json:"provider" db:"provider"`
	EncryptedKey string     `json:"-" db:"encrypted_key"`
	CreatedAt    time.Time  `json:"createdAt" db:"created_at"`
	UpdatedAt    time.Time  `json:"updatedAt" db:"updated_at"`
}

// ProviderKeyInfo is the safe-to-return view (no encrypted key).
type ProviderKeyInfo struct {
	Provider  string    `json:"provider" db:"provider"`
	CreatedAt time.Time `json:"createdAt" db:"created_at"`
}

// LLMProvider represents a supported LLM provider with its available models.
type LLMProvider struct {
	ID       uuid.UUID `json:"id" db:"id"`
	Provider string    `json:"provider" db:"provider"`
	Models   []string  `json:"models" db:"-"` // populated by join query
}

// ReplayRun represents a replay job with its configuration and results.
type ReplayRun struct {
	ID           uuid.UUID  `json:"id" db:"id"`
	UserID       uuid.UUID  `json:"userId" db:"user_id"`
	OrgID        *uuid.UUID `json:"orgId,omitempty" db:"org_id"`
	Status       string     `json:"status" db:"status"`
	ErrorMessage *string    `json:"errorMessage,omitempty" db:"error_message"`

	// Source filters
	SourceAPIKeyID *uuid.UUID `json:"sourceApiKeyId,omitempty" db:"source_api_key_id"`
	SourceProvider *string    `json:"sourceProvider,omitempty" db:"source_provider"`
	SourceModel    *string    `json:"sourceModel,omitempty" db:"source_model"`
	SourceStart    *time.Time `json:"sourceStart,omitempty" db:"source_start"`
	SourceEnd      *time.Time `json:"sourceEnd,omitempty" db:"source_end"`
	SourceMetadata []byte     `json:"-" db:"source_metadata"`
	SourceLimit    int        `json:"sourceLimit" db:"source_limit"`

	// Target model
	TargetProvider string `json:"targetProvider" db:"target_provider"`
	TargetModel    string `json:"targetModel" db:"target_model"`

	// Judge config
	JudgeEnabled  bool    `json:"judgeEnabled" db:"judge_enabled"`
	JudgeProvider *string `json:"judgeProvider,omitempty" db:"judge_provider"`
	JudgeModel    *string `json:"judgeModel,omitempty" db:"judge_model"`

	// Summary stats (populated on completion)
	TotalRequests       *int     `json:"totalRequests,omitempty" db:"total_requests"`
	ExactMatches        *int     `json:"exactMatches,omitempty" db:"exact_matches"`
	JudgeEquivalent     *int     `json:"judgeEquivalent,omitempty" db:"judge_equivalent"`
	Divergent           *int     `json:"divergent,omitempty" db:"divergent"`
	OriginalTotalCost   *float64 `json:"originalTotalCost,omitempty" db:"original_total_cost"`
	ReplayTotalCost     *float64 `json:"replayTotalCost,omitempty" db:"replay_total_cost"`
	OriginalAvgLatencyMs *int    `json:"originalAvgLatencyMs,omitempty" db:"original_avg_latency_ms"`
	ReplayAvgLatencyMs  *int     `json:"replayAvgLatencyMs,omitempty" db:"replay_avg_latency_ms"`

	StartedAt   *time.Time `json:"startedAt,omitempty" db:"started_at"`
	CompletedAt *time.Time `json:"completedAt,omitempty" db:"completed_at"`
	CreatedAt   time.Time  `json:"createdAt" db:"created_at"`

	// Parsed source_metadata for JSON response (not from DB)
	SourceMetadataParsed map[string]string `json:"sourceMetadata,omitempty" db:"-"`
}

// ReplayRunListItem is a lightweight view for listing runs.
type ReplayRunListItem struct {
	ID              uuid.UUID  `json:"id" db:"id"`
	Status          string     `json:"status" db:"status"`
	SourceModel     *string    `json:"sourceModel,omitempty" db:"source_model"`
	TargetProvider  string     `json:"targetProvider" db:"target_provider"`
	TargetModel     string     `json:"targetModel" db:"target_model"`
	TotalRequests   *int       `json:"totalRequests,omitempty" db:"total_requests"`
	ExactMatches    *int       `json:"exactMatches,omitempty" db:"exact_matches"`
	JudgeEquivalent *int       `json:"judgeEquivalent,omitempty" db:"judge_equivalent"`
	Divergent       *int       `json:"divergent,omitempty" db:"divergent"`
	OriginalTotalCost *float64 `json:"originalTotalCost,omitempty" db:"original_total_cost"`
	ReplayTotalCost   *float64 `json:"replayTotalCost,omitempty" db:"replay_total_cost"`
	CreatedAt       time.Time  `json:"createdAt" db:"created_at"`
}

// ReplayResult stores the per-request comparison for a replay run.
type ReplayResult struct {
	ID              uuid.UUID `json:"id" db:"id"`
	ReplayRunID     uuid.UUID `json:"replayRunId" db:"replay_run_id"`
	SourceRequestID uuid.UUID `json:"sourceRequestId" db:"source_request_id"`

	OriginalProvider     string  `json:"originalProvider" db:"original_provider"`
	OriginalModel        string  `json:"originalModel" db:"original_model"`
	OriginalCost         float64 `json:"originalCost" db:"original_cost"`
	OriginalLatencyMs    int     `json:"originalLatencyMs" db:"original_latency_ms"`
	OriginalInputTokens  int     `json:"originalInputTokens" db:"original_input_tokens"`
	OriginalOutputTokens int     `json:"originalOutputTokens" db:"original_output_tokens"`

	ReplayResponse     *string  `json:"replayResponse,omitempty" db:"replay_response"`
	ReplayCost         *float64 `json:"replayCost,omitempty" db:"replay_cost"`
	ReplayLatencyMs    *int     `json:"replayLatencyMs,omitempty" db:"replay_latency_ms"`
	ReplayInputTokens  *int     `json:"replayInputTokens,omitempty" db:"replay_input_tokens"`
	ReplayOutputTokens *int     `json:"replayOutputTokens,omitempty" db:"replay_output_tokens"`

	ExactMatch      *bool   `json:"exactMatch,omitempty" db:"exact_match"`
	JudgeEquivalent *bool   `json:"judgeEquivalent,omitempty" db:"judge_equivalent"`
	JudgeReason     *string `json:"judgeReason,omitempty" db:"judge_reason"`

	ErrorMessage *string   `json:"errorMessage,omitempty" db:"error_message"`
	CreatedAt    time.Time `json:"createdAt" db:"created_at"`
}
