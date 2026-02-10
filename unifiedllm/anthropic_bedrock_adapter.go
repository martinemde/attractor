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
	"time"
)

// AnthropicBedrockAdapter implements ProviderAdapter for Anthropic models
// hosted on AWS Bedrock, using the Bedrock InvokeModel API.
//
// The request/response body format matches the Anthropic Messages API,
// but authentication uses AWS Signature V4 and the model ID is specified
// in the URL path rather than the request body.
type AnthropicBedrockAdapter struct {
	region  string
	creds   awsCredentials
	baseURL string
	http    *httpClient

	// format provides shared Anthropic message format helpers.
	format *AnthropicAdapter
}

// AnthropicBedrockAdapterOption configures an AnthropicBedrockAdapter.
type AnthropicBedrockAdapterOption func(*AnthropicBedrockAdapter)

// WithBedrockRegion sets the AWS region.
func WithBedrockRegion(region string) AnthropicBedrockAdapterOption {
	return func(a *AnthropicBedrockAdapter) {
		a.region = region
	}
}

// WithBedrockCredentials sets explicit AWS credentials.
func WithBedrockCredentials(accessKey, secretKey, sessionToken string) AnthropicBedrockAdapterOption {
	return func(a *AnthropicBedrockAdapter) {
		a.creds = awsCredentials{
			AccessKeyID:    accessKey,
			SecretAccessKey: secretKey,
			SessionToken:   sessionToken,
		}
	}
}

// WithBedrockBaseURL sets a custom base URL (useful for testing or VPC endpoints).
func WithBedrockBaseURL(url string) AnthropicBedrockAdapterOption {
	return func(a *AnthropicBedrockAdapter) {
		a.baseURL = strings.TrimRight(url, "/")
	}
}

// NewAnthropicBedrockAdapter creates a new adapter for Anthropic models on AWS Bedrock.
// Credentials and region are read from environment variables if not provided via options.
//
// Environment variables:
//   - AWS_REGION or AWS_DEFAULT_REGION
//   - AWS_ACCESS_KEY_ID
//   - AWS_SECRET_ACCESS_KEY
//   - AWS_SESSION_TOKEN (optional, for temporary credentials)
func NewAnthropicBedrockAdapter(opts ...AnthropicBedrockAdapterOption) (*AnthropicBedrockAdapter, error) {
	a := &AnthropicBedrockAdapter{
		http:   newHTTPClient(),
		format: &AnthropicAdapter{},
	}

	for _, opt := range opts {
		opt(a)
	}

	if a.region == "" {
		a.region = os.Getenv("AWS_REGION")
		if a.region == "" {
			a.region = os.Getenv("AWS_DEFAULT_REGION")
		}
	}
	if a.creds.AccessKeyID == "" {
		a.creds.AccessKeyID = os.Getenv("AWS_ACCESS_KEY_ID")
	}
	if a.creds.SecretAccessKey == "" {
		a.creds.SecretAccessKey = os.Getenv("AWS_SECRET_ACCESS_KEY")
	}
	if a.creds.SessionToken == "" {
		a.creds.SessionToken = os.Getenv("AWS_SESSION_TOKEN")
	}

	if a.region == "" {
		return nil, &ConfigurationError{SDKError: SDKError{
			Message: "AWS region is required: set AWS_REGION or use WithBedrockRegion",
		}}
	}
	if a.creds.AccessKeyID == "" || a.creds.SecretAccessKey == "" {
		return nil, &ConfigurationError{SDKError: SDKError{
			Message: "AWS credentials are required: set AWS_ACCESS_KEY_ID and AWS_SECRET_ACCESS_KEY or use WithBedrockCredentials",
		}}
	}

	if a.baseURL == "" {
		a.baseURL = fmt.Sprintf("https://bedrock-runtime.%s.amazonaws.com", a.region)
	}

	return a, nil
}

func (a *AnthropicBedrockAdapter) Name() string { return "anthropic_bedrock" }

func (a *AnthropicBedrockAdapter) SupportsToolChoice(mode string) bool {
	switch mode {
	case "auto", "none", "required", "named":
		return true
	}
	return false
}

// Complete sends a blocking request to the Bedrock InvokeModel endpoint.
func (a *AnthropicBedrockAdapter) Complete(ctx context.Context, req Request) (*Response, error) {
	body, err := a.buildRequestBody(req)
	if err != nil {
		return nil, err
	}

	endpoint := a.baseURL + "/model/" + req.Model + "/invoke"
	httpReq, err := http.NewRequestWithContext(ctx, "POST", endpoint, bytes.NewReader(body))
	if err != nil {
		return nil, &NetworkError{SDKError: SDKError{Message: "failed to create request", Cause: err}}
	}

	httpReq.Header.Set("content-type", "application/json")
	httpReq.Header.Set("accept", "application/json")
	awsSigV4Sign(httpReq, body, a.creds, a.region, "bedrock", time.Now().UTC())

	resp, err := a.http.Do(httpReq)
	if err != nil {
		return nil, &NetworkError{SDKError: SDKError{Message: "request failed", Cause: err}}
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return nil, buildErrorFromResponse(resp, "anthropic_bedrock")
	}

	return a.parseResponse(resp)
}

// Stream sends a streaming request to the Bedrock InvokeModelWithResponseStream endpoint.
func (a *AnthropicBedrockAdapter) Stream(ctx context.Context, req Request) (<-chan StreamEvent, error) {
	body, err := a.buildRequestBody(req)
	if err != nil {
		return nil, err
	}

	endpoint := a.baseURL + "/model/" + req.Model + "/invoke-with-response-stream"
	httpReq, err := http.NewRequestWithContext(ctx, "POST", endpoint, bytes.NewReader(body))
	if err != nil {
		return nil, &NetworkError{SDKError: SDKError{Message: "failed to create request", Cause: err}}
	}

	httpReq.Header.Set("content-type", "application/json")
	awsSigV4Sign(httpReq, body, a.creds, a.region, "bedrock", time.Now().UTC())

	resp, err := a.http.Do(httpReq)
	if err != nil {
		return nil, &NetworkError{SDKError: SDKError{Message: "request failed", Cause: err}}
	}

	if resp.StatusCode != http.StatusOK {
		defer resp.Body.Close()
		return nil, buildErrorFromResponse(resp, "anthropic_bedrock")
	}

	ch := make(chan StreamEvent, 64)
	go a.readStream(ctx, resp.Body, ch)
	return ch, nil
}

// buildRequestBody delegates to the Anthropic adapter for message format
// translation, then applies Bedrock-specific modifications.
func (a *AnthropicBedrockAdapter) buildRequestBody(req Request) ([]byte, error) {
	// Use the shared Anthropic adapter to build the body.
	// Pass stream=false because Bedrock streaming is controlled by endpoint, not body field.
	rawBody, _, err := a.format.buildRequestBody(req, false)
	if err != nil {
		return nil, err
	}

	var body map[string]interface{}
	if err := json.Unmarshal(rawBody, &body); err != nil {
		return nil, &InvalidRequestError{ProviderError: ProviderError{
			SDKError: SDKError{Message: "failed to process request body", Cause: err},
			Provider: "anthropic_bedrock",
		}}
	}

	// Bedrock-specific: model is in the URL, not the body
	delete(body, "model")

	// Bedrock-specific: anthropic_version goes in the body, not a header
	body["anthropic_version"] = "bedrock-2023-05-31"

	// Allow override from provider options
	if opts, ok := req.ProviderOptions["anthropic_bedrock"].(map[string]interface{}); ok {
		if v, ok := opts["anthropic_version"].(string); ok {
			body["anthropic_version"] = v
		}
	}

	return json.Marshal(body)
}

// parseResponse delegates to the Anthropic adapter since Bedrock returns
// the same response format as the direct Anthropic API.
func (a *AnthropicBedrockAdapter) parseResponse(resp *http.Response) (*Response, error) {
	result, err := a.format.parseResponse(resp)
	if err != nil {
		return nil, err
	}
	result.Provider = "anthropic_bedrock"
	return result, nil
}

// readStream reads Bedrock's AWS event stream format and emits unified StreamEvents.
// Each event stream message contains a base64-encoded Anthropic streaming event.
func (a *AnthropicBedrockAdapter) readStream(ctx context.Context, body io.ReadCloser, ch chan<- StreamEvent) {
	defer close(ch)
	defer body.Close()

	reader := newEventStreamReader(body)
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

		msg, err := reader.Next()
		if err == io.EOF {
			break
		}
		if err != nil {
			ch <- StreamEvent{Type: StreamError, Error: &StreamErrorType{SDKError: SDKError{Message: "event stream read error", Cause: err}}}
			return
		}

		eventType := msg.Headers[":event-type"]

		// Handle error events
		if exceptionType := msg.Headers[":exception-type"]; exceptionType != "" {
			errMsg := fmt.Sprintf("Bedrock stream error: %s", exceptionType)
			if len(msg.Payload) > 0 {
				var errData map[string]interface{}
				if json.Unmarshal(msg.Payload, &errData) == nil {
					if m, ok := errData["message"].(string); ok {
						errMsg = m
					}
				}
			}
			ch <- StreamEvent{Type: StreamError, Error: &StreamErrorType{SDKError: SDKError{Message: errMsg}}}
			return
		}

		if eventType != "chunk" {
			continue
		}

		// Decode the chunk payload: {"bytes": "<base64>"}
		var chunk struct {
			Bytes string `json:"bytes"`
		}
		if err := json.Unmarshal(msg.Payload, &chunk); err != nil {
			continue
		}

		decoded, err := base64.StdEncoding.DecodeString(chunk.Bytes)
		if err != nil {
			continue
		}

		var data map[string]interface{}
		if err := json.Unmarshal(decoded, &data); err != nil {
			continue
		}

		anthropicEventType, _ := data["type"].(string)

		switch anthropicEventType {
		case "message_start":
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
			ch <- StreamEvent{Type: StreamStart}

		case "content_block_start":
			if cb, ok := data["content_block"].(map[string]interface{}); ok {
				blockType, _ := cb["type"].(string)
				currentBlockType = blockType
				if idx, ok := data["index"].(float64); ok {
					currentBlockIndex = int(idx)
				}

				switch blockType {
				case "text":
					ch <- StreamEvent{Type: TextStart, TextID: fmt.Sprintf("text_%d", currentBlockIndex)}
				case "tool_use":
					currentToolCallID, _ = cb["id"].(string)
					currentToolCallName, _ = cb["name"].(string)
					toolCallArgs.Reset()
					ch <- StreamEvent{
						Type: ToolCallStart,
						ToolCall: &ToolCall{
							ID:   currentToolCallID,
							Name: currentToolCallName,
						},
					}
				case "thinking":
					ch <- StreamEvent{Type: ReasoningStart}
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
					ch <- StreamEvent{
						Type:  ToolCallDelta,
						Delta: jsonDelta,
						ToolCall: &ToolCall{
							ID:   currentToolCallID,
							Name: currentToolCallName,
						},
					}
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
				ch <- StreamEvent{Type: TextEnd, TextID: fmt.Sprintf("text_%d", currentBlockIndex)}
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
				ch <- StreamEvent{Type: ReasoningEnd}
			}

		case "message_delta":
			if delta, ok := data["delta"].(map[string]interface{}); ok {
				if stopReason, ok := delta["stop_reason"].(string); ok {
					fr := a.format.mapFinishReason(stopReason)
					if usageData, ok := data["usage"].(map[string]interface{}); ok {
						if v, ok := usageData["output_tokens"].(float64); ok {
							inputUsage.OutputTokens = int(v)
						}
					}
					inputUsage.TotalTokens = inputUsage.InputTokens + inputUsage.OutputTokens

					accResp := accumulator.Response()
					accResp.Provider = "anthropic_bedrock"
					accResp.Usage = inputUsage
					accResp.FinishReason = fr

					ch <- StreamEvent{
						Type:         StreamFinish,
						FinishReason: &fr,
						Usage:        &inputUsage,
						Response:     accResp,
					}
					return
				}
			}

		case "message_stop":
			inputUsage.TotalTokens = inputUsage.InputTokens + inputUsage.OutputTokens

			accResp := accumulator.Response()
			accResp.Provider = "anthropic_bedrock"
			accResp.Usage = inputUsage

			fr := FinishReason{Reason: "stop"}
			ch <- StreamEvent{
				Type:         StreamFinish,
				FinishReason: &fr,
				Usage:        &inputUsage,
				Response:     accResp,
			}
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
