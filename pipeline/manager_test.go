package pipeline

import (
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/martinemde/attractor/dotparser"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// MockSleeper records sleep calls without actually sleeping.
type MockSleeper struct {
	Calls []time.Duration
}

func (s *MockSleeper) Sleep(d time.Duration) {
	s.Calls = append(s.Calls, d)
}

func TestManagerLoopHandler_ChildCompleted(t *testing.T) {
	handler := NewManagerLoopHandler()
	sleeper := &MockSleeper{}
	handler.Sleeper = sleeper

	graph := newTestGraph(nil, nil, []dotparser.Attr{
		strAttr("stack.child_dotfile", "/path/to/child.dot"),
	})

	node := newNode("manager",
		strAttr("manager.max_cycles", "5"),
	)
	// Add int attribute for max_cycles
	node.Attrs = []dotparser.Attr{
		{Key: "manager.max_cycles", Value: dotparser.Value{Kind: dotparser.ValueInt, Int: 5}},
	}

	ctx := NewContext()
	// Simulate child completing on first observation
	ctx.Set("stack.child.status", "completed")
	ctx.Set("stack.child.outcome", "success")

	outcome, err := handler.Execute(node, ctx, graph, "")

	require.NoError(t, err)
	assert.Equal(t, StatusSuccess, outcome.Status)
	assert.Contains(t, outcome.Notes, "completed successfully")
}

func TestManagerLoopHandler_ChildFailed(t *testing.T) {
	handler := NewManagerLoopHandler()
	sleeper := &MockSleeper{}
	handler.Sleeper = sleeper

	graph := newTestGraph(nil, nil, []dotparser.Attr{
		strAttr("stack.child_dotfile", "/path/to/child.dot"),
	})

	node := newNode("manager")
	node.Attrs = []dotparser.Attr{
		{Key: "manager.max_cycles", Value: dotparser.Value{Kind: dotparser.ValueInt, Int: 5}},
	}

	ctx := NewContext()
	// Simulate child failing
	ctx.Set("stack.child.status", "failed")
	ctx.Set("stack.child.failure_reason", "child pipeline error")

	outcome, err := handler.Execute(node, ctx, graph, "")

	require.NoError(t, err)
	assert.Equal(t, StatusFail, outcome.Status)
	assert.Contains(t, outcome.FailureReason, "child pipeline error")
}

func TestManagerLoopHandler_MaxCyclesExceeded(t *testing.T) {
	handler := NewManagerLoopHandler()
	sleeper := &MockSleeper{}
	handler.Sleeper = sleeper

	graph := newTestGraph(nil, nil, []dotparser.Attr{
		strAttr("stack.child_dotfile", "/path/to/child.dot"),
	})

	node := newNode("manager")
	node.Attrs = []dotparser.Attr{
		{Key: "manager.max_cycles", Value: dotparser.Value{Kind: dotparser.ValueInt, Int: 3}},
		{Key: "manager.actions", Value: dotparser.Value{Kind: dotparser.ValueString, Str: "observe"}},
	}

	ctx := NewContext()
	// Child never completes - status stays "running"
	ctx.Set("stack.child.status", "running")

	outcome, err := handler.Execute(node, ctx, graph, "")

	require.NoError(t, err)
	assert.Equal(t, StatusFail, outcome.Status)
	assert.Contains(t, outcome.FailureReason, "max cycles exceeded")
}

func TestManagerLoopHandler_StopConditionSatisfied(t *testing.T) {
	handler := NewManagerLoopHandler()
	sleeper := &MockSleeper{}
	handler.Sleeper = sleeper

	graph := newTestGraph(nil, nil, []dotparser.Attr{
		strAttr("stack.child_dotfile", "/path/to/child.dot"),
	})

	node := newNode("manager")
	node.Attrs = []dotparser.Attr{
		{Key: "manager.max_cycles", Value: dotparser.Value{Kind: dotparser.ValueInt, Int: 100}},
		{Key: "manager.stop_condition", Value: dotparser.Value{Kind: dotparser.ValueString, Str: "context.custom_flag=done"}},
		{Key: "manager.actions", Value: dotparser.Value{Kind: dotparser.ValueString, Str: "observe"}},
	}

	ctx := NewContext()
	ctx.Set("stack.child.status", "running")
	ctx.Set("custom_flag", "done") // This should satisfy the stop condition

	outcome, err := handler.Execute(node, ctx, graph, "")

	require.NoError(t, err)
	assert.Equal(t, StatusSuccess, outcome.Status)
	assert.Contains(t, outcome.Notes, "Stop condition satisfied")
}

func TestManagerLoopHandler_PollIntervalRespected(t *testing.T) {
	handler := NewManagerLoopHandler()
	sleeper := &MockSleeper{}
	handler.Sleeper = sleeper

	graph := newTestGraph(nil, nil, []dotparser.Attr{
		strAttr("stack.child_dotfile", "/path/to/child.dot"),
	})

	node := newNode("manager")
	node.Attrs = []dotparser.Attr{
		{Key: "manager.max_cycles", Value: dotparser.Value{Kind: dotparser.ValueInt, Int: 3}},
		{Key: "manager.poll_interval", Value: dotparser.Value{Kind: dotparser.ValueString, Str: "100ms", Raw: "100ms"}},
		{Key: "manager.actions", Value: dotparser.Value{Kind: dotparser.ValueString, Str: "observe,wait"}},
	}

	ctx := NewContext()
	ctx.Set("stack.child.status", "running")

	_, err := handler.Execute(node, ctx, graph, "")
	require.NoError(t, err)

	// Should have waited 3 times (once per cycle)
	require.Len(t, sleeper.Calls, 3)
	for _, d := range sleeper.Calls {
		assert.Equal(t, 100*time.Millisecond, d)
	}
}

func TestManagerLoopHandler_NoWaitAction(t *testing.T) {
	handler := NewManagerLoopHandler()
	sleeper := &MockSleeper{}
	handler.Sleeper = sleeper

	graph := newTestGraph(nil, nil, []dotparser.Attr{
		strAttr("stack.child_dotfile", "/path/to/child.dot"),
	})

	node := newNode("manager")
	node.Attrs = []dotparser.Attr{
		{Key: "manager.max_cycles", Value: dotparser.Value{Kind: dotparser.ValueInt, Int: 3}},
		{Key: "manager.actions", Value: dotparser.Value{Kind: dotparser.ValueString, Str: "observe"}}, // no wait
	}

	ctx := NewContext()
	ctx.Set("stack.child.status", "running")

	_, err := handler.Execute(node, ctx, graph, "")
	require.NoError(t, err)

	// Should not have slept because "wait" action is not included
	assert.Len(t, sleeper.Calls, 0)
}

func TestManagerLoopHandler_AutoStartSetsContext(t *testing.T) {
	// Create a temporary child DOT file
	tmpDir := t.TempDir()
	childDotfile := filepath.Join(tmpDir, "child.dot")
	childDOT := `digraph ChildPipeline {
		start [shape=Mdiamond]
		done [shape=Msquare]
		start -> done
	}`
	err := os.WriteFile(childDotfile, []byte(childDOT), 0o644)
	require.NoError(t, err)

	handler := NewManagerLoopHandler()
	sleeper := &MockSleeper{}
	handler.Sleeper = sleeper

	graph := newTestGraph(nil, nil, []dotparser.Attr{
		strAttr("stack.child_dotfile", childDotfile),
	})

	node := newNode("manager")
	node.Attrs = []dotparser.Attr{
		{Key: "manager.max_cycles", Value: dotparser.Value{Kind: dotparser.ValueInt, Int: 1}},
		{Key: "manager.actions", Value: dotparser.Value{Kind: dotparser.ValueString, Str: "observe"}},
	}

	ctx := NewContext()
	// Don't pre-set child status - let autostart set it

	// Use a dedicated logs directory that we can clean up manually
	logsDir := filepath.Join(tmpDir, "logs")
	require.NoError(t, os.MkdirAll(logsDir, 0o755))

	_, err = handler.Execute(node, ctx, graph, logsDir)
	require.NoError(t, err)

	// Autostart should have set the child status to "running"
	status := ctx.GetString("stack.child.status", "")
	assert.Equal(t, "running", status)

	// Should have set the dotfile path
	dotfile := ctx.GetString("stack.child.dotfile", "")
	assert.Equal(t, childDotfile, dotfile)

	// Should have set the graph name
	graphName := ctx.GetString("stack.child.graph_name", "")
	assert.Equal(t, "ChildPipeline", graphName)

	// Wait briefly for the child goroutine to complete so temp cleanup works
	time.Sleep(50 * time.Millisecond)
}

func TestManagerLoopHandler_ChildPartialSuccess(t *testing.T) {
	handler := NewManagerLoopHandler()
	sleeper := &MockSleeper{}
	handler.Sleeper = sleeper

	graph := newTestGraph(nil, nil, []dotparser.Attr{
		strAttr("stack.child_dotfile", "/path/to/child.dot"),
	})

	node := newNode("manager")
	node.Attrs = []dotparser.Attr{
		{Key: "manager.max_cycles", Value: dotparser.Value{Kind: dotparser.ValueInt, Int: 5}},
	}

	ctx := NewContext()
	ctx.Set("stack.child.status", "completed")
	ctx.Set("stack.child.outcome", "partial_success")

	outcome, err := handler.Execute(node, ctx, graph, "")

	require.NoError(t, err)
	assert.Equal(t, StatusPartialSuccess, outcome.Status)
	assert.Contains(t, outcome.Notes, "partial_success")
}

func TestManagerLoopHandler_RegisteredInDefaultRegistry(t *testing.T) {
	registry := DefaultRegistry()

	// Create a node with type=stack.manager_loop
	node := newNode("manager", strAttr("type", "stack.manager_loop"))

	handler := registry.Resolve(node)
	require.NotNil(t, handler)

	_, ok := handler.(*ManagerLoopHandler)
	assert.True(t, ok, "expected ManagerLoopHandler")
}

func TestManagerLoopHandler_RegisteredByShape(t *testing.T) {
	registry := DefaultRegistry()

	// Create a node with shape=house (maps to stack.manager_loop)
	node := newNode("manager", strAttr("shape", "house"))

	handler := registry.Resolve(node)
	require.NotNil(t, handler)

	_, ok := handler.(*ManagerLoopHandler)
	assert.True(t, ok, "expected ManagerLoopHandler for shape=house")
}

func TestManagerLoopHandler_StartChild_ParsesAndRunsChild(t *testing.T) {
	// Create a temporary child DOT file with a simple pipeline
	tmpDir := t.TempDir()
	childDotfile := filepath.Join(tmpDir, "child.dot")
	childDOT := `digraph TestChild {
		start [shape=Mdiamond]
		work [label="Work Node"]
		done [shape=Msquare]
		start -> work -> done
	}`
	err := os.WriteFile(childDotfile, []byte(childDOT), 0o644)
	require.NoError(t, err)

	handler := NewManagerLoopHandler()
	sleeper := &MockSleeper{}
	handler.Sleeper = sleeper

	graph := newTestGraph(nil, nil, []dotparser.Attr{
		strAttr("stack.child_dotfile", childDotfile),
	})

	node := newNode("manager")
	node.Attrs = []dotparser.Attr{
		{Key: "manager.max_cycles", Value: dotparser.Value{Kind: dotparser.ValueInt, Int: 50}},
		{Key: "manager.poll_interval", Value: dotparser.Value{Kind: dotparser.ValueString, Str: "10ms", Raw: "10ms"}},
		{Key: "manager.actions", Value: dotparser.Value{Kind: dotparser.ValueString, Str: "observe,wait"}},
	}

	ctx := NewContext()
	logsRoot := filepath.Join(tmpDir, "logs")

	// Execute should start the child and eventually complete
	outcome, err := handler.Execute(node, ctx, graph, logsRoot)
	require.NoError(t, err)

	// Child should have completed successfully
	assert.Equal(t, StatusSuccess, outcome.Status)
	assert.Contains(t, outcome.Notes, "completed successfully")

	// Verify child logs directory was created
	childLogsDir := filepath.Join(logsRoot, "child-TestChild")
	_, err = os.Stat(childLogsDir)
	assert.NoError(t, err, "child logs directory should exist")
}

func TestManagerLoopHandler_StartChild_FailsOnMissingDotfile(t *testing.T) {
	handler := NewManagerLoopHandler()
	sleeper := &MockSleeper{}
	handler.Sleeper = sleeper

	graph := newTestGraph(nil, nil, []dotparser.Attr{
		strAttr("stack.child_dotfile", "/nonexistent/path/child.dot"),
	})

	node := newNode("manager")
	node.Attrs = []dotparser.Attr{
		{Key: "manager.max_cycles", Value: dotparser.Value{Kind: dotparser.ValueInt, Int: 1}},
	}

	ctx := NewContext()

	outcome, err := handler.Execute(node, ctx, graph, "")
	require.NoError(t, err)
	assert.Equal(t, StatusFail, outcome.Status)
	assert.Contains(t, outcome.FailureReason, "failed to start child pipeline")
	assert.Contains(t, outcome.FailureReason, "failed to read child dotfile")
}

func TestManagerLoopHandler_StartChild_FailsOnInvalidDotfile(t *testing.T) {
	tmpDir := t.TempDir()
	childDotfile := filepath.Join(tmpDir, "invalid.dot")
	err := os.WriteFile(childDotfile, []byte("this is not valid DOT syntax { }}}"), 0o644)
	require.NoError(t, err)

	handler := NewManagerLoopHandler()
	sleeper := &MockSleeper{}
	handler.Sleeper = sleeper

	graph := newTestGraph(nil, nil, []dotparser.Attr{
		strAttr("stack.child_dotfile", childDotfile),
	})

	node := newNode("manager")
	node.Attrs = []dotparser.Attr{
		{Key: "manager.max_cycles", Value: dotparser.Value{Kind: dotparser.ValueInt, Int: 1}},
	}

	ctx := NewContext()

	outcome, err := handler.Execute(node, ctx, graph, "")
	require.NoError(t, err)
	assert.Equal(t, StatusFail, outcome.Status)
	assert.Contains(t, outcome.FailureReason, "failed to start child pipeline")
	assert.Contains(t, outcome.FailureReason, "failed to parse child dotfile")
}

func TestManagerLoopHandler_StartChild_FailsOnMissingDotfileAttr(t *testing.T) {
	handler := NewManagerLoopHandler()
	sleeper := &MockSleeper{}
	handler.Sleeper = sleeper

	// Graph without stack.child_dotfile attribute
	graph := newTestGraph(nil, nil, nil)

	node := newNode("manager")
	node.Attrs = []dotparser.Attr{
		{Key: "manager.max_cycles", Value: dotparser.Value{Kind: dotparser.ValueInt, Int: 1}},
	}

	ctx := NewContext()

	outcome, err := handler.Execute(node, ctx, graph, "")
	require.NoError(t, err)
	assert.Equal(t, StatusFail, outcome.Status)
	assert.Contains(t, outcome.FailureReason, "stack.child_dotfile not specified")
}

func TestManagerLoopHandler_IngestTelemetry_ReadsCheckpoint(t *testing.T) {
	tmpDir := t.TempDir()
	logsRoot := filepath.Join(tmpDir, "child-logs")
	require.NoError(t, os.MkdirAll(logsRoot, 0o755))

	// Create a checkpoint file
	checkpoint := &Checkpoint{
		Timestamp:      time.Now(),
		CurrentNode:    "stage2",
		CompletedNodes: []string{"start", "stage1", "stage2"},
		NodeRetries:    map[string]int{"stage1": 1, "stage2": 0},
		ContextValues:  map[string]any{"some_key": "some_value"},
	}
	err := SaveCheckpoint(checkpoint, logsRoot)
	require.NoError(t, err)

	// Create status.json files in node directories
	stage1Dir := filepath.Join(logsRoot, "stage1")
	require.NoError(t, os.MkdirAll(stage1Dir, 0o755))
	require.NoError(t, writeTestStatusJSON(filepath.Join(stage1Dir, "status.json"), "success", "completed stage1"))
	// Add an artifact file
	require.NoError(t, os.WriteFile(filepath.Join(stage1Dir, "output.txt"), []byte("artifact content"), 0o644))

	stage2Dir := filepath.Join(logsRoot, "stage2")
	require.NoError(t, os.MkdirAll(stage2Dir, 0o755))
	require.NoError(t, writeTestStatusJSON(filepath.Join(stage2Dir, "status.json"), "success", ""))

	handler := NewManagerLoopHandler()
	ctx := NewContext()
	ctx.Set("stack.child.logs_root", logsRoot)

	// Ingest telemetry
	handler.ingestChildTelemetry(ctx)

	// Verify checkpoint data was ingested
	currentNode := ctx.GetString("stack.child.current_node", "")
	assert.Equal(t, "stage2", currentNode)

	completedNodes, ok := ctx.Get("stack.child.completed_nodes")
	assert.True(t, ok)
	assert.Equal(t, []string{"start", "stage1", "stage2"}, completedNodes)

	// Verify retry counts
	retryCount1, ok := ctx.Get("stack.child.retry_count.stage1")
	assert.True(t, ok)
	assert.Equal(t, 1, retryCount1)

	retryCount2, ok := ctx.Get("stack.child.retry_count.stage2")
	assert.True(t, ok)
	assert.Equal(t, 0, retryCount2)

	// Verify node outcomes
	nodeOutcomes, ok := ctx.Get("stack.child.node_outcomes")
	assert.True(t, ok)
	outcomesMap, ok := nodeOutcomes.(map[string]string)
	assert.True(t, ok)
	assert.Equal(t, "success", outcomesMap["stage1"])
	assert.Equal(t, "success", outcomesMap["stage2"])

	// Verify artifacts
	artifacts, ok := ctx.Get("stack.child.artifacts")
	assert.True(t, ok)
	artifactsMap, ok := artifacts.(map[string][]string)
	assert.True(t, ok)
	assert.Contains(t, artifactsMap["stage1"], filepath.Join(stage1Dir, "output.txt"))
}

// writeTestStatusJSON is a helper to write status.json files for tests
func writeTestStatusJSON(path, status, notes string) error {
	data := `{"status":"` + status + `"`
	if notes != "" {
		data += `,"notes":"` + notes + `"`
	}
	data += `}`
	return os.WriteFile(path, []byte(data), 0o644)
}

func TestManagerLoopHandler_IngestTelemetry_FromChildState(t *testing.T) {
	handler := NewManagerLoopHandler()

	// Simulate a completed child goroutine
	handler.childState = &childPipelineState{
		logsRoot: "",
		done:     true,
		result: &RunResult{
			FinalOutcome:   Success().WithNotes("child completed"),
			CompletedNodes: []string{"start", "work", "done"},
		},
	}

	ctx := NewContext()
	ctx.Set("stack.child.status", "running") // Initially running

	// Ingest telemetry - should detect completed goroutine
	handler.ingestChildTelemetry(ctx)

	// Verify child completion was detected
	status := ctx.GetString("stack.child.status", "")
	assert.Equal(t, "completed", status)

	outcome := ctx.GetString("stack.child.outcome", "")
	assert.Equal(t, "success", outcome)

	completedNodes, ok := ctx.Get("stack.child.completed_nodes")
	assert.True(t, ok)
	assert.Equal(t, []string{"start", "work", "done"}, completedNodes)
}

func TestManagerLoopHandler_IngestTelemetry_FromChildStateWithError(t *testing.T) {
	handler := NewManagerLoopHandler()

	// Simulate a failed child goroutine
	handler.childState = &childPipelineState{
		logsRoot: "",
		done:     true,
		err:      assert.AnError, // Use testify's standard error
	}

	ctx := NewContext()
	ctx.Set("stack.child.status", "running")

	// Ingest telemetry - should detect failed goroutine
	handler.ingestChildTelemetry(ctx)

	// Verify child failure was detected
	status := ctx.GetString("stack.child.status", "")
	assert.Equal(t, "failed", status)

	failureReason := ctx.GetString("stack.child.failure_reason", "")
	assert.Contains(t, failureReason, "assert.AnError")
}
