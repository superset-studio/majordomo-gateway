package provider

import (
	"testing"
)

func TestOpenAIParser_ParseResponse(t *testing.T) {
	parser := &OpenAIParser{}

	tests := []struct {
		name         string
		responseBody []byte
		wantInput    int
		wantOutput   int
		wantCached   int
		wantModel    string
		wantErr      bool
	}{
		{
			name: "gpt-4o chat completion",
			responseBody: []byte(`{
				"id": "chatcmpl-abc123",
				"object": "chat.completion",
				"created": 1677858242,
				"model": "gpt-4o-2024-08-06",
				"choices": [
					{
						"index": 0,
						"message": {
							"role": "assistant",
							"content": "Hello! How can I assist you today?"
						},
						"finish_reason": "stop"
					}
				],
				"usage": {
					"prompt_tokens": 13,
					"completion_tokens": 9,
					"total_tokens": 22
				}
			}`),
			wantInput:  13,
			wantOutput: 9,
			wantCached: 0,
			wantModel:  "gpt-4o-2024-08-06",
		},
		{
			name: "gpt-4o with cached tokens",
			responseBody: []byte(`{
				"id": "chatcmpl-xyz789",
				"object": "chat.completion",
				"created": 1677858242,
				"model": "gpt-4o-2024-08-06",
				"choices": [{"index": 0, "message": {"role": "assistant", "content": "Hi"}, "finish_reason": "stop"}],
				"usage": {
					"prompt_tokens": 1024,
					"completion_tokens": 256,
					"total_tokens": 1280,
					"prompt_tokens_details": {
						"cached_tokens": 512
					}
				}
			}`),
			wantInput:  1024,
			wantOutput: 256,
			wantCached: 512,
			wantModel:  "gpt-4o-2024-08-06",
		},
		{
			name: "gpt-3.5-turbo legacy format",
			responseBody: []byte(`{
				"id": "chatcmpl-legacy",
				"object": "chat.completion",
				"model": "gpt-3.5-turbo-0125",
				"choices": [{"message": {"content": "Response"}, "finish_reason": "stop"}],
				"usage": {
					"prompt_tokens": 50,
					"completion_tokens": 25,
					"total_tokens": 75
				}
			}`),
			wantInput:  50,
			wantOutput: 25,
			wantCached: 0,
			wantModel:  "gpt-3.5-turbo-0125",
		},
		{
			name: "Responses API format (o1/o3 models)",
			responseBody: []byte(`{
				"id": "resp_abc123",
				"object": "response",
				"created_at": 1710000000,
				"model": "o1-2024-12-17",
				"output": [
					{
						"type": "message",
						"id": "msg_123",
						"role": "assistant",
						"content": [{"type": "output_text", "text": "Reasoning complete."}]
					}
				],
				"usage": {
					"input_tokens": 200,
					"output_tokens": 150,
					"total_tokens": 350,
					"input_tokens_details": {
						"cached_tokens": 100
					},
					"output_tokens_details": {
						"reasoning_tokens": 80
					}
				}
			}`),
			wantInput:  200,
			wantOutput: 150,
			wantCached: 100,
			wantModel:  "o1-2024-12-17",
		},
		{
			name: "Responses API without cache",
			responseBody: []byte(`{
				"id": "resp_xyz",
				"object": "response",
				"model": "o3-mini-2025-01-31",
				"usage": {
					"input_tokens": 500,
					"output_tokens": 1000,
					"total_tokens": 1500
				}
			}`),
			wantInput:  500,
			wantOutput: 1000,
			wantCached: 0,
			wantModel:  "o3-mini-2025-01-31",
		},
		{
			name: "function calling response",
			responseBody: []byte(`{
				"id": "chatcmpl-func",
				"object": "chat.completion",
				"model": "gpt-4o-2024-08-06",
				"choices": [
					{
						"index": 0,
						"message": {
							"role": "assistant",
							"content": null,
							"tool_calls": [
								{
									"id": "call_abc123",
									"type": "function",
									"function": {
										"name": "get_weather",
										"arguments": "{\"location\":\"Boston\"}"
									}
								}
							]
						},
						"finish_reason": "tool_calls"
					}
				],
				"usage": {
					"prompt_tokens": 82,
					"completion_tokens": 17,
					"total_tokens": 99
				}
			}`),
			wantInput:  82,
			wantOutput: 17,
			wantCached: 0,
			wantModel:  "gpt-4o-2024-08-06",
		},
		{
			name: "embeddings response",
			responseBody: []byte(`{
				"object": "list",
				"data": [
					{
						"object": "embedding",
						"embedding": [0.0023, -0.009, 0.0157],
						"index": 0
					}
				],
				"model": "text-embedding-3-small",
				"usage": {
					"prompt_tokens": 8,
					"total_tokens": 8
				}
			}`),
			wantInput:  8,
			wantOutput: 0,
			wantCached: 0,
			wantModel:  "text-embedding-3-small",
		},
		{
			name:         "malformed JSON",
			responseBody: []byte(`{"usage": {`),
			wantErr:      true,
		},
		{
			name:         "empty body",
			responseBody: []byte(``),
			wantErr:      true,
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
			if result.Model != tt.wantModel {
				t.Errorf("Model = %q, want %q", result.Model, tt.wantModel)
			}
			if result.Provider != "openai" {
				t.Errorf("Provider = %q, want %q", result.Provider, "openai")
			}
		})
	}
}
