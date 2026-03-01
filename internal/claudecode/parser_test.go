package claudecode

import (
	"testing"
)

func TestParseRequestResponse(t *testing.T) {
	tests := []struct {
		name                  string
		reqBody               []byte
		respBody              []byte
		wantMessageCount      int
		wantUserMsgCount      int
		wantAssistantMsgCount int
		wantToolNames         []string
		wantToolUseCount      int
		wantHasThinking       bool
		wantIsPlanMode        bool
		wantStopReason        string
		wantSystemHashEmpty   bool
		wantErr               bool
	}{
		{
			name: "basic metadata with message counts and tool use",
			reqBody: []byte(`{
				"system": "You are a helpful assistant.",
				"messages": [
					{"role": "user", "content": "Hello"},
					{"role": "assistant", "content": "Hi there"},
					{"role": "user", "content": "Write code"}
				],
				"tools": [
					{"name": "Read"},
					{"name": "Write"},
					{"name": "Bash"}
				]
			}`),
			respBody: []byte(`{
				"stop_reason": "end_turn",
				"content": [
					{"type": "text", "text": "Here is the code"},
					{"type": "tool_use", "name": "Write", "id": "t1", "input": {}},
					{"type": "tool_use", "name": "Bash", "id": "t2", "input": {}}
				]
			}`),
			wantMessageCount:      3,
			wantUserMsgCount:      2,
			wantAssistantMsgCount: 1,
			wantToolNames:         []string{"Bash", "Write"},
			wantToolUseCount:      2,
			wantHasThinking:       false,
			wantIsPlanMode:        false,
			wantStopReason:        "end_turn",
		},
		{
			name: "thinking detection",
			reqBody: []byte(`{
				"messages": [{"role": "user", "content": "Think about this"}],
				"tools": []
			}`),
			respBody: []byte(`{
				"stop_reason": "end_turn",
				"content": [
					{"type": "thinking", "thinking": "Let me consider..."},
					{"type": "text", "text": "After thinking..."}
				]
			}`),
			wantMessageCount: 1,
			wantUserMsgCount: 1,
			wantHasThinking:  true,
			wantStopReason:   "end_turn",
			wantSystemHashEmpty: true,
		},
		{
			name: "plan mode - read tools without write tools",
			reqBody: []byte(`{
				"system": "You are in plan mode.",
				"messages": [
					{"role": "user", "content": "Plan this feature"}
				],
				"tools": [
					{"name": "Read"},
					{"name": "Grep"},
					{"name": "Glob"},
					{"name": "Bash"},
					{"name": "Task"}
				]
			}`),
			respBody: []byte(`{
				"stop_reason": "end_turn",
				"content": [
					{"type": "text", "text": "Here is the plan"}
				]
			}`),
			wantMessageCount: 1,
			wantUserMsgCount: 1,
			wantIsPlanMode:   true,
			wantStopReason:   "end_turn",
		},
		{
			name: "non-plan mode - has Write tool",
			reqBody: []byte(`{
				"messages": [{"role": "user", "content": "Write code"}],
				"tools": [
					{"name": "Read"},
					{"name": "Grep"},
					{"name": "Write"},
					{"name": "Edit"},
					{"name": "Bash"}
				]
			}`),
			respBody: []byte(`{
				"stop_reason": "tool_use",
				"content": [
					{"type": "tool_use", "name": "Write", "id": "t1", "input": {}}
				]
			}`),
			wantMessageCount:    1,
			wantUserMsgCount:    1,
			wantIsPlanMode:      false,
			wantStopReason:      "tool_use",
			wantToolNames:       []string{"Write"},
			wantToolUseCount:    1,
			wantSystemHashEmpty: true,
		},
		{
			name: "system prompt hash with string system",
			reqBody: []byte(`{
				"system": "You are a coding assistant.",
				"messages": [{"role": "user", "content": "Hello"}]
			}`),
			respBody: []byte(`{
				"stop_reason": "end_turn",
				"content": [{"type": "text", "text": "Hi"}]
			}`),
			wantMessageCount: 1,
			wantUserMsgCount: 1,
			wantStopReason:   "end_turn",
		},
		{
			name: "system prompt hash with array system",
			reqBody: []byte(`{
				"system": [{"type": "text", "text": "You are helpful"}],
				"messages": [{"role": "user", "content": "Hello"}]
			}`),
			respBody: []byte(`{
				"stop_reason": "end_turn",
				"content": [{"type": "text", "text": "Hi"}]
			}`),
			wantMessageCount: 1,
			wantUserMsgCount: 1,
			wantStopReason:   "end_turn",
		},
		{
			name:    "malformed request JSON",
			reqBody: []byte(`{not valid json`),
			respBody: []byte(`{
				"stop_reason": "end_turn",
				"content": []
			}`),
			wantErr: true,
		},
		{
			name: "malformed response JSON",
			reqBody: []byte(`{
				"messages": [{"role": "user", "content": "Hello"}]
			}`),
			respBody: []byte(`{not valid`),
			wantErr:  true,
		},
		{
			name: "empty content array",
			reqBody: []byte(`{
				"messages": [{"role": "user", "content": "Hello"}]
			}`),
			respBody: []byte(`{
				"stop_reason": "end_turn",
				"content": []
			}`),
			wantMessageCount:    1,
			wantUserMsgCount:    1,
			wantStopReason:      "end_turn",
			wantSystemHashEmpty: true,
		},
		{
			name: "no tools in request",
			reqBody: []byte(`{
				"messages": [{"role": "user", "content": "Hello"}]
			}`),
			respBody: []byte(`{
				"stop_reason": "end_turn",
				"content": [{"type": "text", "text": "Hi"}]
			}`),
			wantMessageCount:    1,
			wantUserMsgCount:    1,
			wantIsPlanMode:      false,
			wantStopReason:      "end_turn",
			wantSystemHashEmpty: true,
		},
		{
			name: "streaming SSE response with tool use and thinking",
			reqBody: []byte(`{
				"system": "You are a coding assistant.",
				"messages": [
					{"role": "user", "content": "Read the file"},
					{"role": "assistant", "content": "Sure"},
					{"role": "user", "content": "Now edit it"}
				],
				"tools": [{"name": "Read"}, {"name": "Write"}, {"name": "Edit"}]
			}`),
			respBody: []byte("event: message_start\ndata: {\"type\":\"message_start\",\"message\":{\"id\":\"msg_01\",\"type\":\"message\",\"role\":\"assistant\",\"model\":\"claude-opus-4-6\"}}\n\nevent: content_block_start\ndata: {\"type\":\"content_block_start\",\"index\":0,\"content_block\":{\"type\":\"thinking\",\"thinking\":\"\"}}\n\nevent: content_block_stop\ndata: {\"type\":\"content_block_stop\",\"index\":0}\n\nevent: content_block_start\ndata: {\"type\":\"content_block_start\",\"index\":1,\"content_block\":{\"type\":\"tool_use\",\"id\":\"t1\",\"name\":\"Read\",\"input\":{}}}\n\nevent: content_block_stop\ndata: {\"type\":\"content_block_stop\",\"index\":1}\n\nevent: content_block_start\ndata: {\"type\":\"content_block_start\",\"index\":2,\"content_block\":{\"type\":\"tool_use\",\"id\":\"t2\",\"name\":\"Edit\",\"input\":{}}}\n\nevent: content_block_stop\ndata: {\"type\":\"content_block_stop\",\"index\":2}\n\nevent: message_delta\ndata: {\"type\":\"message_delta\",\"delta\":{\"stop_reason\":\"tool_use\"}}\n\nevent: message_stop\ndata: {\"type\":\"message_stop\"}\n\n"),
			wantMessageCount:      3,
			wantUserMsgCount:      2,
			wantAssistantMsgCount: 1,
			wantToolNames:         []string{"Edit", "Read"},
			wantToolUseCount:      2,
			wantHasThinking:       true,
			wantIsPlanMode:        false,
			wantStopReason:        "tool_use",
		},
		{
			name: "duplicate tool names in response deduplicated and sorted",
			reqBody: []byte(`{
				"messages": [{"role": "user", "content": "Do stuff"}],
				"tools": [{"name": "Read"}, {"name": "Write"}, {"name": "Bash"}]
			}`),
			respBody: []byte(`{
				"stop_reason": "tool_use",
				"content": [
					{"type": "tool_use", "name": "Bash", "id": "t1", "input": {}},
					{"type": "tool_use", "name": "Read", "id": "t2", "input": {}},
					{"type": "tool_use", "name": "Bash", "id": "t3", "input": {}}
				]
			}`),
			wantMessageCount:    1,
			wantUserMsgCount:    1,
			wantToolNames:       []string{"Bash", "Read"},
			wantToolUseCount:    3,
			wantStopReason:      "tool_use",
			wantSystemHashEmpty: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			meta, err := ParseRequestResponse(tt.reqBody, tt.respBody)
			if tt.wantErr {
				if err == nil {
					t.Error("expected error, got nil")
				}
				return
			}
			if err != nil {
				t.Fatalf("unexpected error: %v", err)
			}

			if meta.MessageCount != tt.wantMessageCount {
				t.Errorf("MessageCount = %d, want %d", meta.MessageCount, tt.wantMessageCount)
			}
			if meta.UserMessageCount != tt.wantUserMsgCount {
				t.Errorf("UserMessageCount = %d, want %d", meta.UserMessageCount, tt.wantUserMsgCount)
			}
			if meta.AssistantMessageCount != tt.wantAssistantMsgCount {
				t.Errorf("AssistantMessageCount = %d, want %d", meta.AssistantMessageCount, tt.wantAssistantMsgCount)
			}
			if meta.ToolUseCount != tt.wantToolUseCount {
				t.Errorf("ToolUseCount = %d, want %d", meta.ToolUseCount, tt.wantToolUseCount)
			}
			if meta.HasThinking != tt.wantHasThinking {
				t.Errorf("HasThinking = %v, want %v", meta.HasThinking, tt.wantHasThinking)
			}
			if meta.IsPlanMode != tt.wantIsPlanMode {
				t.Errorf("IsPlanMode = %v, want %v", meta.IsPlanMode, tt.wantIsPlanMode)
			}
			if meta.StopReason != tt.wantStopReason {
				t.Errorf("StopReason = %q, want %q", meta.StopReason, tt.wantStopReason)
			}

			// Check tool names
			if len(tt.wantToolNames) == 0 {
				if len(meta.ToolNames) != 0 {
					t.Errorf("ToolNames = %v, want empty", meta.ToolNames)
				}
			} else {
				if len(meta.ToolNames) != len(tt.wantToolNames) {
					t.Errorf("ToolNames length = %d, want %d", len(meta.ToolNames), len(tt.wantToolNames))
				} else {
					for i, name := range meta.ToolNames {
						if name != tt.wantToolNames[i] {
							t.Errorf("ToolNames[%d] = %q, want %q", i, name, tt.wantToolNames[i])
						}
					}
				}
			}

			// Check system prompt hash
			if tt.wantSystemHashEmpty {
				if meta.SystemPromptHash != "" {
					t.Errorf("SystemPromptHash = %q, want empty", meta.SystemPromptHash)
				}
			} else {
				if meta.SystemPromptHash == "" {
					t.Error("SystemPromptHash is empty, want non-empty hash")
				}
				if len(meta.SystemPromptHash) != 64 {
					t.Errorf("SystemPromptHash length = %d, want 64", len(meta.SystemPromptHash))
				}
			}
		})
	}
}

func TestDetectPlanMode(t *testing.T) {
	tests := []struct {
		name  string
		tools []toolDef
		want  bool
	}{
		{
			name:  "empty tools",
			tools: nil,
			want:  false,
		},
		{
			name:  "read-only tools (plan mode)",
			tools: []toolDef{{"Read"}, {"Grep"}, {"Glob"}, {"Bash"}, {"Task"}},
			want:  true,
		},
		{
			name:  "has Write tool (not plan mode)",
			tools: []toolDef{{"Read"}, {"Grep"}, {"Glob"}, {"Write"}, {"Bash"}},
			want:  false,
		},
		{
			name:  "has Edit tool (not plan mode)",
			tools: []toolDef{{"Read"}, {"Grep"}, {"Edit"}, {"Bash"}},
			want:  false,
		},
		{
			name:  "no read tools (not plan mode)",
			tools: []toolDef{{"Bash"}, {"Task"}},
			want:  false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := detectPlanMode(tt.tools)
			if got != tt.want {
				t.Errorf("detectPlanMode() = %v, want %v", got, tt.want)
			}
		})
	}
}
