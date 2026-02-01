package provider

import (
	"encoding/json"

	"github.com/superset-studio/majordomo-gateway/internal/models"
)

type OpenAIParser struct{}

// openAIResponse handles both Chat Completions API and Responses API formats.
// Chat Completions uses: prompt_tokens, completion_tokens, prompt_tokens_details
// Responses API uses: input_tokens, output_tokens, input_tokens_details
type openAIResponse struct {
	Model string `json:"model"`
	Usage struct {
		// Chat Completions API fields
		PromptTokens        int `json:"prompt_tokens"`
		CompletionTokens    int `json:"completion_tokens"`
		PromptTokensDetails struct {
			CachedTokens int `json:"cached_tokens"`
		} `json:"prompt_tokens_details"`

		// Responses API fields
		InputTokens       int `json:"input_tokens"`
		OutputTokens      int `json:"output_tokens"`
		InputTokensDetails struct {
			CachedTokens int `json:"cached_tokens"`
		} `json:"input_tokens_details"`
		OutputTokensDetails struct {
			ReasoningTokens int `json:"reasoning_tokens"`
		} `json:"output_tokens_details"`

		TotalTokens int `json:"total_tokens"`
	} `json:"usage"`
}

func (p *OpenAIParser) ParseResponse(body []byte) (*models.UsageMetrics, error) {
	var resp openAIResponse
	if err := json.Unmarshal(body, &resp); err != nil {
		return nil, err
	}

	// Determine which API format was used based on which fields are populated
	inputTokens := resp.Usage.PromptTokens
	outputTokens := resp.Usage.CompletionTokens
	cachedTokens := resp.Usage.PromptTokensDetails.CachedTokens

	// If Responses API fields are populated, use those instead
	if resp.Usage.InputTokens > 0 || resp.Usage.OutputTokens > 0 {
		inputTokens = resp.Usage.InputTokens
		outputTokens = resp.Usage.OutputTokens
		cachedTokens = resp.Usage.InputTokensDetails.CachedTokens
	}

	return &models.UsageMetrics{
		Provider:     string(ProviderOpenAI),
		Model:        resp.Model,
		InputTokens:  inputTokens,
		OutputTokens: outputTokens,
		CachedTokens: cachedTokens,
	}, nil
}

func (p *OpenAIParser) ExtractModel(requestBody []byte) string {
	return extractModelFromRequest(requestBody)
}
