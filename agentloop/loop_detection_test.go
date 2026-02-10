package agentloop

import (
	"encoding/json"
	"testing"

	"github.com/martinemde/attractor/unifiedllm"
	"github.com/stretchr/testify/assert"
)

// makeToolCallTurn creates an assistant turn with the given tool calls
// for testing loop detection.
func makeToolCallTurn(calls ...struct{ name, args string }) Turn {
	toolCalls := make([]unifiedllm.ToolCall, len(calls))
	for i, c := range calls {
		toolCalls[i] = unifiedllm.ToolCall{
			ID:        "call_" + c.name,
			Name:      c.name,
			Arguments: json.RawMessage(c.args),
		}
	}
	return NewAssistantTurn("", toolCalls, "", unifiedllm.Usage{}, "")
}

// TestDetectLoopPatternLength1 verifies detection of the same call repeated
// N times (spec: pattern of length 1).
func TestDetectLoopPatternLength1(t *testing.T) {
	tc := struct{ name, args string }{"read_file", `{"file_path":"/tmp/test"}`}
	var history []Turn
	for i := 0; i < 10; i++ {
		history = append(history, makeToolCallTurn(tc))
	}

	assert.True(t, DetectLoop(history, 10))
}

// TestDetectLoopPatternLength2 verifies detection of alternating calls
// (spec: pattern of length 2).
func TestDetectLoopPatternLength2(t *testing.T) {
	tcA := struct{ name, args string }{"read_file", `{"file_path":"/a"}`}
	tcB := struct{ name, args string }{"edit_file", `{"file_path":"/a","old_string":"x","new_string":"y"}`}

	var history []Turn
	for i := 0; i < 10; i++ {
		if i%2 == 0 {
			history = append(history, makeToolCallTurn(tcA))
		} else {
			history = append(history, makeToolCallTurn(tcB))
		}
	}

	assert.True(t, DetectLoop(history, 10))
}

// TestDetectLoopPatternLength3 verifies detection of a 3-call cycle.
func TestDetectLoopPatternLength3(t *testing.T) {
	calls := []struct{ name, args string }{
		{"read_file", `{"file_path":"/a"}`},
		{"edit_file", `{"file_path":"/a","old_string":"x","new_string":"y"}`},
		{"shell", `{"command":"make test"}`},
	}

	var history []Turn
	// 9 calls = 3 repetitions of the 3-call pattern.
	for i := 0; i < 9; i++ {
		history = append(history, makeToolCallTurn(calls[i%3]))
	}

	assert.True(t, DetectLoop(history, 9))
}

// TestDetectLoopNoPattern verifies no false positive with varied calls.
func TestDetectLoopNoPattern(t *testing.T) {
	var history []Turn
	for i := 0; i < 10; i++ {
		tc := struct{ name, args string }{
			"read_file",
			`{"file_path":"/file_` + string(rune('a'+i)) + `"}`,
		}
		history = append(history, makeToolCallTurn(tc))
	}

	assert.False(t, DetectLoop(history, 10))
}

// TestDetectLoopInsufficientHistory verifies no detection with too few calls.
func TestDetectLoopInsufficientHistory(t *testing.T) {
	tc := struct{ name, args string }{"read_file", `{"file_path":"/tmp/test"}`}
	history := []Turn{makeToolCallTurn(tc)}

	assert.False(t, DetectLoop(history, 10))
}

// TestDetectLoopEmptyHistory verifies no detection with empty history.
func TestDetectLoopEmptyHistory(t *testing.T) {
	assert.False(t, DetectLoop(nil, 10))
}

// TestToolCallSignatureDeterminism verifies that the same tool call
// always produces the same signature.
func TestToolCallSignatureDeterminism(t *testing.T) {
	args := json.RawMessage(`{"file_path":"/tmp/test"}`)
	sig1 := toolCallSignature("read_file", args)
	sig2 := toolCallSignature("read_file", args)
	assert.Equal(t, sig1, sig2)
}

// TestToolCallSignatureDifference verifies that different arguments
// produce different signatures.
func TestToolCallSignatureDifference(t *testing.T) {
	sig1 := toolCallSignature("read_file", json.RawMessage(`{"file_path":"/a"}`))
	sig2 := toolCallSignature("read_file", json.RawMessage(`{"file_path":"/b"}`))
	assert.NotEqual(t, sig1, sig2)
}
