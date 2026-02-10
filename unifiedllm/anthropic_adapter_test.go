package unifiedllm

import (
	"context"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func newTestAnthropicAdapter(t *testing.T, handler http.HandlerFunc) (*AnthropicAdapter, *httptest.Server) {
	t.Helper()
	server := httptest.NewServer(handler)
	adapter := &AnthropicAdapter{
		apiKey:  "test-key",
		baseURL: server.URL,
		http:    newHTTPClient(),
	}
	return adapter, server
}

func TestAnthropicAdapterName(t *testing.T) {
	adapter := &AnthropicAdapter{}
	assert.Equal(t, "anthropic", adapter.Name())
}

func TestAnthropicAdapterComplete(t *testing.T) {
	handler := func(w http.ResponseWriter, r *http.Request) {
		assert.Equal(t, "POST", r.Method)
		assert.Equal(t, "/v1/messages", r.URL.Path)
		assert.Equal(t, "test-key", r.Header.Get("x-api-key"))
		assert.Equal(t, "2023-06-01", r.Header.Get("anthropic-version"))
		assert.Equal(t, "application/json", r.Header.Get("content-type"))

		body, _ := io.ReadAll(r.Body)
		var reqBody map[string]interface{}
		require.NoError(t, json.Unmarshal(body, &reqBody))
		assert.Equal(t, "claude-opus-4-6", reqBody["model"])

		// Verify system extracted
		system, ok := reqBody["system"].([]interface{})
		require.True(t, ok)
		require.Len(t, system, 1)

		// Verify max_tokens defaulted
		assert.Equal(t, float64(4096), reqBody["max_tokens"])

		w.WriteHeader(http.StatusOK)
		json.NewEncoder(w).Encode(map[string]interface{}{
			"id":    "msg_123",
			"model": "claude-opus-4-6",
			"content": []interface{}{
				map[string]interface{}{
					"type": "text",
					"text": "Hello from Claude!",
				},
			},
			"stop_reason": "end_turn",
			"usage": map[string]interface{}{
				"input_tokens":               float64(15),
				"output_tokens":              float64(8),
				"cache_read_input_tokens":    float64(5),
				"cache_creation_input_tokens": float64(10),
			},
		})
	}

	adapter, server := newTestAnthropicAdapter(t, handler)
	defer server.Close()

	resp, err := adapter.Complete(context.Background(), Request{
		Model: "claude-opus-4-6",
		Messages: []Message{
			SystemMessage("You are helpful."),
			UserMessage("Hi"),
		},
	})

	require.NoError(t, err)
	assert.Equal(t, "msg_123", resp.ID)
	assert.Equal(t, "claude-opus-4-6", resp.Model)
	assert.Equal(t, "anthropic", resp.Provider)
	assert.Equal(t, "Hello from Claude!", resp.Text())
	assert.Equal(t, "stop", resp.FinishReason.Reason)
	assert.Equal(t, "end_turn", resp.FinishReason.Raw)
	assert.Equal(t, 15, resp.Usage.InputTokens)
	assert.Equal(t, 8, resp.Usage.OutputTokens)
	require.NotNil(t, resp.Usage.CacheReadTokens)
	assert.Equal(t, 5, *resp.Usage.CacheReadTokens)
	require.NotNil(t, resp.Usage.CacheWriteTokens)
	assert.Equal(t, 10, *resp.Usage.CacheWriteTokens)
}

func TestAnthropicAdapterMaxTokensOverride(t *testing.T) {
	var capturedBody map[string]interface{}
	handler := func(w http.ResponseWriter, r *http.Request) {
		body, _ := io.ReadAll(r.Body)
		json.Unmarshal(body, &capturedBody)
		w.WriteHeader(http.StatusOK)
		json.NewEncoder(w).Encode(map[string]interface{}{
			"id": "msg_mt", "model": "claude-opus-4-6",
			"content":     []interface{}{map[string]interface{}{"type": "text", "text": "ok"}},
			"stop_reason": "end_turn",
			"usage":       map[string]interface{}{"input_tokens": float64(1), "output_tokens": float64(1)},
		})
	}

	adapter, server := newTestAnthropicAdapter(t, handler)
	defer server.Close()

	maxTokens := 8192
	_, err := adapter.Complete(context.Background(), Request{
		Model:     "claude-opus-4-6",
		Messages:  []Message{UserMessage("test")},
		MaxTokens: &maxTokens,
	})
	require.NoError(t, err)
	assert.Equal(t, float64(8192), capturedBody["max_tokens"])
}

func TestAnthropicAdapterToolCalls(t *testing.T) {
	var capturedBody map[string]interface{}
	handler := func(w http.ResponseWriter, r *http.Request) {
		body, _ := io.ReadAll(r.Body)
		json.Unmarshal(body, &capturedBody)

		w.WriteHeader(http.StatusOK)
		json.NewEncoder(w).Encode(map[string]interface{}{
			"id":    "msg_tc",
			"model": "claude-opus-4-6",
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

	adapter, server := newTestAnthropicAdapter(t, handler)
	defer server.Close()

	resp, err := adapter.Complete(context.Background(), Request{
		Model:    "claude-opus-4-6",
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

	// Verify tool definition format
	tools, ok := capturedBody["tools"].([]interface{})
	require.True(t, ok)
	require.Len(t, tools, 1)
	tool := tools[0].(map[string]interface{})
	assert.Equal(t, "get_weather", tool["name"])
	assert.NotNil(t, tool["input_schema"])

	// Verify tool choice
	tc, ok := capturedBody["tool_choice"].(map[string]interface{})
	require.True(t, ok)
	assert.Equal(t, "auto", tc["type"])
}

func TestAnthropicAdapterToolResultRoundTrip(t *testing.T) {
	var capturedBody map[string]interface{}
	handler := func(w http.ResponseWriter, r *http.Request) {
		body, _ := io.ReadAll(r.Body)
		json.Unmarshal(body, &capturedBody)
		w.WriteHeader(http.StatusOK)
		json.NewEncoder(w).Encode(map[string]interface{}{
			"id": "msg_tr", "model": "claude-opus-4-6",
			"content":     []interface{}{map[string]interface{}{"type": "text", "text": "It's sunny."}},
			"stop_reason": "end_turn",
			"usage":       map[string]interface{}{"input_tokens": float64(30), "output_tokens": float64(5)},
		})
	}

	adapter, server := newTestAnthropicAdapter(t, handler)
	defer server.Close()

	_, err := adapter.Complete(context.Background(), Request{
		Model: "claude-opus-4-6",
		Messages: []Message{
			UserMessage("What's the weather?"),
			{Role: RoleAssistant, Content: []ContentPart{
				ToolCallPart("toolu_abc", "get_weather", json.RawMessage(`{"location":"NYC"}`)),
			}},
			ToolResultMessage("toolu_abc", "72F and sunny", false),
		},
	})
	require.NoError(t, err)

	// Verify messages include tool_result in user message
	messages := capturedBody["messages"].([]interface{})
	// Should be: user, assistant, user (with tool_result)
	require.Len(t, messages, 3)
	toolResultMsg := messages[2].(map[string]interface{})
	assert.Equal(t, "user", toolResultMsg["role"])
	content := toolResultMsg["content"].([]interface{})
	require.Len(t, content, 1)
	trBlock := content[0].(map[string]interface{})
	assert.Equal(t, "tool_result", trBlock["type"])
	assert.Equal(t, "toolu_abc", trBlock["tool_use_id"])
}

func TestAnthropicAdapterPromptCaching(t *testing.T) {
	var capturedBody map[string]interface{}
	var capturedHeaders http.Header
	handler := func(w http.ResponseWriter, r *http.Request) {
		capturedHeaders = r.Header
		body, _ := io.ReadAll(r.Body)
		json.Unmarshal(body, &capturedBody)
		w.WriteHeader(http.StatusOK)
		json.NewEncoder(w).Encode(map[string]interface{}{
			"id": "msg_pc", "model": "claude-opus-4-6",
			"content":     []interface{}{map[string]interface{}{"type": "text", "text": "ok"}},
			"stop_reason": "end_turn",
			"usage":       map[string]interface{}{"input_tokens": float64(1), "output_tokens": float64(1)},
		})
	}

	adapter, server := newTestAnthropicAdapter(t, handler)
	defer server.Close()

	_, err := adapter.Complete(context.Background(), Request{
		Model: "claude-opus-4-6",
		Messages: []Message{
			SystemMessage("System prompt"),
			UserMessage("First message"),
			AssistantMessage("Response"),
			UserMessage("Second message"),
		},
		ToolDefs: []ToolDefinition{
			{Name: "my_tool", Description: "A tool", Parameters: map[string]interface{}{"type": "object"}},
		},
	})
	require.NoError(t, err)

	// Verify prompt-caching beta header is present
	betaHeader := capturedHeaders.Get("anthropic-beta")
	assert.Contains(t, betaHeader, "prompt-caching-2024-07-31")

	// Verify cache_control on last system block
	system := capturedBody["system"].([]interface{})
	lastSystem := system[len(system)-1].(map[string]interface{})
	assert.NotNil(t, lastSystem["cache_control"])

	// Verify cache_control on last tool
	tools := capturedBody["tools"].([]interface{})
	lastTool := tools[len(tools)-1].(map[string]interface{})
	assert.NotNil(t, lastTool["cache_control"])
}

func TestAnthropicAdapterPromptCachingDisabled(t *testing.T) {
	var capturedBody map[string]interface{}
	var capturedHeaders http.Header
	handler := func(w http.ResponseWriter, r *http.Request) {
		capturedHeaders = r.Header
		body, _ := io.ReadAll(r.Body)
		json.Unmarshal(body, &capturedBody)
		w.WriteHeader(http.StatusOK)
		json.NewEncoder(w).Encode(map[string]interface{}{
			"id": "msg_npc", "model": "claude-opus-4-6",
			"content":     []interface{}{map[string]interface{}{"type": "text", "text": "ok"}},
			"stop_reason": "end_turn",
			"usage":       map[string]interface{}{"input_tokens": float64(1), "output_tokens": float64(1)},
		})
	}

	adapter, server := newTestAnthropicAdapter(t, handler)
	defer server.Close()

	_, err := adapter.Complete(context.Background(), Request{
		Model:    "claude-opus-4-6",
		Messages: []Message{SystemMessage("System"), UserMessage("Hello")},
		ProviderOptions: map[string]interface{}{
			"anthropic": map[string]interface{}{
				"auto_cache": false,
			},
		},
	})
	require.NoError(t, err)

	// Should not have prompt-caching beta header
	assert.Empty(t, capturedHeaders.Get("anthropic-beta"))

	// Should not have cache_control on system
	system := capturedBody["system"].([]interface{})
	lastSystem := system[len(system)-1].(map[string]interface{})
	assert.Nil(t, lastSystem["cache_control"])
}

func TestAnthropicAdapterBetaHeaders(t *testing.T) {
	var capturedHeaders http.Header
	handler := func(w http.ResponseWriter, r *http.Request) {
		capturedHeaders = r.Header
		w.WriteHeader(http.StatusOK)
		json.NewEncoder(w).Encode(map[string]interface{}{
			"id": "msg_bh", "model": "claude-opus-4-6",
			"content":     []interface{}{map[string]interface{}{"type": "text", "text": "ok"}},
			"stop_reason": "end_turn",
			"usage":       map[string]interface{}{"input_tokens": float64(1), "output_tokens": float64(1)},
		})
	}

	adapter, server := newTestAnthropicAdapter(t, handler)
	defer server.Close()

	_, err := adapter.Complete(context.Background(), Request{
		Model:    "claude-opus-4-6",
		Messages: []Message{UserMessage("test")},
		ProviderOptions: map[string]interface{}{
			"anthropic": map[string]interface{}{
				"auto_cache": false,
				"beta_headers": []interface{}{
					"interleaved-thinking-2025-05-14",
					"token-efficient-tools-2025-02-19",
				},
			},
		},
	})
	require.NoError(t, err)

	betaHeader := capturedHeaders.Get("anthropic-beta")
	assert.Contains(t, betaHeader, "interleaved-thinking-2025-05-14")
	assert.Contains(t, betaHeader, "token-efficient-tools-2025-02-19")
}

func TestAnthropicAdapterThinking(t *testing.T) {
	handler := func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
		json.NewEncoder(w).Encode(map[string]interface{}{
			"id":    "msg_th",
			"model": "claude-opus-4-6",
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

	adapter, server := newTestAnthropicAdapter(t, handler)
	defer server.Close()

	resp, err := adapter.Complete(context.Background(), Request{
		Model:    "claude-opus-4-6",
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
	require.NotNil(t, resp.Usage.ReasoningTokens)
	assert.Greater(t, *resp.Usage.ReasoningTokens, 0)
}

func TestAnthropicAdapterRedactedThinking(t *testing.T) {
	handler := func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
		json.NewEncoder(w).Encode(map[string]interface{}{
			"id":    "msg_rt",
			"model": "claude-opus-4-6",
			"content": []interface{}{
				map[string]interface{}{
					"type": "redacted_thinking",
					"data": "opaque_data_here",
				},
				map[string]interface{}{
					"type": "text",
					"text": "Response.",
				},
			},
			"stop_reason": "end_turn",
			"usage":       map[string]interface{}{"input_tokens": float64(10), "output_tokens": float64(5)},
		})
	}

	adapter, server := newTestAnthropicAdapter(t, handler)
	defer server.Close()

	resp, err := adapter.Complete(context.Background(), Request{
		Model:    "claude-opus-4-6",
		Messages: []Message{UserMessage("test")},
	})

	require.NoError(t, err)
	// First content part should be redacted thinking
	require.Len(t, resp.Message.Content, 2)
	assert.Equal(t, ContentRedactedThinking, resp.Message.Content[0].Kind)
	assert.True(t, resp.Message.Content[0].Thinking.Redacted)
	assert.Equal(t, "opaque_data_here", resp.Message.Content[0].Thinking.Text)
}

func TestAnthropicAdapterMessageAlternation(t *testing.T) {
	var capturedBody map[string]interface{}
	handler := func(w http.ResponseWriter, r *http.Request) {
		body, _ := io.ReadAll(r.Body)
		json.Unmarshal(body, &capturedBody)
		w.WriteHeader(http.StatusOK)
		json.NewEncoder(w).Encode(map[string]interface{}{
			"id": "msg_ma", "model": "claude-opus-4-6",
			"content":     []interface{}{map[string]interface{}{"type": "text", "text": "ok"}},
			"stop_reason": "end_turn",
			"usage":       map[string]interface{}{"input_tokens": float64(1), "output_tokens": float64(1)},
		})
	}

	adapter, server := newTestAnthropicAdapter(t, handler)
	defer server.Close()

	// Send consecutive user messages that should be merged
	_, err := adapter.Complete(context.Background(), Request{
		Model: "claude-opus-4-6",
		Messages: []Message{
			UserMessage("First"),
			UserMessage("Second"),
		},
	})
	require.NoError(t, err)

	messages := capturedBody["messages"].([]interface{})
	// Consecutive user messages should be merged
	require.Len(t, messages, 1)
	msg := messages[0].(map[string]interface{})
	assert.Equal(t, "user", msg["role"])
	content := msg["content"].([]interface{})
	assert.Len(t, content, 2)
}

func TestAnthropicAdapterToolChoiceNone(t *testing.T) {
	var capturedBody map[string]interface{}
	handler := func(w http.ResponseWriter, r *http.Request) {
		body, _ := io.ReadAll(r.Body)
		json.Unmarshal(body, &capturedBody)
		w.WriteHeader(http.StatusOK)
		json.NewEncoder(w).Encode(map[string]interface{}{
			"id": "msg_tcn", "model": "claude-opus-4-6",
			"content":     []interface{}{map[string]interface{}{"type": "text", "text": "ok"}},
			"stop_reason": "end_turn",
			"usage":       map[string]interface{}{"input_tokens": float64(1), "output_tokens": float64(1)},
		})
	}

	adapter, server := newTestAnthropicAdapter(t, handler)
	defer server.Close()

	_, err := adapter.Complete(context.Background(), Request{
		Model:    "claude-opus-4-6",
		Messages: []Message{UserMessage("test")},
		ToolDefs: []ToolDefinition{
			{Name: "my_tool", Description: "Tool", Parameters: map[string]interface{}{"type": "object"}},
		},
		ToolChoice: &ToolChoice{Mode: "none"},
	})
	require.NoError(t, err)

	// Tools should be omitted for none mode
	_, hasTools := capturedBody["tools"]
	assert.False(t, hasTools)
}

func TestAnthropicAdapterStreaming(t *testing.T) {
	handler := func(w http.ResponseWriter, r *http.Request) {
		body, _ := io.ReadAll(r.Body)
		var reqBody map[string]interface{}
		json.Unmarshal(body, &reqBody)
		assert.True(t, reqBody["stream"].(bool))

		w.Header().Set("Content-Type", "text/event-stream")
		w.WriteHeader(http.StatusOK)

		events := []string{
			`event: message_start` + "\n" + `data: {"type":"message_start","message":{"id":"msg_s","model":"claude-opus-4-6","usage":{"input_tokens":10}}}` + "\n\n",
			`event: content_block_start` + "\n" + `data: {"type":"content_block_start","index":0,"content_block":{"type":"text","text":""}}` + "\n\n",
			`event: content_block_delta` + "\n" + `data: {"type":"content_block_delta","index":0,"delta":{"type":"text_delta","text":"Hello"}}` + "\n\n",
			`event: content_block_delta` + "\n" + `data: {"type":"content_block_delta","index":0,"delta":{"type":"text_delta","text":" world"}}` + "\n\n",
			`event: content_block_stop` + "\n" + `data: {"type":"content_block_stop","index":0}` + "\n\n",
			`event: message_delta` + "\n" + `data: {"type":"message_delta","delta":{"stop_reason":"end_turn"},"usage":{"output_tokens":5}}` + "\n\n",
		}

		for _, event := range events {
			w.Write([]byte(event))
			w.(http.Flusher).Flush()
		}
	}

	adapter, server := newTestAnthropicAdapter(t, handler)
	defer server.Close()

	ch, err := adapter.Stream(context.Background(), Request{
		Model:    "claude-opus-4-6",
		Messages: []Message{UserMessage("Hi")},
	})
	require.NoError(t, err)

	var events []StreamEvent
	for event := range ch {
		events = append(events, event)
	}

	// Verify event sequence: StreamStart, TextStart, TextDelta, TextDelta, TextEnd, StreamFinish
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

func TestAnthropicAdapterStreamingWithThinking(t *testing.T) {
	handler := func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/event-stream")
		w.WriteHeader(http.StatusOK)

		events := []string{
			`event: message_start` + "\n" + `data: {"type":"message_start","message":{"id":"msg_st","model":"claude-opus-4-6","usage":{"input_tokens":10}}}` + "\n\n",
			`event: content_block_start` + "\n" + `data: {"type":"content_block_start","index":0,"content_block":{"type":"thinking","thinking":""}}` + "\n\n",
			`event: content_block_delta` + "\n" + `data: {"type":"content_block_delta","index":0,"delta":{"type":"thinking_delta","thinking":"Let me think..."}}` + "\n\n",
			`event: content_block_stop` + "\n" + `data: {"type":"content_block_stop","index":0}` + "\n\n",
			`event: content_block_start` + "\n" + `data: {"type":"content_block_start","index":1,"content_block":{"type":"text","text":""}}` + "\n\n",
			`event: content_block_delta` + "\n" + `data: {"type":"content_block_delta","index":1,"delta":{"type":"text_delta","text":"Answer"}}` + "\n\n",
			`event: content_block_stop` + "\n" + `data: {"type":"content_block_stop","index":1}` + "\n\n",
			`event: message_delta` + "\n" + `data: {"type":"message_delta","delta":{"stop_reason":"end_turn"},"usage":{"output_tokens":15}}` + "\n\n",
		}

		for _, event := range events {
			w.Write([]byte(event))
			w.(http.Flusher).Flush()
		}
	}

	adapter, server := newTestAnthropicAdapter(t, handler)
	defer server.Close()

	ch, err := adapter.Stream(context.Background(), Request{
		Model:    "claude-opus-4-6",
		Messages: []Message{UserMessage("Think about this")},
	})
	require.NoError(t, err)

	var events []StreamEvent
	for event := range ch {
		events = append(events, event)
	}

	// Should have reasoning events
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

func TestAnthropicAdapterStreamingToolCalls(t *testing.T) {
	handler := func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/event-stream")
		w.WriteHeader(http.StatusOK)

		events := []string{
			`event: message_start` + "\n" + `data: {"type":"message_start","message":{"id":"msg_stc","model":"claude-opus-4-6","usage":{"input_tokens":10}}}` + "\n\n",
			`event: content_block_start` + "\n" + `data: {"type":"content_block_start","index":0,"content_block":{"type":"tool_use","id":"toolu_123","name":"get_weather"}}` + "\n\n",
			`event: content_block_delta` + "\n" + `data: {"type":"content_block_delta","index":0,"delta":{"type":"input_json_delta","partial_json":"{\"location\":"}}` + "\n\n",
			`event: content_block_delta` + "\n" + `data: {"type":"content_block_delta","index":0,"delta":{"type":"input_json_delta","partial_json":"\"NYC\"}"}}` + "\n\n",
			`event: content_block_stop` + "\n" + `data: {"type":"content_block_stop","index":0}` + "\n\n",
			`event: message_delta` + "\n" + `data: {"type":"message_delta","delta":{"stop_reason":"tool_use"},"usage":{"output_tokens":12}}` + "\n\n",
		}

		for _, event := range events {
			w.Write([]byte(event))
			w.(http.Flusher).Flush()
		}
	}

	adapter, server := newTestAnthropicAdapter(t, handler)
	defer server.Close()

	ch, err := adapter.Stream(context.Background(), Request{
		Model:    "claude-opus-4-6",
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

	// Last event should be finish with tool_calls reason
	last := events[len(events)-1]
	assert.Equal(t, StreamFinish, last.Type)
	assert.Equal(t, "tool_calls", last.FinishReason.Reason)
}

func TestAnthropicAdapterErrorHandling(t *testing.T) {
	tests := []struct {
		name       string
		statusCode int
		body       string
		errType    string
	}{
		{
			name:       "401 Unauthorized",
			statusCode: 401,
			body:       `{"type":"error","error":{"type":"authentication_error","message":"Invalid API key"}}`,
			errType:    "AuthenticationError",
		},
		{
			name:       "429 Rate Limited",
			statusCode: 429,
			body:       `{"type":"error","error":{"type":"rate_limit_error","message":"Rate limit exceeded"}}`,
			errType:    "RateLimitError",
		},
		{
			name:       "500 Server Error",
			statusCode: 500,
			body:       `{"type":"error","error":{"type":"api_error","message":"Internal server error"}}`,
			errType:    "ServerError",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			handler := func(w http.ResponseWriter, r *http.Request) {
				w.WriteHeader(tt.statusCode)
				w.Write([]byte(tt.body))
			}
			adapter, server := newTestAnthropicAdapter(t, handler)
			defer server.Close()

			_, err := adapter.Complete(context.Background(), Request{
				Model:    "claude-opus-4-6",
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

func TestAnthropicAdapterConstructorRequiresKey(t *testing.T) {
	t.Setenv("ANTHROPIC_API_KEY", "")
	_, err := NewAnthropicAdapter("")
	assert.Error(t, err)
	assert.IsType(t, &ConfigurationError{}, err)
}

func TestAnthropicAdapterConstructorFromEnv(t *testing.T) {
	t.Setenv("ANTHROPIC_API_KEY", "sk-ant-test-123")
	adapter, err := NewAnthropicAdapter("")
	require.NoError(t, err)
	assert.Equal(t, "sk-ant-test-123", adapter.apiKey)
}

func TestAnthropicAdapterImageInput(t *testing.T) {
	var capturedBody map[string]interface{}
	handler := func(w http.ResponseWriter, r *http.Request) {
		body, _ := io.ReadAll(r.Body)
		json.Unmarshal(body, &capturedBody)
		w.WriteHeader(http.StatusOK)
		json.NewEncoder(w).Encode(map[string]interface{}{
			"id": "msg_img", "model": "claude-opus-4-6",
			"content":     []interface{}{map[string]interface{}{"type": "text", "text": "ok"}},
			"stop_reason": "end_turn",
			"usage":       map[string]interface{}{"input_tokens": float64(1), "output_tokens": float64(1)},
		})
	}

	adapter, server := newTestAnthropicAdapter(t, handler)
	defer server.Close()

	_, err := adapter.Complete(context.Background(), Request{
		Model: "claude-opus-4-6",
		Messages: []Message{
			{Role: RoleUser, Content: []ContentPart{
				TextPart("What's this?"),
				ImageURLPart("https://example.com/img.png", "image/png", ""),
			}},
		},
	})
	require.NoError(t, err)

	messages := capturedBody["messages"].([]interface{})
	msg := messages[0].(map[string]interface{})
	content := msg["content"].([]interface{})
	require.Len(t, content, 2)
	imgBlock := content[1].(map[string]interface{})
	assert.Equal(t, "image", imgBlock["type"])
	source := imgBlock["source"].(map[string]interface{})
	assert.Equal(t, "url", source["type"])
	assert.Equal(t, "https://example.com/img.png", source["url"])
}

func TestAnthropicAdapterNamedToolChoice(t *testing.T) {
	var capturedBody map[string]interface{}
	handler := func(w http.ResponseWriter, r *http.Request) {
		body, _ := io.ReadAll(r.Body)
		json.Unmarshal(body, &capturedBody)
		w.WriteHeader(http.StatusOK)
		json.NewEncoder(w).Encode(map[string]interface{}{
			"id": "msg_ntc", "model": "claude-opus-4-6",
			"content":     []interface{}{map[string]interface{}{"type": "text", "text": "ok"}},
			"stop_reason": "end_turn",
			"usage":       map[string]interface{}{"input_tokens": float64(1), "output_tokens": float64(1)},
		})
	}

	adapter, server := newTestAnthropicAdapter(t, handler)
	defer server.Close()

	_, err := adapter.Complete(context.Background(), Request{
		Model:    "claude-opus-4-6",
		Messages: []Message{UserMessage("test")},
		ToolDefs: []ToolDefinition{
			{Name: "my_tool", Description: "A tool", Parameters: map[string]interface{}{"type": "object"}},
		},
		ToolChoice: &ToolChoice{Mode: "named", ToolName: "my_tool"},
	})
	require.NoError(t, err)

	tc := capturedBody["tool_choice"].(map[string]interface{})
	assert.Equal(t, "tool", tc["type"])
	assert.Equal(t, "my_tool", tc["name"])
}
