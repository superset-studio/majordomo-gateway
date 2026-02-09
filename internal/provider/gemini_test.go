package provider

import (
	"testing"
)

func TestGeminiParser_ParseResponse(t *testing.T) {
	parser := &GeminiParser{}

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
			name: "gemini-1.5-pro generateContent response",
			responseBody: []byte(`{
				"candidates": [
					{
						"content": {
							"parts": [{"text": "Hello! How can I help you today?"}],
							"role": "model"
						},
						"finishReason": "STOP",
						"index": 0
					}
				],
				"usageMetadata": {
					"promptTokenCount": 10,
					"candidatesTokenCount": 12,
					"totalTokenCount": 22
				},
				"modelVersion": "gemini-1.5-pro-002"
			}`),
			wantInput:  10,
			wantOutput: 12,
			wantModel:  "gemini-1.5-pro-002",
		},
		{
			name: "gemini-1.5-flash response",
			responseBody: []byte(`{
				"candidates": [
					{
						"content": {
							"parts": [{"text": "Quick response"}],
							"role": "model"
						},
						"finishReason": "STOP"
					}
				],
				"usageMetadata": {
					"promptTokenCount": 5,
					"candidatesTokenCount": 3,
					"totalTokenCount": 8
				},
				"modelVersion": "gemini-1.5-flash-002"
			}`),
			wantInput:  5,
			wantOutput: 3,
			wantModel:  "gemini-1.5-flash-002",
		},
		{
			name: "gemini-2.0-flash response",
			responseBody: []byte(`{
				"candidates": [
					{
						"content": {
							"parts": [{"text": "Latest model response"}],
							"role": "model"
						},
						"finishReason": "STOP"
					}
				],
				"usageMetadata": {
					"promptTokenCount": 100,
					"candidatesTokenCount": 200,
					"totalTokenCount": 300
				},
				"modelVersion": "gemini-2.0-flash-001"
			}`),
			wantInput:  100,
			wantOutput: 200,
			wantModel:  "gemini-2.0-flash-001",
		},
		{
			name: "function calling response",
			responseBody: []byte(`{
				"candidates": [
					{
						"content": {
							"parts": [
								{
									"functionCall": {
										"name": "get_current_weather",
										"args": {"location": "Boston, MA"}
									}
								}
							],
							"role": "model"
						},
						"finishReason": "STOP"
					}
				],
				"usageMetadata": {
					"promptTokenCount": 50,
					"candidatesTokenCount": 25,
					"totalTokenCount": 75
				},
				"modelVersion": "gemini-1.5-pro-002"
			}`),
			wantInput:  50,
			wantOutput: 25,
			wantModel:  "gemini-1.5-pro-002",
		},
		{
			name: "safety blocked response (no candidates)",
			responseBody: []byte(`{
				"promptFeedback": {
					"blockReason": "SAFETY"
				},
				"usageMetadata": {
					"promptTokenCount": 15,
					"candidatesTokenCount": 0,
					"totalTokenCount": 15
				},
				"modelVersion": "gemini-1.5-pro-002"
			}`),
			wantInput:  15,
			wantOutput: 0,
			wantModel:  "gemini-1.5-pro-002",
		},
		{
			name: "multimodal response with image input",
			responseBody: []byte(`{
				"candidates": [
					{
						"content": {
							"parts": [{"text": "This image shows a sunset over the ocean."}],
							"role": "model"
						},
						"finishReason": "STOP"
					}
				],
				"usageMetadata": {
					"promptTokenCount": 258,
					"candidatesTokenCount": 15,
					"totalTokenCount": 273
				},
				"modelVersion": "gemini-1.5-pro-002"
			}`),
			wantInput:  258,
			wantOutput: 15,
			wantModel:  "gemini-1.5-pro-002",
		},
		{
			name: "response with cachedContentTokenCount",
			responseBody: []byte(`{
				"candidates": [
					{
						"content": {
							"parts": [{"text": "Using cached context."}],
							"role": "model"
						},
						"finishReason": "STOP"
					}
				],
				"usageMetadata": {
					"promptTokenCount": 500,
					"candidatesTokenCount": 30,
					"totalTokenCount": 530,
					"cachedContentTokenCount": 400
				},
				"modelVersion": "gemini-2.5-pro-preview-05-06"
			}`),
			wantInput:  500,
			wantOutput: 30,
			wantCached: 400,
			wantModel:  "gemini-2.5-pro-preview-05-06",
		},
		{
			name:         "malformed JSON",
			responseBody: []byte(`{usageMetadata`),
			wantErr:      true,
		},
		{
			name:         "empty body",
			responseBody: []byte(``),
			wantErr:      true,
		},
		{
			name: "missing usageMetadata returns zero tokens",
			responseBody: []byte(`{
				"candidates": [],
				"modelVersion": "gemini-1.5-flash-002"
			}`),
			wantInput:  0,
			wantOutput: 0,
			wantModel:  "gemini-1.5-flash-002",
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
			if result.Model != tt.wantModel {
				t.Errorf("Model = %q, want %q", result.Model, tt.wantModel)
			}
			if result.Provider != "gemini" {
				t.Errorf("Provider = %q, want %q", result.Provider, "gemini")
			}
			if result.CachedTokens != tt.wantCached {
				t.Errorf("CachedTokens = %d, want %d", result.CachedTokens, tt.wantCached)
			}
		})
	}
}
