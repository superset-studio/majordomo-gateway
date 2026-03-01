package claudecode

import (
	"bufio"
	"bytes"
	"crypto/sha256"
	"encoding/json"
	"fmt"
	"sort"
	"strings"
)

// RequestMetadata holds parsed metadata from a Claude Code request/response pair.
type RequestMetadata struct {
	MessageCount          int
	UserMessageCount      int
	AssistantMessageCount int
	ToolNames             []string
	ToolUseCount          int
	HasThinking           bool
	IsPlanMode            bool
	StopReason            string
	SystemPromptHash      string
}

// toolDef represents a tool definition in a Claude Code request.
type toolDef struct {
	Name string `json:"name"`
}

// anthropicRequest is the minimal structure needed to extract metadata from a Messages API request.
type anthropicRequest struct {
	System   json.RawMessage `json:"system"`
	Messages []struct {
		Role string `json:"role"`
	} `json:"messages"`
	Tools []toolDef `json:"tools"`
}

// anthropicResponseMsg is the minimal structure needed to extract metadata from a Messages API response.
type anthropicResponseMsg struct {
	StopReason string `json:"stop_reason"`
	Content    []struct {
		Type string `json:"type"`
		Name string `json:"name"`
	} `json:"content"`
}

// ParseRequestResponse extracts structural metadata from Anthropic Messages API
// request and response bodies. It never reads message content text.
func ParseRequestResponse(reqBody, respBody []byte) (*RequestMetadata, error) {
	var req anthropicRequest
	if err := json.Unmarshal(reqBody, &req); err != nil {
		return nil, fmt.Errorf("parsing request body: %w", err)
	}

	var resp anthropicResponseMsg
	if isSSEResponse(respBody) {
		parsed, err := parseSSEResponse(respBody)
		if err != nil {
			return nil, fmt.Errorf("parsing streaming response: %w", err)
		}
		resp = *parsed
	} else {
		if err := json.Unmarshal(respBody, &resp); err != nil {
			return nil, fmt.Errorf("parsing response body: %w", err)
		}
	}

	meta := &RequestMetadata{}

	// Count messages by role
	meta.MessageCount = len(req.Messages)
	for _, msg := range req.Messages {
		switch msg.Role {
		case "user":
			meta.UserMessageCount++
		case "assistant":
			meta.AssistantMessageCount++
		}
	}

	// Hash the system prompt (supports both string and array forms)
	if len(req.System) > 0 && string(req.System) != "null" {
		hash := sha256.Sum256(req.System)
		meta.SystemPromptHash = fmt.Sprintf("%x", hash)
	}

	// Detect plan mode from available tools
	meta.IsPlanMode = detectPlanMode(req.Tools)

	// Extract response metadata
	meta.StopReason = resp.StopReason

	toolNameSet := make(map[string]struct{})
	for _, block := range resp.Content {
		switch block.Type {
		case "thinking":
			meta.HasThinking = true
		case "tool_use":
			meta.ToolUseCount++
			if block.Name != "" {
				toolNameSet[block.Name] = struct{}{}
			}
		}
	}

	// Sorted unique tool names from response
	for name := range toolNameSet {
		meta.ToolNames = append(meta.ToolNames, name)
	}
	sort.Strings(meta.ToolNames)

	return meta, nil
}

// isSSEResponse checks if the body is a Server-Sent Events stream.
func isSSEResponse(body []byte) bool {
	trimmed := bytes.TrimLeft(body, " \t\r\n")
	return bytes.HasPrefix(trimmed, []byte("event:")) || bytes.HasPrefix(trimmed, []byte("data:"))
}

// parseSSEResponse reconstructs an anthropicResponseMsg from SSE events.
// It extracts:
//   - stop_reason from message_delta events
//   - content blocks from content_block_start events (type and name)
func parseSSEResponse(body []byte) (*anthropicResponseMsg, error) {
	resp := &anthropicResponseMsg{}

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
		case "content_block_start":
			var event struct {
				ContentBlock struct {
					Type string `json:"type"`
					Name string `json:"name"`
				} `json:"content_block"`
			}
			if err := json.Unmarshal([]byte(data), &event); err == nil {
				resp.Content = append(resp.Content, struct {
					Type string `json:"type"`
					Name string `json:"name"`
				}{
					Type: event.ContentBlock.Type,
					Name: event.ContentBlock.Name,
				})
			}

		case "message_delta":
			var event struct {
				Delta struct {
					StopReason string `json:"stop_reason"`
				} `json:"delta"`
			}
			if err := json.Unmarshal([]byte(data), &event); err == nil {
				if event.Delta.StopReason != "" {
					resp.StopReason = event.Delta.StopReason
				}
			}
		}
	}

	return resp, nil
}

// detectPlanMode checks if the tool set indicates Claude Code plan mode.
// In plan mode, Claude Code strips editing tools (Write, Edit) but keeps
// read-only tools (Read, Grep, Glob).
func detectPlanMode(tools []toolDef) bool {
	if len(tools) == 0 {
		return false
	}

	hasReadTools := false
	hasWriteTools := false

	readToolNames := map[string]bool{"Read": true, "Grep": true, "Glob": true}
	writeToolNames := map[string]bool{"Write": true, "Edit": true}

	for _, tool := range tools {
		if readToolNames[tool.Name] {
			hasReadTools = true
		}
		if writeToolNames[tool.Name] {
			hasWriteTools = true
		}
	}

	return hasReadTools && !hasWriteTools
}
