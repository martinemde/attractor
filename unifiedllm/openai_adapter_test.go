package unifiedllm

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func newTestOpenAIAdapter(t *testing.T, handler http.HandlerFunc) (*OpenAIAdapter, *httptest.Server) {
	t.Helper()
	server := httptest.NewServer(handler)
	adapter := &OpenAIAdapter{
		apiKey:  "test-key",
		baseURL: server.URL,
		http:    newHTTPClient(),
	}
	return adapter, server
}

func TestOpenAIAdapterName(t *testing.T) {
	adapter := &OpenAIAdapter{}
	assert.Equal(t, "openai", adapter.Name())
}

func TestOpenAIAdapterComplete(t *testing.T) {
	handler := func(w http.ResponseWriter, r *http.Request) {
		// Verify request
		assert.Equal(t, "POST", r.Method)
		assert.Equal(t, "/v1/responses", r.URL.Path)
		assert.Equal(t, "Bearer test-key", r.Header.Get("Authorization"))
		assert.Equal(t, "application/json", r.Header.Get("Content-Type"))

		body, _ := io.ReadAll(r.Body)
		var reqBody map[string]interface{}
		require.NoError(t, json.Unmarshal(body, &reqBody))
		assert.Equal(t, "gpt-5.2", reqBody["model"])

		// Verify instructions extracted from system message
		assert.Equal(t, "You are helpful.", reqBody["instructions"])

		w.Header().Set("x-ratelimit-remaining-requests", "99")
		w.Header().Set("x-ratelimit-limit-requests", "100")
		w.WriteHeader(http.StatusOK)
		json.NewEncoder(w).Encode(map[string]interface{}{
			"id":     "resp_123",
			"model":  "gpt-5.2",
			"status": "completed",
			"output": []interface{}{
				map[string]interface{}{
					"type": "message",
					"role": "assistant",
					"content": []interface{}{
						map[string]interface{}{
							"type": "output_text",
							"text": "Hello, world!",
						},
					},
				},
			},
			"usage": map[string]interface{}{
				"input_tokens":  float64(10),
				"output_tokens": float64(5),
				"output_tokens_details": map[string]interface{}{
					"reasoning_tokens": float64(2),
				},
				"input_tokens_details": map[string]interface{}{
					"cached_tokens": float64(3),
				},
			},
		})
	}

	adapter, server := newTestOpenAIAdapter(t, handler)
	defer server.Close()

	resp, err := adapter.Complete(context.Background(), Request{
		Model: "gpt-5.2",
		Messages: []Message{
			SystemMessage("You are helpful."),
			UserMessage("Hi"),
		},
	})

	require.NoError(t, err)
	assert.Equal(t, "resp_123", resp.ID)
	assert.Equal(t, "gpt-5.2", resp.Model)
	assert.Equal(t, "openai", resp.Provider)
	assert.Equal(t, "Hello, world!", resp.Text())
	assert.Equal(t, "stop", resp.FinishReason.Reason)
	assert.Equal(t, 10, resp.Usage.InputTokens)
	assert.Equal(t, 5, resp.Usage.OutputTokens)
	assert.Equal(t, 15, resp.Usage.TotalTokens)
	require.NotNil(t, resp.Usage.ReasoningTokens)
	assert.Equal(t, 2, *resp.Usage.ReasoningTokens)
	require.NotNil(t, resp.Usage.CacheReadTokens)
	assert.Equal(t, 3, *resp.Usage.CacheReadTokens)
	require.NotNil(t, resp.RateLimit)
	assert.Equal(t, 99, *resp.RateLimit.RequestsRemaining)
}

func TestOpenAIAdapterToolCalls(t *testing.T) {
	handler := func(w http.ResponseWriter, r *http.Request) {
		body, _ := io.ReadAll(r.Body)
		var reqBody map[string]interface{}
		json.Unmarshal(body, &reqBody)

		// Verify tool definitions
		tools, ok := reqBody["tools"].([]interface{})
		require.True(t, ok)
		assert.Len(t, tools, 1)
		tool := tools[0].(map[string]interface{})
		assert.Equal(t, "function", tool["type"])
		fn := tool["function"].(map[string]interface{})
		assert.Equal(t, "get_weather", fn["name"])

		// Verify tool choice
		assert.Equal(t, "auto", reqBody["tool_choice"])

		w.WriteHeader(http.StatusOK)
		json.NewEncoder(w).Encode(map[string]interface{}{
			"id":     "resp_456",
			"model":  "gpt-5.2",
			"status": "completed",
			"output": []interface{}{
				map[string]interface{}{
					"type":      "function_call",
					"id":        "call_abc",
					"name":      "get_weather",
					"arguments": `{"location":"NYC"}`,
				},
			},
			"usage": map[string]interface{}{
				"input_tokens":  float64(20),
				"output_tokens": float64(10),
			},
		})
	}

	adapter, server := newTestOpenAIAdapter(t, handler)
	defer server.Close()

	resp, err := adapter.Complete(context.Background(), Request{
		Model:    "gpt-5.2",
		Messages: []Message{UserMessage("What's the weather?")},
		ToolDefs: []ToolDefinition{
			{
				Name:        "get_weather",
				Description: "Get weather",
				Parameters:  map[string]interface{}{"type": "object"},
			},
		},
		ToolChoice: &ToolChoice{Mode: "auto"},
	})

	require.NoError(t, err)
	assert.Equal(t, "tool_calls", resp.FinishReason.Reason)
	calls := resp.ToolCallsFromResponse()
	require.Len(t, calls, 1)
	assert.Equal(t, "call_abc", calls[0].ID)
	assert.Equal(t, "get_weather", calls[0].Name)
}

func TestOpenAIAdapterToolResultRoundTrip(t *testing.T) {
	var capturedBody map[string]interface{}
	handler := func(w http.ResponseWriter, r *http.Request) {
		body, _ := io.ReadAll(r.Body)
		json.Unmarshal(body, &capturedBody)
		w.WriteHeader(http.StatusOK)
		json.NewEncoder(w).Encode(map[string]interface{}{
			"id": "resp_789", "model": "gpt-5.2", "status": "completed",
			"output": []interface{}{
				map[string]interface{}{
					"type": "message", "role": "assistant",
					"content": []interface{}{
						map[string]interface{}{"type": "output_text", "text": "It's sunny."},
					},
				},
			},
			"usage": map[string]interface{}{"input_tokens": float64(30), "output_tokens": float64(5)},
		})
	}

	adapter, server := newTestOpenAIAdapter(t, handler)
	defer server.Close()

	_, err := adapter.Complete(context.Background(), Request{
		Model: "gpt-5.2",
		Messages: []Message{
			UserMessage("What's the weather?"),
			{Role: RoleAssistant, Content: []ContentPart{
				ToolCallPart("call_abc", "get_weather", json.RawMessage(`{"location":"NYC"}`)),
			}},
			ToolResultMessage("call_abc", "72F and sunny", false),
		},
	})
	require.NoError(t, err)

	// Verify input includes function_call and function_call_output
	input, ok := capturedBody["input"].([]interface{})
	require.True(t, ok)

	// Should have: user message, function_call, function_call_output
	require.Len(t, input, 3)
	fcItem := input[1].(map[string]interface{})
	assert.Equal(t, "function_call", fcItem["type"])

	fcoItem := input[2].(map[string]interface{})
	assert.Equal(t, "function_call_output", fcoItem["type"])
	assert.Equal(t, "call_abc", fcoItem["call_id"])
}

func TestOpenAIAdapterReasoningEffort(t *testing.T) {
	var capturedBody map[string]interface{}
	handler := func(w http.ResponseWriter, r *http.Request) {
		body, _ := io.ReadAll(r.Body)
		json.Unmarshal(body, &capturedBody)
		w.WriteHeader(http.StatusOK)
		json.NewEncoder(w).Encode(map[string]interface{}{
			"id": "resp_r", "model": "gpt-5.2", "status": "completed",
			"output": []interface{}{},
			"usage":  map[string]interface{}{"input_tokens": float64(1), "output_tokens": float64(1)},
		})
	}

	adapter, server := newTestOpenAIAdapter(t, handler)
	defer server.Close()

	_, err := adapter.Complete(context.Background(), Request{
		Model:           "gpt-5.2",
		Messages:        []Message{UserMessage("test")},
		ReasoningEffort: "high",
	})
	require.NoError(t, err)

	reasoning, ok := capturedBody["reasoning"].(map[string]interface{})
	require.True(t, ok)
	assert.Equal(t, "high", reasoning["effort"])
}

func TestOpenAIAdapterResponseFormat(t *testing.T) {
	var capturedBody map[string]interface{}
	handler := func(w http.ResponseWriter, r *http.Request) {
		body, _ := io.ReadAll(r.Body)
		json.Unmarshal(body, &capturedBody)
		w.WriteHeader(http.StatusOK)
		json.NewEncoder(w).Encode(map[string]interface{}{
			"id": "resp_f", "model": "gpt-5.2", "status": "completed",
			"output": []interface{}{},
			"usage":  map[string]interface{}{"input_tokens": float64(1), "output_tokens": float64(1)},
		})
	}

	adapter, server := newTestOpenAIAdapter(t, handler)
	defer server.Close()

	schema := map[string]interface{}{"type": "object", "properties": map[string]interface{}{"name": map[string]interface{}{"type": "string"}}}
	_, err := adapter.Complete(context.Background(), Request{
		Model:          "gpt-5.2",
		Messages:       []Message{UserMessage("test")},
		ResponseFormat: &ResponseFormat{Type: "json_schema", JSONSchema: schema, Strict: true},
	})
	require.NoError(t, err)

	textObj, ok := capturedBody["text"].(map[string]interface{})
	require.True(t, ok)
	format, ok := textObj["format"].(map[string]interface{})
	require.True(t, ok)
	assert.Equal(t, "json_schema", format["type"])
}

func TestOpenAIAdapterProviderOptions(t *testing.T) {
	var capturedBody map[string]interface{}
	handler := func(w http.ResponseWriter, r *http.Request) {
		body, _ := io.ReadAll(r.Body)
		json.Unmarshal(body, &capturedBody)
		w.WriteHeader(http.StatusOK)
		json.NewEncoder(w).Encode(map[string]interface{}{
			"id": "resp_p", "model": "gpt-5.2", "status": "completed",
			"output": []interface{}{},
			"usage":  map[string]interface{}{"input_tokens": float64(1), "output_tokens": float64(1)},
		})
	}

	adapter, server := newTestOpenAIAdapter(t, handler)
	defer server.Close()

	_, err := adapter.Complete(context.Background(), Request{
		Model:    "gpt-5.2",
		Messages: []Message{UserMessage("test")},
		ProviderOptions: map[string]interface{}{
			"openai": map[string]interface{}{
				"custom_field": "custom_value",
			},
		},
	})
	require.NoError(t, err)
	assert.Equal(t, "custom_value", capturedBody["custom_field"])
}

func TestOpenAIAdapterErrorHandling(t *testing.T) {
	tests := []struct {
		name       string
		statusCode int
		body       string
		errType    string
	}{
		{
			name:       "401 Unauthorized",
			statusCode: 401,
			body:       `{"error":{"message":"Invalid API key","code":"invalid_api_key"}}`,
			errType:    "AuthenticationError",
		},
		{
			name:       "429 Rate Limited",
			statusCode: 429,
			body:       `{"error":{"message":"Rate limit exceeded"}}`,
			errType:    "RateLimitError",
		},
		{
			name:       "500 Server Error",
			statusCode: 500,
			body:       `{"error":{"message":"Internal server error"}}`,
			errType:    "ServerError",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			handler := func(w http.ResponseWriter, r *http.Request) {
				w.WriteHeader(tt.statusCode)
				w.Write([]byte(tt.body))
			}
			adapter, server := newTestOpenAIAdapter(t, handler)
			defer server.Close()

			_, err := adapter.Complete(context.Background(), Request{
				Model:    "gpt-5.2",
				Messages: []Message{UserMessage("test")},
			})

			require.Error(t, err)
			switch tt.errType {
			case "AuthenticationError":
				assert.IsType(t, &AuthenticationError{}, err)
			case "RateLimitError":
				assert.IsType(t, &RateLimitError{}, err)
			case "ServerError":
				assert.IsType(t, &ServerError{}, err)
			}
		})
	}
}

func TestOpenAIAdapterStreaming(t *testing.T) {
	handler := func(w http.ResponseWriter, r *http.Request) {
		body, _ := io.ReadAll(r.Body)
		var reqBody map[string]interface{}
		json.Unmarshal(body, &reqBody)
		assert.True(t, reqBody["stream"].(bool))

		w.Header().Set("Content-Type", "text/event-stream")
		w.WriteHeader(http.StatusOK)

		events := []string{
			`event: response.output_text.delta` + "\n" + `data: {"delta":"Hello"}` + "\n\n",
			`event: response.output_text.delta` + "\n" + `data: {"delta":" world"}` + "\n\n",
			`event: response.output_item.done` + "\n" + `data: {"type":"message","item":{"type":"message"}}` + "\n\n",
			`event: response.completed` + "\n" + `data: {"response":{"id":"resp_s","model":"gpt-5.2","status":"completed","usage":{"input_tokens":5,"output_tokens":3}}}` + "\n\n",
		}

		for _, event := range events {
			w.Write([]byte(event))
			w.(http.Flusher).Flush()
		}
	}

	adapter, server := newTestOpenAIAdapter(t, handler)
	defer server.Close()

	ch, err := adapter.Stream(context.Background(), Request{
		Model:    "gpt-5.2",
		Messages: []Message{UserMessage("Hi")},
	})
	require.NoError(t, err)

	var events []StreamEvent
	for event := range ch {
		events = append(events, event)
	}

	// Expect: StreamStart, TextStart, TextDelta("Hello"), TextDelta(" world"), TextEnd, StreamFinish
	require.GreaterOrEqual(t, len(events), 4)

	// First should be StreamStart
	assert.Equal(t, StreamStart, events[0].Type)

	// Verify text deltas exist
	var fullText strings.Builder
	for _, evt := range events {
		if evt.Type == TextDelta {
			fullText.WriteString(evt.Delta)
		}
	}
	assert.Equal(t, "Hello world", fullText.String())

	// Last should be StreamFinish
	last := events[len(events)-1]
	assert.Equal(t, StreamFinish, last.Type)
	require.NotNil(t, last.Usage)
	assert.Equal(t, 5, last.Usage.InputTokens)
}

func TestOpenAIAdapterStreamingToolCalls(t *testing.T) {
	handler := func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/event-stream")
		w.WriteHeader(http.StatusOK)

		events := []string{
			`event: response.function_call_arguments.delta` + "\n" + `data: {"delta":"{\"loc","item_id":"call_1","name":"get_weather"}` + "\n\n",
			`event: response.function_call_arguments.delta` + "\n" + `data: {"delta":"ation\":\"NYC\"}","item_id":"call_1","name":"get_weather"}` + "\n\n",
			`event: response.output_item.done` + "\n" + `data: {"type":"function_call","item":{"type":"function_call","id":"call_1","name":"get_weather","arguments":"{\"location\":\"NYC\"}"}}` + "\n\n",
			`event: response.completed` + "\n" + `data: {"response":{"id":"resp_tc","model":"gpt-5.2","status":"completed","usage":{"input_tokens":10,"output_tokens":8}}}` + "\n\n",
		}

		for _, event := range events {
			w.Write([]byte(event))
			w.(http.Flusher).Flush()
		}
	}

	adapter, server := newTestOpenAIAdapter(t, handler)
	defer server.Close()

	ch, err := adapter.Stream(context.Background(), Request{
		Model:    "gpt-5.2",
		Messages: []Message{UserMessage("Weather?")},
	})
	require.NoError(t, err)

	var events []StreamEvent
	for event := range ch {
		events = append(events, event)
	}

	// Should have ToolCallStart, ToolCallDelta(s), ToolCallEnd, StreamFinish
	hasToolCallEnd := false
	for _, evt := range events {
		if evt.Type == ToolCallEnd {
			hasToolCallEnd = true
			require.NotNil(t, evt.ToolCall)
			assert.Equal(t, "call_1", evt.ToolCall.ID)
			assert.Equal(t, "get_weather", evt.ToolCall.Name)
		}
	}
	assert.True(t, hasToolCallEnd)
}

func TestOpenAIAdapterNamedToolChoice(t *testing.T) {
	var capturedBody map[string]interface{}
	handler := func(w http.ResponseWriter, r *http.Request) {
		body, _ := io.ReadAll(r.Body)
		json.Unmarshal(body, &capturedBody)
		w.WriteHeader(http.StatusOK)
		json.NewEncoder(w).Encode(map[string]interface{}{
			"id": "resp_tc", "model": "gpt-5.2", "status": "completed",
			"output": []interface{}{},
			"usage":  map[string]interface{}{"input_tokens": float64(1), "output_tokens": float64(1)},
		})
	}

	adapter, server := newTestOpenAIAdapter(t, handler)
	defer server.Close()

	_, err := adapter.Complete(context.Background(), Request{
		Model:    "gpt-5.2",
		Messages: []Message{UserMessage("test")},
		ToolDefs: []ToolDefinition{
			{Name: "my_tool", Description: "A tool", Parameters: map[string]interface{}{"type": "object"}},
		},
		ToolChoice: &ToolChoice{Mode: "named", ToolName: "my_tool"},
	})
	require.NoError(t, err)

	tc, ok := capturedBody["tool_choice"].(map[string]interface{})
	require.True(t, ok)
	assert.Equal(t, "function", tc["type"])
	fn, ok := tc["function"].(map[string]interface{})
	require.True(t, ok)
	assert.Equal(t, "my_tool", fn["name"])
}

func TestOpenAIAdapterImageInput(t *testing.T) {
	var capturedBody map[string]interface{}
	handler := func(w http.ResponseWriter, r *http.Request) {
		body, _ := io.ReadAll(r.Body)
		json.Unmarshal(body, &capturedBody)
		w.WriteHeader(http.StatusOK)
		json.NewEncoder(w).Encode(map[string]interface{}{
			"id": "resp_i", "model": "gpt-5.2", "status": "completed",
			"output": []interface{}{},
			"usage":  map[string]interface{}{"input_tokens": float64(1), "output_tokens": float64(1)},
		})
	}

	adapter, server := newTestOpenAIAdapter(t, handler)
	defer server.Close()

	_, err := adapter.Complete(context.Background(), Request{
		Model: "gpt-5.2",
		Messages: []Message{
			{Role: RoleUser, Content: []ContentPart{
				TextPart("What's in this image?"),
				ImageURLPart("https://example.com/img.png", "image/png", ""),
			}},
		},
	})
	require.NoError(t, err)

	input, ok := capturedBody["input"].([]interface{})
	require.True(t, ok)
	msg := input[0].(map[string]interface{})
	content := msg["content"].([]interface{})
	require.Len(t, content, 2)
	imgPart := content[1].(map[string]interface{})
	assert.Equal(t, "input_image", imgPart["type"])
	assert.Equal(t, "https://example.com/img.png", imgPart["image_url"])
}

func TestOpenAIAdapterConstructorRequiresKey(t *testing.T) {
	t.Setenv("OPENAI_API_KEY", "")
	_, err := NewOpenAIAdapter("")
	assert.Error(t, err)
	assert.IsType(t, &ConfigurationError{}, err)
}

func TestOpenAIAdapterConstructorFromEnv(t *testing.T) {
	t.Setenv("OPENAI_API_KEY", "sk-test-123")
	adapter, err := NewOpenAIAdapter("")
	require.NoError(t, err)
	assert.Equal(t, "sk-test-123", adapter.apiKey)
}

func TestOpenAIAdapterSupportsToolChoice(t *testing.T) {
	adapter := &OpenAIAdapter{}
	assert.True(t, adapter.SupportsToolChoice("auto"))
	assert.True(t, adapter.SupportsToolChoice("none"))
	assert.True(t, adapter.SupportsToolChoice("required"))
	assert.True(t, adapter.SupportsToolChoice("named"))
	assert.False(t, adapter.SupportsToolChoice("invalid"))
}

func TestOpenAIAdapterRetryAfterHeader(t *testing.T) {
	handler := func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Retry-After", "30")
		w.WriteHeader(429)
		fmt.Fprint(w, `{"error":{"message":"Rate limited","code":"rate_limit_exceeded"}}`)
	}

	adapter, server := newTestOpenAIAdapter(t, handler)
	defer server.Close()

	_, err := adapter.Complete(context.Background(), Request{
		Model:    "gpt-5.2",
		Messages: []Message{UserMessage("test")},
	})

	require.Error(t, err)
	var rlErr *RateLimitError
	require.ErrorAs(t, err, &rlErr)
	require.NotNil(t, rlErr.RetryAfter)
	assert.Equal(t, float64(30), *rlErr.RetryAfter)
}
