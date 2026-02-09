package agentloop

import (
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
)

// TestTruncateOutputNoTruncation verifies that output within the limit is
// returned unchanged.
func TestTruncateOutputNoTruncation(t *testing.T) {
	output := "short output"
	result := TruncateOutput(output, 100, TruncateHeadTail)
	assert.Equal(t, output, result)
}

// TestTruncateOutputHeadTail verifies the head/tail split truncation mode
// per spec Section 5.1.
func TestTruncateOutputHeadTail(t *testing.T) {
	output := strings.Repeat("x", 1000)
	result := TruncateOutput(output, 100, TruncateHeadTail)

	assert.Contains(t, result, "[WARNING: Tool output was truncated.")
	assert.Contains(t, result, "900 characters were removed from the middle")
	// First half should be present.
	assert.True(t, strings.HasPrefix(result, strings.Repeat("x", 50)))
	// Last half should be present.
	assert.True(t, strings.HasSuffix(result, strings.Repeat("x", 50)))
}

// TestTruncateOutputTail verifies the tail-only truncation mode.
func TestTruncateOutputTail(t *testing.T) {
	output := strings.Repeat("a", 200) + strings.Repeat("b", 200)
	result := TruncateOutput(output, 200, TruncateTail)

	assert.Contains(t, result, "[WARNING: Tool output was truncated. First 200 characters were removed.")
	// Should end with the tail portion.
	assert.True(t, strings.HasSuffix(result, strings.Repeat("b", 200)))
}

// TestTruncateLinesNoTruncation verifies that line count within the limit is
// returned unchanged.
func TestTruncateLinesNoTruncation(t *testing.T) {
	output := "line1\nline2\nline3"
	result := TruncateLines(output, 10)
	assert.Equal(t, output, result)
}

// TestTruncateLinesExceedsLimit verifies head/tail line split.
func TestTruncateLinesExceedsLimit(t *testing.T) {
	lines := make([]string, 100)
	for i := range lines {
		lines[i] = "line"
	}
	output := strings.Join(lines, "\n")

	result := TruncateLines(output, 10)
	assert.Contains(t, result, "[... 90 lines omitted ...]")
}

// TestTruncateToolOutputDefaultLimits verifies that the full truncation
// pipeline applies correct per-tool defaults (spec Section 5.2).
func TestTruncateToolOutputDefaultLimits(t *testing.T) {
	tests := []struct {
		toolName string
		charLimit int
	}{
		{"read_file", 50000},
		{"shell", 30000},
		{"grep", 20000},
		{"glob", 20000},
		{"edit_file", 10000},
		{"apply_patch", 10000},
		{"write_file", 1000},
		{"spawn_agent", 20000},
	}

	for _, tt := range tests {
		t.Run(tt.toolName, func(t *testing.T) {
			// Output exactly at limit should not be truncated.
			output := strings.Repeat("x", tt.charLimit)
			result := TruncateToolOutput(output, tt.toolName, nil, nil)
			assert.Equal(t, output, result)

			// Output exceeding limit should be truncated.
			bigOutput := strings.Repeat("x", tt.charLimit+1000)
			result = TruncateToolOutput(bigOutput, tt.toolName, nil, nil)
			assert.Contains(t, result, "[WARNING: Tool output was truncated.")
		})
	}
}

// TestTruncateToolOutputCustomLimits verifies that custom limits from
// SessionConfig override defaults.
func TestTruncateToolOutputCustomLimits(t *testing.T) {
	customChars := map[string]int{"shell": 100}
	output := strings.Repeat("x", 200)

	result := TruncateToolOutput(output, "shell", customChars, nil)
	assert.Contains(t, result, "[WARNING: Tool output was truncated.")
}

// TestTruncateToolOutputLineLimits verifies that line-based truncation
// runs after character truncation (spec Section 5.3).
func TestTruncateToolOutputLineLimits(t *testing.T) {
	// Create output with many short lines.
	lines := make([]string, 500)
	for i := range lines {
		lines[i] = "line"
	}
	output := strings.Join(lines, "\n")

	// Shell default line limit is 256.
	result := TruncateToolOutput(output, "shell", nil, nil)
	assert.Contains(t, result, "lines omitted")
}

// TestTruncateToolOutputCharBeforeLines verifies character truncation runs
// FIRST (spec: "Character-based truncation must come first").
// A pathological case: 2 lines of 50000 chars each.
func TestTruncateToolOutputCharBeforeLines(t *testing.T) {
	// Two huge lines that line-based truncation would pass through.
	hugeLine := strings.Repeat("x", 50000)
	output := hugeLine + "\n" + hugeLine

	// Shell has 30000 char limit but only 256 line limit.
	// If lines ran first, it would see 2 lines (< 256) and pass through 100000 chars.
	// Character truncation must catch this.
	result := TruncateToolOutput(output, "shell", nil, nil)
	assert.Contains(t, result, "[WARNING: Tool output was truncated.")
	assert.Less(t, len(result), 100000)
}

// TestTruncateToolOutputUnknownTool verifies fallback for unknown tools.
func TestTruncateToolOutputUnknownTool(t *testing.T) {
	output := strings.Repeat("x", 40000)
	result := TruncateToolOutput(output, "unknown_tool", nil, nil)
	// Should use fallback of 30000 chars.
	assert.Contains(t, result, "[WARNING: Tool output was truncated.")
}

// TestDefaultTruncationModes verifies the spec-mandated truncation modes.
func TestDefaultTruncationModes(t *testing.T) {
	assert.Equal(t, TruncateHeadTail, DefaultTruncationModes["read_file"])
	assert.Equal(t, TruncateHeadTail, DefaultTruncationModes["shell"])
	assert.Equal(t, TruncateTail, DefaultTruncationModes["grep"])
	assert.Equal(t, TruncateTail, DefaultTruncationModes["glob"])
	assert.Equal(t, TruncateTail, DefaultTruncationModes["edit_file"])
	assert.Equal(t, TruncateTail, DefaultTruncationModes["apply_patch"])
	assert.Equal(t, TruncateTail, DefaultTruncationModes["write_file"])
	assert.Equal(t, TruncateHeadTail, DefaultTruncationModes["spawn_agent"])
}

// TestDefaultLineLimits verifies the spec-mandated line limits.
func TestDefaultLineLimits(t *testing.T) {
	assert.Equal(t, 256, DefaultToolLineLimits["shell"])
	assert.Equal(t, 200, DefaultToolLineLimits["grep"])
	assert.Equal(t, 500, DefaultToolLineLimits["glob"])
	// read_file and edit_file should NOT have line limits.
	_, hasReadFile := DefaultToolLineLimits["read_file"]
	assert.False(t, hasReadFile)
	_, hasEditFile := DefaultToolLineLimits["edit_file"]
	assert.False(t, hasEditFile)
}
