package provider

import (
	"encoding/json"

	"github.com/superset-studio/majordomo-gateway/internal/models"
)

type GeminiParser struct{}

type geminiResponse struct {
	UsageMetadata struct {
		PromptTokenCount         int `json:"promptTokenCount"`
		CandidatesTokenCount     int `json:"candidatesTokenCount"`
		TotalTokenCount          int `json:"totalTokenCount"`
		CachedContentTokenCount  int `json:"cachedContentTokenCount"`
	} `json:"usageMetadata"`
	ModelVersion string `json:"modelVersion"`
}

func (p *GeminiParser) ParseResponse(body []byte) (*models.UsageMetrics, error) {
	var resp geminiResponse
	if err := json.Unmarshal(body, &resp); err != nil {
		return nil, err
	}

	return &models.UsageMetrics{
		Provider:     string(ProviderGemini),
		Model:        resp.ModelVersion,
		InputTokens:  resp.UsageMetadata.PromptTokenCount,
		OutputTokens: resp.UsageMetadata.CandidatesTokenCount,
		CachedTokens: resp.UsageMetadata.CachedContentTokenCount,
	}, nil
}

func (p *GeminiParser) ExtractModel(requestBody []byte) string {
	return extractModelFromRequest(requestBody)
}
