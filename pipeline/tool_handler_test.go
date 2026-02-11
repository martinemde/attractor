package pipeline

import (
	"runtime"
	"testing"
	"time"

	"github.com/martinemde/attractor/dotparser"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestToolHandler_ExecutesCommandAndCapturesStdout(t *testing.T) {
	handler := &ToolHandler{}
	node := newNode("tool_test", strAttr("tool_command", "echo hello world"))
	graph := newTestGraph([]*dotparser.Node{node}, nil, nil)
	ctx := NewContext()

	outcome, err := handler.Execute(node, ctx, graph, "")

	require.NoError(t, err)
	assert.Equal(t, StatusSuccess, outcome.Status)
	assert.Contains(t, outcome.Notes, "Tool completed")

	// Check context update
	output, ok := outcome.ContextUpdates["tool.output"]
	require.True(t, ok)
	assert.Equal(t, "hello world\n", output)
}

func TestToolHandler_ReturnsFailWhenNoToolCommand(t *testing.T) {
	handler := &ToolHandler{}
	node := newNode("tool_test") // no tool_command attribute
	graph := newTestGraph([]*dotparser.Node{node}, nil, nil)
	ctx := NewContext()

	outcome, err := handler.Execute(node, ctx, graph, "")

	require.NoError(t, err)
	assert.Equal(t, StatusFail, outcome.Status)
	assert.Equal(t, "No tool_command specified", outcome.FailureReason)
}

func TestToolHandler_ReturnsFailWhenEmptyToolCommand(t *testing.T) {
	handler := &ToolHandler{}
	node := newNode("tool_test", strAttr("tool_command", ""))
	graph := newTestGraph([]*dotparser.Node{node}, nil, nil)
	ctx := NewContext()

	outcome, err := handler.Execute(node, ctx, graph, "")

	require.NoError(t, err)
	assert.Equal(t, StatusFail, outcome.Status)
	assert.Equal(t, "No tool_command specified", outcome.FailureReason)
}

func TestToolHandler_ReturnsFailOnCommandError(t *testing.T) {
	handler := &ToolHandler{}
	node := newNode("tool_test", strAttr("tool_command", "exit 1"))
	graph := newTestGraph([]*dotparser.Node{node}, nil, nil)
	ctx := NewContext()

	outcome, err := handler.Execute(node, ctx, graph, "")

	require.NoError(t, err)
	assert.Equal(t, StatusFail, outcome.Status)
	assert.Contains(t, outcome.FailureReason, "exit status 1")
}

func TestToolHandler_ReturnsFailWithStderrOnError(t *testing.T) {
	handler := &ToolHandler{}
	node := newNode("tool_test", strAttr("tool_command", "echo error message >&2; exit 1"))
	graph := newTestGraph([]*dotparser.Node{node}, nil, nil)
	ctx := NewContext()

	outcome, err := handler.Execute(node, ctx, graph, "")

	require.NoError(t, err)
	assert.Equal(t, StatusFail, outcome.Status)
	assert.Contains(t, outcome.FailureReason, "error message")
}

func TestToolHandler_TimeoutStopsLongRunningCommand(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("Skipping timeout test on Windows")
	}

	handler := &ToolHandler{}
	// Create a node with a short timeout and a long-running command
	node := newNode("tool_test",
		strAttr("tool_command", "sleep 10"),
		strAttr("timeout", "100ms"),
	)
	graph := newTestGraph([]*dotparser.Node{node}, nil, nil)
	ctx := NewContext()

	start := time.Now()
	outcome, err := handler.Execute(node, ctx, graph, "")
	elapsed := time.Since(start)

	require.NoError(t, err)
	assert.Equal(t, StatusFail, outcome.Status)
	assert.Contains(t, outcome.FailureReason, "timed out")

	// Verify it didn't wait the full 10 seconds
	assert.Less(t, elapsed, 2*time.Second)
}

func TestToolHandler_TimeoutFromStringDuration(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("Skipping timeout test on Windows")
	}

	handler := &ToolHandler{}
	// Use a quoted string duration (e.g. timeout="100ms") instead of bare duration
	node := newNode("tool_test",
		strAttr("tool_command", "sleep 10"),
		strAttr("timeout", "100ms"),
	)
	graph := newTestGraph([]*dotparser.Node{node}, nil, nil)
	ctx := NewContext()

	start := time.Now()
	outcome, err := handler.Execute(node, ctx, graph, "")
	elapsed := time.Since(start)

	require.NoError(t, err)
	assert.Equal(t, StatusFail, outcome.Status)
	assert.Contains(t, outcome.FailureReason, "timed out")
	assert.Less(t, elapsed, 2*time.Second)
}

func TestToolHandler_UsesDefaultTimeoutWhenNotSpecified(t *testing.T) {
	handler := &ToolHandler{}
	// Quick command should succeed with default timeout
	node := newNode("tool_test", strAttr("tool_command", "echo quick"))
	graph := newTestGraph([]*dotparser.Node{node}, nil, nil)
	ctx := NewContext()

	outcome, err := handler.Execute(node, ctx, graph, "")

	require.NoError(t, err)
	assert.Equal(t, StatusSuccess, outcome.Status)
}

func TestToolHandler_ContextUpdateHasToolOutput(t *testing.T) {
	handler := &ToolHandler{}
	node := newNode("tool_test", strAttr("tool_command", "printf 'line1\\nline2\\nline3'"))
	graph := newTestGraph([]*dotparser.Node{node}, nil, nil)
	ctx := NewContext()

	outcome, err := handler.Execute(node, ctx, graph, "")

	require.NoError(t, err)
	assert.Equal(t, StatusSuccess, outcome.Status)

	output, ok := outcome.ContextUpdates["tool.output"]
	require.True(t, ok)
	assert.Equal(t, "line1\nline2\nline3", output)
}

func TestToolHandler_RegisteredInDefaultRegistry(t *testing.T) {
	registry := DefaultRegistry()

	// Test explicit type resolution
	node := newNode("tool", strAttr("type", "tool"))
	handler := registry.Resolve(node)
	assert.NotNil(t, handler)

	// Test shape-based resolution
	shapeNode := newNode("tool", strAttr("shape", "parallelogram"))
	shapeHandler := registry.Resolve(shapeNode)
	assert.NotNil(t, shapeHandler)
}

