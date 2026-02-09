package provider

import (
	"encoding/json"

	"github.com/superset-studio/majordomo-gateway/internal/models"
)

type AnthropicParser struct{}

type anthropicResponse struct {
	Model string `json:"model"`
	Usage struct {
		InputTokens              int `json:"input_tokens"`
		OutputTokens             int `json:"output_tokens"`
		CacheReadInputTokens     int `json:"cache_read_input_tokens"`
		CacheCreationInputTokens int `json:"cache_creation_input_tokens"`
	} `json:"usage"`
}

func (p *AnthropicParser) ParseResponse(body []byte) (*models.UsageMetrics, error) {
	var resp anthropicResponse
	if err := json.Unmarshal(body, &resp); err != nil {
		return nil, err
	}

	// Normalize InputTokens to total input (matching OpenAI's convention where
	// prompt_tokens includes cached tokens). Anthropic's input_tokens excludes
	// cache_read and cache_creation tokens, so we add them back.
	totalInput := resp.Usage.InputTokens + resp.Usage.CacheReadInputTokens + resp.Usage.CacheCreationInputTokens

	return &models.UsageMetrics{
		Provider:             string(ProviderAnthropic),
		Model:                resp.Model,
		InputTokens:          totalInput,
		OutputTokens:         resp.Usage.OutputTokens,
		CachedTokens:         resp.Usage.CacheReadInputTokens,
		CacheCreationTokens:  resp.Usage.CacheCreationInputTokens,
	}, nil
}

func (p *AnthropicParser) ExtractModel(requestBody []byte) string {
	return extractModelFromRequest(requestBody)
}
