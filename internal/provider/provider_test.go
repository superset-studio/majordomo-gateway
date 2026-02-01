package provider

import (
	"fmt"
	"testing"
)

func TestExtractModel(t *testing.T) {
	tests := []struct {
		name        string
		requestBody []byte
		want        string
	}{
		{
			name:        "OpenAI chat request",
			requestBody: []byte(`{"model": "gpt-4o", "messages": [{"role": "user", "content": "Hi"}]}`),
			want:        "gpt-4o",
		},
		{
			name:        "Anthropic request",
			requestBody: []byte(`{"model": "claude-3-5-sonnet-20241022", "max_tokens": 1024, "messages": []}`),
			want:        "claude-3-5-sonnet-20241022",
		},
		{
			name:        "Gemini request",
			requestBody: []byte(`{"model": "gemini-1.5-pro", "contents": []}`),
			want:        "gemini-1.5-pro",
		},
		{
			name:        "missing model field",
			requestBody: []byte(`{"messages": [{"role": "user", "content": "Hi"}]}`),
			want:        "unknown",
		},
		{
			name:        "empty model field",
			requestBody: []byte(`{"model": "", "messages": []}`),
			want:        "unknown",
		},
		{
			name:        "malformed JSON",
			requestBody: []byte(`{model: invalid`),
			want:        "unknown",
		},
		{
			name:        "empty body",
			requestBody: []byte(``),
			want:        "unknown",
		},
	}

	parsers := []struct {
		name   string
		parser ResponseParser
	}{
		{"OpenAI", &OpenAIParser{}},
		{"Anthropic", &AnthropicParser{}},
		{"Gemini", &GeminiParser{}},
	}

	for _, p := range parsers {
		for _, tt := range tests {
			t.Run(p.name+"/"+tt.name, func(t *testing.T) {
				got := p.parser.ExtractModel(tt.requestBody)
				if got != tt.want {
					t.Errorf("%s.ExtractModel() = %q, want %q", p.name, got, tt.want)
				}
			})
		}
	}
}

func TestProviderDetect(t *testing.T) {
	tests := []struct {
		name         string
		path         string
		headers      map[string]string
		wantProvider Provider
	}{
		{
			name:         "explicit openai header",
			path:         "/anything",
			headers:      map[string]string{"x-majordomo-provider": "openai"},
			wantProvider: ProviderOpenAI,
		},
		{
			name:         "explicit anthropic header",
			path:         "/anything",
			headers:      map[string]string{"x-majordomo-provider": "anthropic"},
			wantProvider: ProviderAnthropic,
		},
		{
			name:         "explicit gemini header case insensitive",
			path:         "/anything",
			headers:      map[string]string{"x-majordomo-provider": "GEMINI"},
			wantProvider: ProviderGemini,
		},
		{
			name:         "chat completions path",
			path:         "/v1/chat/completions",
			headers:      map[string]string{},
			wantProvider: ProviderOpenAI,
		},
		{
			name:         "responses API path",
			path:         "/v1/responses",
			headers:      map[string]string{},
			wantProvider: ProviderOpenAI,
		},
		{
			name:         "embeddings path",
			path:         "/v1/embeddings",
			headers:      map[string]string{},
			wantProvider: ProviderOpenAI,
		},
		{
			name:         "anthropic messages path",
			path:         "/v1/messages",
			headers:      map[string]string{},
			wantProvider: ProviderAnthropic,
		},
		{
			name:         "gemini generateContent path",
			path:         "/v1beta/models/gemini-1.5-pro:generateContent",
			headers:      map[string]string{},
			wantProvider: ProviderGemini,
		},
		{
			name:         "gemini streamGenerateContent path",
			path:         "/v1beta/models/gemini-1.5-pro:streamGenerateContent",
			headers:      map[string]string{},
			wantProvider: ProviderGemini,
		},
		{
			name:         "unknown path defaults to openai",
			path:         "/some/random/path",
			headers:      map[string]string{},
			wantProvider: ProviderOpenAI,
		},
		{
			name:         "explicit header overrides path",
			path:         "/v1/messages",
			headers:      map[string]string{"x-majordomo-provider": "openai"},
			wantProvider: ProviderOpenAI,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			info := Detect(tt.path, tt.headers)
			if info.Provider != tt.wantProvider {
				t.Errorf("Detect() provider = %v, want %v", info.Provider, tt.wantProvider)
			}
		})
	}
}

func TestGetParser(t *testing.T) {
	tests := []struct {
		provider Provider
		wantType string
	}{
		{ProviderOpenAI, "*provider.OpenAIParser"},
		{ProviderAnthropic, "*provider.AnthropicParser"},
		{ProviderGemini, "*provider.GeminiParser"},
		{ProviderAzure, "*provider.OpenAIParser"},   // Azure uses OpenAI parser
		{ProviderUnknown, "*provider.OpenAIParser"}, // Unknown defaults to OpenAI
	}

	for _, tt := range tests {
		t.Run(string(tt.provider), func(t *testing.T) {
			parser := GetParser(tt.provider)
			gotType := fmt.Sprintf("%T", parser)
			if gotType != tt.wantType {
				t.Errorf("GetParser(%v) = %v, want %v", tt.provider, gotType, tt.wantType)
			}
		})
	}
}
