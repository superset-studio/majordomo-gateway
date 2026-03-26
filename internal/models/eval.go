package models

import (
	"encoding/json"
	"time"

	"github.com/google/uuid"
)

// EvalSet is a named, reusable collection of logged request IDs.
type EvalSet struct {
	ID          uuid.UUID  `json:"id" db:"id"`
	UserID      uuid.UUID  `json:"userId" db:"user_id"`
	OrgID       *uuid.UUID `json:"orgId,omitempty" db:"org_id"`
	Name        string     `json:"name" db:"name"`
	Description *string    `json:"description,omitempty" db:"description"`
	ItemCount   int        `json:"itemCount" db:"item_count"`
	CreatedAt   time.Time  `json:"createdAt" db:"created_at"`
	UpdatedAt   time.Time  `json:"updatedAt" db:"updated_at"`
}

// EvalSetItem links an eval set to a logged request.
type EvalSetItem struct {
	ID        uuid.UUID `json:"id" db:"id"`
	EvalSetID uuid.UUID `json:"evalSetId" db:"eval_set_id"`
	RequestID uuid.UUID `json:"requestId" db:"request_id"`
	CreatedAt time.Time `json:"createdAt" db:"created_at"`
}

// EvalRun represents an eval job with its configuration and results.
type EvalRun struct {
	ID           uuid.UUID  `json:"id" db:"id"`
	UserID       uuid.UUID  `json:"userId" db:"user_id"`
	OrgID        *uuid.UUID `json:"orgId,omitempty" db:"org_id"`
	EvalSetID    uuid.UUID  `json:"evalSetId" db:"eval_set_id"`
	Status       string     `json:"status" db:"status"`
	ErrorMessage *string    `json:"errorMessage,omitempty" db:"error_message"`

	TargetProvider string `json:"targetProvider" db:"target_provider"`
	TargetModel    string `json:"targetModel" db:"target_model"`

	// Evaluator configuration (raw JSONB)
	Evaluators json.RawMessage `json:"-" db:"evaluators"`
	// Per-evaluator aggregate scores (raw JSONB, populated on completion)
	EvaluatorSummary json.RawMessage `json:"-" db:"evaluator_summary"`

	// Summary stats (populated on completion)
	TotalRequests        *int     `json:"totalRequests,omitempty" db:"total_requests"`
	SuccessfulRequests   *int     `json:"successfulRequests,omitempty" db:"successful_requests"`
	FailedRequests       *int     `json:"failedRequests,omitempty" db:"failed_requests"`
	OriginalTotalCost    *float64 `json:"originalTotalCost,omitempty" db:"original_total_cost"`
	ReplayTotalCost      *float64 `json:"replayTotalCost,omitempty" db:"replay_total_cost"`
	JudgeTotalCost       *float64 `json:"judgeTotalCost,omitempty" db:"judge_total_cost"`
	OriginalAvgLatencyMs *int     `json:"originalAvgLatencyMs,omitempty" db:"original_avg_latency_ms"`
	ReplayAvgLatencyMs   *int     `json:"replayAvgLatencyMs,omitempty" db:"replay_avg_latency_ms"`

	StartedAt   *time.Time `json:"startedAt,omitempty" db:"started_at"`
	CompletedAt *time.Time `json:"completedAt,omitempty" db:"completed_at"`
	CreatedAt   time.Time  `json:"createdAt" db:"created_at"`

	// Parsed JSONB fields for JSON response (not from DB)
	EvaluatorsParsed       []any `json:"evaluators" db:"-"`
	EvaluatorSummaryParsed []any `json:"evaluatorSummary,omitempty" db:"-"`
}

// ParseJSONFields unmarshals raw JSONB fields into their parsed counterparts.
func (r *EvalRun) ParseJSONFields() {
	if len(r.Evaluators) > 0 {
		json.Unmarshal(r.Evaluators, &r.EvaluatorsParsed)
	}
	if r.EvaluatorsParsed == nil {
		r.EvaluatorsParsed = []any{}
	}
	if len(r.EvaluatorSummary) > 0 {
		json.Unmarshal(r.EvaluatorSummary, &r.EvaluatorSummaryParsed)
	}
}

// EvalRunListItem is a lightweight view for listing eval runs.
type EvalRunListItem struct {
	ID                   uuid.UUID  `json:"id" db:"id"`
	EvalSetID            uuid.UUID  `json:"evalSetId" db:"eval_set_id"`
	Status               string     `json:"status" db:"status"`
	TargetProvider       string     `json:"targetProvider" db:"target_provider"`
	TargetModel          string     `json:"targetModel" db:"target_model"`
	TotalRequests        *int       `json:"totalRequests,omitempty" db:"total_requests"`
	SuccessfulRequests   *int       `json:"successfulRequests,omitempty" db:"successful_requests"`
	FailedRequests       *int       `json:"failedRequests,omitempty" db:"failed_requests"`
	OriginalTotalCost    *float64   `json:"originalTotalCost,omitempty" db:"original_total_cost"`
	ReplayTotalCost      *float64   `json:"replayTotalCost,omitempty" db:"replay_total_cost"`
	JudgeTotalCost       *float64   `json:"judgeTotalCost,omitempty" db:"judge_total_cost"`
	CreatedAt            time.Time  `json:"createdAt" db:"created_at"`
	EvalSetName          string     `json:"evalSetName" db:"eval_set_name"`
}

// EvalResult stores the per-request result for an eval run.
type EvalResult struct {
	ID              uuid.UUID `json:"id" db:"id"`
	EvalRunID       uuid.UUID `json:"evalRunId" db:"eval_run_id"`
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

	ErrorMessage *string   `json:"errorMessage,omitempty" db:"error_message"`
	CreatedAt    time.Time `json:"createdAt" db:"created_at"`

	// Scores populated by secondary query (not from main SELECT)
	Scores []EvalResultScore `json:"scores,omitempty" db:"-"`
}

// EvalResultScore stores a single evaluator's score for an eval result.
type EvalResultScore struct {
	ID            uuid.UUID `json:"id" db:"id"`
	EvalResultID  uuid.UUID `json:"evalResultId" db:"eval_result_id"`
	EvaluatorName string    `json:"evaluatorName" db:"evaluator_name"`
	Score         float64   `json:"score" db:"score"`
	Reason        *string   `json:"reason,omitempty" db:"reason"`
	CreatedAt     time.Time `json:"createdAt" db:"created_at"`
}
