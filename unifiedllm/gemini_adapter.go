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

	"github.com/google/uuid"
)

// GeminiAdapter implements ProviderAdapter for the Gemini API.
type GeminiAdapter struct {
	apiKey  string
	baseURL string
	http    *httpClient
}

// GeminiAdapterOption configures a GeminiAdapter.
type GeminiAdapterOption func(*GeminiAdapter)

// WithGeminiBaseURL sets a custom base URL.
func WithGeminiBaseURL(url string) GeminiAdapterOption {
	return func(a *GeminiAdapter) {
		a.baseURL = url
	}
}

// NewGeminiAdapter creates a new Gemini adapter.
func NewGeminiAdapter(apiKey string, opts ...GeminiAdapterOption) (*GeminiAdapter, error) {
	if apiKey == "" {
		apiKey = os.Getenv("GEMINI_API_KEY")
	}
	if apiKey == "" {
		apiKey = os.Getenv("GOOGLE_API_KEY")
	}
	if apiKey == "" {
		return nil, &ConfigurationError{SDKError: SDKError{
			Message: "GEMINI_API_KEY or GOOGLE_API_KEY is required",
		}}
	}

	a := &GeminiAdapter{
		apiKey:  apiKey,
		baseURL: "https://generativelanguage.googleapis.com",
		http:    newHTTPClient(),
	}

	if envURL := os.Getenv("GEMINI_BASE_URL"); envURL != "" {
		a.baseURL = strings.TrimRight(envURL, "/")
	}

	for _, opt := range opts {
		opt(a)
	}

	return a, nil
}

func (a *GeminiAdapter) Name() string { return "gemini" }

func (a *GeminiAdapter) SupportsToolChoice(mode string) bool {
	switch mode {
	case "auto", "none", "required", "named":
		return true
	}
	return false
}

// Complete sends a blocking request to the Gemini API.
func (a *GeminiAdapter) Complete(ctx context.Context, req Request) (*Response, error) {
	body, err := a.buildRequestBody(req)
	if err != nil {
		return nil, err
	}

	url := fmt.Sprintf("%s/v1beta/models/%s:generateContent?key=%s", a.baseURL, req.Model, a.apiKey)
	httpReq, err := http.NewRequestWithContext(ctx, "POST", url, bytes.NewReader(body))
	if err != nil {
		return nil, &NetworkError{SDKError: SDKError{Message: "failed to create request", Cause: err}}
	}
	httpReq.Header.Set("Content-Type", "application/json")

	resp, err := a.http.Do(httpReq)
	if err != nil {
		return nil, &NetworkError{SDKError: SDKError{Message: "request failed", Cause: err}}
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return nil, buildErrorFromResponse(resp, "gemini")
	}

	return a.parseResponse(resp)
}

// Stream sends a streaming request to the Gemini API.
func (a *GeminiAdapter) Stream(ctx context.Context, req Request) (<-chan StreamEvent, error) {
	body, err := a.buildRequestBody(req)
	if err != nil {
		return nil, err
	}

	url := fmt.Sprintf("%s/v1beta/models/%s:streamGenerateContent?alt=sse&key=%s", a.baseURL, req.Model, a.apiKey)
	httpReq, err := http.NewRequestWithContext(ctx, "POST", url, bytes.NewReader(body))
	if err != nil {
		return nil, &NetworkError{SDKError: SDKError{Message: "failed to create request", Cause: err}}
	}
	httpReq.Header.Set("Content-Type", "application/json")

	resp, err := a.http.Do(httpReq)
	if err != nil {
		return nil, &NetworkError{SDKError: SDKError{Message: "request failed", Cause: err}}
	}

	if resp.StatusCode != http.StatusOK {
		defer resp.Body.Close()
		return nil, buildErrorFromResponse(resp, "gemini")
	}

	ch := make(chan StreamEvent, 64)
	go a.readStream(ctx, resp.Body, ch)
	return ch, nil
}

func (a *GeminiAdapter) buildRequestBody(req Request) ([]byte, error) {
	body := map[string]interface{}{}

	// Build system instruction from system/developer messages
	var systemParts []interface{}
	var contents []interface{}

	// Build a name->ID mapping for correlating tool results
	// When we see tool results, we need to map the callID back to a function name
	toolCallIDToName := make(map[string]string)
	for _, msg := range req.Messages {
		if msg.Role == RoleAssistant {
			for _, part := range msg.Content {
				if part.Kind == ContentToolCall && part.ToolCall != nil {
					toolCallIDToName[part.ToolCall.ID] = part.ToolCall.Name
				}
			}
		}
	}

	for _, msg := range req.Messages {
		switch msg.Role {
		case RoleSystem, RoleDeveloper:
			for _, part := range msg.Content {
				if part.Kind == ContentText {
					systemParts = append(systemParts, map[string]interface{}{
						"text": part.Text,
					})
				}
			}
		case RoleUser:
			parts := a.translateUserParts(msg)
			contents = append(contents, map[string]interface{}{
				"role":  "user",
				"parts": parts,
			})
		case RoleAssistant:
			parts := a.translateAssistantParts(msg)
			contents = append(contents, map[string]interface{}{
				"role":  "model",
				"parts": parts,
			})
		case RoleTool:
			parts := a.translateToolParts(msg, toolCallIDToName)
			contents = append(contents, map[string]interface{}{
				"role":  "user",
				"parts": parts,
			})
		}
	}

	if len(systemParts) > 0 {
		body["systemInstruction"] = map[string]interface{}{
			"parts": systemParts,
		}
	}
	if len(contents) > 0 {
		body["contents"] = contents
	}

	// Generation config
	genConfig := map[string]interface{}{}
	if req.Temperature != nil {
		genConfig["temperature"] = *req.Temperature
	}
	if req.TopP != nil {
		genConfig["topP"] = *req.TopP
	}
	if req.MaxTokens != nil {
		genConfig["maxOutputTokens"] = *req.MaxTokens
	}
	if len(req.StopSequences) > 0 {
		genConfig["stopSequences"] = req.StopSequences
	}
	if req.ResponseFormat != nil {
		switch req.ResponseFormat.Type {
		case "json", "json_schema":
			genConfig["responseMimeType"] = "application/json"
			if req.ResponseFormat.JSONSchema != nil {
				genConfig["responseSchema"] = req.ResponseFormat.JSONSchema
			}
		}
	}
	if len(genConfig) > 0 {
		body["generationConfig"] = genConfig
	}

	// Tools
	if len(req.ToolDefs) > 0 && (req.ToolChoice == nil || req.ToolChoice.Mode != "none") {
		var funcDecls []interface{}
		for _, td := range req.ToolDefs {
			funcDecls = append(funcDecls, map[string]interface{}{
				"name":        td.Name,
				"description": td.Description,
				"parameters":  td.Parameters,
			})
		}
		body["tools"] = []interface{}{
			map[string]interface{}{
				"functionDeclarations": funcDecls,
			},
		}
	}

	// Tool choice
	if req.ToolChoice != nil && len(req.ToolDefs) > 0 {
		tc := a.translateToolChoice(req.ToolChoice)
		if tc != nil {
			body["toolConfig"] = map[string]interface{}{
				"functionCallingConfig": tc,
			}
		}
	}

	// Provider options
	if opts, ok := req.ProviderOptions["gemini"].(map[string]interface{}); ok {
		if thinking, ok := opts["thinking"]; ok {
			body["thinkingConfig"] = thinking
		}
		// Allow other options to be merged
		for k, v := range opts {
			if k != "thinking" {
				body[k] = v
			}
		}
	}

	return json.Marshal(body)
}

func (a *GeminiAdapter) translateUserParts(msg Message) []interface{} {
	var parts []interface{}
	for _, part := range msg.Content {
		switch part.Kind {
		case ContentText:
			parts = append(parts, map[string]interface{}{
				"text": part.Text,
			})
		case ContentImage:
			if part.Image != nil {
				if part.Image.URL != "" {
					mediaType := part.Image.MediaType
					if mediaType == "" {
						mediaType = "image/png"
					}
					parts = append(parts, map[string]interface{}{
						"fileData": map[string]interface{}{
							"mimeType": mediaType,
							"fileUri":  part.Image.URL,
						},
					})
				} else if len(part.Image.Data) > 0 {
					mediaType := part.Image.MediaType
					if mediaType == "" {
						mediaType = "image/png"
					}
					parts = append(parts, map[string]interface{}{
						"inlineData": map[string]interface{}{
							"mimeType": mediaType,
							"data":     base64.StdEncoding.EncodeToString(part.Image.Data),
						},
					})
				}
			}
		}
	}
	if len(parts) == 0 {
		parts = append(parts, map[string]interface{}{"text": ""})
	}
	return parts
}

func (a *GeminiAdapter) translateAssistantParts(msg Message) []interface{} {
	var parts []interface{}
	for _, part := range msg.Content {
		switch part.Kind {
		case ContentText:
			parts = append(parts, map[string]interface{}{
				"text": part.Text,
			})
		case ContentToolCall:
			if part.ToolCall != nil {
				var args interface{}
				if err := json.Unmarshal(part.ToolCall.Arguments, &args); err != nil {
					args = map[string]interface{}{}
				}
				parts = append(parts, map[string]interface{}{
					"functionCall": map[string]interface{}{
						"name": part.ToolCall.Name,
						"args": args,
					},
				})
			}
		}
	}
	if len(parts) == 0 {
		parts = append(parts, map[string]interface{}{"text": ""})
	}
	return parts
}

func (a *GeminiAdapter) translateToolParts(msg Message, idToName map[string]string) []interface{} {
	var parts []interface{}
	for _, part := range msg.Content {
		if part.Kind == ContentToolResult && part.ToolResult != nil {
			funcName := idToName[part.ToolResult.ToolCallID]
			if funcName == "" {
				funcName = part.ToolResult.ToolCallID
			}

			// Gemini expects response as a dict, wrap strings
			var responseObj interface{}
			var parsed interface{}
			if err := json.Unmarshal(part.ToolResult.Content, &parsed); err == nil {
				if _, isMap := parsed.(map[string]interface{}); isMap {
					responseObj = parsed
				} else {
					responseObj = map[string]interface{}{"result": parsed}
				}
			} else {
				responseObj = map[string]interface{}{"result": string(part.ToolResult.Content)}
			}

			parts = append(parts, map[string]interface{}{
				"functionResponse": map[string]interface{}{
					"name":     funcName,
					"response": responseObj,
				},
			})
		}
	}
	return parts
}

func (a *GeminiAdapter) translateToolChoice(tc *ToolChoice) interface{} {
	switch tc.Mode {
	case "auto":
		return map[string]interface{}{"mode": "AUTO"}
	case "none":
		return map[string]interface{}{"mode": "NONE"}
	case "required":
		return map[string]interface{}{"mode": "ANY"}
	case "named":
		return map[string]interface{}{
			"mode":                 "ANY",
			"allowedFunctionNames": []string{tc.ToolName},
		}
	}
	return nil
}

func (a *GeminiAdapter) parseResponse(resp *http.Response) (*Response, error) {
	bodyBytes, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, &NetworkError{SDKError: SDKError{Message: "failed to read response", Cause: err}}
	}

	var raw map[string]interface{}
	if err := json.Unmarshal(bodyBytes, &raw); err != nil {
		return nil, &InvalidRequestError{ProviderError: ProviderError{
			SDKError: SDKError{Message: "failed to parse response JSON", Cause: err},
			Provider: "gemini",
		}}
	}

	response := &Response{
		Provider: "gemini",
		Raw:      raw,
		Message:  Message{Role: RoleAssistant},
	}

	// Parse candidates
	hasToolCalls := false
	if candidates, ok := raw["candidates"].([]interface{}); ok && len(candidates) > 0 {
		candidate, ok := candidates[0].(map[string]interface{})
		if ok {
			if content, ok := candidate["content"].(map[string]interface{}); ok {
				if parts, ok := content["parts"].([]interface{}); ok {
					for _, p := range parts {
						pm, ok := p.(map[string]interface{})
						if !ok {
							continue
						}
						if text, ok := pm["text"].(string); ok {
							response.Message.Content = append(response.Message.Content, TextPart(text))
						}
						if fc, ok := pm["functionCall"].(map[string]interface{}); ok {
							name, _ := fc["name"].(string)
							args, _ := json.Marshal(fc["args"])
							callID := "call_" + uuid.New().String()
							response.Message.Content = append(response.Message.Content, ToolCallPart(callID, name, args))
							hasToolCalls = true
						}
					}
				}
			}

			// Finish reason
			if fr, ok := candidate["finishReason"].(string); ok {
				response.FinishReason = a.mapFinishReason(fr, hasToolCalls)
			} else if hasToolCalls {
				response.FinishReason = FinishReason{Reason: "tool_calls", Raw: ""}
			}
		}
	}

	// Usage
	response.Usage = a.parseUsage(raw)

	// Model
	if model, ok := raw["modelVersion"].(string); ok {
		response.Model = model
	}

	return response, nil
}

func (a *GeminiAdapter) mapFinishReason(reason string, hasToolCalls bool) FinishReason {
	if hasToolCalls {
		return FinishReason{Reason: "tool_calls", Raw: reason}
	}
	switch reason {
	case "STOP":
		return FinishReason{Reason: "stop", Raw: reason}
	case "MAX_TOKENS":
		return FinishReason{Reason: "length", Raw: reason}
	case "SAFETY", "RECITATION":
		return FinishReason{Reason: "content_filter", Raw: reason}
	default:
		return FinishReason{Reason: "other", Raw: reason}
	}
}

func (a *GeminiAdapter) parseUsage(raw map[string]interface{}) Usage {
	usage := Usage{}
	usageMap, ok := raw["usageMetadata"].(map[string]interface{})
	if !ok {
		return usage
	}

	if v, ok := usageMap["promptTokenCount"].(float64); ok {
		usage.InputTokens = int(v)
	}
	if v, ok := usageMap["candidatesTokenCount"].(float64); ok {
		usage.OutputTokens = int(v)
	}
	usage.TotalTokens = usage.InputTokens + usage.OutputTokens

	if v, ok := usageMap["thoughtsTokenCount"].(float64); ok {
		rt := int(v)
		usage.ReasoningTokens = &rt
	}
	if v, ok := usageMap["cachedContentTokenCount"].(float64); ok {
		ct := int(v)
		usage.CacheReadTokens = &ct
	}

	usage.Raw = usageMap
	return usage
}

func (a *GeminiAdapter) readStream(ctx context.Context, body io.ReadCloser, ch chan<- StreamEvent) {
	defer close(ch)
	defer body.Close()

	reader := newSSEReader(body)
	accumulator := NewStreamAccumulator()
	textStarted := false
	var lastUsage *Usage

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

		if event.Data == "[DONE]" {
			break
		}

		var data map[string]interface{}
		if err := json.Unmarshal([]byte(event.Data), &data); err != nil {
			continue
		}

		// Parse usage metadata from each chunk
		if usageMap, ok := data["usageMetadata"].(map[string]interface{}); ok {
			u := Usage{}
			if v, ok := usageMap["promptTokenCount"].(float64); ok {
				u.InputTokens = int(v)
			}
			if v, ok := usageMap["candidatesTokenCount"].(float64); ok {
				u.OutputTokens = int(v)
			}
			u.TotalTokens = u.InputTokens + u.OutputTokens
			if v, ok := usageMap["thoughtsTokenCount"].(float64); ok {
				rt := int(v)
				u.ReasoningTokens = &rt
			}
			if v, ok := usageMap["cachedContentTokenCount"].(float64); ok {
				ct := int(v)
				u.CacheReadTokens = &ct
			}
			u.Raw = usageMap
			lastUsage = &u
		}

		var finishReason string
		hasToolCalls := false

		if candidates, ok := data["candidates"].([]interface{}); ok && len(candidates) > 0 {
			candidate, ok := candidates[0].(map[string]interface{})
			if !ok {
				continue
			}

			if fr, ok := candidate["finishReason"].(string); ok {
				finishReason = fr
			}

			if content, ok := candidate["content"].(map[string]interface{}); ok {
				if parts, ok := content["parts"].([]interface{}); ok {
					for _, p := range parts {
						pm, ok := p.(map[string]interface{})
						if !ok {
							continue
						}

						if text, ok := pm["text"].(string); ok {
							if !textStarted {
								evt := StreamEvent{Type: TextStart, TextID: "default"}
								ch <- evt
								textStarted = true
							}
							evt := StreamEvent{Type: TextDelta, Delta: text, TextID: "default"}
							ch <- evt
							accumulator.Process(evt)
						}

						if fc, ok := pm["functionCall"].(map[string]interface{}); ok {
							name, _ := fc["name"].(string)
							args, _ := json.Marshal(fc["args"])
							callID := "call_" + uuid.New().String()
							tc := &ToolCall{
								ID:        callID,
								Name:      name,
								Arguments: args,
							}
							hasToolCalls = true

							// Emit start and end (Gemini sends complete function calls)
							ch <- StreamEvent{Type: ToolCallStart, ToolCall: tc}
							evt := StreamEvent{Type: ToolCallEnd, ToolCall: tc}
							ch <- evt
							accumulator.Process(evt)
						}
					}
				}
			}

			if finishReason != "" {
				if textStarted {
					ch <- StreamEvent{Type: TextEnd, TextID: "default"}
					textStarted = false
				}
				fr := a.mapFinishReason(finishReason, hasToolCalls)
				accResp := accumulator.Response()
				accResp.Provider = "gemini"
				accResp.Raw = data
				accResp.FinishReason = fr
				if lastUsage != nil {
					accResp.Usage = *lastUsage
				}

				ch <- StreamEvent{
					Type:         StreamFinish,
					FinishReason: &fr,
					Usage:        lastUsage,
					Response:     accResp,
				}
				return
			}
		}
	}

	// End of stream without explicit finish
	if textStarted {
		ch <- StreamEvent{Type: TextEnd, TextID: "default"}
	}

	fr := FinishReason{Reason: "stop"}
	accResp := accumulator.Response()
	accResp.Provider = "gemini"
	if lastUsage != nil {
		accResp.Usage = *lastUsage
	}

	ch <- StreamEvent{
		Type:         StreamFinish,
		FinishReason: &fr,
		Usage:        lastUsage,
		Response:     accResp,
	}
}
