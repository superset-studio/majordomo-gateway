package provider

import (
	"encoding/json"
	"strings"

	"github.com/superset-studio/majordomo-gateway/internal/models"
)

type Provider string

const (
	ProviderOpenAI       Provider = "openai"
	ProviderAnthropic    Provider = "anthropic"
	ProviderGemini       Provider = "gemini"
	ProviderGeminiOpenAI    Provider = "gemini-openai"    // Gemini via OpenAI-compatible endpoint
	ProviderAnthropicOpenAI Provider = "anthropic-openai" // Anthropic via OpenAI-compatible translation
	ProviderAzure           Provider = "azure"
	ProviderBedrock      Provider = "bedrock"
	ProviderUnknown      Provider = "unknown"
)

type ResponseParser interface {
	ParseResponse(body []byte) (*models.UsageMetrics, error)
	ExtractModel(requestBody []byte) string
}

type ProviderInfo struct {
	Provider Provider
	BaseURL  string
}

func Detect(path string, headers map[string]string) ProviderInfo {
	if explicit, ok := headers["x-majordomo-provider"]; ok {
		return resolveExplicitProvider(explicit)
	}

	return detectFromPath(path)
}

func resolveExplicitProvider(name string) ProviderInfo {
	switch Provider(strings.ToLower(name)) {
	case ProviderOpenAI:
		return ProviderInfo{Provider: ProviderOpenAI, BaseURL: "https://api.openai.com"}
	case ProviderAnthropic:
		return ProviderInfo{Provider: ProviderAnthropic, BaseURL: "https://api.anthropic.com"}
	case ProviderGemini:
		return ProviderInfo{Provider: ProviderGemini, BaseURL: "https://generativelanguage.googleapis.com"}
	case ProviderGeminiOpenAI:
		// Gemini's OpenAI-compatible endpoint: https://ai.google.dev/gemini-api/docs/openai
		return ProviderInfo{Provider: ProviderGeminiOpenAI, BaseURL: "https://generativelanguage.googleapis.com/v1beta/openai"}
	case ProviderAnthropicOpenAI:
		// Accepts OpenAI-format requests, translates to Anthropic API format
		return ProviderInfo{Provider: ProviderAnthropicOpenAI, BaseURL: "https://api.anthropic.com"}
	case ProviderAzure:
		return ProviderInfo{Provider: ProviderAzure, BaseURL: ""}
	case ProviderBedrock:
		return ProviderInfo{Provider: ProviderBedrock, BaseURL: ""}
	default:
		return ProviderInfo{Provider: ProviderUnknown, BaseURL: ""}
	}
}

func detectFromPath(path string) ProviderInfo {
	switch {
	case strings.HasPrefix(path, "/v1/chat/completions"),
		strings.HasPrefix(path, "/v1/completions"),
		strings.HasPrefix(path, "/v1/embeddings"),
		strings.HasPrefix(path, "/v1/responses"):
		return ProviderInfo{Provider: ProviderOpenAI, BaseURL: "https://api.openai.com"}

	case strings.HasPrefix(path, "/v1/messages"):
		return ProviderInfo{Provider: ProviderAnthropic, BaseURL: "https://api.anthropic.com"}

	case strings.Contains(path, "generateContent"),
		strings.Contains(path, "streamGenerateContent"):
		return ProviderInfo{Provider: ProviderGemini, BaseURL: "https://generativelanguage.googleapis.com"}

	default:
		return ProviderInfo{Provider: ProviderOpenAI, BaseURL: "https://api.openai.com"}
	}
}

func GetParser(p Provider) ResponseParser {
	switch p {
	case ProviderOpenAI, ProviderAzure, ProviderGeminiOpenAI, ProviderAnthropicOpenAI:
		return &OpenAIParser{}
	case ProviderAnthropic:
		return &AnthropicParser{}
	case ProviderGemini:
		return &GeminiParser{}
	default:
		return &OpenAIParser{}
	}
}

func extractModelFromRequest(body []byte) string {
	var req struct {
		Model string `json:"model"`
	}
	if err := json.Unmarshal(body, &req); err != nil {
		return "unknown"
	}
	if req.Model == "" {
		return "unknown"
	}
	return req.Model
}
