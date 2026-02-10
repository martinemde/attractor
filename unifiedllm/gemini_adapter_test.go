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

func newTestGeminiAdapter(t *testing.T, handler http.HandlerFunc) (*GeminiAdapter, *httptest.Server) {
	t.Helper()
	server := httptest.NewServer(handler)
	adapter := &GeminiAdapter{
		apiKey:  "test-key",
		baseURL: server.URL,
		http:    newHTTPClient(),
	}
	return adapter, server
}

func TestGeminiAdapterName(t *testing.T) {
	adapter := &GeminiAdapter{}
	assert.Equal(t, "gemini", adapter.Name())
}

func TestGeminiAdapterComplete(t *testing.T) {
	handler := func(w http.ResponseWriter, r *http.Request) {
		assert.Equal(t, "POST", r.Method)
		assert.Contains(t, r.URL.Path, "/v1beta/models/gemini-3-flash-preview:generateContent")
		assert.Equal(t, "test-key", r.URL.Query().Get("key"))
		assert.Equal(t, "application/json", r.Header.Get("Content-Type"))

		body, _ := io.ReadAll(r.Body)
		var reqBody map[string]interface{}
		require.NoError(t, json.Unmarshal(body, &reqBody))

		// Verify system instruction
		sysInst, ok := reqBody["systemInstruction"].(map[string]interface{})
		require.True(t, ok)
		parts := sysInst["parts"].([]interface{})
		require.Len(t, parts, 1)

		w.WriteHeader(http.StatusOK)
		json.NewEncoder(w).Encode(map[string]interface{}{
			"candidates": []interface{}{
				map[string]interface{}{
					"content": map[string]interface{}{
						"role": "model",
						"parts": []interface{}{
							map[string]interface{}{"text": "Hello from Gemini!"},
						},
					},
					"finishReason": "STOP",
				},
			},
			"usageMetadata": map[string]interface{}{
				"promptTokenCount":     float64(12),
				"candidatesTokenCount": float64(6),
				"thoughtsTokenCount":   float64(3),
			},
			"modelVersion": "gemini-3-flash-preview",
		})
	}

	adapter, server := newTestGeminiAdapter(t, handler)
	defer server.Close()

	resp, err := adapter.Complete(context.Background(), Request{
		Model: "gemini-3-flash-preview",
		Messages: []Message{
			SystemMessage("You are helpful."),
			UserMessage("Hi"),
		},
	})

	require.NoError(t, err)
	assert.Equal(t, "gemini", resp.Provider)
	assert.Equal(t, "Hello from Gemini!", resp.Text())
	assert.Equal(t, "stop", resp.FinishReason.Reason)
	assert.Equal(t, "STOP", resp.FinishReason.Raw)
	assert.Equal(t, 12, resp.Usage.InputTokens)
	assert.Equal(t, 6, resp.Usage.OutputTokens)
	assert.Equal(t, 18, resp.Usage.TotalTokens)
	require.NotNil(t, resp.Usage.ReasoningTokens)
	assert.Equal(t, 3, *resp.Usage.ReasoningTokens)
}

func TestGeminiAdapterToolCalls(t *testing.T) {
	var capturedBody map[string]interface{}
	handler := func(w http.ResponseWriter, r *http.Request) {
		body, _ := io.ReadAll(r.Body)
		json.Unmarshal(body, &capturedBody)

		w.WriteHeader(http.StatusOK)
		json.NewEncoder(w).Encode(map[string]interface{}{
			"candidates": []interface{}{
				map[string]interface{}{
					"content": map[string]interface{}{
						"role": "model",
						"parts": []interface{}{
							map[string]interface{}{
								"functionCall": map[string]interface{}{
									"name": "get_weather",
									"args": map[string]interface{}{"location": "NYC"},
								},
							},
						},
					},
					"finishReason": "STOP",
				},
			},
			"usageMetadata": map[string]interface{}{
				"promptTokenCount":     float64(20),
				"candidatesTokenCount": float64(10),
			},
		})
	}

	adapter, server := newTestGeminiAdapter(t, handler)
	defer server.Close()

	resp, err := adapter.Complete(context.Background(), Request{
		Model:    "gemini-3-flash-preview",
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
	assert.Equal(t, "get_weather", calls[0].Name)
	assert.True(t, strings.HasPrefix(calls[0].ID, "call_"))

	// Verify tool definition format
	tools, ok := capturedBody["tools"].([]interface{})
	require.True(t, ok)
	require.Len(t, tools, 1)
	toolObj := tools[0].(map[string]interface{})
	funcDecls := toolObj["functionDeclarations"].([]interface{})
	require.Len(t, funcDecls, 1)
	fd := funcDecls[0].(map[string]interface{})
	assert.Equal(t, "get_weather", fd["name"])

	// Verify tool config
	toolConfig, ok := capturedBody["toolConfig"].(map[string]interface{})
	require.True(t, ok)
	fcc := toolConfig["functionCallingConfig"].(map[string]interface{})
	assert.Equal(t, "AUTO", fcc["mode"])
}

func TestGeminiAdapterToolResultRoundTrip(t *testing.T) {
	var capturedBody map[string]interface{}
	handler := func(w http.ResponseWriter, r *http.Request) {
		body, _ := io.ReadAll(r.Body)
		json.Unmarshal(body, &capturedBody)
		w.WriteHeader(http.StatusOK)
		json.NewEncoder(w).Encode(map[string]interface{}{
			"candidates": []interface{}{
				map[string]interface{}{
					"content": map[string]interface{}{
						"role":  "model",
						"parts": []interface{}{map[string]interface{}{"text": "It's sunny."}},
					},
					"finishReason": "STOP",
				},
			},
			"usageMetadata": map[string]interface{}{
				"promptTokenCount":     float64(30),
				"candidatesTokenCount": float64(5),
			},
		})
	}

	adapter, server := newTestGeminiAdapter(t, handler)
	defer server.Close()

	_, err := adapter.Complete(context.Background(), Request{
		Model: "gemini-3-flash-preview",
		Messages: []Message{
			UserMessage("What's the weather?"),
			{Role: RoleAssistant, Content: []ContentPart{
				ToolCallPart("call_abc", "get_weather", json.RawMessage(`{"location":"NYC"}`)),
			}},
			ToolResultMessage("call_abc", "72F and sunny", false),
		},
	})
	require.NoError(t, err)

	// Verify functionResponse in user content
	contents := capturedBody["contents"].([]interface{})
	require.Len(t, contents, 3) // user, model, user (with functionResponse)

	toolResponseMsg := contents[2].(map[string]interface{})
	assert.Equal(t, "user", toolResponseMsg["role"])
	parts := toolResponseMsg["parts"].([]interface{})
	require.Len(t, parts, 1)
	fr := parts[0].(map[string]interface{})
	funcResp := fr["functionResponse"].(map[string]interface{})
	assert.Equal(t, "get_weather", funcResp["name"])
}

func TestGeminiAdapterGenerationConfig(t *testing.T) {
	var capturedBody map[string]interface{}
	handler := func(w http.ResponseWriter, r *http.Request) {
		body, _ := io.ReadAll(r.Body)
		json.Unmarshal(body, &capturedBody)
		w.WriteHeader(http.StatusOK)
		json.NewEncoder(w).Encode(map[string]interface{}{
			"candidates": []interface{}{
				map[string]interface{}{
					"content": map[string]interface{}{
						"role":  "model",
						"parts": []interface{}{map[string]interface{}{"text": "ok"}},
					},
					"finishReason": "STOP",
				},
			},
			"usageMetadata": map[string]interface{}{
				"promptTokenCount":     float64(1),
				"candidatesTokenCount": float64(1),
			},
		})
	}

	adapter, server := newTestGeminiAdapter(t, handler)
	defer server.Close()

	temp := 0.7
	topP := 0.9
	maxTokens := 1024
	_, err := adapter.Complete(context.Background(), Request{
		Model:         "gemini-3-flash-preview",
		Messages:      []Message{UserMessage("test")},
		Temperature:   &temp,
		TopP:          &topP,
		MaxTokens:     &maxTokens,
		StopSequences: []string{"END"},
	})
	require.NoError(t, err)

	genConfig, ok := capturedBody["generationConfig"].(map[string]interface{})
	require.True(t, ok)
	assert.Equal(t, 0.7, genConfig["temperature"])
	assert.Equal(t, 0.9, genConfig["topP"])
	assert.Equal(t, float64(1024), genConfig["maxOutputTokens"])
	stopSeqs := genConfig["stopSequences"].([]interface{})
	assert.Contains(t, stopSeqs, "END")
}

func TestGeminiAdapterToolChoiceModes(t *testing.T) {
	tests := []struct {
		mode     string
		toolName string
		expected map[string]interface{}
	}{
		{mode: "auto", expected: map[string]interface{}{"mode": "AUTO"}},
		{mode: "none", expected: map[string]interface{}{"mode": "NONE"}},
		{mode: "required", expected: map[string]interface{}{"mode": "ANY"}},
		{mode: "named", toolName: "my_func", expected: map[string]interface{}{"mode": "ANY", "allowedFunctionNames": []interface{}{"my_func"}}},
	}

	for _, tt := range tests {
		t.Run(tt.mode, func(t *testing.T) {
			var capturedBody map[string]interface{}
			handler := func(w http.ResponseWriter, r *http.Request) {
				body, _ := io.ReadAll(r.Body)
				json.Unmarshal(body, &capturedBody)
				w.WriteHeader(http.StatusOK)
				json.NewEncoder(w).Encode(map[string]interface{}{
					"candidates": []interface{}{
						map[string]interface{}{
							"content":      map[string]interface{}{"role": "model", "parts": []interface{}{map[string]interface{}{"text": "ok"}}},
							"finishReason": "STOP",
						},
					},
					"usageMetadata": map[string]interface{}{"promptTokenCount": float64(1), "candidatesTokenCount": float64(1)},
				})
			}

			adapter, server := newTestGeminiAdapter(t, handler)
			defer server.Close()

			tc := &ToolChoice{Mode: tt.mode, ToolName: tt.toolName}
			toolDefs := []ToolDefinition{{Name: "my_func", Description: "A func", Parameters: map[string]interface{}{"type": "object"}}}

			// Skip none mode for tools check -- none should omit tools
			if tt.mode == "none" {
				_, err := adapter.Complete(context.Background(), Request{
					Model: "gemini-3-flash-preview", Messages: []Message{UserMessage("test")},
					ToolDefs: toolDefs, ToolChoice: tc,
				})
				require.NoError(t, err)
				_, hasTools := capturedBody["tools"]
				assert.False(t, hasTools)
				return
			}

			_, err := adapter.Complete(context.Background(), Request{
				Model: "gemini-3-flash-preview", Messages: []Message{UserMessage("test")},
				ToolDefs: toolDefs, ToolChoice: tc,
			})
			require.NoError(t, err)

			if tc.Mode != "none" {
				toolConfig := capturedBody["toolConfig"].(map[string]interface{})
				fcc := toolConfig["functionCallingConfig"].(map[string]interface{})
				assert.Equal(t, tt.expected["mode"], fcc["mode"])
			}
		})
	}
}

func TestGeminiAdapterStreaming(t *testing.T) {
	handler := func(w http.ResponseWriter, r *http.Request) {
		assert.Contains(t, r.URL.Path, ":streamGenerateContent")
		assert.Equal(t, "sse", r.URL.Query().Get("alt"))

		w.Header().Set("Content-Type", "text/event-stream")
		w.WriteHeader(http.StatusOK)

		events := []string{
			`data: {"candidates":[{"content":{"role":"model","parts":[{"text":"Hello"}]}}],"usageMetadata":{"promptTokenCount":5,"candidatesTokenCount":2}}` + "\n\n",
			`data: {"candidates":[{"content":{"role":"model","parts":[{"text":" world"}]},"finishReason":"STOP"}],"usageMetadata":{"promptTokenCount":5,"candidatesTokenCount":4}}` + "\n\n",
		}

		for _, event := range events {
			w.Write([]byte(event))
			w.(http.Flusher).Flush()
		}
	}

	adapter, server := newTestGeminiAdapter(t, handler)
	defer server.Close()

	ch, err := adapter.Stream(context.Background(), Request{
		Model:    "gemini-3-flash-preview",
		Messages: []Message{UserMessage("Hi")},
	})
	require.NoError(t, err)

	var events []StreamEvent
	for event := range ch {
		events = append(events, event)
	}

	assert.Equal(t, StreamStart, events[0].Type)

	var fullText strings.Builder
	for _, evt := range events {
		if evt.Type == TextDelta {
			fullText.WriteString(evt.Delta)
		}
	}
	assert.Equal(t, "Hello world", fullText.String())

	last := events[len(events)-1]
	assert.Equal(t, StreamFinish, last.Type)
	require.NotNil(t, last.Usage)
	assert.Equal(t, 5, last.Usage.InputTokens)
	assert.Equal(t, 4, last.Usage.OutputTokens)
}

func TestGeminiAdapterStreamingToolCalls(t *testing.T) {
	handler := func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/event-stream")
		w.WriteHeader(http.StatusOK)

		events := []string{
			`data: {"candidates":[{"content":{"role":"model","parts":[{"functionCall":{"name":"get_weather","args":{"location":"NYC"}}}]},"finishReason":"STOP"}],"usageMetadata":{"promptTokenCount":10,"candidatesTokenCount":8}}` + "\n\n",
		}

		for _, event := range events {
			w.Write([]byte(event))
			w.(http.Flusher).Flush()
		}
	}

	adapter, server := newTestGeminiAdapter(t, handler)
	defer server.Close()

	ch, err := adapter.Stream(context.Background(), Request{
		Model:    "gemini-3-flash-preview",
		Messages: []Message{UserMessage("Weather?")},
	})
	require.NoError(t, err)

	var events []StreamEvent
	for event := range ch {
		events = append(events, event)
	}

	hasToolCallStart := false
	hasToolCallEnd := false
	for _, evt := range events {
		if evt.Type == ToolCallStart {
			hasToolCallStart = true
			assert.Equal(t, "get_weather", evt.ToolCall.Name)
		}
		if evt.Type == ToolCallEnd {
			hasToolCallEnd = true
			assert.Equal(t, "get_weather", evt.ToolCall.Name)
			assert.True(t, strings.HasPrefix(evt.ToolCall.ID, "call_"))
		}
	}
	assert.True(t, hasToolCallStart)
	assert.True(t, hasToolCallEnd)
}

func TestGeminiAdapterFinishReasons(t *testing.T) {
	tests := []struct {
		geminiReason string
		expected     string
	}{
		{"STOP", "stop"},
		{"MAX_TOKENS", "length"},
		{"SAFETY", "content_filter"},
		{"RECITATION", "content_filter"},
	}

	for _, tt := range tests {
		t.Run(tt.geminiReason, func(t *testing.T) {
			handler := func(w http.ResponseWriter, r *http.Request) {
				w.WriteHeader(http.StatusOK)
				json.NewEncoder(w).Encode(map[string]interface{}{
					"candidates": []interface{}{
						map[string]interface{}{
							"content":      map[string]interface{}{"role": "model", "parts": []interface{}{map[string]interface{}{"text": "ok"}}},
							"finishReason": tt.geminiReason,
						},
					},
					"usageMetadata": map[string]interface{}{"promptTokenCount": float64(1), "candidatesTokenCount": float64(1)},
				})
			}

			adapter, server := newTestGeminiAdapter(t, handler)
			defer server.Close()

			resp, err := adapter.Complete(context.Background(), Request{
				Model: "gemini-3-flash-preview", Messages: []Message{UserMessage("test")},
			})
			require.NoError(t, err)
			assert.Equal(t, tt.expected, resp.FinishReason.Reason)
			assert.Equal(t, tt.geminiReason, resp.FinishReason.Raw)
		})
	}
}

func TestGeminiAdapterErrorHandling(t *testing.T) {
	tests := []struct {
		name       string
		statusCode int
		body       string
		errType    string
	}{
		{
			name:       "400 Bad Request",
			statusCode: 400,
			body:       `{"error":{"message":"Invalid request","code":400}}`,
			errType:    "InvalidRequestError",
		},
		{
			name:       "401 Unauthorized",
			statusCode: 401,
			body:       `{"error":{"message":"API key invalid"}}`,
			errType:    "AuthenticationError",
		},
		{
			name:       "429 Rate Limited",
			statusCode: 429,
			body:       `{"error":{"message":"Quota exceeded"}}`,
			errType:    "RateLimitError",
		},
		{
			name:       "500 Server Error",
			statusCode: 500,
			body:       `{"error":{"message":"Internal error"}}`,
			errType:    "ServerError",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			handler := func(w http.ResponseWriter, r *http.Request) {
				w.WriteHeader(tt.statusCode)
				w.Write([]byte(tt.body))
			}
			adapter, server := newTestGeminiAdapter(t, handler)
			defer server.Close()

			_, err := adapter.Complete(context.Background(), Request{
				Model:    "gemini-3-flash-preview",
				Messages: []Message{UserMessage("test")},
			})

			require.Error(t, err)
			switch tt.errType {
			case "InvalidRequestError":
				assert.IsType(t, &InvalidRequestError{}, err)
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

func TestGeminiAdapterConstructorRequiresKey(t *testing.T) {
	t.Setenv("GEMINI_API_KEY", "")
	t.Setenv("GOOGLE_API_KEY", "")
	_, err := NewGeminiAdapter("")
	assert.Error(t, err)
	assert.IsType(t, &ConfigurationError{}, err)
}

func TestGeminiAdapterConstructorFromEnv(t *testing.T) {
	t.Setenv("GEMINI_API_KEY", "gemini-test-key")
	adapter, err := NewGeminiAdapter("")
	require.NoError(t, err)
	assert.Equal(t, "gemini-test-key", adapter.apiKey)
}

func TestGeminiAdapterConstructorFallbackGoogleKey(t *testing.T) {
	t.Setenv("GEMINI_API_KEY", "")
	t.Setenv("GOOGLE_API_KEY", "google-test-key")
	adapter, err := NewGeminiAdapter("")
	require.NoError(t, err)
	assert.Equal(t, "google-test-key", adapter.apiKey)
}

func TestGeminiAdapterImageInput(t *testing.T) {
	var capturedBody map[string]interface{}
	handler := func(w http.ResponseWriter, r *http.Request) {
		body, _ := io.ReadAll(r.Body)
		json.Unmarshal(body, &capturedBody)
		w.WriteHeader(http.StatusOK)
		json.NewEncoder(w).Encode(map[string]interface{}{
			"candidates": []interface{}{
				map[string]interface{}{
					"content":      map[string]interface{}{"role": "model", "parts": []interface{}{map[string]interface{}{"text": "ok"}}},
					"finishReason": "STOP",
				},
			},
			"usageMetadata": map[string]interface{}{"promptTokenCount": float64(1), "candidatesTokenCount": float64(1)},
		})
	}

	adapter, server := newTestGeminiAdapter(t, handler)
	defer server.Close()

	_, err := adapter.Complete(context.Background(), Request{
		Model: "gemini-3-flash-preview",
		Messages: []Message{
			{Role: RoleUser, Content: []ContentPart{
				TextPart("What's this?"),
				ImageURLPart("https://example.com/img.png", "image/png", ""),
			}},
		},
	})
	require.NoError(t, err)

	contents := capturedBody["contents"].([]interface{})
	msg := contents[0].(map[string]interface{})
	parts := msg["parts"].([]interface{})
	require.Len(t, parts, 2)
	imgPart := parts[1].(map[string]interface{})
	fileData := imgPart["fileData"].(map[string]interface{})
	assert.Equal(t, "image/png", fileData["mimeType"])
	assert.Equal(t, "https://example.com/img.png", fileData["fileUri"])
}

func TestGeminiAdapterCachedTokens(t *testing.T) {
	handler := func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
		json.NewEncoder(w).Encode(map[string]interface{}{
			"candidates": []interface{}{
				map[string]interface{}{
					"content":      map[string]interface{}{"role": "model", "parts": []interface{}{map[string]interface{}{"text": "ok"}}},
					"finishReason": "STOP",
				},
			},
			"usageMetadata": map[string]interface{}{
				"promptTokenCount":        float64(100),
				"candidatesTokenCount":    float64(10),
				"cachedContentTokenCount": float64(50),
			},
		})
	}

	adapter, server := newTestGeminiAdapter(t, handler)
	defer server.Close()

	resp, err := adapter.Complete(context.Background(), Request{
		Model:    "gemini-3-flash-preview",
		Messages: []Message{UserMessage("test")},
	})
	require.NoError(t, err)
	require.NotNil(t, resp.Usage.CacheReadTokens)
	assert.Equal(t, 50, *resp.Usage.CacheReadTokens)
}

func TestGeminiAdapterResponseFormat(t *testing.T) {
	var capturedBody map[string]interface{}
	handler := func(w http.ResponseWriter, r *http.Request) {
		body, _ := io.ReadAll(r.Body)
		json.Unmarshal(body, &capturedBody)
		w.WriteHeader(http.StatusOK)
		json.NewEncoder(w).Encode(map[string]interface{}{
			"candidates": []interface{}{
				map[string]interface{}{
					"content":      map[string]interface{}{"role": "model", "parts": []interface{}{map[string]interface{}{"text": "{}"}}},
					"finishReason": "STOP",
				},
			},
			"usageMetadata": map[string]interface{}{"promptTokenCount": float64(1), "candidatesTokenCount": float64(1)},
		})
	}

	adapter, server := newTestGeminiAdapter(t, handler)
	defer server.Close()

	schema := map[string]interface{}{"type": "object"}
	_, err := adapter.Complete(context.Background(), Request{
		Model:          "gemini-3-flash-preview",
		Messages:       []Message{UserMessage("test")},
		ResponseFormat: &ResponseFormat{Type: "json_schema", JSONSchema: schema},
	})
	require.NoError(t, err)

	genConfig := capturedBody["generationConfig"].(map[string]interface{})
	assert.Equal(t, "application/json", genConfig["responseMimeType"])
	assert.NotNil(t, genConfig["responseSchema"])
}

func TestGeminiAdapterThinkingConfig(t *testing.T) {
	var capturedBody map[string]interface{}
	handler := func(w http.ResponseWriter, r *http.Request) {
		body, _ := io.ReadAll(r.Body)
		json.Unmarshal(body, &capturedBody)
		w.WriteHeader(http.StatusOK)
		json.NewEncoder(w).Encode(map[string]interface{}{
			"candidates": []interface{}{
				map[string]interface{}{
					"content":      map[string]interface{}{"role": "model", "parts": []interface{}{map[string]interface{}{"text": "ok"}}},
					"finishReason": "STOP",
				},
			},
			"usageMetadata": map[string]interface{}{"promptTokenCount": float64(1), "candidatesTokenCount": float64(1)},
		})
	}

	adapter, server := newTestGeminiAdapter(t, handler)
	defer server.Close()

	_, err := adapter.Complete(context.Background(), Request{
		Model:    "gemini-3-flash-preview",
		Messages: []Message{UserMessage("test")},
		ProviderOptions: map[string]interface{}{
			"gemini": map[string]interface{}{
				"thinking": map[string]interface{}{"thinkingBudget": 1024},
			},
		},
	})
	require.NoError(t, err)

	thinkingConfig := capturedBody["thinkingConfig"].(map[string]interface{})
	assert.Equal(t, float64(1024), thinkingConfig["thinkingBudget"])
}

func TestGeminiAdapterSupportsToolChoice(t *testing.T) {
	adapter := &GeminiAdapter{}
	assert.True(t, adapter.SupportsToolChoice("auto"))
	assert.True(t, adapter.SupportsToolChoice("none"))
	assert.True(t, adapter.SupportsToolChoice("required"))
	assert.True(t, adapter.SupportsToolChoice("named"))
	assert.False(t, adapter.SupportsToolChoice("invalid"))
}
