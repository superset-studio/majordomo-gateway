package provider

import (
	"bufio"
	"bytes"
	"encoding/json"
	"strings"

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

// SSE event structs for streaming responses
type messageStartEvent struct {
	Message struct {
		Model string `json:"model"`
		Usage struct {
			InputTokens              int `json:"input_tokens"`
			OutputTokens             int `json:"output_tokens"`
			CacheReadInputTokens     int `json:"cache_read_input_tokens"`
			CacheCreationInputTokens int `json:"cache_creation_input_tokens"`
		} `json:"usage"`
	} `json:"message"`
}

type messageDeltaEvent struct {
	Usage struct {
		OutputTokens int `json:"output_tokens"`
	} `json:"usage"`
}

func (p *AnthropicParser) ParseResponse(body []byte) (*models.UsageMetrics, error) {
	if isSSEResponse(body) {
		return p.parseStreamingResponse(body)
	}

	var resp anthropicResponse
	if err := json.Unmarshal(body, &resp); err != nil {
		return nil, err
	}

	return buildMetrics(&resp), nil
}

func (p *AnthropicParser) ExtractModel(requestBody []byte) string {
	return extractModelFromRequest(requestBody)
}

func (p *AnthropicParser) parseStreamingResponse(body []byte) (*models.UsageMetrics, error) {
	var model string
	var inputTokens, outputTokens, cacheReadTokens, cacheCreationTokens int

	scanner := bufio.NewScanner(bytes.NewReader(body))
	var currentEvent string

	for scanner.Scan() {
		line := scanner.Text()

		if strings.HasPrefix(line, "event: ") {
			currentEvent = strings.TrimPrefix(line, "event: ")
			continue
		}

		if !strings.HasPrefix(line, "data: ") {
			continue
		}

		data := strings.TrimPrefix(line, "data: ")

		switch currentEvent {
		case "message_start":
			var event messageStartEvent
			if err := json.Unmarshal([]byte(data), &event); err == nil {
				model = event.Message.Model
				inputTokens = event.Message.Usage.InputTokens
				cacheReadTokens = event.Message.Usage.CacheReadInputTokens
				cacheCreationTokens = event.Message.Usage.CacheCreationInputTokens
			}
		case "message_delta":
			var event messageDeltaEvent
			if err := json.Unmarshal([]byte(data), &event); err == nil {
				outputTokens = event.Usage.OutputTokens
			}
		}
	}

	totalInput := inputTokens + cacheReadTokens + cacheCreationTokens

	return &models.UsageMetrics{
		Provider:            string(ProviderAnthropic),
		Model:               model,
		InputTokens:         totalInput,
		OutputTokens:        outputTokens,
		CachedTokens:        cacheReadTokens,
		CacheCreationTokens: cacheCreationTokens,
	}, nil
}

func isSSEResponse(body []byte) bool {
	trimmed := bytes.TrimLeft(body, " \t\r\n")
	return bytes.HasPrefix(trimmed, []byte("event:")) || bytes.HasPrefix(trimmed, []byte("data:"))
}

// buildMetrics creates UsageMetrics from a non-streaming Anthropic response.
func buildMetrics(resp *anthropicResponse) *models.UsageMetrics {
	// Normalize InputTokens to total input (matching OpenAI's convention where
	// prompt_tokens includes cached tokens). Anthropic's input_tokens excludes
	// cache_read and cache_creation tokens, so we add them back.
	totalInput := resp.Usage.InputTokens + resp.Usage.CacheReadInputTokens + resp.Usage.CacheCreationInputTokens

	return &models.UsageMetrics{
		Provider:            string(ProviderAnthropic),
		Model:               resp.Model,
		InputTokens:         totalInput,
		OutputTokens:        resp.Usage.OutputTokens,
		CachedTokens:        resp.Usage.CacheReadInputTokens,
		CacheCreationTokens: resp.Usage.CacheCreationInputTokens,
	}
}
