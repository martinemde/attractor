package unifiedllm

import (
	"context"
	"encoding/base64"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func newTestBedrockAdapter(t *testing.T, handler http.HandlerFunc) (*AnthropicBedrockAdapter, *httptest.Server) {
	t.Helper()
	server := httptest.NewServer(handler)
	adapter := &AnthropicBedrockAdapter{
		region: "us-east-1",
		creds: awsCredentials{
			AccessKeyID:    "AKIAIOSFODNN7EXAMPLE",
			SecretAccessKey: "wJalrXUtnFEMI/K7MDENG/bPxRfiCYEXAMPLEKEY",
		},
		baseURL: server.URL,
		http:    newHTTPClient(),
		format:  &AnthropicAdapter{},
	}
	return adapter, server
}

// bedrockEventStreamChunk wraps an Anthropic event in a Bedrock event stream frame.
func bedrockEventStreamChunk(anthropicEvent map[string]interface{}) []byte {
	eventJSON, _ := json.Marshal(anthropicEvent)
	encoded := base64.StdEncoding.EncodeToString(eventJSON)
	payload, _ := json.Marshal(map[string]string{"bytes": encoded})

	headers := map[string]string{
		":event-type":  "chunk",
		":content-type": "application/json",
	}
	return buildEventStreamMessage(headers, payload)
}

func TestAnthropicBedrockAdapterName(t *testing.T) {
	adapter := &AnthropicBedrockAdapter{}
	assert.Equal(t, "anthropic_bedrock", adapter.Name())
}

func TestAnthropicBedrockAdapterComplete(t *testing.T) {
	handler := func(w http.ResponseWriter, r *http.Request) {
		assert.Equal(t, "POST", r.Method)
		assert.Contains(t, r.URL.Path, "/model/")
		assert.Contains(t, r.URL.Path, "/invoke")
		assert.Equal(t, "application/json", r.Header.Get("content-type"))

		// Verify SigV4 headers are present
		assert.NotEmpty(t, r.Header.Get("Authorization"))
		assert.True(t, strings.HasPrefix(r.Header.Get("Authorization"), "AWS4-HMAC-SHA256"))
		assert.NotEmpty(t, r.Header.Get("x-amz-date"))
		assert.NotEmpty(t, r.Header.Get("x-amz-content-sha256"))

		// Verify request body
		body, _ := io.ReadAll(r.Body)
		var reqBody map[string]interface{}
		require.NoError(t, json.Unmarshal(body, &reqBody))

		// Bedrock-specific: anthropic_version in body, no model field
		assert.Equal(t, "bedrock-2023-05-31", reqBody["anthropic_version"])
		assert.Nil(t, reqBody["model"])

		// System extracted
		system, ok := reqBody["system"].([]interface{})
		require.True(t, ok)
		require.Len(t, system, 1)

		// max_tokens defaulted
		assert.Equal(t, float64(4096), reqBody["max_tokens"])

		w.WriteHeader(http.StatusOK)
		json.NewEncoder(w).Encode(map[string]interface{}{
			"id":    "msg_bedrock_123",
			"model": "anthropic.claude-opus-4-6-v1:0",
			"content": []interface{}{
				map[string]interface{}{
					"type": "text",
					"text": "Hello from Bedrock!",
				},
			},
			"stop_reason": "end_turn",
			"usage": map[string]interface{}{
				"input_tokens":  float64(15),
				"output_tokens": float64(8),
			},
		})
	}

	adapter, server := newTestBedrockAdapter(t, handler)
	defer server.Close()

	resp, err := adapter.Complete(context.Background(), Request{
		Model: "anthropic.claude-opus-4-6-v1:0",
		Messages: []Message{
			SystemMessage("You are helpful."),
			UserMessage("Hi"),
		},
	})

	require.NoError(t, err)
	assert.Equal(t, "msg_bedrock_123", resp.ID)
	assert.Equal(t, "anthropic_bedrock", resp.Provider)
	assert.Equal(t, "Hello from Bedrock!", resp.Text())
	assert.Equal(t, "stop", resp.FinishReason.Reason)
	assert.Equal(t, 15, resp.Usage.InputTokens)
	assert.Equal(t, 8, resp.Usage.OutputTokens)
}

func TestAnthropicBedrockAdapterModelInURLPath(t *testing.T) {
	var capturedPath string
	handler := func(w http.ResponseWriter, r *http.Request) {
		capturedPath = r.URL.Path
		w.WriteHeader(http.StatusOK)
		json.NewEncoder(w).Encode(map[string]interface{}{
			"id": "msg_path", "model": "anthropic.claude-opus-4-6-v1:0",
			"content":     []interface{}{map[string]interface{}{"type": "text", "text": "ok"}},
			"stop_reason": "end_turn",
			"usage":       map[string]interface{}{"input_tokens": float64(1), "output_tokens": float64(1)},
		})
	}

	adapter, server := newTestBedrockAdapter(t, handler)
	defer server.Close()

	_, err := adapter.Complete(context.Background(), Request{
		Model:    "anthropic.claude-opus-4-6-v1:0",
		Messages: []Message{UserMessage("test")},
	})
	require.NoError(t, err)
	assert.Equal(t, "/model/anthropic.claude-opus-4-6-v1:0/invoke", capturedPath)
}

func TestAnthropicBedrockAdapterCrossRegionModelID(t *testing.T) {
	var capturedPath string
	handler := func(w http.ResponseWriter, r *http.Request) {
		capturedPath = r.URL.Path
		w.WriteHeader(http.StatusOK)
		json.NewEncoder(w).Encode(map[string]interface{}{
			"id": "msg_xr", "model": "us.anthropic.claude-opus-4-6-v1:0",
			"content":     []interface{}{map[string]interface{}{"type": "text", "text": "ok"}},
			"stop_reason": "end_turn",
			"usage":       map[string]interface{}{"input_tokens": float64(1), "output_tokens": float64(1)},
		})
	}

	adapter, server := newTestBedrockAdapter(t, handler)
	defer server.Close()

	_, err := adapter.Complete(context.Background(), Request{
		Model:    "us.anthropic.claude-opus-4-6-v1:0",
		Messages: []Message{UserMessage("test")},
	})
	require.NoError(t, err)
	assert.Equal(t, "/model/us.anthropic.claude-opus-4-6-v1:0/invoke", capturedPath)
}

func TestAnthropicBedrockAdapterSessionToken(t *testing.T) {
	var capturedHeaders http.Header
	handler := func(w http.ResponseWriter, r *http.Request) {
		capturedHeaders = r.Header
		w.WriteHeader(http.StatusOK)
		json.NewEncoder(w).Encode(map[string]interface{}{
			"id": "msg_st", "model": "anthropic.claude-opus-4-6-v1:0",
			"content":     []interface{}{map[string]interface{}{"type": "text", "text": "ok"}},
			"stop_reason": "end_turn",
			"usage":       map[string]interface{}{"input_tokens": float64(1), "output_tokens": float64(1)},
		})
	}

	server := httptest.NewServer(http.HandlerFunc(handler))
	defer server.Close()
	adapter := &AnthropicBedrockAdapter{
		region: "us-east-1",
		creds: awsCredentials{
			AccessKeyID:    "AKIAIOSFODNN7EXAMPLE",
			SecretAccessKey: "wJalrXUtnFEMI/K7MDENG/bPxRfiCYEXAMPLEKEY",
			SessionToken:   "FwoGZXIvYXdzEBYaDHqa",
		},
		baseURL: server.URL,
		http:    newHTTPClient(),
		format:  &AnthropicAdapter{},
	}

	_, err := adapter.Complete(context.Background(), Request{
		Model:    "anthropic.claude-opus-4-6-v1:0",
		Messages: []Message{UserMessage("test")},
	})
	require.NoError(t, err)
	assert.NotEmpty(t, capturedHeaders.Get("x-amz-security-token"))
}

func TestAnthropicBedrockAdapterToolCalls(t *testing.T) {
	var capturedBody map[string]interface{}
	handler := func(w http.ResponseWriter, r *http.Request) {
		body, _ := io.ReadAll(r.Body)
		json.Unmarshal(body, &capturedBody)

		w.WriteHeader(http.StatusOK)
		json.NewEncoder(w).Encode(map[string]interface{}{
			"id":    "msg_tc",
			"model": "anthropic.claude-opus-4-6-v1:0",
			"content": []interface{}{
				map[string]interface{}{
					"type":  "tool_use",
					"id":    "toolu_abc",
					"name":  "get_weather",
					"input": map[string]interface{}{"location": "NYC"},
				},
			},
			"stop_reason": "tool_use",
			"usage":       map[string]interface{}{"input_tokens": float64(20), "output_tokens": float64(15)},
		})
	}

	adapter, server := newTestBedrockAdapter(t, handler)
	defer server.Close()

	resp, err := adapter.Complete(context.Background(), Request{
		Model:    "anthropic.claude-opus-4-6-v1:0",
		Messages: []Message{UserMessage("Weather?")},
		ToolDefs: []ToolDefinition{
			{Name: "get_weather", Description: "Get weather", Parameters: map[string]interface{}{"type": "object"}},
		},
		ToolChoice: &ToolChoice{Mode: "auto"},
	})

	require.NoError(t, err)
	assert.Equal(t, "tool_calls", resp.FinishReason.Reason)
	calls := resp.ToolCallsFromResponse()
	require.Len(t, calls, 1)
	assert.Equal(t, "toolu_abc", calls[0].ID)
	assert.Equal(t, "get_weather", calls[0].Name)

	// Verify tool definition format in body
	tools, ok := capturedBody["tools"].([]interface{})
	require.True(t, ok)
	require.Len(t, tools, 1)
	tool := tools[0].(map[string]interface{})
	assert.Equal(t, "get_weather", tool["name"])
}

func TestAnthropicBedrockAdapterThinking(t *testing.T) {
	handler := func(w http.ResponseWriter, r *http.Request) {
		body, _ := io.ReadAll(r.Body)
		var reqBody map[string]interface{}
		json.Unmarshal(body, &reqBody)

		// Verify thinking passed through
		assert.NotNil(t, reqBody["thinking"])

		w.WriteHeader(http.StatusOK)
		json.NewEncoder(w).Encode(map[string]interface{}{
			"id":    "msg_th",
			"model": "anthropic.claude-opus-4-6-v1:0",
			"content": []interface{}{
				map[string]interface{}{
					"type":      "thinking",
					"thinking":  "Let me think about this...",
					"signature": "sig_abc",
				},
				map[string]interface{}{
					"type": "text",
					"text": "The answer is 42.",
				},
			},
			"stop_reason": "end_turn",
			"usage":       map[string]interface{}{"input_tokens": float64(10), "output_tokens": float64(20)},
		})
	}

	adapter, server := newTestBedrockAdapter(t, handler)
	defer server.Close()

	resp, err := adapter.Complete(context.Background(), Request{
		Model:    "anthropic.claude-opus-4-6-v1:0",
		Messages: []Message{UserMessage("What is the meaning of life?")},
		ProviderOptions: map[string]interface{}{
			"anthropic": map[string]interface{}{
				"thinking": map[string]interface{}{"type": "enabled", "budget_tokens": 10000},
			},
		},
	})

	require.NoError(t, err)
	assert.Equal(t, "The answer is 42.", resp.Text())
	assert.Equal(t, "Let me think about this...", resp.Reasoning())
}

func TestAnthropicBedrockAdapterAnthropicVersionOverride(t *testing.T) {
	var capturedBody map[string]interface{}
	handler := func(w http.ResponseWriter, r *http.Request) {
		body, _ := io.ReadAll(r.Body)
		json.Unmarshal(body, &capturedBody)
		w.WriteHeader(http.StatusOK)
		json.NewEncoder(w).Encode(map[string]interface{}{
			"id": "msg_av", "model": "anthropic.claude-opus-4-6-v1:0",
			"content":     []interface{}{map[string]interface{}{"type": "text", "text": "ok"}},
			"stop_reason": "end_turn",
			"usage":       map[string]interface{}{"input_tokens": float64(1), "output_tokens": float64(1)},
		})
	}

	adapter, server := newTestBedrockAdapter(t, handler)
	defer server.Close()

	_, err := adapter.Complete(context.Background(), Request{
		Model:    "anthropic.claude-opus-4-6-v1:0",
		Messages: []Message{UserMessage("test")},
		ProviderOptions: map[string]interface{}{
			"anthropic_bedrock": map[string]interface{}{
				"anthropic_version": "bedrock-2024-04-01",
			},
		},
	})
	require.NoError(t, err)
	assert.Equal(t, "bedrock-2024-04-01", capturedBody["anthropic_version"])
}

func TestAnthropicBedrockAdapterNoStreamFieldInBody(t *testing.T) {
	var capturedBody map[string]interface{}
	handler := func(w http.ResponseWriter, r *http.Request) {
		body, _ := io.ReadAll(r.Body)
		json.Unmarshal(body, &capturedBody)
		w.WriteHeader(http.StatusOK)
		json.NewEncoder(w).Encode(map[string]interface{}{
			"id": "msg_ns", "model": "anthropic.claude-opus-4-6-v1:0",
			"content":     []interface{}{map[string]interface{}{"type": "text", "text": "ok"}},
			"stop_reason": "end_turn",
			"usage":       map[string]interface{}{"input_tokens": float64(1), "output_tokens": float64(1)},
		})
	}

	adapter, server := newTestBedrockAdapter(t, handler)
	defer server.Close()

	_, err := adapter.Complete(context.Background(), Request{
		Model:    "anthropic.claude-opus-4-6-v1:0",
		Messages: []Message{UserMessage("test")},
	})
	require.NoError(t, err)

	// Bedrock should not have "stream" in the body
	_, hasStream := capturedBody["stream"]
	assert.False(t, hasStream)
}

func TestAnthropicBedrockAdapterPromptCaching(t *testing.T) {
	var capturedBody map[string]interface{}
	handler := func(w http.ResponseWriter, r *http.Request) {
		body, _ := io.ReadAll(r.Body)
		json.Unmarshal(body, &capturedBody)

		// Bedrock should NOT have anthropic-beta headers (no such header mechanism)
		assert.Empty(t, r.Header.Get("anthropic-beta"))

		w.WriteHeader(http.StatusOK)
		json.NewEncoder(w).Encode(map[string]interface{}{
			"id": "msg_pc", "model": "anthropic.claude-opus-4-6-v1:0",
			"content":     []interface{}{map[string]interface{}{"type": "text", "text": "ok"}},
			"stop_reason": "end_turn",
			"usage": map[string]interface{}{
				"input_tokens":               float64(10),
				"output_tokens":              float64(5),
				"cache_read_input_tokens":    float64(100),
				"cache_creation_input_tokens": float64(50),
			},
		})
	}

	adapter, server := newTestBedrockAdapter(t, handler)
	defer server.Close()

	resp, err := adapter.Complete(context.Background(), Request{
		Model: "anthropic.claude-opus-4-6-v1:0",
		Messages: []Message{
			SystemMessage("System prompt"),
			UserMessage("First"),
			AssistantMessage("Response"),
			UserMessage("Second"),
		},
		ToolDefs: []ToolDefinition{
			{Name: "my_tool", Description: "A tool", Parameters: map[string]interface{}{"type": "object"}},
		},
	})
	require.NoError(t, err)

	// cache_control should still be in the body for Bedrock
	system := capturedBody["system"].([]interface{})
	lastSystem := system[len(system)-1].(map[string]interface{})
	assert.NotNil(t, lastSystem["cache_control"])

	tools := capturedBody["tools"].([]interface{})
	lastTool := tools[len(tools)-1].(map[string]interface{})
	assert.NotNil(t, lastTool["cache_control"])

	// Verify cache usage parsed
	require.NotNil(t, resp.Usage.CacheReadTokens)
	assert.Equal(t, 100, *resp.Usage.CacheReadTokens)
	require.NotNil(t, resp.Usage.CacheWriteTokens)
	assert.Equal(t, 50, *resp.Usage.CacheWriteTokens)
}

func TestAnthropicBedrockAdapterStreaming(t *testing.T) {
	handler := func(w http.ResponseWriter, r *http.Request) {
		assert.Contains(t, r.URL.Path, "/invoke-with-response-stream")

		w.Header().Set("Content-Type", "application/vnd.amazon.eventstream")
		w.WriteHeader(http.StatusOK)

		events := []map[string]interface{}{
			{"type": "message_start", "message": map[string]interface{}{
				"id": "msg_s", "model": "anthropic.claude-opus-4-6-v1:0",
				"usage": map[string]interface{}{"input_tokens": 10},
			}},
			{"type": "content_block_start", "index": 0, "content_block": map[string]interface{}{
				"type": "text", "text": "",
			}},
			{"type": "content_block_delta", "index": 0, "delta": map[string]interface{}{
				"type": "text_delta", "text": "Hello",
			}},
			{"type": "content_block_delta", "index": 0, "delta": map[string]interface{}{
				"type": "text_delta", "text": " world",
			}},
			{"type": "content_block_stop", "index": 0},
			{"type": "message_delta", "delta": map[string]interface{}{
				"stop_reason": "end_turn",
			}, "usage": map[string]interface{}{"output_tokens": 5}},
		}

		for _, event := range events {
			frame := bedrockEventStreamChunk(event)
			w.Write(frame)
			w.(http.Flusher).Flush()
		}
	}

	adapter, server := newTestBedrockAdapter(t, handler)
	defer server.Close()

	ch, err := adapter.Stream(context.Background(), Request{
		Model:    "anthropic.claude-opus-4-6-v1:0",
		Messages: []Message{UserMessage("Hi")},
	})
	require.NoError(t, err)

	var events []StreamEvent
	for event := range ch {
		events = append(events, event)
	}

	// Verify event sequence
	require.GreaterOrEqual(t, len(events), 5)
	assert.Equal(t, StreamStart, events[0].Type)
	assert.Equal(t, TextStart, events[1].Type)

	var fullText strings.Builder
	for _, evt := range events {
		if evt.Type == TextDelta {
			fullText.WriteString(evt.Delta)
		}
	}
	assert.Equal(t, "Hello world", fullText.String())

	last := events[len(events)-1]
	assert.Equal(t, StreamFinish, last.Type)
	require.NotNil(t, last.FinishReason)
	assert.Equal(t, "stop", last.FinishReason.Reason)
	require.NotNil(t, last.Usage)
	assert.Equal(t, 10, last.Usage.InputTokens)
	assert.Equal(t, 5, last.Usage.OutputTokens)
}

func TestAnthropicBedrockAdapterStreamingWithThinking(t *testing.T) {
	handler := func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/vnd.amazon.eventstream")
		w.WriteHeader(http.StatusOK)

		events := []map[string]interface{}{
			{"type": "message_start", "message": map[string]interface{}{
				"id": "msg_st", "model": "anthropic.claude-opus-4-6-v1:0",
				"usage": map[string]interface{}{"input_tokens": 10},
			}},
			{"type": "content_block_start", "index": 0, "content_block": map[string]interface{}{
				"type": "thinking", "thinking": "",
			}},
			{"type": "content_block_delta", "index": 0, "delta": map[string]interface{}{
				"type": "thinking_delta", "thinking": "Let me think...",
			}},
			{"type": "content_block_stop", "index": 0},
			{"type": "content_block_start", "index": 1, "content_block": map[string]interface{}{
				"type": "text", "text": "",
			}},
			{"type": "content_block_delta", "index": 1, "delta": map[string]interface{}{
				"type": "text_delta", "text": "Answer",
			}},
			{"type": "content_block_stop", "index": 1},
			{"type": "message_delta", "delta": map[string]interface{}{
				"stop_reason": "end_turn",
			}, "usage": map[string]interface{}{"output_tokens": 15}},
		}

		for _, event := range events {
			frame := bedrockEventStreamChunk(event)
			w.Write(frame)
			w.(http.Flusher).Flush()
		}
	}

	adapter, server := newTestBedrockAdapter(t, handler)
	defer server.Close()

	ch, err := adapter.Stream(context.Background(), Request{
		Model:    "anthropic.claude-opus-4-6-v1:0",
		Messages: []Message{UserMessage("Think about this")},
	})
	require.NoError(t, err)

	var events []StreamEvent
	for event := range ch {
		events = append(events, event)
	}

	hasReasoningStart := false
	hasReasoningDelta := false
	hasReasoningEnd := false
	for _, evt := range events {
		switch evt.Type {
		case ReasoningStart:
			hasReasoningStart = true
		case ReasoningDelta:
			hasReasoningDelta = true
			assert.Equal(t, "Let me think...", evt.ReasoningDelta)
		case ReasoningEnd:
			hasReasoningEnd = true
		}
	}
	assert.True(t, hasReasoningStart)
	assert.True(t, hasReasoningDelta)
	assert.True(t, hasReasoningEnd)
}

func TestAnthropicBedrockAdapterStreamingToolCalls(t *testing.T) {
	handler := func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/vnd.amazon.eventstream")
		w.WriteHeader(http.StatusOK)

		events := []map[string]interface{}{
			{"type": "message_start", "message": map[string]interface{}{
				"id": "msg_stc", "model": "anthropic.claude-opus-4-6-v1:0",
				"usage": map[string]interface{}{"input_tokens": 10},
			}},
			{"type": "content_block_start", "index": 0, "content_block": map[string]interface{}{
				"type": "tool_use", "id": "toolu_123", "name": "get_weather",
			}},
			{"type": "content_block_delta", "index": 0, "delta": map[string]interface{}{
				"type": "input_json_delta", "partial_json": `{"location":`,
			}},
			{"type": "content_block_delta", "index": 0, "delta": map[string]interface{}{
				"type": "input_json_delta", "partial_json": `"NYC"}`,
			}},
			{"type": "content_block_stop", "index": 0},
			{"type": "message_delta", "delta": map[string]interface{}{
				"stop_reason": "tool_use",
			}, "usage": map[string]interface{}{"output_tokens": 12}},
		}

		for _, event := range events {
			frame := bedrockEventStreamChunk(event)
			w.Write(frame)
			w.(http.Flusher).Flush()
		}
	}

	adapter, server := newTestBedrockAdapter(t, handler)
	defer server.Close()

	ch, err := adapter.Stream(context.Background(), Request{
		Model:    "anthropic.claude-opus-4-6-v1:0",
		Messages: []Message{UserMessage("Weather?")},
	})
	require.NoError(t, err)

	var events []StreamEvent
	for event := range ch {
		events = append(events, event)
	}

	hasToolCallEnd := false
	for _, evt := range events {
		if evt.Type == ToolCallEnd {
			hasToolCallEnd = true
			require.NotNil(t, evt.ToolCall)
			assert.Equal(t, "toolu_123", evt.ToolCall.ID)
			assert.Equal(t, "get_weather", evt.ToolCall.Name)
			assert.Equal(t, `{"location":"NYC"}`, string(evt.ToolCall.Arguments))
		}
	}
	assert.True(t, hasToolCallEnd)

	last := events[len(events)-1]
	assert.Equal(t, StreamFinish, last.Type)
	assert.Equal(t, "tool_calls", last.FinishReason.Reason)
}

func TestAnthropicBedrockAdapterErrorHandling(t *testing.T) {
	tests := []struct {
		name       string
		statusCode int
		body       string
		errType    string
	}{
		{
			name:       "403 Access Denied",
			statusCode: 403,
			body:       `{"message":"Access denied to model"}`,
			errType:    "AccessDeniedError",
		},
		{
			name:       "429 Throttled",
			statusCode: 429,
			body:       `{"message":"Too many requests"}`,
			errType:    "RateLimitError",
		},
		{
			name:       "500 Internal Error",
			statusCode: 500,
			body:       `{"message":"Internal server error"}`,
			errType:    "ServerError",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			handler := func(w http.ResponseWriter, r *http.Request) {
				w.WriteHeader(tt.statusCode)
				w.Write([]byte(tt.body))
			}
			adapter, server := newTestBedrockAdapter(t, handler)
			defer server.Close()

			_, err := adapter.Complete(context.Background(), Request{
				Model:    "anthropic.claude-opus-4-6-v1:0",
				Messages: []Message{UserMessage("test")},
			})

			require.Error(t, err)
			switch tt.errType {
			case "AccessDeniedError":
				assert.IsType(t, &AccessDeniedError{}, err)
			case "RateLimitError":
				assert.IsType(t, &RateLimitError{}, err)
			case "ServerError":
				assert.IsType(t, &ServerError{}, err)
			}
		})
	}
}

func TestAnthropicBedrockAdapterConstructorRequiresRegion(t *testing.T) {
	t.Setenv("AWS_REGION", "")
	t.Setenv("AWS_DEFAULT_REGION", "")
	t.Setenv("AWS_ACCESS_KEY_ID", "AKID")
	t.Setenv("AWS_SECRET_ACCESS_KEY", "SECRET")

	_, err := NewAnthropicBedrockAdapter()
	assert.Error(t, err)
	assert.IsType(t, &ConfigurationError{}, err)
	assert.Contains(t, err.Error(), "region")
}

func TestAnthropicBedrockAdapterConstructorRequiresCredentials(t *testing.T) {
	t.Setenv("AWS_REGION", "us-east-1")
	t.Setenv("AWS_ACCESS_KEY_ID", "")
	t.Setenv("AWS_SECRET_ACCESS_KEY", "")

	_, err := NewAnthropicBedrockAdapter()
	assert.Error(t, err)
	assert.IsType(t, &ConfigurationError{}, err)
	assert.Contains(t, err.Error(), "credentials")
}

func TestAnthropicBedrockAdapterConstructorFromEnv(t *testing.T) {
	t.Setenv("AWS_REGION", "eu-west-1")
	t.Setenv("AWS_ACCESS_KEY_ID", "AKID_TEST")
	t.Setenv("AWS_SECRET_ACCESS_KEY", "SECRET_TEST")
	t.Setenv("AWS_SESSION_TOKEN", "TOKEN_TEST")

	adapter, err := NewAnthropicBedrockAdapter()
	require.NoError(t, err)
	assert.Equal(t, "eu-west-1", adapter.region)
	assert.Equal(t, "AKID_TEST", adapter.creds.AccessKeyID)
	assert.Equal(t, "SECRET_TEST", adapter.creds.SecretAccessKey)
	assert.Equal(t, "TOKEN_TEST", adapter.creds.SessionToken)
	assert.Equal(t, "https://bedrock-runtime.eu-west-1.amazonaws.com", adapter.baseURL)
}

func TestAnthropicBedrockAdapterConstructorWithOptions(t *testing.T) {
	t.Setenv("AWS_REGION", "")
	t.Setenv("AWS_DEFAULT_REGION", "")
	t.Setenv("AWS_ACCESS_KEY_ID", "")
	t.Setenv("AWS_SECRET_ACCESS_KEY", "")

	adapter, err := NewAnthropicBedrockAdapter(
		WithBedrockRegion("ap-southeast-1"),
		WithBedrockCredentials("AKID", "SECRET", "TOKEN"),
		WithBedrockBaseURL("https://custom-endpoint.example.com"),
	)
	require.NoError(t, err)
	assert.Equal(t, "ap-southeast-1", adapter.region)
	assert.Equal(t, "AKID", adapter.creds.AccessKeyID)
	assert.Equal(t, "SECRET", adapter.creds.SecretAccessKey)
	assert.Equal(t, "TOKEN", adapter.creds.SessionToken)
	assert.Equal(t, "https://custom-endpoint.example.com", adapter.baseURL)
}

func TestAnthropicBedrockAdapterSupportsToolChoice(t *testing.T) {
	adapter := &AnthropicBedrockAdapter{}
	assert.True(t, adapter.SupportsToolChoice("auto"))
	assert.True(t, adapter.SupportsToolChoice("none"))
	assert.True(t, adapter.SupportsToolChoice("required"))
	assert.True(t, adapter.SupportsToolChoice("named"))
	assert.False(t, adapter.SupportsToolChoice("unknown"))
}
