package provider

import (
	"encoding/json"

	"github.com/superset-studio/majordomo-gateway/internal/models"
)

type AnthropicParser struct{}

type anthropicResponse struct {
	Model string `json:"model"`
	Usage struct {
		InputTokens          int `json:"input_tokens"`
		OutputTokens         int `json:"output_tokens"`
		CacheReadInputTokens int `json:"cache_read_input_tokens"`
	} `json:"usage"`
}

func (p *AnthropicParser) ParseResponse(body []byte) (*models.UsageMetrics, error) {
	var resp anthropicResponse
	if err := json.Unmarshal(body, &resp); err != nil {
		return nil, err
	}

	return &models.UsageMetrics{
		Provider:     string(ProviderAnthropic),
		Model:        resp.Model,
		InputTokens:  resp.Usage.InputTokens,
		OutputTokens: resp.Usage.OutputTokens,
		CachedTokens: resp.Usage.CacheReadInputTokens,
	}, nil
}

func (p *AnthropicParser) ExtractModel(requestBody []byte) string {
	return extractModelFromRequest(requestBody)
}
