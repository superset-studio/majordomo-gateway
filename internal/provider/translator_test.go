package provider

import (
	"encoding/json"
	"testing"
)

func TestTranslateOpenAIToAnthropic(t *testing.T) {
	tests := []struct {
		name        string
		input       string
		wantSystem  string
		wantMsgLen  int
		wantMaxTok  int
		wantPath    string
		wantErr     bool
	}{
		{
			name: "basic request with system message",
			input: `{
				"model": "claude-sonnet-4-20250514",
				"messages": [
					{"role": "system", "content": "You are helpful."},
					{"role": "user", "content": "Hello"}
				],
				"max_tokens": 1024
			}`,
			wantSystem: "You are helpful.",
			wantMsgLen: 1,
			wantMaxTok: 1024,
			wantPath:   "/v1/messages",
		},
		{
			name: "request without system message",
			input: `{
				"model": "claude-sonnet-4-20250514",
				"messages": [
					{"role": "user", "content": "Hello"}
				],
				"max_tokens": 2048
			}`,
			wantSystem: "",
			wantMsgLen: 1,
			wantMaxTok: 2048,
			wantPath:   "/v1/messages",
		},
		{
			name: "request without max_tokens uses default",
			input: `{
				"model": "claude-sonnet-4-20250514",
				"messages": [
					{"role": "user", "content": "Hello"}
				]
			}`,
			wantSystem: "",
			wantMsgLen: 1,
			wantMaxTok: 4096,
			wantPath:   "/v1/messages",
		},
		{
			name: "multi-turn conversation",
			input: `{
				"model": "claude-sonnet-4-20250514",
				"messages": [
					{"role": "system", "content": "Be brief."},
					{"role": "user", "content": "Hi"},
					{"role": "assistant", "content": "Hello!"},
					{"role": "user", "content": "How are you?"}
				],
				"max_tokens": 512
			}`,
			wantSystem: "Be brief.",
			wantMsgLen: 3,
			wantMaxTok: 512,
			wantPath:   "/v1/messages",
		},
		{
			name:    "malformed JSON",
			input:   `{invalid`,
			wantErr: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			translated, path, err := TranslateOpenAIToAnthropic([]byte(tt.input))

			if tt.wantErr {
				if err == nil {
					t.Fatal("expected error, got nil")
				}
				return
			}

			if err != nil {
				t.Fatalf("unexpected error: %v", err)
			}

			if path != tt.wantPath {
				t.Errorf("path = %q, want %q", path, tt.wantPath)
			}

			var req AnthropicRequest
			if err := json.Unmarshal(translated, &req); err != nil {
				t.Fatalf("failed to unmarshal translated request: %v", err)
			}

			if req.System != tt.wantSystem {
				t.Errorf("system = %q, want %q", req.System, tt.wantSystem)
			}

			if len(req.Messages) != tt.wantMsgLen {
				t.Errorf("messages length = %d, want %d", len(req.Messages), tt.wantMsgLen)
			}

			if req.MaxTokens != tt.wantMaxTok {
				t.Errorf("max_tokens = %d, want %d", req.MaxTokens, tt.wantMaxTok)
			}
		})
	}
}

func TestTranslateAnthropicToOpenAI(t *testing.T) {
	tests := []struct {
		name             string
		input            string
		wantContent      string
		wantFinishReason string
		wantPromptTokens int
		wantCompTokens   int
		wantErr          bool
	}{
		{
			name: "standard text response",
			input: `{
				"id": "msg_123",
				"type": "message",
				"role": "assistant",
				"content": [{"type": "text", "text": "Hello!"}],
				"model": "claude-sonnet-4-20250514",
				"stop_reason": "end_turn",
				"usage": {"input_tokens": 10, "output_tokens": 5}
			}`,
			wantContent:      "Hello!",
			wantFinishReason: "stop",
			wantPromptTokens: 10,
			wantCompTokens:   5,
		},
		{
			name: "max_tokens stop reason",
			input: `{
				"id": "msg_456",
				"type": "message",
				"role": "assistant",
				"content": [{"type": "text", "text": "Truncated..."}],
				"model": "claude-sonnet-4-20250514",
				"stop_reason": "max_tokens",
				"usage": {"input_tokens": 100, "output_tokens": 4096}
			}`,
			wantContent:      "Truncated...",
			wantFinishReason: "length",
			wantPromptTokens: 100,
			wantCompTokens:   4096,
		},
		{
			name: "multiple content blocks",
			input: `{
				"id": "msg_789",
				"type": "message",
				"role": "assistant",
				"content": [
					{"type": "text", "text": "Part 1. "},
					{"type": "text", "text": "Part 2."}
				],
				"model": "claude-sonnet-4-20250514",
				"stop_reason": "end_turn",
				"usage": {"input_tokens": 20, "output_tokens": 15}
			}`,
			wantContent:      "Part 1. Part 2.",
			wantFinishReason: "stop",
			wantPromptTokens: 20,
			wantCompTokens:   15,
		},
		{
			name:    "malformed JSON",
			input:   `{invalid`,
			wantErr: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			translated, err := TranslateAnthropicToOpenAI([]byte(tt.input), "")

			if tt.wantErr {
				if err == nil {
					t.Fatal("expected error, got nil")
				}
				return
			}

			if err != nil {
				t.Fatalf("unexpected error: %v", err)
			}

			var resp OpenAIResponse
			if err := json.Unmarshal(translated, &resp); err != nil {
				t.Fatalf("failed to unmarshal translated response: %v", err)
			}

			if len(resp.Choices) != 1 {
				t.Fatalf("choices length = %d, want 1", len(resp.Choices))
			}

			if resp.Choices[0].Message.Content != tt.wantContent {
				t.Errorf("content = %q, want %q", resp.Choices[0].Message.Content, tt.wantContent)
			}

			if resp.Choices[0].FinishReason != tt.wantFinishReason {
				t.Errorf("finish_reason = %q, want %q", resp.Choices[0].FinishReason, tt.wantFinishReason)
			}

			if resp.Usage.PromptTokens != tt.wantPromptTokens {
				t.Errorf("prompt_tokens = %d, want %d", resp.Usage.PromptTokens, tt.wantPromptTokens)
			}

			if resp.Usage.CompletionTokens != tt.wantCompTokens {
				t.Errorf("completion_tokens = %d, want %d", resp.Usage.CompletionTokens, tt.wantCompTokens)
			}

			if resp.Object != "chat.completion" {
				t.Errorf("object = %q, want %q", resp.Object, "chat.completion")
			}
		})
	}
}
