package provider

import (
	"testing"
)

func TestAnthropicParser_ParseResponse(t *testing.T) {
	parser := &AnthropicParser{}

	tests := []struct {
		name              string
		responseBody      []byte
		wantInput         int
		wantOutput        int
		wantCached        int
		wantCacheCreation int
		wantModel         string
		wantErr           bool
	}{
		{
			name: "claude-3-5-sonnet standard response",
			responseBody: []byte(`{
				"id": "msg_01XFDUDYJgAACzvnptvVoYEL",
				"type": "message",
				"role": "assistant",
				"content": [
					{"type": "text", "text": "Hello! How can I help you today?"}
				],
				"model": "claude-3-5-sonnet-20241022",
				"stop_reason": "end_turn",
				"stop_sequence": null,
				"usage": {
					"input_tokens": 12,
					"output_tokens": 15
				}
			}`),
			wantInput:  12,
			wantOutput: 15,
			wantCached: 0,
			wantModel:  "claude-3-5-sonnet-20241022",
		},
		{
			name: "claude-3-opus with cache_read_input_tokens",
			responseBody: []byte(`{
				"id": "msg_abc123def456",
				"type": "message",
				"role": "assistant",
				"content": [{"type": "text", "text": "Based on the cached context..."}],
				"model": "claude-3-opus-20240229",
				"stop_reason": "end_turn",
				"usage": {
					"input_tokens": 150,
					"output_tokens": 89,
					"cache_read_input_tokens": 2048
				}
			}`),
			wantInput:  2198, // 150 + 2048 (normalized total)
			wantOutput: 89,
			wantCached: 2048,
			wantModel:  "claude-3-opus-20240229",
		},
		{
			name: "cache_creation_input_tokens only",
			responseBody: []byte(`{
				"id": "msg_cache_create",
				"type": "message",
				"role": "assistant",
				"content": [{"type": "text", "text": "Caching established."}],
				"model": "claude-sonnet-4-20250514",
				"stop_reason": "end_turn",
				"usage": {
					"input_tokens": 100,
					"output_tokens": 50,
					"cache_creation_input_tokens": 5000
				}
			}`),
			wantInput:         5100, // 100 + 5000 (normalized total)
			wantOutput:        50,
			wantCached:        0,
			wantCacheCreation: 5000,
			wantModel:         "claude-sonnet-4-20250514",
		},
		{
			name: "both cache_creation and cache_read",
			responseBody: []byte(`{
				"id": "msg_both_cache",
				"type": "message",
				"role": "assistant",
				"content": [{"type": "text", "text": "Mixed cache scenario."}],
				"model": "claude-sonnet-4-20250514",
				"stop_reason": "end_turn",
				"usage": {
					"input_tokens": 200,
					"output_tokens": 75,
					"cache_read_input_tokens": 1024,
					"cache_creation_input_tokens": 3000
				}
			}`),
			wantInput:         4224, // 200 + 1024 + 3000 (normalized total)
			wantOutput:        75,
			wantCached:        1024,
			wantCacheCreation: 3000,
			wantModel:         "claude-sonnet-4-20250514",
		},
		{
			name: "claude-3-haiku minimal response",
			responseBody: []byte(`{
				"id": "msg_xyz",
				"type": "message",
				"role": "assistant",
				"model": "claude-3-haiku-20240307",
				"content": [],
				"usage": {"input_tokens": 5, "output_tokens": 0}
			}`),
			wantInput:  5,
			wantOutput: 0,
			wantCached: 0,
			wantModel:  "claude-3-haiku-20240307",
		},
		{
			name: "tool use response",
			responseBody: []byte(`{
				"id": "msg_01Aq9w938a90dw8q",
				"type": "message",
				"role": "assistant",
				"content": [
					{
						"type": "tool_use",
						"id": "toolu_01A09q90qw90lq917835lgs",
						"name": "get_weather",
						"input": {"location": "San Francisco, CA"}
					}
				],
				"model": "claude-3-5-sonnet-20241022",
				"stop_reason": "tool_use",
				"usage": {
					"input_tokens": 345,
					"output_tokens": 76
				}
			}`),
			wantInput:  345,
			wantOutput: 76,
			wantCached: 0,
			wantModel:  "claude-3-5-sonnet-20241022",
		},
		{
			name:         "malformed JSON",
			responseBody: []byte(`{not valid json`),
			wantErr:      true,
		},
		{
			name:         "empty body",
			responseBody: []byte(``),
			wantErr:      true,
		},
		{
			name: "missing usage field returns zero tokens",
			responseBody: []byte(`{
				"id": "msg_abc",
				"model": "claude-3-5-sonnet-20241022"
			}`),
			wantInput:  0,
			wantOutput: 0,
			wantCached: 0,
			wantModel:  "claude-3-5-sonnet-20241022",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result, err := parser.ParseResponse(tt.responseBody)
			if tt.wantErr {
				if err == nil {
					t.Error("expected error, got nil")
				}
				return
			}
			if err != nil {
				t.Fatalf("unexpected error: %v", err)
			}
			if result.InputTokens != tt.wantInput {
				t.Errorf("InputTokens = %d, want %d", result.InputTokens, tt.wantInput)
			}
			if result.OutputTokens != tt.wantOutput {
				t.Errorf("OutputTokens = %d, want %d", result.OutputTokens, tt.wantOutput)
			}
			if result.CachedTokens != tt.wantCached {
				t.Errorf("CachedTokens = %d, want %d", result.CachedTokens, tt.wantCached)
			}
			if result.CacheCreationTokens != tt.wantCacheCreation {
				t.Errorf("CacheCreationTokens = %d, want %d", result.CacheCreationTokens, tt.wantCacheCreation)
			}
			if result.Model != tt.wantModel {
				t.Errorf("Model = %q, want %q", result.Model, tt.wantModel)
			}
			if result.Provider != "anthropic" {
				t.Errorf("Provider = %q, want %q", result.Provider, "anthropic")
			}
		})
	}
}
