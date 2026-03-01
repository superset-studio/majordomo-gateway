package provider

import (
	"bytes"
	"encoding/json"

	"github.com/superset-studio/majordomo-gateway/internal/models"
)

type GeminiParser struct{}

type geminiResponse struct {
	UsageMetadata struct {
		PromptTokenCount        int `json:"promptTokenCount"`
		CandidatesTokenCount    int `json:"candidatesTokenCount"`
		TotalTokenCount         int `json:"totalTokenCount"`
		CachedContentTokenCount int `json:"cachedContentTokenCount"`
	} `json:"usageMetadata"`
	ModelVersion string `json:"modelVersion"`
}

func (p *GeminiParser) ParseResponse(body []byte) (*models.UsageMetrics, error) {
	trimmed := bytes.TrimLeft(body, " \t\r\n")

	// Gemini streamGenerateContent returns a JSON array of chunks.
	// The last element contains usageMetadata with the final totals.
	if bytes.HasPrefix(trimmed, []byte("[")) {
		return p.parseStreamingResponse(trimmed)
	}

	var resp geminiResponse
	if err := json.Unmarshal(body, &resp); err != nil {
		return nil, err
	}

	return buildGeminiMetrics(&resp), nil
}

func (p *GeminiParser) ExtractModel(requestBody []byte) string {
	return extractModelFromRequest(requestBody)
}

// parseStreamingResponse parses a JSON array of Gemini chunks and extracts
// usage from the last element (which contains the cumulative totals).
func (p *GeminiParser) parseStreamingResponse(body []byte) (*models.UsageMetrics, error) {
	var chunks []geminiResponse
	if err := json.Unmarshal(body, &chunks); err != nil {
		return nil, err
	}

	if len(chunks) == 0 {
		return &models.UsageMetrics{
			Provider: string(ProviderGemini),
		}, nil
	}

	// The last chunk contains the final cumulative usage metadata
	last := &chunks[len(chunks)-1]
	return buildGeminiMetrics(last), nil
}

func buildGeminiMetrics(resp *geminiResponse) *models.UsageMetrics {
	return &models.UsageMetrics{
		Provider:     string(ProviderGemini),
		Model:        resp.ModelVersion,
		InputTokens:  resp.UsageMetadata.PromptTokenCount,
		OutputTokens: resp.UsageMetadata.CandidatesTokenCount,
		CachedTokens: resp.UsageMetadata.CachedContentTokenCount,
	}
}
