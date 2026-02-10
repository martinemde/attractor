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

// OpenAIAdapter implements ProviderAdapter for the OpenAI Responses API.
type OpenAIAdapter struct {
	apiKey  string
	baseURL string
	http    *httpClient
}

// OpenAIAdapterOption configures an OpenAIAdapter.
type OpenAIAdapterOption func(*OpenAIAdapter)

// WithOpenAIBaseURL sets a custom base URL.
func WithOpenAIBaseURL(url string) OpenAIAdapterOption {
	return func(a *OpenAIAdapter) {
		a.baseURL = url
	}
}

// NewOpenAIAdapter creates a new OpenAI adapter using the Responses API.
func NewOpenAIAdapter(apiKey string, opts ...OpenAIAdapterOption) (*OpenAIAdapter, error) {
	if apiKey == "" {
		apiKey = os.Getenv("OPENAI_API_KEY")
	}
	if apiKey == "" {
		return nil, &ConfigurationError{SDKError: SDKError{
			Message: "OPENAI_API_KEY is required",
		}}
	}

	a := &OpenAIAdapter{
		apiKey:  apiKey,
		baseURL: "https://api.openai.com",
		http:    newHTTPClient(),
	}

	if envURL := os.Getenv("OPENAI_BASE_URL"); envURL != "" {
		a.baseURL = strings.TrimRight(envURL, "/")
	}

	for _, opt := range opts {
		opt(a)
	}

	return a, nil
}

func (a *OpenAIAdapter) Name() string { return "openai" }

func (a *OpenAIAdapter) SupportsToolChoice(mode string) bool {
	switch mode {
	case "auto", "none", "required", "named":
		return true
	}
	return false
}

// Complete sends a blocking request to the OpenAI Responses API.
func (a *OpenAIAdapter) Complete(ctx context.Context, req Request) (*Response, error) {
	body, err := a.buildRequestBody(req, false)
	if err != nil {
		return nil, err
	}

	httpReq, err := http.NewRequestWithContext(ctx, "POST", a.baseURL+"/v1/responses", bytes.NewReader(body))
	if err != nil {
		return nil, &NetworkError{SDKError: SDKError{Message: "failed to create request", Cause: err}}
	}
	httpReq.Header.Set("Authorization", "Bearer "+a.apiKey)
	httpReq.Header.Set("Content-Type", "application/json")

	resp, err := a.http.Do(httpReq)
	if err != nil {
		return nil, &NetworkError{SDKError: SDKError{Message: "request failed", Cause: err}}
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return nil, buildErrorFromResponse(resp, "openai")
	}

	return a.parseResponse(resp)
}

// Stream sends a streaming request to the OpenAI Responses API.
func (a *OpenAIAdapter) Stream(ctx context.Context, req Request) (<-chan StreamEvent, error) {
	body, err := a.buildRequestBody(req, true)
	if err != nil {
		return nil, err
	}

	httpReq, err := http.NewRequestWithContext(ctx, "POST", a.baseURL+"/v1/responses", bytes.NewReader(body))
	if err != nil {
		return nil, &NetworkError{SDKError: SDKError{Message: "failed to create request", Cause: err}}
	}
	httpReq.Header.Set("Authorization", "Bearer "+a.apiKey)
	httpReq.Header.Set("Content-Type", "application/json")

	resp, err := a.http.Do(httpReq)
	if err != nil {
		return nil, &NetworkError{SDKError: SDKError{Message: "request failed", Cause: err}}
	}

	if resp.StatusCode != http.StatusOK {
		defer resp.Body.Close()
		return nil, buildErrorFromResponse(resp, "openai")
	}

	ch := make(chan StreamEvent, 64)
	go a.readStream(ctx, resp.Body, ch)
	return ch, nil
}

func (a *OpenAIAdapter) buildRequestBody(req Request, stream bool) ([]byte, error) {
	body := map[string]interface{}{
		"model": req.Model,
	}

	// Extract system/developer messages into instructions
	var instructions []string
	var input []interface{}

	for _, msg := range req.Messages {
		switch msg.Role {
		case RoleSystem, RoleDeveloper:
			instructions = append(instructions, msg.TextContent())
		case RoleUser:
			input = append(input, a.translateUserMessage(msg))
		case RoleAssistant:
			items := a.translateAssistantMessage(msg)
			input = append(input, items...)
		case RoleTool:
			for _, part := range msg.Content {
				if part.Kind == ContentToolResult && part.ToolResult != nil {
					item := map[string]interface{}{
						"type":    "function_call_output",
						"call_id": part.ToolResult.ToolCallID,
						"output":  string(part.ToolResult.Content),
					}
					input = append(input, item)
				}
			}
		}
	}

	if len(instructions) > 0 {
		body["instructions"] = strings.Join(instructions, "\n\n")
	}
	if len(input) > 0 {
		body["input"] = input
	}

	// Tools
	if len(req.ToolDefs) > 0 {
		var tools []interface{}
		for _, td := range req.ToolDefs {
			tools = append(tools, map[string]interface{}{
				"type": "function",
				"function": map[string]interface{}{
					"name":        td.Name,
					"description": td.Description,
					"parameters":  td.Parameters,
				},
			})
		}
		body["tools"] = tools
	}

	// Tool choice
	if req.ToolChoice != nil {
		body["tool_choice"] = a.translateToolChoice(req.ToolChoice)
	}

	// Generation parameters
	if req.Temperature != nil {
		body["temperature"] = *req.Temperature
	}
	if req.TopP != nil {
		body["top_p"] = *req.TopP
	}
	if req.MaxTokens != nil {
		body["max_output_tokens"] = *req.MaxTokens
	}

	// Reasoning effort
	if req.ReasoningEffort != "" {
		body["reasoning"] = map[string]interface{}{
			"effort": req.ReasoningEffort,
		}
	}

	// Response format
	if req.ResponseFormat != nil {
		switch req.ResponseFormat.Type {
		case "json_schema":
			body["text"] = map[string]interface{}{
				"format": map[string]interface{}{
					"type":        "json_schema",
					"json_schema": req.ResponseFormat.JSONSchema,
					"strict":      req.ResponseFormat.Strict,
				},
			}
		case "json":
			body["text"] = map[string]interface{}{
				"format": map[string]interface{}{
					"type": "json_object",
				},
			}
		}
	}

	// Streaming
	if stream {
		body["stream"] = true
	}

	// Provider options
	if opts, ok := req.ProviderOptions["openai"].(map[string]interface{}); ok {
		for k, v := range opts {
			body[k] = v
		}
	}

	return json.Marshal(body)
}

func (a *OpenAIAdapter) translateUserMessage(msg Message) map[string]interface{} {
	item := map[string]interface{}{
		"type": "message",
		"role": "user",
	}

	var content []interface{}
	for _, part := range msg.Content {
		switch part.Kind {
		case ContentText:
			content = append(content, map[string]interface{}{
				"type": "input_text",
				"text": part.Text,
			})
		case ContentImage:
			if part.Image != nil {
				var imageURL string
				if part.Image.URL != "" {
					imageURL = part.Image.URL
				} else if len(part.Image.Data) > 0 {
					mediaType := part.Image.MediaType
					if mediaType == "" {
						mediaType = "image/png"
					}
					imageURL = fmt.Sprintf("data:%s;base64,%s", mediaType, base64.StdEncoding.EncodeToString(part.Image.Data))
				}
				if imageURL != "" {
					content = append(content, map[string]interface{}{
						"type":      "input_image",
						"image_url": imageURL,
					})
				}
			}
		}
	}

	item["content"] = content
	return item
}

func (a *OpenAIAdapter) translateAssistantMessage(msg Message) []interface{} {
	var items []interface{}

	// Collect text parts into a message
	var textParts []interface{}
	for _, part := range msg.Content {
		switch part.Kind {
		case ContentText:
			textParts = append(textParts, map[string]interface{}{
				"type": "output_text",
				"text": part.Text,
			})
		case ContentToolCall:
			if part.ToolCall != nil {
				argStr := string(part.ToolCall.Arguments)
				items = append(items, map[string]interface{}{
					"type":      "function_call",
					"id":        part.ToolCall.ID,
					"name":      part.ToolCall.Name,
					"arguments": argStr,
				})
			}
		}
	}

	if len(textParts) > 0 {
		items = append([]interface{}{map[string]interface{}{
			"type":    "message",
			"role":    "assistant",
			"content": textParts,
		}}, items...)
	}

	return items
}

func (a *OpenAIAdapter) translateToolChoice(tc *ToolChoice) interface{} {
	switch tc.Mode {
	case "auto":
		return "auto"
	case "none":
		return "none"
	case "required":
		return "required"
	case "named":
		return map[string]interface{}{
			"type": "function",
			"function": map[string]interface{}{
				"name": tc.ToolName,
			},
		}
	}
	return "auto"
}

func (a *OpenAIAdapter) parseResponse(resp *http.Response) (*Response, error) {
	bodyBytes, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, &NetworkError{SDKError: SDKError{Message: "failed to read response", Cause: err}}
	}

	var raw map[string]interface{}
	if err := json.Unmarshal(bodyBytes, &raw); err != nil {
		return nil, &InvalidRequestError{ProviderError: ProviderError{
			SDKError: SDKError{Message: "failed to parse response JSON", Cause: err},
			Provider: "openai",
		}}
	}

	response := &Response{
		Provider: "openai",
		Raw:      raw,
		Message: Message{
			Role: RoleAssistant,
		},
	}

	// ID
	if id, ok := raw["id"].(string); ok {
		response.ID = id
	}
	// Model
	if model, ok := raw["model"].(string); ok {
		response.Model = model
	}

	// Parse output items
	if output, ok := raw["output"].([]interface{}); ok {
		for _, item := range output {
			itemMap, ok := item.(map[string]interface{})
			if !ok {
				continue
			}
			itemType, _ := itemMap["type"].(string)
			switch itemType {
			case "message":
				if content, ok := itemMap["content"].([]interface{}); ok {
					for _, c := range content {
						cMap, ok := c.(map[string]interface{})
						if !ok {
							continue
						}
						cType, _ := cMap["type"].(string)
						if cType == "output_text" {
							if text, ok := cMap["text"].(string); ok {
								response.Message.Content = append(response.Message.Content, TextPart(text))
							}
						}
					}
				}
			case "function_call":
				name, _ := itemMap["name"].(string)
				id, _ := itemMap["id"].(string)
				argsStr, _ := itemMap["arguments"].(string)
				response.Message.Content = append(response.Message.Content, ToolCallPart(id, name, json.RawMessage(argsStr)))
			}
		}
	}

	// Finish reason
	if status, ok := raw["status"].(string); ok {
		response.FinishReason = a.mapFinishReason(status, response.Message.Content)
	}

	// Usage
	response.Usage = a.parseUsage(raw)

	// Rate limit headers
	response.RateLimit = parseRateLimitHeaders(resp.Header)

	return response, nil
}

func (a *OpenAIAdapter) mapFinishReason(status string, content []ContentPart) FinishReason {
	// Check if we have tool calls
	hasToolCalls := false
	for _, part := range content {
		if part.Kind == ContentToolCall {
			hasToolCalls = true
			break
		}
	}

	switch status {
	case "completed":
		if hasToolCalls {
			return FinishReason{Reason: "tool_calls", Raw: status}
		}
		return FinishReason{Reason: "stop", Raw: status}
	case "incomplete":
		return FinishReason{Reason: "length", Raw: status}
	case "failed":
		return FinishReason{Reason: "error", Raw: status}
	default:
		return FinishReason{Reason: "other", Raw: status}
	}
}

func (a *OpenAIAdapter) parseUsage(raw map[string]interface{}) Usage {
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

	// Reasoning tokens
	if details, ok := usageMap["output_tokens_details"].(map[string]interface{}); ok {
		if v, ok := details["reasoning_tokens"].(float64); ok {
			rt := int(v)
			usage.ReasoningTokens = &rt
		}
	}

	// Cache read tokens
	if details, ok := usageMap["input_tokens_details"].(map[string]interface{}); ok {
		if v, ok := details["cached_tokens"].(float64); ok {
			ct := int(v)
			usage.CacheReadTokens = &ct
		}
	}

	usage.Raw = usageMap
	return usage
}

func (a *OpenAIAdapter) readStream(ctx context.Context, body io.ReadCloser, ch chan<- StreamEvent) {
	defer close(ch)
	defer body.Close()

	reader := newSSEReader(body)
	textStarted := false
	var currentToolCall *ToolCall
	accumulator := NewStreamAccumulator()

	// Emit StreamStart
	ch <- StreamEvent{Type: StreamStart}

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

		if event.Event == "[DONE]" || event.Data == "[DONE]" {
			break
		}

		var data map[string]interface{}
		if err := json.Unmarshal([]byte(event.Data), &data); err != nil {
			continue
		}

		switch event.Event {
		case "response.output_text.delta":
			delta, _ := data["delta"].(string)
			if !textStarted {
				evt := StreamEvent{Type: TextStart, TextID: "default"}
				ch <- evt
				accumulator.Process(evt)
				textStarted = true
			}
			evt := StreamEvent{Type: TextDelta, Delta: delta, TextID: "default"}
			ch <- evt
			accumulator.Process(evt)

		case "response.function_call_arguments.delta":
			delta, _ := data["delta"].(string)
			callID, _ := data["item_id"].(string)
			name, _ := data["name"].(string)

			if currentToolCall == nil || currentToolCall.ID != callID {
				// Start new tool call
				currentToolCall = &ToolCall{ID: callID, Name: name, RawArguments: delta}
				evt := StreamEvent{
					Type:     ToolCallStart,
					ToolCall: &ToolCall{ID: callID, Name: name},
				}
				ch <- evt
			} else {
				currentToolCall.RawArguments += delta
			}
			evt := StreamEvent{Type: ToolCallDelta, Delta: delta, ToolCall: &ToolCall{ID: callID, Name: name}}
			ch <- evt

		case "response.output_item.done":
			itemType, _ := data["type"].(string)
			if itemType == "function_call" || (data["item"] != nil && isFunction(data["item"])) {
				if currentToolCall != nil {
					// Parse arguments
					if item, ok := data["item"].(map[string]interface{}); ok {
						if args, ok := item["arguments"].(string); ok {
							currentToolCall.Arguments = json.RawMessage(args)
						}
						if name, ok := item["name"].(string); ok {
							currentToolCall.Name = name
						}
						if id, ok := item["id"].(string); ok {
							currentToolCall.ID = id
						}
					}
					evt := StreamEvent{Type: ToolCallEnd, ToolCall: currentToolCall}
					ch <- evt
					accumulator.Process(evt)
					currentToolCall = nil
				}
			} else {
				// Text end
				if textStarted {
					evt := StreamEvent{Type: TextEnd, TextID: "default"}
					ch <- evt
					textStarted = false
				}
			}

		case "response.completed":
			var parsedResp map[string]interface{}
			if respData, ok := data["response"].(map[string]interface{}); ok {
				parsedResp = respData
			} else {
				parsedResp = data
			}

			usage := a.parseUsage(parsedResp)

			status, _ := parsedResp["status"].(string)
			fr := a.mapFinishReason(status, accumulator.Response().Message.Content)

			accResp := accumulator.Response()
			accResp.Provider = "openai"
			accResp.Raw = parsedResp
			accResp.Usage = usage
			accResp.FinishReason = fr
			if id, ok := parsedResp["id"].(string); ok {
				accResp.ID = id
			}
			if model, ok := parsedResp["model"].(string); ok {
				accResp.Model = model
			}

			evt := StreamEvent{
				Type:         StreamFinish,
				FinishReason: &fr,
				Usage:        &usage,
				Response:     accResp,
			}
			ch <- evt
			return
		}
	}

	// If we reach here without a response.completed event, send finish
	accResp := accumulator.Response()
	accResp.Provider = "openai"
	fr := FinishReason{Reason: "stop"}
	ch <- StreamEvent{
		Type:         StreamFinish,
		FinishReason: &fr,
		Response:     accResp,
	}
}

func isFunction(item interface{}) bool {
	m, ok := item.(map[string]interface{})
	if !ok {
		return false
	}
	t, _ := m["type"].(string)
	return t == "function_call"
}
