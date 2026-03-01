package provider

import (
	"bufio"
	"bytes"
	"encoding/json"
	"strings"

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
	if isSSEResponse(body) {
		return p.parseStreamingResponse(body)
	}

	var resp openAIResponse
	if err := json.Unmarshal(body, &resp); err != nil {
		return nil, err
	}

	return buildOpenAIMetrics(&resp), nil
}

func (p *OpenAIParser) ExtractModel(requestBody []byte) string {
	return extractModelFromRequest(requestBody)
}

// parseStreamingResponse extracts usage from the last SSE data chunk that
// contains a non-null "usage" field (sent when stream_options.include_usage
// is true). Falls back to model extraction from any chunk.
func (p *OpenAIParser) parseStreamingResponse(body []byte) (*models.UsageMetrics, error) {
	var lastModel string
	var usageChunk []byte

	scanner := bufio.NewScanner(bytes.NewReader(body))
	for scanner.Scan() {
		line := scanner.Text()
		if !strings.HasPrefix(line, "data: ") {
			continue
		}
		data := strings.TrimPrefix(line, "data: ")
		if data == "[DONE]" {
			continue
		}

		// Track model from every chunk — check both top-level and nested
		var peek struct {
			Model    string           `json:"model"`
			Usage    *json.RawMessage `json:"usage"`
			Response *json.RawMessage `json:"response"`
		}
		if err := json.Unmarshal([]byte(data), &peek); err != nil {
			continue
		}
		if peek.Model != "" {
			lastModel = peek.Model
		}
		// Keep the last chunk that has a non-null usage object
		if peek.Usage != nil && string(*peek.Usage) != "null" {
			usageChunk = []byte(data)
		}
		// Responses API streaming: usage is inside "response" object
		if peek.Response != nil {
			var nested struct {
				Model string           `json:"model"`
				Usage *json.RawMessage `json:"usage"`
			}
			if err := json.Unmarshal(*peek.Response, &nested); err == nil {
				if nested.Model != "" {
					lastModel = nested.Model
				}
				if nested.Usage != nil && string(*nested.Usage) != "null" {
					// Treat the nested response as the chunk to parse
					usageChunk = *peek.Response
				}
			}
		}
	}

	if usageChunk != nil {
		var resp openAIResponse
		if err := json.Unmarshal(usageChunk, &resp); err == nil {
			return buildOpenAIMetrics(&resp), nil
		}
	}

	// No usage data found (stream_options.include_usage was not set)
	return &models.UsageMetrics{
		Provider: string(ProviderOpenAI),
		Model:    lastModel,
	}, nil
}

func buildOpenAIMetrics(resp *openAIResponse) *models.UsageMetrics {
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
	}
}
