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
