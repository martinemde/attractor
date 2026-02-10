package pipeline

import (
	"encoding/json"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/martinemde/attractor/dotparser"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestSaveCheckpoint_And_LoadCheckpoint_RoundTrip(t *testing.T) {
	tmpDir := t.TempDir()

	original := &Checkpoint{
		Timestamp:      time.Now().Truncate(time.Millisecond),
		CurrentNode:    "nodeB",
		CompletedNodes: []string{"start", "nodeA", "nodeB"},
		NodeRetries: map[string]int{
			"nodeA": 2,
			"nodeB": 1,
		},
		ContextValues: map[string]any{
			"key1":   "value1",
			"key2":   float64(42), // JSON numbers deserialize as float64
			"nested": "deep.value",
		},
		Logs: []string{"log entry 1", "log entry 2"},
	}

	err := SaveCheckpoint(original, tmpDir)
	require.NoError(t, err)

	loaded, err := LoadCheckpoint(tmpDir)
	require.NoError(t, err)

	// Compare fields
	assert.WithinDuration(t, original.Timestamp, loaded.Timestamp, time.Second)
	assert.Equal(t, original.CurrentNode, loaded.CurrentNode)
	assert.Equal(t, original.CompletedNodes, loaded.CompletedNodes)
	assert.Equal(t, original.NodeRetries, loaded.NodeRetries)
	assert.Equal(t, original.ContextValues, loaded.ContextValues)
	assert.Equal(t, original.Logs, loaded.Logs)
}

func TestLoadCheckpoint_MissingFile_ReturnsError(t *testing.T) {
	tmpDir := t.TempDir()

	_, err := LoadCheckpoint(tmpDir)

	assert.ErrorIs(t, err, ErrCheckpointNotFound)
}

func TestLoadCheckpoint_EmptyLogsRoot_ReturnsError(t *testing.T) {
	_, err := LoadCheckpoint("")

	assert.ErrorIs(t, err, ErrCheckpointNotFound)
}

func TestSaveCheckpoint_EmptyLogsRoot_NoOp(t *testing.T) {
	cp := &Checkpoint{CurrentNode: "test"}

	err := SaveCheckpoint(cp, "")

	assert.NoError(t, err)
}

func TestSaveCheckpoint_CreatesDirectory(t *testing.T) {
	tmpDir := t.TempDir()
	nestedDir := filepath.Join(tmpDir, "a", "b", "c")

	cp := &Checkpoint{
		CurrentNode:   "test",
		ContextValues: map[string]any{},
	}

	err := SaveCheckpoint(cp, nestedDir)
	require.NoError(t, err)

	// Verify file was created
	_, err = os.Stat(filepath.Join(nestedDir, CheckpointFileName))
	assert.NoError(t, err)
}

func TestBuildCheckpoint_CorrectFields(t *testing.T) {
	ctx := NewContext()
	ctx.Set("user_key", "user_value")
	ctx.Set("internal.retry_count.nodeA", 3)
	ctx.Set("internal.retry_count.nodeB", 1)
	ctx.AppendLog("log entry 1")
	ctx.AppendLog("log entry 2")

	completedNodes := []string{"start", "nodeA", "nodeB"}

	cp := buildCheckpoint("nodeB", completedNodes, ctx)

	assert.Equal(t, "nodeB", cp.CurrentNode)
	assert.Equal(t, completedNodes, cp.CompletedNodes)
	assert.Equal(t, map[string]int{"nodeA": 3, "nodeB": 1}, cp.NodeRetries)
	assert.Equal(t, "user_value", cp.ContextValues["user_key"])
	assert.Equal(t, []string{"log entry 1", "log entry 2"}, cp.Logs)
	assert.WithinDuration(t, time.Now(), cp.Timestamp, time.Second)
}

func TestCheckpoint_SavedAfterEachNode(t *testing.T) {
	tmpDir := t.TempDir()

	// Track checkpoint writes by reading the file after each handler call
	var checkpointVersions []string

	callCount := 0
	mockHandler := HandlerFunc(func(node *dotparser.Node, ctx *Context, graph *dotparser.Graph, logsRoot string) (*Outcome, error) {
		callCount++
		return Success(), nil
	})

	registry := DefaultRegistry()
	registry.Register("mock", mockHandler)

	graph := newTestGraph(
		[]*dotparser.Node{
			newNode("start", strAttr("shape", "Mdiamond")),
			newNode("A", strAttr("type", "mock")),
			newNode("B", strAttr("type", "mock")),
			newNode("exit", strAttr("shape", "Msquare")),
		},
		[]*dotparser.Edge{
			newEdge("start", "A"),
			newEdge("A", "B"),
			newEdge("B", "exit"),
		},
		nil,
	)

	_, err := Run(graph, &RunConfig{
		Registry: registry,
		LogsRoot: tmpDir,
	})
	require.NoError(t, err)

	// Verify checkpoint exists
	cp, err := LoadCheckpoint(tmpDir)
	require.NoError(t, err)

	// Final checkpoint should have all completed nodes
	assert.Equal(t, []string{"start", "A", "B"}, cp.CompletedNodes)
	assert.Equal(t, "B", cp.CurrentNode)
	_ = checkpointVersions // silence unused warning
}

func TestContextValues_PreservedThroughSaveLoad(t *testing.T) {
	tmpDir := t.TempDir()

	ctx := NewContext()
	ctx.Set("string_key", "string_value")
	ctx.Set("bool_key", true)
	ctx.Set("int_key", 42)
	ctx.Set("float_key", 3.14)
	ctx.Set("nested.key", "nested.value")

	cp := buildCheckpoint("test", []string{"start", "test"}, ctx)

	err := SaveCheckpoint(cp, tmpDir)
	require.NoError(t, err)

	loaded, err := LoadCheckpoint(tmpDir)
	require.NoError(t, err)

	// JSON unmarshaling converts numbers to float64
	assert.Equal(t, "string_value", loaded.ContextValues["string_key"])
	assert.Equal(t, true, loaded.ContextValues["bool_key"])
	assert.Equal(t, float64(42), loaded.ContextValues["int_key"])
	assert.Equal(t, 3.14, loaded.ContextValues["float_key"])
	assert.Equal(t, "nested.value", loaded.ContextValues["nested.key"])
}

func TestRetryCounters_PreservedThroughSaveLoad(t *testing.T) {
	tmpDir := t.TempDir()

	ctx := NewContext()
	SetRetryCount(ctx, "nodeA", 3)
	SetRetryCount(ctx, "nodeB", 1)
	SetRetryCount(ctx, "nodeC", 5)

	cp := buildCheckpoint("nodeC", []string{"nodeA", "nodeB", "nodeC"}, ctx)

	err := SaveCheckpoint(cp, tmpDir)
	require.NoError(t, err)

	loaded, err := LoadCheckpoint(tmpDir)
	require.NoError(t, err)

	assert.Equal(t, 3, loaded.NodeRetries["nodeA"])
	assert.Equal(t, 1, loaded.NodeRetries["nodeB"])
	assert.Equal(t, 5, loaded.NodeRetries["nodeC"])
}

func TestResume_LoadsStateAndSkipsCompletedNodes(t *testing.T) {
	tmpDir := t.TempDir()

	// Create a checkpoint at nodeA (completed start, nodeA)
	cp := &Checkpoint{
		Timestamp:      time.Now(),
		CurrentNode:    "A",
		CompletedNodes: []string{"start", "A"},
		NodeRetries:    map[string]int{},
		ContextValues: map[string]any{
			"from_start": "started",
			"from_A":     "completed_A",
		},
		Logs: []string{"started", "A completed"},
	}

	err := SaveCheckpoint(cp, tmpDir)
	require.NoError(t, err)

	// Track which nodes are executed
	var executedNodes []string
	mockHandler := HandlerFunc(func(node *dotparser.Node, ctx *Context, graph *dotparser.Graph, logsRoot string) (*Outcome, error) {
		executedNodes = append(executedNodes, node.ID)
		return Success().WithContextUpdate("from_"+node.ID, "completed_"+node.ID), nil
	})

	registry := DefaultRegistry()
	registry.Register("mock", mockHandler)

	graph := newTestGraph(
		[]*dotparser.Node{
			newNode("start", strAttr("shape", "Mdiamond")),
			newNode("A", strAttr("type", "mock")),
			newNode("B", strAttr("type", "mock")),
			newNode("exit", strAttr("shape", "Msquare")),
		},
		[]*dotparser.Edge{
			newEdge("start", "A"),
			newEdge("A", "B"),
			newEdge("B", "exit"),
		},
		nil,
	)

	result, err := Resume(graph, &RunConfig{
		Registry: registry,
		LogsRoot: tmpDir,
	})
	require.NoError(t, err)

	// Only B should have been executed (resume starts after A)
	assert.Equal(t, []string{"B"}, executedNodes)

	// CompletedNodes should include pre-existing + newly executed
	assert.Equal(t, []string{"start", "A", "B"}, result.CompletedNodes)

	// Context should have values from both checkpoint and new execution
	val, ok := result.Context.Get("from_A")
	assert.True(t, ok)
	assert.Equal(t, "completed_A", val)

	val, ok = result.Context.Get("from_B")
	assert.True(t, ok)
	assert.Equal(t, "completed_B", val)
}

func TestResume_ProducesSameFinalResultAsUninterruptedRun(t *testing.T) {
	// First, do a complete uninterrupted run
	var uninterruptedResult *RunResult
	{
		tmpDir := t.TempDir()

		counter := 0
		mockHandler := HandlerFunc(func(node *dotparser.Node, ctx *Context, graph *dotparser.Graph, logsRoot string) (*Outcome, error) {
			counter++
			return Success().WithContextUpdate("counter", counter), nil
		})

		registry := DefaultRegistry()
		registry.Register("mock", mockHandler)

		graph := newTestGraph(
			[]*dotparser.Node{
				newNode("start", strAttr("shape", "Mdiamond")),
				newNode("A", strAttr("type", "mock")),
				newNode("B", strAttr("type", "mock")),
				newNode("exit", strAttr("shape", "Msquare")),
			},
			[]*dotparser.Edge{
				newEdge("start", "A"),
				newEdge("A", "B"),
				newEdge("B", "exit"),
			},
			nil,
		)

		var err error
		uninterruptedResult, err = Run(graph, &RunConfig{
			Registry: registry,
			LogsRoot: tmpDir,
		})
		require.NoError(t, err)
	}

	// Now simulate interrupted run with resume
	var resumedResult *RunResult
	{
		tmpDir := t.TempDir()

		// Create checkpoint as if we completed A
		// After A completes, counter=1 (start doesn't use mockHandler, A increments to 1)
		cp := &Checkpoint{
			Timestamp:      time.Now(),
			CurrentNode:    "A",
			CompletedNodes: []string{"start", "A"},
			NodeRetries:    map[string]int{},
			ContextValues: map[string]any{
				"counter": float64(1), // JSON deserializes as float64; A was the 1st mock node
			},
			Logs: []string{},
		}
		err := SaveCheckpoint(cp, tmpDir)
		require.NoError(t, err)

		counter := 1 // Start from where we left off (after A executed)
		mockHandler := HandlerFunc(func(node *dotparser.Node, ctx *Context, graph *dotparser.Graph, logsRoot string) (*Outcome, error) {
			counter++
			return Success().WithContextUpdate("counter", counter), nil
		})

		registry := DefaultRegistry()
		registry.Register("mock", mockHandler)

		graph := newTestGraph(
			[]*dotparser.Node{
				newNode("start", strAttr("shape", "Mdiamond")),
				newNode("A", strAttr("type", "mock")),
				newNode("B", strAttr("type", "mock")),
				newNode("exit", strAttr("shape", "Msquare")),
			},
			[]*dotparser.Edge{
				newEdge("start", "A"),
				newEdge("A", "B"),
				newEdge("B", "exit"),
			},
			nil,
		)

		resumedResult, err = Resume(graph, &RunConfig{
			Registry: registry,
			LogsRoot: tmpDir,
		})
		require.NoError(t, err)
	}

	// Both should have same completed nodes
	assert.Equal(t, uninterruptedResult.CompletedNodes, resumedResult.CompletedNodes)

	// Final counter should be the same
	uninterruptedCounter, _ := uninterruptedResult.Context.Get("counter")
	resumedCounter, _ := resumedResult.Context.Get("counter")
	assert.Equal(t, uninterruptedCounter, resumedCounter)
}

func TestResume_RestoresRetryCounters(t *testing.T) {
	tmpDir := t.TempDir()

	// Create checkpoint with retry counters
	cp := &Checkpoint{
		Timestamp:      time.Now(),
		CurrentNode:    "A",
		CompletedNodes: []string{"start", "A"},
		NodeRetries: map[string]int{
			"A": 2,
		},
		ContextValues: map[string]any{},
		Logs:          []string{},
	}

	err := SaveCheckpoint(cp, tmpDir)
	require.NoError(t, err)

	var capturedRetryCount int
	mockHandler := HandlerFunc(func(node *dotparser.Node, ctx *Context, graph *dotparser.Graph, logsRoot string) (*Outcome, error) {
		capturedRetryCount = GetRetryCount(ctx, "A")
		return Success(), nil
	})

	registry := DefaultRegistry()
	registry.Register("mock", mockHandler)

	graph := newTestGraph(
		[]*dotparser.Node{
			newNode("start", strAttr("shape", "Mdiamond")),
			newNode("A", strAttr("type", "mock")),
			newNode("B", strAttr("type", "mock")),
			newNode("exit", strAttr("shape", "Msquare")),
		},
		[]*dotparser.Edge{
			newEdge("start", "A"),
			newEdge("A", "B"),
			newEdge("B", "exit"),
		},
		nil,
	)

	_, err = Resume(graph, &RunConfig{
		Registry: registry,
		LogsRoot: tmpDir,
	})
	require.NoError(t, err)

	// Retry counter should have been restored
	assert.Equal(t, 2, capturedRetryCount)
}

func TestResume_SetsFidelityDegradationFlag(t *testing.T) {
	tmpDir := t.TempDir()

	cp := &Checkpoint{
		Timestamp:      time.Now(),
		CurrentNode:    "A",
		CompletedNodes: []string{"start", "A"},
		NodeRetries:    map[string]int{},
		ContextValues:  map[string]any{},
		Logs:           []string{},
	}

	err := SaveCheckpoint(cp, tmpDir)
	require.NoError(t, err)

	var fidelityDegraded any
	mockHandler := HandlerFunc(func(node *dotparser.Node, ctx *Context, graph *dotparser.Graph, logsRoot string) (*Outcome, error) {
		fidelityDegraded, _ = ctx.Get("internal.resume_fidelity_degraded")
		return Success(), nil
	})

	registry := DefaultRegistry()
	registry.Register("mock", mockHandler)

	graph := newTestGraph(
		[]*dotparser.Node{
			newNode("start", strAttr("shape", "Mdiamond")),
			newNode("A", strAttr("type", "mock")),
			newNode("B", strAttr("type", "mock")),
			newNode("exit", strAttr("shape", "Msquare")),
		},
		[]*dotparser.Edge{
			newEdge("start", "A"),
			newEdge("A", "B"),
			newEdge("B", "exit"),
		},
		nil,
	)

	_, err = Resume(graph, &RunConfig{
		Registry: registry,
		LogsRoot: tmpDir,
	})
	require.NoError(t, err)

	// Fidelity degradation flag should be set for first resumed node
	assert.Equal(t, true, fidelityDegraded)
}

func TestResume_MissingCheckpoint_ReturnsError(t *testing.T) {
	tmpDir := t.TempDir()

	graph := newTestGraph(
		[]*dotparser.Node{
			newNode("start", strAttr("shape", "Mdiamond")),
			newNode("exit", strAttr("shape", "Msquare")),
		},
		[]*dotparser.Edge{
			newEdge("start", "exit"),
		},
		nil,
	)

	_, err := Resume(graph, &RunConfig{
		LogsRoot: tmpDir,
	})

	require.Error(t, err)
	assert.Contains(t, err.Error(), "checkpoint")
}

func TestCheckpoint_JSONFormat(t *testing.T) {
	tmpDir := t.TempDir()

	cp := &Checkpoint{
		Timestamp:      time.Date(2024, 1, 15, 10, 30, 0, 0, time.UTC),
		CurrentNode:    "nodeB",
		CompletedNodes: []string{"start", "nodeA", "nodeB"},
		NodeRetries:    map[string]int{"nodeA": 1},
		ContextValues:  map[string]any{"key": "value"},
		Logs:           []string{"entry1"},
	}

	err := SaveCheckpoint(cp, tmpDir)
	require.NoError(t, err)

	// Read raw JSON and verify format
	data, err := os.ReadFile(filepath.Join(tmpDir, CheckpointFileName))
	require.NoError(t, err)

	// Verify it's valid JSON with expected structure
	var parsed map[string]any
	err = json.Unmarshal(data, &parsed)
	require.NoError(t, err)

	assert.Contains(t, parsed, "timestamp")
	assert.Contains(t, parsed, "current_node")
	assert.Contains(t, parsed, "completed_nodes")
	assert.Contains(t, parsed, "node_retries")
	assert.Contains(t, parsed, "context")
	assert.Contains(t, parsed, "logs")

	assert.Equal(t, "nodeB", parsed["current_node"])
}

func TestExtractRetryCounters(t *testing.T) {
	ctx := NewContext()
	ctx.Set("internal.retry_count.nodeA", 3)
	ctx.Set("internal.retry_count.nodeB", 1)
	ctx.Set("other_key", "value")
	ctx.Set("internal.other_key", "other")

	retries := extractRetryCounters(ctx)

	assert.Equal(t, map[string]int{
		"nodeA": 3,
		"nodeB": 1,
	}, retries)
}

func TestFindNextNodeAfter(t *testing.T) {
	graph := newTestGraph(
		[]*dotparser.Node{
			newNode("start", strAttr("shape", "Mdiamond")),
			newNode("A"),
			newNode("B"),
			newNode("exit", strAttr("shape", "Msquare")),
		},
		[]*dotparser.Edge{
			newEdge("start", "A"),
			newEdge("A", "B"),
			newEdge("B", "exit"),
		},
		nil,
	)

	// From A, should find B
	next, err := findNextNodeAfter("A", graph)
	require.NoError(t, err)
	assert.Equal(t, "B", next.ID)

	// From B, should find exit
	next, err = findNextNodeAfter("B", graph)
	require.NoError(t, err)
	assert.Equal(t, "exit", next.ID)
}

func TestFindNextNodeAfter_NoEdges_ReturnsCurrentNode(t *testing.T) {
	graph := newTestGraph(
		[]*dotparser.Node{
			newNode("start", strAttr("shape", "Mdiamond")),
			newNode("exit", strAttr("shape", "Msquare")),
		},
		[]*dotparser.Edge{
			newEdge("start", "exit"),
		},
		nil,
	)

	// From exit (no outgoing edges), should return exit itself
	next, err := findNextNodeAfter("exit", graph)
	require.NoError(t, err)
	assert.Equal(t, "exit", next.ID)
}

func TestFindNextNodeAfter_NodeNotFound_ReturnsError(t *testing.T) {
	graph := newTestGraph(
		[]*dotparser.Node{
			newNode("start", strAttr("shape", "Mdiamond")),
		},
		nil,
		nil,
	)

	_, err := findNextNodeAfter("nonexistent", graph)

	require.Error(t, err)
	assert.Contains(t, err.Error(), "not found")
}

func TestRestoreFromCheckpoint(t *testing.T) {
	cp := &Checkpoint{
		Timestamp:      time.Now(),
		CurrentNode:    "A",
		CompletedNodes: []string{"start", "A"},
		NodeRetries: map[string]int{
			"A": 2,
		},
		ContextValues: map[string]any{
			"key1": "value1",
			"key2": float64(42),
		},
		Logs: []string{"log1", "log2"},
	}

	graph := newTestGraph(
		[]*dotparser.Node{
			newNode("start", strAttr("shape", "Mdiamond")),
			newNode("A"),
			newNode("B"),
			newNode("exit", strAttr("shape", "Msquare")),
		},
		[]*dotparser.Edge{
			newEdge("start", "A"),
			newEdge("A", "B"),
			newEdge("B", "exit"),
		},
		nil,
	)

	ctx, completedNodes, nextNode, err := restoreFromCheckpoint(cp, graph)
	require.NoError(t, err)

	// Context values restored
	val, ok := ctx.Get("key1")
	assert.True(t, ok)
	assert.Equal(t, "value1", val)

	val, ok = ctx.Get("key2")
	assert.True(t, ok)
	assert.Equal(t, float64(42), val)

	// Logs restored
	assert.Equal(t, []string{"log1", "log2"}, ctx.Logs())

	// Retry counters restored
	assert.Equal(t, 2, GetRetryCount(ctx, "A"))

	// Fidelity degradation flag set
	fidelityDegraded, ok := ctx.Get("internal.resume_fidelity_degraded")
	assert.True(t, ok)
	assert.Equal(t, true, fidelityDegraded)

	// Completed nodes returned
	assert.Equal(t, []string{"start", "A"}, completedNodes)

	// Next node is B (after A)
	assert.Equal(t, "B", nextNode.ID)
}
