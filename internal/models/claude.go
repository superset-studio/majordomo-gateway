package models

import (
	"time"

	"github.com/google/uuid"
	"github.com/lib/pq"
)

// ClaudeSession represents a Claude Code session tracked by the gateway.
type ClaudeSession struct {
	ID                uuid.UUID  `json:"id" db:"id"`
	MajordomoAPIKeyID uuid.UUID  `json:"majordomoApiKeyId" db:"majordomo_api_key_id"`
	OrgID             *uuid.UUID `json:"orgId,omitempty" db:"org_id"`
	SessionName       *string    `json:"sessionName,omitempty" db:"session_name"`
	StartedAt         time.Time  `json:"startedAt" db:"started_at"`
	EndedAt           *time.Time `json:"endedAt,omitempty" db:"ended_at"`
	TotalRequests     int        `json:"totalRequests" db:"total_requests"`
	TotalInputTokens  int        `json:"totalInputTokens" db:"total_input_tokens"`
	TotalOutputTokens int        `json:"totalOutputTokens" db:"total_output_tokens"`
	TotalCost         float64    `json:"totalCost" db:"total_cost"`
	CreatedAt         time.Time  `json:"createdAt" db:"created_at"`
}

// ClaudeRequestDetail holds parsed metadata from a Claude Code request/response pair.
type ClaudeRequestDetail struct {
	ID                    uuid.UUID      `json:"id" db:"id"`
	LLMRequestID          uuid.UUID      `json:"llmRequestId" db:"llm_request_id"`
	SessionID             *uuid.UUID     `json:"sessionId,omitempty" db:"session_id"`
	MessageCount          int            `json:"messageCount" db:"message_count"`
	UserMessageCount      int            `json:"userMessageCount" db:"user_message_count"`
	AssistantMessageCount int            `json:"assistantMessageCount" db:"assistant_message_count"`
	ToolNames             pq.StringArray `json:"toolNames" db:"tool_names"`
	ToolUseCount          int            `json:"toolUseCount" db:"tool_use_count"`
	HasThinking           bool           `json:"hasThinking" db:"has_thinking"`
	IsPlanMode            bool           `json:"isPlanMode" db:"is_plan_mode"`
	StopReason            *string        `json:"stopReason,omitempty" db:"stop_reason"`
	SystemPromptHash      *string        `json:"systemPromptHash,omitempty" db:"system_prompt_hash"`
	CreatedAt             time.Time      `json:"createdAt" db:"created_at"`
}

// ClaudeSummary holds aggregate metrics for Claude Code sessions.
type ClaudeSummary struct {
	TotalSessions            int64   `json:"totalSessions" db:"total_sessions"`
	TotalCost                float64 `json:"totalCost" db:"total_cost"`
	AvgDurationMinutes       float64 `json:"avgDurationMinutes" db:"avg_duration_minutes"`
	AvgCostPerSession        float64 `json:"avgCostPerSession" db:"avg_cost_per_session"`
	AvgRequestsPerSession    float64 `json:"avgRequestsPerSession" db:"avg_requests_per_session"`
	CacheHitRate             float64 `json:"cacheHitRate" db:"cache_hit_rate"`
	ThinkingRate             float64 `json:"thinkingRate" db:"thinking_rate"`
	PlanModeRate             float64 `json:"planModeRate" db:"plan_mode_rate"`
	TotalCacheCreationTokens int64   `json:"totalCacheCreationTokens" db:"total_cache_creation_tokens"`
	TotalCachedTokens        int64   `json:"totalCachedTokens" db:"total_cached_tokens"`
	TotalInputTokens         int64   `json:"totalInputTokens" db:"total_input_tokens"`
}

// ClaudeModelUsage holds per-model request and cost metrics.
type ClaudeModelUsage struct {
	Model        string  `json:"model" db:"model"`
	RequestCount int64   `json:"requestCount" db:"request_count"`
	InputTokens  int64   `json:"inputTokens" db:"input_tokens"`
	OutputTokens int64   `json:"outputTokens" db:"output_tokens"`
	TotalCost    float64 `json:"totalCost" db:"total_cost"`
}

// ClaudeDailyStats holds per-day Claude Code session metrics.
type ClaudeDailyStats struct {
	Date                 string  `json:"date" db:"date"`
	SessionCount         int64   `json:"sessionCount" db:"session_count"`
	TotalCost            float64 `json:"totalCost" db:"total_cost"`
	AvgDurationMinutes   float64 `json:"avgDurationMinutes" db:"avg_duration_minutes"`
	AvgRequestsPerSession float64 `json:"avgRequestsPerSession" db:"avg_requests_per_session"`
}

// ClaudeToolUsage holds tool usage frequency and percentage.
type ClaudeToolUsage struct {
	ToolName   string  `json:"toolName" db:"tool_name"`
	UseCount   int64   `json:"useCount" db:"use_count"`
	Percentage float64 `json:"percentage" db:"percentage"`
}

// ClaudePerformance holds latency percentiles and error rates.
type ClaudePerformance struct {
	P50Ms         float64 `json:"p50Ms" db:"p50_ms"`
	P95Ms         float64 `json:"p95Ms" db:"p95_ms"`
	P99Ms         float64 `json:"p99Ms" db:"p99_ms"`
	TotalRequests int64   `json:"totalRequests" db:"total_requests"`
	ErrorCount    int64   `json:"errorCount" db:"error_count"`
	ErrorRate     float64 `json:"errorRate" db:"error_rate"`
}

// ClaudeSessionListItem holds a session row with aggregated request metadata.
type ClaudeSessionListItem struct {
	ID                uuid.UUID  `json:"id" db:"id"`
	MajordomoAPIKeyID uuid.UUID  `json:"majordomoApiKeyId" db:"majordomo_api_key_id"`
	APIKeyName        string     `json:"apiKeyName" db:"api_key_name"`
	SessionName       *string    `json:"sessionName,omitempty" db:"session_name"`
	StartedAt         time.Time  `json:"startedAt" db:"started_at"`
	EndedAt           *time.Time `json:"endedAt,omitempty" db:"ended_at"`
	DurationMinutes   *float64   `json:"durationMinutes,omitempty" db:"duration_minutes"`
	TotalRequests     int        `json:"totalRequests" db:"total_requests"`
	TotalInputTokens  int        `json:"totalInputTokens" db:"total_input_tokens"`
	TotalOutputTokens int        `json:"totalOutputTokens" db:"total_output_tokens"`
	TotalCost         float64    `json:"totalCost" db:"total_cost"`
	ToolCount         int64      `json:"toolCount" db:"tool_count"`
	ThinkingCount     int64      `json:"thinkingCount" db:"thinking_count"`
	PlanModeCount     int64      `json:"planModeCount" db:"plan_mode_count"`
	CreatedAt         time.Time  `json:"createdAt" db:"created_at"`
}

// ClaudeSessionDetailRow holds an aggregated row grouped by model, tool, and flags.
type ClaudeSessionDetailRow struct {
	Model             string  `json:"model" db:"model"`
	ToolName          string  `json:"toolName" db:"tool_name"`
	HasThinking       bool    `json:"hasThinking" db:"has_thinking"`
	IsPlanMode        bool    `json:"isPlanMode" db:"is_plan_mode"`
	UseCount          int64   `json:"useCount" db:"use_count"`
	InputTokens       int64   `json:"inputTokens" db:"input_tokens"`
	OutputTokens      int64   `json:"outputTokens" db:"output_tokens"`
	CachedTokens      int64   `json:"cachedTokens" db:"cached_tokens"`
	TotalCost         float64 `json:"totalCost" db:"total_cost"`
	AvgResponseTimeMs float64 `json:"avgResponseTimeMs" db:"avg_response_time_ms"`
}

// ClaudeAPIKeyUsage holds per-API-key aggregate metrics for Claude Code sessions.
type ClaudeAPIKeyUsage struct {
	APIKeyID          uuid.UUID `json:"apiKeyId" db:"api_key_id"`
	APIKeyName        string    `json:"apiKeyName" db:"api_key_name"`
	SessionCount      int64     `json:"sessionCount" db:"session_count"`
	TotalRequests     int64     `json:"totalRequests" db:"total_requests"`
	TotalCost         float64   `json:"totalCost" db:"total_cost"`
	TotalInputTokens  int64     `json:"totalInputTokens" db:"total_input_tokens"`
	TotalOutputTokens int64     `json:"totalOutputTokens" db:"total_output_tokens"`
}

// ClaudeSessionDetail holds a full session with aggregated rows.
type ClaudeSessionDetail struct {
	Session *ClaudeSession            `json:"session"`
	Rows    []*ClaudeSessionDetailRow `json:"rows"`
}
