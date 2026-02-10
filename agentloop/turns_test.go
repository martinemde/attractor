package agentloop

import (
	"encoding/json"
	"testing"

	"github.com/martinemde/attractor/unifiedllm"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestNewUserTurn(t *testing.T) {
	turn := NewUserTurn("hello world")
	assert.Equal(t, TurnUser, turn.Kind)
	require.NotNil(t, turn.User)
	assert.Equal(t, "hello world", turn.User.Content)
	assert.False(t, turn.Timestamp.IsZero())
}

func TestNewAssistantTurn(t *testing.T) {
	toolCalls := []unifiedllm.ToolCall{
		{ID: "call_1", Name: "read_file", Arguments: json.RawMessage(`{"file_path":"/tmp/test"}`)},
	}
	usage := unifiedllm.Usage{InputTokens: 100, OutputTokens: 50, TotalTokens: 150}

	turn := NewAssistantTurn("response text", toolCalls, "reasoning text", usage, "resp_123")
	assert.Equal(t, TurnAssistant, turn.Kind)
	require.NotNil(t, turn.Assistant)
	assert.Equal(t, "response text", turn.Assistant.Content)
	assert.Len(t, turn.Assistant.ToolCalls, 1)
	assert.Equal(t, "read_file", turn.Assistant.ToolCalls[0].Name)
	assert.Equal(t, "reasoning text", turn.Assistant.Reasoning)
	assert.Equal(t, 150, turn.Assistant.Usage.TotalTokens)
	assert.Equal(t, "resp_123", turn.Assistant.ResponseID)
}

func TestNewToolResultsTurn(t *testing.T) {
	results := []unifiedllm.ToolResult{
		{ToolCallID: "call_1", Content: "file contents", IsError: false},
		{ToolCallID: "call_2", Content: "error msg", IsError: true},
	}

	turn := NewToolResultsTurn(results)
	assert.Equal(t, TurnToolResults, turn.Kind)
	require.NotNil(t, turn.ToolResults)
	assert.Len(t, turn.ToolResults.Results, 2)
	assert.False(t, turn.ToolResults.Results[0].IsError)
	assert.True(t, turn.ToolResults.Results[1].IsError)
}

func TestNewSystemTurn(t *testing.T) {
	turn := NewSystemTurn("system message")
	assert.Equal(t, TurnSystem, turn.Kind)
	require.NotNil(t, turn.System)
	assert.Equal(t, "system message", turn.System.Content)
}

func TestNewSteeringTurn(t *testing.T) {
	turn := NewSteeringTurn("change approach")
	assert.Equal(t, TurnSteering, turn.Kind)
	require.NotNil(t, turn.Steering)
	assert.Equal(t, "change approach", turn.Steering.Content)
}

func TestTurnTextContent(t *testing.T) {
	tests := []struct {
		name     string
		turn     Turn
		expected string
	}{
		{"user turn", NewUserTurn("user text"), "user text"},
		{"assistant turn", NewAssistantTurn("assistant text", nil, "", unifiedllm.Usage{}, ""), "assistant text"},
		{"system turn", NewSystemTurn("system text"), "system text"},
		{"steering turn", NewSteeringTurn("steering text"), "steering text"},
		{"tool results turn", NewToolResultsTurn(nil), ""},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			assert.Equal(t, tt.expected, tt.turn.TextContent())
		})
	}
}

// TestConvertHistoryToMessages verifies the spec requirement (Section 2.5)
// that the history is correctly converted to LLM messages:
// - User turns -> user messages
// - Assistant turns -> assistant messages with tool call parts
// - Tool results -> tool result messages
// - System turns -> system messages
// - Steering turns -> user messages (so the model treats them as instructions)
func TestConvertHistoryToMessages(t *testing.T) {
	history := []Turn{
		NewUserTurn("what is 2+2?"),
		NewAssistantTurn("Let me calculate.", []unifiedllm.ToolCall{
			{ID: "call_1", Name: "shell", Arguments: json.RawMessage(`{"command":"echo 4"}`)},
		}, "", unifiedllm.Usage{}, ""),
		NewToolResultsTurn([]unifiedllm.ToolResult{
			{ToolCallID: "call_1", Content: "4", IsError: false},
		}),
		NewSteeringTurn("explain your work"),
		NewAssistantTurn("The answer is 4.", nil, "", unifiedllm.Usage{}, ""),
		NewSystemTurn("internal note"),
	}

	messages := ConvertHistoryToMessages(history)
	require.Len(t, messages, 6)

	// User turn -> user message.
	assert.Equal(t, unifiedllm.RoleUser, messages[0].Role)
	assert.Equal(t, "what is 2+2?", messages[0].TextContent())

	// Assistant turn with tool call -> assistant message with tool call part.
	assert.Equal(t, unifiedllm.RoleAssistant, messages[1].Role)
	assert.Equal(t, "Let me calculate.", messages[1].TextContent())
	// Should have text part + tool call part.
	require.Len(t, messages[1].Content, 2)
	assert.Equal(t, unifiedllm.ContentToolCall, messages[1].Content[1].Kind)
	assert.Equal(t, "shell", messages[1].Content[1].ToolCall.Name)

	// Tool results -> tool role messages.
	assert.Equal(t, unifiedllm.RoleTool, messages[2].Role)
	assert.Equal(t, "call_1", messages[2].ToolCallID)

	// Steering turn -> user message (spec requirement).
	assert.Equal(t, unifiedllm.RoleUser, messages[3].Role)
	assert.Equal(t, "explain your work", messages[3].TextContent())

	// Final assistant turn.
	assert.Equal(t, unifiedllm.RoleAssistant, messages[4].Role)

	// System turn.
	assert.Equal(t, unifiedllm.RoleSystem, messages[5].Role)
}

func TestConvertHistoryToMessagesEmpty(t *testing.T) {
	messages := ConvertHistoryToMessages(nil)
	assert.Empty(t, messages)
}
