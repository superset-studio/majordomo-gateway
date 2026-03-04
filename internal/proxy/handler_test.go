package proxy

import (
	"testing"
)

func TestExtractCustomMetadata(t *testing.T) {
	tests := []struct {
		name     string
		headers  map[string]string
		expected map[string]string
	}{
		{
			name: "regular metadata headers pass through",
			headers: map[string]string{
				"x-majordomo-developer": "alice",
				"x-majordomo-project":   "backend",
			},
			expected: map[string]string{
				"developer": "alice",
				"project":   "backend",
			},
		},
		{
			name: "session-id is no longer reserved and passes through as metadata",
			headers: map[string]string{
				"x-majordomo-session-id": "my-session-123",
				"x-majordomo-developer":  "alice",
			},
			expected: map[string]string{
				"session-id": "my-session-123",
				"developer":  "alice",
			},
		},
		{
			name: "x-majordomo-client is excluded",
			headers: map[string]string{
				"x-majordomo-client":    "claude-code",
				"x-majordomo-developer": "alice",
			},
			expected: map[string]string{
				"developer": "alice",
			},
		},
		{
			name: "x-majordomo-claudecode-session-id is excluded",
			headers: map[string]string{
				"x-majordomo-claudecode-session-id": "abc-123",
				"x-majordomo-developer":             "alice",
			},
			expected: map[string]string{
				"developer": "alice",
			},
		},
		{
			name: "all reserved headers are excluded",
			headers: map[string]string{
				"x-majordomo-key":                   "mdm_sk_test",
				"x-majordomo-provider":              "anthropic",
				"x-majordomo-provider-alias":        "prod-key",
				"x-majordomo-client":                "claude-code",
				"x-majordomo-claudecode-session-id": "abc-123",
				"x-majordomo-developer":             "alice",
			},
			expected: map[string]string{
				"developer": "alice",
			},
		},
		{
			name:     "empty headers returns empty map",
			headers:  map[string]string{},
			expected: map[string]string{},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := extractCustomMetadata(tt.headers)
			if len(result) != len(tt.expected) {
				t.Fatalf("expected %d entries, got %d: %v", len(tt.expected), len(result), result)
			}
			for key, expectedVal := range tt.expected {
				if gotVal, ok := result[key]; !ok {
					t.Errorf("expected key %q not found in result", key)
				} else if gotVal != expectedVal {
					t.Errorf("key %q: expected %q, got %q", key, expectedVal, gotVal)
				}
			}
		})
	}
}
