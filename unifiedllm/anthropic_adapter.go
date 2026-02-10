package unifiedllm

import (
	"bytes"
	"context"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"os"
	"strings"
)

// AnthropicAdapter implements ProviderAdapter for the Anthropic Messages API.
type AnthropicAdapter struct {
	apiKey  string
	baseURL string
	http    *httpClient
}

// AnthropicAdapterOption configures an AnthropicAdapter.
type AnthropicAdapterOption func(*AnthropicAdapter)

// WithAnthropicBaseURL sets a custom base URL.
func WithAnthropicBaseURL(url string) AnthropicAdapterOption {
	return func(a *AnthropicAdapter) {
		a.baseURL = url
	}
}

// NewAnthropicAdapter creates a new Anthropic adapter using the Messages API.
func NewAnthropicAdapter(apiKey string, opts ...AnthropicAdapterOption) (*AnthropicAdapter, error) {
	if apiKey == "" {
		apiKey = os.Getenv("ANTHROPIC_API_KEY")
	}
	if apiKey == "" {
		return nil, &ConfigurationError{SDKError: SDKError{
			Message: "ANTHROPIC_API_KEY is required",
		}}
	}

	a := &AnthropicAdapter{
		apiKey:  apiKey,
		baseURL: "https://api.anthropic.com",
		http:    newHTTPClient(),
	}

	if envURL := os.Getenv("ANTHROPIC_BASE_URL"); envURL != "" {
		a.baseURL = strings.TrimRight(envURL, "/")
	}

	for _, opt := range opts {
		opt(a)
	}

	return a, nil
}

func (a *AnthropicAdapter) Name() string { return "anthropic" }

func (a *AnthropicAdapter) SupportsToolChoice(mode string) bool {
	switch mode {
	case "auto", "none", "required", "named":
		return true
	}
	return false
}

// Complete sends a blocking request to the Anthropic Messages API.
func (a *AnthropicAdapter) Complete(ctx context.Context, req Request) (*Response, error) {
	body, betaHeaders, err := a.buildRequestBody(req, false)
	if err != nil {
		return nil, err
	}

	httpReq, err := http.NewRequestWithContext(ctx, "POST", a.baseURL+"/v1/messages", bytes.NewReader(body))
	if err != nil {
		return nil, &NetworkError{SDKError: SDKError{Message: "failed to create request", Cause: err}}
	}
	a.setHeaders(httpReq, betaHeaders)

	resp, err := a.http.Do(httpReq)
	if err != nil {
		return nil, &NetworkError{SDKError: SDKError{Message: "request failed", Cause: err}}
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return nil, buildErrorFromResponse(resp, "anthropic")
	}

	return a.parseResponse(resp)
}

// Stream sends a streaming request to the Anthropic Messages API.
func (a *AnthropicAdapter) Stream(ctx context.Context, req Request) (<-chan StreamEvent, error) {
	body, betaHeaders, err := a.buildRequestBody(req, true)
	if err != nil {
		return nil, err
	}

	httpReq, err := http.NewRequestWithContext(ctx, "POST", a.baseURL+"/v1/messages", bytes.NewReader(body))
	if err != nil {
		return nil, &NetworkError{SDKError: SDKError{Message: "failed to create request", Cause: err}}
	}
	a.setHeaders(httpReq, betaHeaders)

	resp, err := a.http.Do(httpReq)
	if err != nil {
		return nil, &NetworkError{SDKError: SDKError{Message: "request failed", Cause: err}}
	}

	if resp.StatusCode != http.StatusOK {
		defer resp.Body.Close()
		return nil, buildErrorFromResponse(resp, "anthropic")
	}

	ch := make(chan StreamEvent, 64)
	go a.readStream(ctx, resp.Body, ch)
	return ch, nil
}

func (a *AnthropicAdapter) setHeaders(req *http.Request, betaHeaders []string) {
	req.Header.Set("x-api-key", a.apiKey)
	req.Header.Set("anthropic-version", "2023-06-01")
	req.Header.Set("content-type", "application/json")
	if len(betaHeaders) > 0 {
		req.Header.Set("anthropic-beta", strings.Join(betaHeaders, ","))
	}
}

func (a *AnthropicAdapter) buildRequestBody(req Request, stream bool) ([]byte, []string, error) {
	body := map[string]interface{}{
		"model": req.Model,
	}

	var betaHeaders []string
	hasCacheControl := false

	// Get provider options
	autoCache := true
	var providerOpts map[string]interface{}
	if opts, ok := req.ProviderOptions["anthropic"].(map[string]interface{}); ok {
		providerOpts = opts
		if ac, ok := opts["auto_cache"].(bool); ok {
			autoCache = ac
		}
		if bh, ok := opts["beta_headers"].([]interface{}); ok {
			for _, h := range bh {
				if s, ok := h.(string); ok {
					betaHeaders = append(betaHeaders, s)
				}
			}
		}
		if thinking, ok := opts["thinking"]; ok {
			body["thinking"] = thinking
		}
	}

	// Extract system messages
	var systemBlocks []interface{}
	var messages []map[string]interface{}

	for _, msg := range req.Messages {
		switch msg.Role {
		case RoleSystem, RoleDeveloper:
			for _, part := range msg.Content {
				if part.Kind == ContentText {
					systemBlocks = append(systemBlocks, map[string]interface{}{
						"type": "text",
						"text": part.Text,
					})
				}
			}
		case RoleUser:
			messages = append(messages, a.translateUserMessage(msg))
		case RoleAssistant:
			messages = append(messages, a.translateAssistantMessage(msg))
		case RoleTool:
			// Tool results go into user messages
			messages = append(messages, a.translateToolMessage(msg))
		}
	}

	// Enforce strict user/assistant alternation by merging consecutive same-role messages
	messages = a.mergeConsecutiveMessages(messages)

	// Build tool definitions
	var tools []interface{}
	if req.ToolChoice == nil || req.ToolChoice.Mode != "none" {
		for _, td := range req.ToolDefs {
			tools = append(tools, map[string]interface{}{
				"name":         td.Name,
				"description":  td.Description,
				"input_schema": td.Parameters,
			})
		}
	}

	// Inject cache_control for prompt caching
	if autoCache && len(tools) > 0 {
		hasCacheControl = true
		// Add cache_control to the last tool
		if lastTool, ok := tools[len(tools)-1].(map[string]interface{}); ok {
			lastTool["cache_control"] = map[string]interface{}{"type": "ephemeral"}
		}
	}

	if autoCache && len(systemBlocks) > 0 {
		hasCacheControl = true
		// Add cache_control to the last system block
		if lastBlock, ok := systemBlocks[len(systemBlocks)-1].(map[string]interface{}); ok {
			lastBlock["cache_control"] = map[string]interface{}{"type": "ephemeral"}
		}
	}

	if autoCache && len(messages) >= 2 {
		hasCacheControl = true
		// Find the last user message in the prefix (second-to-last message)
		secondToLast := messages[len(messages)-2]
		if secondToLast["role"] == "user" {
			if content, ok := secondToLast["content"].([]interface{}); ok && len(content) > 0 {
				if lastBlock, ok := content[len(content)-1].(map[string]interface{}); ok {
					lastBlock["cache_control"] = map[string]interface{}{"type": "ephemeral"}
				}
			}
		}
	}

	if hasCacheControl {
		// Add prompt caching beta header if not already present
		found := false
		for _, h := range betaHeaders {
			if h == "prompt-caching-2024-07-31" {
				found = true
				break
			}
		}
		if !found {
			betaHeaders = append(betaHeaders, "prompt-caching-2024-07-31")
		}
	}

	if len(systemBlocks) > 0 {
		body["system"] = systemBlocks
	}
	body["messages"] = messages

	if len(tools) > 0 {
		body["tools"] = tools
	}

	// Tool choice
	if req.ToolChoice != nil && len(tools) > 0 {
		tc := a.translateToolChoice(req.ToolChoice)
		if tc != nil {
			body["tool_choice"] = tc
		}
	}

	// max_tokens is required for Anthropic
	maxTokens := 4096
	if req.MaxTokens != nil {
		maxTokens = *req.MaxTokens
	}
	body["max_tokens"] = maxTokens

	// Generation parameters
	if req.Temperature != nil {
		body["temperature"] = *req.Temperature
	}
	if req.TopP != nil {
		body["top_p"] = *req.TopP
	}
	if len(req.StopSequences) > 0 {
		body["stop_sequences"] = req.StopSequences
	}

	// Stream
	if stream {
		body["stream"] = true
	}

	// Merge remaining provider options (except ones already handled)
	if providerOpts != nil {
		for k, v := range providerOpts {
			switch k {
			case "auto_cache", "beta_headers", "thinking":
				continue
			default:
				body[k] = v
			}
		}
	}

	data, err := json.Marshal(body)
	return data, betaHeaders, err
}

func (a *AnthropicAdapter) translateUserMessage(msg Message) map[string]interface{} {
	var content []interface{}
	for _, part := range msg.Content {
		switch part.Kind {
		case ContentText:
			content = append(content, map[string]interface{}{
				"type": "text",
				"text": part.Text,
			})
		case ContentImage:
			if part.Image != nil {
				if part.Image.URL != "" {
					content = append(content, map[string]interface{}{
						"type": "image",
						"source": map[string]interface{}{
							"type": "url",
							"url":  part.Image.URL,
						},
					})
				} else if len(part.Image.Data) > 0 {
					mediaType := part.Image.MediaType
					if mediaType == "" {
						mediaType = "image/png"
					}
					content = append(content, map[string]interface{}{
						"type": "image",
						"source": map[string]interface{}{
							"type":       "base64",
							"media_type": mediaType,
							"data":       base64.StdEncoding.EncodeToString(part.Image.Data),
						},
					})
				}
			}
		}
	}
	if len(content) == 0 {
		content = append(content, map[string]interface{}{
			"type": "text",
			"text": "",
		})
	}
	return map[string]interface{}{
		"role":    "user",
		"content": content,
	}
}

func (a *AnthropicAdapter) translateAssistantMessage(msg Message) map[string]interface{} {
	var content []interface{}
	for _, part := range msg.Content {
		switch part.Kind {
		case ContentText:
			content = append(content, map[string]interface{}{
				"type": "text",
				"text": part.Text,
			})
		case ContentToolCall:
			if part.ToolCall != nil {
				var input interface{}
				if err := json.Unmarshal(part.ToolCall.Arguments, &input); err != nil {
					input = map[string]interface{}{}
				}
				content = append(content, map[string]interface{}{
					"type":  "tool_use",
					"id":    part.ToolCall.ID,
					"name":  part.ToolCall.Name,
					"input": input,
				})
			}
		case ContentThinking:
			if part.Thinking != nil {
				if part.Thinking.Redacted {
					content = append(content, map[string]interface{}{
						"type": "redacted_thinking",
						"data": part.Thinking.Text,
					})
				} else {
					block := map[string]interface{}{
						"type":     "thinking",
						"thinking": part.Thinking.Text,
					}
					if part.Thinking.Signature != "" {
						block["signature"] = part.Thinking.Signature
					}
					content = append(content, block)
				}
			}
		case ContentRedactedThinking:
			if part.Thinking != nil {
				content = append(content, map[string]interface{}{
					"type": "redacted_thinking",
					"data": part.Thinking.Text,
				})
			}
		}
	}
	if len(content) == 0 {
		content = append(content, map[string]interface{}{
			"type": "text",
			"text": "",
		})
	}
	return map[string]interface{}{
		"role":    "assistant",
		"content": content,
	}
}

func (a *AnthropicAdapter) translateToolMessage(msg Message) map[string]interface{} {
	var content []interface{}
	for _, part := range msg.Content {
		if part.Kind == ContentToolResult && part.ToolResult != nil {
			block := map[string]interface{}{
				"type":        "tool_result",
				"tool_use_id": part.ToolResult.ToolCallID,
				"content":     string(part.ToolResult.Content),
			}
			if part.ToolResult.IsError {
				block["is_error"] = true
			}
			content = append(content, block)
		}
	}
	return map[string]interface{}{
		"role":    "user",
		"content": content,
	}
}

func (a *AnthropicAdapter) mergeConsecutiveMessages(messages []map[string]interface{}) []map[string]interface{} {
	if len(messages) <= 1 {
		return messages
	}

	var merged []map[string]interface{}
	for _, msg := range messages {
		if len(merged) > 0 {
			last := merged[len(merged)-1]
			if last["role"] == msg["role"] {
				// Merge content arrays
				lastContent, _ := last["content"].([]interface{})
				msgContent, _ := msg["content"].([]interface{})
				last["content"] = append(lastContent, msgContent...)
				continue
			}
		}
		merged = append(merged, msg)
	}
	return merged
}

func (a *AnthropicAdapter) translateToolChoice(tc *ToolChoice) interface{} {
	switch tc.Mode {
	case "auto":
		return map[string]interface{}{"type": "auto"}
	case "none":
		// For none, we omit tools entirely (handled in buildRequestBody)
		return nil
	case "required":
		return map[string]interface{}{"type": "any"}
	case "named":
		return map[string]interface{}{
			"type": "tool",
			"name": tc.ToolName,
		}
	}
	return map[string]interface{}{"type": "auto"}
}

func (a *AnthropicAdapter) parseResponse(resp *http.Response) (*Response, error) {
	bodyBytes, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, &NetworkError{SDKError: SDKError{Message: "failed to read response", Cause: err}}
	}

	var raw map[string]interface{}
	if err := json.Unmarshal(bodyBytes, &raw); err != nil {
		return nil, &InvalidRequestError{ProviderError: ProviderError{
			SDKError: SDKError{Message: "failed to parse response JSON", Cause: err},
			Provider: "anthropic",
		}}
	}

	response := &Response{
		Provider: "anthropic",
		Raw:      raw,
		Message:  Message{Role: RoleAssistant},
	}

	if id, ok := raw["id"].(string); ok {
		response.ID = id
	}
	if model, ok := raw["model"].(string); ok {
		response.Model = model
	}

	// Parse content blocks
	if content, ok := raw["content"].([]interface{}); ok {
		for _, block := range content {
			blockMap, ok := block.(map[string]interface{})
			if !ok {
				continue
			}
			blockType, _ := blockMap["type"].(string)
			switch blockType {
			case "text":
				if text, ok := blockMap["text"].(string); ok {
					response.Message.Content = append(response.Message.Content, TextPart(text))
				}
			case "tool_use":
				name, _ := blockMap["name"].(string)
				id, _ := blockMap["id"].(string)
				var args json.RawMessage
				if input, ok := blockMap["input"]; ok {
					args, _ = json.Marshal(input)
				}
				response.Message.Content = append(response.Message.Content, ToolCallPart(id, name, args))
			case "thinking":
				text, _ := blockMap["thinking"].(string)
				sig, _ := blockMap["signature"].(string)
				response.Message.Content = append(response.Message.Content, ThinkingPart(text, sig))
			case "redacted_thinking":
				data, _ := blockMap["data"].(string)
				response.Message.Content = append(response.Message.Content, ContentPart{
					Kind:     ContentRedactedThinking,
					Thinking: &ThinkingData{Text: data, Redacted: true},
				})
			}
		}
	}

	// Finish reason
	if stopReason, ok := raw["stop_reason"].(string); ok {
		response.FinishReason = a.mapFinishReason(stopReason)
	}

	// Usage
	response.Usage = a.parseUsage(raw)

	// Rate limit headers
	response.RateLimit = parseRateLimitHeaders(resp.Header)

	return response, nil
}

func (a *AnthropicAdapter) mapFinishReason(reason string) FinishReason {
	switch reason {
	case "end_turn", "stop_sequence":
		return FinishReason{Reason: "stop", Raw: reason}
	case "max_tokens":
		return FinishReason{Reason: "length", Raw: reason}
	case "tool_use":
		return FinishReason{Reason: "tool_calls", Raw: reason}
	default:
		return FinishReason{Reason: "other", Raw: reason}
	}
}

func (a *AnthropicAdapter) parseUsage(raw map[string]interface{}) Usage {
	usage := Usage{}
	usageMap, ok := raw["usage"].(map[string]interface{})
	if !ok {
		return usage
	}

	if v, ok := usageMap["input_tokens"].(float64); ok {
		usage.InputTokens = int(v)
	}
	if v, ok := usageMap["output_tokens"].(float64); ok {
		usage.OutputTokens = int(v)
	}
	usage.TotalTokens = usage.InputTokens + usage.OutputTokens

	// Cache tokens
	if v, ok := usageMap["cache_read_input_tokens"].(float64); ok {
		ct := int(v)
		usage.CacheReadTokens = &ct
	}
	if v, ok := usageMap["cache_creation_input_tokens"].(float64); ok {
		ct := int(v)
		usage.CacheWriteTokens = &ct
	}

	// Estimate reasoning tokens from thinking blocks
	// Anthropic doesn't provide a separate reasoning token count
	// We estimate from thinking block text length (~4 chars per token)
	if content, ok := raw["content"].([]interface{}); ok {
		thinkingChars := 0
		for _, block := range content {
			if bm, ok := block.(map[string]interface{}); ok {
				if bm["type"] == "thinking" {
					if text, ok := bm["thinking"].(string); ok {
						thinkingChars += len(text)
					}
				}
			}
		}
		if thinkingChars > 0 {
			rt := thinkingChars / 4
			if rt == 0 {
				rt = 1
			}
			usage.ReasoningTokens = &rt
		}
	}

	usage.Raw = usageMap
	return usage
}

func (a *AnthropicAdapter) readStream(ctx context.Context, body io.ReadCloser, ch chan<- StreamEvent) {
	defer close(ch)
	defer body.Close()

	reader := newSSEReader(body)
	accumulator := NewStreamAccumulator()

	var inputUsage Usage
	var currentBlockType string
	var currentBlockIndex int
	var currentToolCallID string
	var currentToolCallName string
	var toolCallArgs strings.Builder

	for {
		select {
		case <-ctx.Done():
			ch <- StreamEvent{Type: StreamError, Error: &AbortError{SDKError: SDKError{Message: "stream cancelled"}}}
			return
		default:
		}

		event, err := reader.Next()
		if err == io.EOF {
			break
		}
		if err != nil {
			ch <- StreamEvent{Type: StreamError, Error: &StreamErrorType{SDKError: SDKError{Message: "stream read error", Cause: err}}}
			return
		}

		var data map[string]interface{}
		if err := json.Unmarshal([]byte(event.Data), &data); err != nil {
			continue
		}

		switch event.Event {
		case "message_start":
			// Extract input token count
			if msgData, ok := data["message"].(map[string]interface{}); ok {
				if usageData, ok := msgData["usage"].(map[string]interface{}); ok {
					if v, ok := usageData["input_tokens"].(float64); ok {
						inputUsage.InputTokens = int(v)
					}
					if v, ok := usageData["cache_read_input_tokens"].(float64); ok {
						ct := int(v)
						inputUsage.CacheReadTokens = &ct
					}
					if v, ok := usageData["cache_creation_input_tokens"].(float64); ok {
						ct := int(v)
						inputUsage.CacheWriteTokens = &ct
					}
				}
			}
			evt := StreamEvent{Type: StreamStart}
			ch <- evt

		case "content_block_start":
			if cb, ok := data["content_block"].(map[string]interface{}); ok {
				blockType, _ := cb["type"].(string)
				currentBlockType = blockType
				if idx, ok := data["index"].(float64); ok {
					currentBlockIndex = int(idx)
				}

				switch blockType {
				case "text":
					textID := fmt.Sprintf("text_%d", currentBlockIndex)
					evt := StreamEvent{Type: TextStart, TextID: textID}
					ch <- evt
				case "tool_use":
					currentToolCallID, _ = cb["id"].(string)
					currentToolCallName, _ = cb["name"].(string)
					toolCallArgs.Reset()
					evt := StreamEvent{
						Type: ToolCallStart,
						ToolCall: &ToolCall{
							ID:   currentToolCallID,
							Name: currentToolCallName,
						},
					}
					ch <- evt
				case "thinking":
					evt := StreamEvent{Type: ReasoningStart}
					ch <- evt
				}
			}

		case "content_block_delta":
			if delta, ok := data["delta"].(map[string]interface{}); ok {
				deltaType, _ := delta["type"].(string)
				switch deltaType {
				case "text_delta":
					text, _ := delta["text"].(string)
					textID := fmt.Sprintf("text_%d", currentBlockIndex)
					evt := StreamEvent{Type: TextDelta, Delta: text, TextID: textID}
					ch <- evt
					accumulator.Process(evt)
				case "input_json_delta":
					jsonDelta, _ := delta["partial_json"].(string)
					toolCallArgs.WriteString(jsonDelta)
					evt := StreamEvent{
						Type:  ToolCallDelta,
						Delta: jsonDelta,
						ToolCall: &ToolCall{
							ID:   currentToolCallID,
							Name: currentToolCallName,
						},
					}
					ch <- evt
				case "thinking_delta":
					thinking, _ := delta["thinking"].(string)
					evt := StreamEvent{Type: ReasoningDelta, ReasoningDelta: thinking}
					ch <- evt
					accumulator.Process(evt)
				}
			}

		case "content_block_stop":
			switch currentBlockType {
			case "text":
				textID := fmt.Sprintf("text_%d", currentBlockIndex)
				evt := StreamEvent{Type: TextEnd, TextID: textID}
				ch <- evt
			case "tool_use":
				tc := &ToolCall{
					ID:        currentToolCallID,
					Name:      currentToolCallName,
					Arguments: json.RawMessage(toolCallArgs.String()),
				}
				evt := StreamEvent{Type: ToolCallEnd, ToolCall: tc}
				ch <- evt
				accumulator.Process(evt)
			case "thinking":
				evt := StreamEvent{Type: ReasoningEnd}
				ch <- evt
			}

		case "message_delta":
			if delta, ok := data["delta"].(map[string]interface{}); ok {
				if stopReason, ok := delta["stop_reason"].(string); ok {
					fr := a.mapFinishReason(stopReason)
					// Update usage with output tokens from delta
					if usageData, ok := data["usage"].(map[string]interface{}); ok {
						if v, ok := usageData["output_tokens"].(float64); ok {
							inputUsage.OutputTokens = int(v)
						}
					}
					inputUsage.TotalTokens = inputUsage.InputTokens + inputUsage.OutputTokens

					accResp := accumulator.Response()
					accResp.Provider = "anthropic"
					accResp.Usage = inputUsage
					accResp.FinishReason = fr

					evt := StreamEvent{
						Type:         StreamFinish,
						FinishReason: &fr,
						Usage:        &inputUsage,
						Response:     accResp,
					}
					ch <- evt
					return
				}
			}

		case "message_stop":
			// Final event
			accResp := accumulator.Response()
			accResp.Provider = "anthropic"
			accResp.Usage = inputUsage
			inputUsage.TotalTokens = inputUsage.InputTokens + inputUsage.OutputTokens

			fr := FinishReason{Reason: "stop"}
			evt := StreamEvent{
				Type:         StreamFinish,
				FinishReason: &fr,
				Usage:        &inputUsage,
				Response:     accResp,
			}
			ch <- evt
			return

		case "error":
			errMsg := "stream error"
			if errData, ok := data["error"].(map[string]interface{}); ok {
				if msg, ok := errData["message"].(string); ok {
					errMsg = msg
				}
			}
			ch <- StreamEvent{
				Type:  StreamError,
				Error: &StreamErrorType{SDKError: SDKError{Message: errMsg}},
			}
			return
		}
	}
}
