package pipeline

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/martinemde/attractor/dotparser"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// MockHandler is a test handler that records calls and returns configured outcomes.
type MockHandler struct {
	Calls    []*dotparser.Node
	Outcomes []*Outcome
	index    int
}

func (h *MockHandler) Execute(node *dotparser.Node, ctx *Context, graph *dotparser.Graph, logsRoot string) (*Outcome, error) {
	h.Calls = append(h.Calls, node)
	if h.index < len(h.Outcomes) {
		outcome := h.Outcomes[h.index]
		h.index++
		return outcome, nil
	}
	return Success(), nil
}

// newTestGraph creates a simple graph for testing.
func newTestGraph(nodes []*dotparser.Node, edges []*dotparser.Edge, graphAttrs []dotparser.Attr) *dotparser.Graph {
	return &dotparser.Graph{
		Name:       "TestGraph",
		Nodes:      nodes,
		Edges:      edges,
		GraphAttrs: graphAttrs,
	}
}

func newNode(id string, attrs ...dotparser.Attr) *dotparser.Node {
	return &dotparser.Node{ID: id, Attrs: attrs}
}

func newEdge(from, to string, attrs ...dotparser.Attr) *dotparser.Edge {
	return &dotparser.Edge{From: from, To: to, Attrs: attrs}
}

func strAttr(key, value string) dotparser.Attr {
	return dotparser.Attr{Key: key, Value: dotparser.Value{Kind: dotparser.ValueString, Str: value, Raw: value}}
}

func intAttr(key string, value int64) dotparser.Attr {
	return dotparser.Attr{Key: key, Value: dotparser.Value{Kind: dotparser.ValueInt, Int: value, Raw: ""}}
}

func boolAttr(key string, value bool) dotparser.Attr {
	return dotparser.Attr{Key: key, Value: dotparser.Value{Kind: dotparser.ValueBool, Bool: value, Raw: ""}}
}

func TestRun_MinimalStartExit(t *testing.T) {
	// Simple start -> exit pipeline
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

	result, err := Run(graph, nil)

	require.NoError(t, err)
	assert.Equal(t, []string{"start"}, result.CompletedNodes)
	assert.NotNil(t, result.FinalOutcome)
	assert.Equal(t, StatusSuccess, result.FinalOutcome.Status)
}

func TestRun_ThreeNodeLinearPipeline(t *testing.T) {
	// start -> A -> exit with mock handler for A
	mockHandler := &MockHandler{
		Outcomes: []*Outcome{
			Success().WithNotes("A completed").WithContextUpdate("from_a", "value"),
		},
	}

	registry := DefaultRegistry()
	registry.Register("mock", mockHandler)

	graph := newTestGraph(
		[]*dotparser.Node{
			newNode("start", strAttr("shape", "Mdiamond")),
			newNode("A", strAttr("type", "mock"), strAttr("label", "Node A")),
			newNode("exit", strAttr("shape", "Msquare")),
		},
		[]*dotparser.Edge{
			newEdge("start", "A"),
			newEdge("A", "exit"),
		},
		nil,
	)

	result, err := Run(graph, &RunConfig{Registry: registry})

	require.NoError(t, err)
	assert.Equal(t, []string{"start", "A"}, result.CompletedNodes)
	require.Len(t, mockHandler.Calls, 1)
	assert.Equal(t, "A", mockHandler.Calls[0].ID)

	// Verify context update was applied
	val, ok := result.Context.Get("from_a")
	assert.True(t, ok)
	assert.Equal(t, "value", val)
}

func TestRun_ContextUpdateVisibleToNextNode(t *testing.T) {
	// Test that context updates from one node are visible to the next
	var capturedContext *Context

	firstHandler := &MockHandler{
		Outcomes: []*Outcome{
			Success().WithContextUpdate("shared_key", "from_first"),
		},
	}

	// Second handler that captures context
	type ContextCapturingHandler struct {
		captured map[string]any
	}
	secondHandler := &ContextCapturingHandler{}

	registry := DefaultRegistry()
	registry.Register("first", firstHandler)
	registry.Register("second", HandlerFunc(func(node *dotparser.Node, ctx *Context, graph *dotparser.Graph, logsRoot string) (*Outcome, error) {
		capturedContext = ctx.Clone()
		return Success(), nil
	}))

	graph := newTestGraph(
		[]*dotparser.Node{
			newNode("start", strAttr("shape", "Mdiamond")),
			newNode("A", strAttr("type", "first")),
			newNode("B", strAttr("type", "second")),
			newNode("exit", strAttr("shape", "Msquare")),
		},
		[]*dotparser.Edge{
			newEdge("start", "A"),
			newEdge("A", "B"),
			newEdge("B", "exit"),
		},
		nil,
	)

	result, err := Run(graph, &RunConfig{Registry: registry})

	require.NoError(t, err)
	assert.Equal(t, []string{"start", "A", "B"}, result.CompletedNodes)

	// Verify the second handler saw the context update from the first
	val, ok := capturedContext.Get("shared_key")
	assert.True(t, ok)
	assert.Equal(t, "from_first", val)

	// Also verify outcome was set in context
	outcome, ok := capturedContext.Get("outcome")
	assert.True(t, ok)
	assert.Equal(t, "success", outcome)
	_ = secondHandler // silence unused warning
}

func TestRun_GraphAttributesMirroredToContext(t *testing.T) {
	graph := newTestGraph(
		[]*dotparser.Node{
			newNode("start", strAttr("shape", "Mdiamond")),
			newNode("exit", strAttr("shape", "Msquare")),
		},
		[]*dotparser.Edge{
			newEdge("start", "exit"),
		},
		[]dotparser.Attr{
			strAttr("goal", "Test the system"),
			strAttr("label", "Test Pipeline"),
		},
	)

	result, err := Run(graph, nil)

	require.NoError(t, err)

	goal, ok := result.Context.Get("graph.goal")
	assert.True(t, ok)
	assert.Equal(t, "Test the system", goal)

	label, ok := result.Context.Get("graph.label")
	assert.True(t, ok)
	assert.Equal(t, "Test Pipeline", label)
}

func TestRun_WritesStatusJSON(t *testing.T) {
	tmpDir := t.TempDir()

	mockHandler := &MockHandler{
		Outcomes: []*Outcome{
			Success().WithNotes("Test notes").WithContextUpdate("key", "value"),
		},
	}

	registry := DefaultRegistry()
	registry.Register("mock", mockHandler)

	graph := newTestGraph(
		[]*dotparser.Node{
			newNode("start", strAttr("shape", "Mdiamond")),
			newNode("task", strAttr("type", "mock")),
			newNode("exit", strAttr("shape", "Msquare")),
		},
		[]*dotparser.Edge{
			newEdge("start", "task"),
			newEdge("task", "exit"),
		},
		nil,
	)

	_, err := Run(graph, &RunConfig{
		Registry: registry,
		LogsRoot: tmpDir,
	})

	require.NoError(t, err)

	// Check status files were written
	startStatus := filepath.Join(tmpDir, "start", "status.json")
	taskStatus := filepath.Join(tmpDir, "task", "status.json")

	_, err = os.Stat(startStatus)
	assert.NoError(t, err, "start status.json should exist")

	_, err = os.Stat(taskStatus)
	assert.NoError(t, err, "task status.json should exist")

	// Verify content of task status
	data, err := os.ReadFile(taskStatus)
	require.NoError(t, err)
	assert.Contains(t, string(data), `"status": "success"`)
	assert.Contains(t, string(data), `"notes": "Test notes"`)
}

func TestRun_NoStartNodeError(t *testing.T) {
	graph := newTestGraph(
		[]*dotparser.Node{
			newNode("A"),
			newNode("exit", strAttr("shape", "Msquare")),
		},
		[]*dotparser.Edge{
			newEdge("A", "exit"),
		},
		nil,
	)

	_, err := Run(graph, nil)

	require.Error(t, err)
	assert.Contains(t, err.Error(), "no start node found")
}

func TestRun_FindsStartByID(t *testing.T) {
	// When no Mdiamond shape, falls back to ID-based lookup for finding start node.
	// The node still needs a handler resolvable type/shape to execute.
	graph := newTestGraph(
		[]*dotparser.Node{
			newNode("start", strAttr("type", "start")), // No Mdiamond shape, but type=start
			newNode("exit", strAttr("shape", "Msquare")),
		},
		[]*dotparser.Edge{
			newEdge("start", "exit"),
		},
		nil,
	)

	result, err := Run(graph, nil)

	require.NoError(t, err)
	assert.Equal(t, []string{"start"}, result.CompletedNodes)
}

func TestRun_FindsStartByIDCapitalized(t *testing.T) {
	// Capitalized "Start" ID also works
	graph := newTestGraph(
		[]*dotparser.Node{
			newNode("Start", strAttr("type", "start")),
			newNode("exit", strAttr("shape", "Msquare")),
		},
		[]*dotparser.Edge{
			newEdge("Start", "exit"),
		},
		nil,
	)

	result, err := Run(graph, nil)

	require.NoError(t, err)
	assert.Equal(t, []string{"Start"}, result.CompletedNodes)
}

func TestRun_FailedNodeWithNoEdges(t *testing.T) {
	// When a node fails and has NO outgoing edges, it should error
	mockHandler := &MockHandler{
		Outcomes: []*Outcome{
			Fail("something went wrong"),
		},
	}

	registry := DefaultRegistry()
	registry.Register("mock", mockHandler)

	graph := newTestGraph(
		[]*dotparser.Node{
			newNode("start", strAttr("shape", "Mdiamond")),
			newNode("A", strAttr("type", "mock")),
			newNode("exit", strAttr("shape", "Msquare")),
		},
		[]*dotparser.Edge{
			newEdge("start", "A"),
			// No edge from A - this should cause an error when A fails
		},
		nil,
	)

	_, err := Run(graph, &RunConfig{Registry: registry})

	require.Error(t, err)
	assert.Contains(t, err.Error(), "failed with no outgoing fail edge")
}

func TestRun_FailedNodeWithFailEdgeContinues(t *testing.T) {
	// When a node fails and has a fail edge (condition="outcome=fail"), follow it
	mockHandler := &MockHandler{
		Outcomes: []*Outcome{
			Fail("something went wrong"),
		},
	}

	registry := DefaultRegistry()
	registry.Register("mock", mockHandler)

	graph := newTestGraph(
		[]*dotparser.Node{
			newNode("start", strAttr("shape", "Mdiamond")),
			newNode("A", strAttr("type", "mock")),
			newNode("exit", strAttr("shape", "Msquare")),
		},
		[]*dotparser.Edge{
			newEdge("start", "A"),
			newEdge("A", "exit", strAttr("condition", "outcome=fail")), // Fail edge
		},
		nil,
	)

	result, err := Run(graph, &RunConfig{Registry: registry})

	// No error - the fail edge is followed
	require.NoError(t, err)
	assert.Equal(t, []string{"start", "A"}, result.CompletedNodes)
	assert.Equal(t, StatusFail, result.FinalOutcome.Status)
}

func TestRun_FailedNodeWithNoFailEdgeNoRetryTargetErrors(t *testing.T) {
	// When a node fails with no fail edge and no retry_target, pipeline errors
	mockHandler := &MockHandler{
		Outcomes: []*Outcome{
			Fail("something went wrong"),
		},
	}

	registry := DefaultRegistry()
	registry.Register("mock", mockHandler)

	graph := newTestGraph(
		[]*dotparser.Node{
			newNode("start", strAttr("shape", "Mdiamond")),
			newNode("A", strAttr("type", "mock")),
			newNode("exit", strAttr("shape", "Msquare")),
		},
		[]*dotparser.Edge{
			newEdge("start", "A"),
			newEdge("A", "exit"), // Unconditional edge - NOT followed on FAIL per Section 3.7
		},
		nil,
	)

	_, err := Run(graph, &RunConfig{Registry: registry})

	// Error because no fail edge and no retry_target
	require.Error(t, err)
	assert.Contains(t, err.Error(), "failed with no outgoing fail edge")
}

// HandlerFunc is an adapter to use a function as a Handler
type HandlerFunc func(node *dotparser.Node, ctx *Context, graph *dotparser.Graph, logsRoot string) (*Outcome, error)

func (f HandlerFunc) Execute(node *dotparser.Node, ctx *Context, graph *dotparser.Graph, logsRoot string) (*Outcome, error) {
	return f(node, ctx, graph, logsRoot)
}

func TestRun_CurrentNodeSetInContext(t *testing.T) {
	var capturedNodes []string

	registry := DefaultRegistry()
	registry.Register("capture", HandlerFunc(func(node *dotparser.Node, ctx *Context, graph *dotparser.Graph, logsRoot string) (*Outcome, error) {
		if val, ok := ctx.Get("current_node"); ok {
			capturedNodes = append(capturedNodes, val.(string))
		}
		return Success(), nil
	}))

	graph := newTestGraph(
		[]*dotparser.Node{
			newNode("start", strAttr("shape", "Mdiamond")),
			newNode("A", strAttr("type", "capture")),
			newNode("B", strAttr("type", "capture")),
			newNode("exit", strAttr("shape", "Msquare")),
		},
		[]*dotparser.Edge{
			newEdge("start", "A"),
			newEdge("A", "B"),
			newEdge("B", "exit"),
		},
		nil,
	)

	result, err := Run(graph, &RunConfig{Registry: registry})

	require.NoError(t, err)
	assert.Equal(t, []string{"A", "B"}, capturedNodes)

	finalNode, ok := result.Context.Get("current_node")
	assert.True(t, ok)
	assert.Equal(t, "exit", finalNode)
}

// ---------- loop_restart edge attribute tests (Spec Section 2.7) ----------

func TestRun_LoopRestartEdge(t *testing.T) {
	// When an edge has loop_restart=true, the pipeline should return
	// with LoopRestart=true and the target node ID
	mockHandler := &MockHandler{
		Outcomes: []*Outcome{Success()},
	}

	registry := DefaultRegistry()
	registry.Register("mock", mockHandler)

	graph := newTestGraph(
		[]*dotparser.Node{
			newNode("start", strAttr("shape", "Mdiamond")),
			newNode("work", strAttr("type", "mock")),
			newNode("restart_target", strAttr("type", "mock")),
			newNode("exit", strAttr("shape", "Msquare")),
		},
		[]*dotparser.Edge{
			newEdge("start", "work"),
			newEdge("work", "restart_target", boolAttr("loop_restart", true)),
			newEdge("restart_target", "exit"),
		},
		nil,
	)

	result, err := Run(graph, &RunConfig{Registry: registry})

	require.NoError(t, err)
	assert.True(t, result.LoopRestart)
	assert.Equal(t, "restart_target", result.LoopRestartTarget)
	assert.Equal(t, []string{"start", "work"}, result.CompletedNodes)
}

func TestRun_LoopRestartFalseDoesNotRestart(t *testing.T) {
	// When loop_restart=false (explicit), normal execution continues
	mockHandler := &MockHandler{
		Outcomes: []*Outcome{Success(), Success()},
	}

	registry := DefaultRegistry()
	registry.Register("mock", mockHandler)

	graph := newTestGraph(
		[]*dotparser.Node{
			newNode("start", strAttr("shape", "Mdiamond")),
			newNode("work", strAttr("type", "mock")),
			newNode("next", strAttr("type", "mock")),
			newNode("exit", strAttr("shape", "Msquare")),
		},
		[]*dotparser.Edge{
			newEdge("start", "work"),
			newEdge("work", "next", boolAttr("loop_restart", false)),
			newEdge("next", "exit"),
		},
		nil,
	)

	result, err := Run(graph, &RunConfig{Registry: registry})

	require.NoError(t, err)
	assert.False(t, result.LoopRestart)
	assert.Equal(t, []string{"start", "work", "next"}, result.CompletedNodes)
}

func TestRun_LoopRestartPreservesNodeOutcomes(t *testing.T) {
	// Verify that NodeOutcomes are captured even when loop restart occurs
	mockHandler := &MockHandler{
		Outcomes: []*Outcome{
			Success().WithNotes("work completed"),
		},
	}

	registry := DefaultRegistry()
	registry.Register("mock", mockHandler)

	graph := newTestGraph(
		[]*dotparser.Node{
			newNode("start", strAttr("shape", "Mdiamond")),
			newNode("work", strAttr("type", "mock")),
			newNode("restart_target", strAttr("type", "mock")),
			newNode("exit", strAttr("shape", "Msquare")),
		},
		[]*dotparser.Edge{
			newEdge("start", "work"),
			newEdge("work", "restart_target", boolAttr("loop_restart", true)),
			newEdge("restart_target", "exit"),
		},
		nil,
	)

	result, err := Run(graph, &RunConfig{Registry: registry})

	require.NoError(t, err)
	assert.True(t, result.LoopRestart)

	// Verify outcomes were recorded
	workOutcome, ok := result.NodeOutcomes["work"]
	require.True(t, ok)
	assert.Equal(t, StatusSuccess, workOutcome.Status)
	assert.Equal(t, "work completed", workOutcome.Notes)
}

func TestRun_LoopRestartWithCondition(t *testing.T) {
	// loop_restart can be combined with conditions on the edge
	// Only restart when the condition is met
	mockHandler := &MockHandler{
		Outcomes: []*Outcome{
			Success().WithContextUpdate("should_restart", "yes"),
		},
	}

	registry := DefaultRegistry()
	registry.Register("mock", mockHandler)

	graph := newTestGraph(
		[]*dotparser.Node{
			newNode("start", strAttr("shape", "Mdiamond")),
			newNode("work", strAttr("type", "mock")),
			newNode("restart_target", strAttr("type", "mock")),
			newNode("continue", strAttr("type", "mock")),
			newNode("exit", strAttr("shape", "Msquare")),
		},
		[]*dotparser.Edge{
			newEdge("start", "work"),
			// Restart edge with condition
			{From: "work", To: "restart_target", Attrs: []dotparser.Attr{
				strAttr("condition", "should_restart=yes"),
				boolAttr("loop_restart", true),
			}},
			// Normal continue edge (lower priority without condition)
			newEdge("work", "continue"),
			newEdge("continue", "exit"),
			newEdge("restart_target", "exit"),
		},
		nil,
	)

	result, err := Run(graph, &RunConfig{Registry: registry})

	require.NoError(t, err)
	assert.True(t, result.LoopRestart)
	assert.Equal(t, "restart_target", result.LoopRestartTarget)
}

func TestRun_LoopRestartNoAttributeDoesNotRestart(t *testing.T) {
	// Without loop_restart attribute, normal execution continues
	mockHandler := &MockHandler{
		Outcomes: []*Outcome{Success(), Success()},
	}

	registry := DefaultRegistry()
	registry.Register("mock", mockHandler)

	graph := newTestGraph(
		[]*dotparser.Node{
			newNode("start", strAttr("shape", "Mdiamond")),
			newNode("work", strAttr("type", "mock")),
			newNode("next", strAttr("type", "mock")),
			newNode("exit", strAttr("shape", "Msquare")),
		},
		[]*dotparser.Edge{
			newEdge("start", "work"),
			newEdge("work", "next"), // No loop_restart attribute
			newEdge("next", "exit"),
		},
		nil,
	)

	result, err := Run(graph, &RunConfig{Registry: registry})

	require.NoError(t, err)
	assert.False(t, result.LoopRestart)
	assert.Empty(t, result.LoopRestartTarget)
	assert.Equal(t, []string{"start", "work", "next"}, result.CompletedNodes)
}

// ---------- StartAt tests ----------

func TestRun_StartAtOverridesDefaultStart(t *testing.T) {
	// StartAt skips the normal start node and begins at the specified node
	mockHandler := &MockHandler{
		Outcomes: []*Outcome{Success()},
	}

	registry := DefaultRegistry()
	registry.Register("mock", mockHandler)

	graph := newTestGraph(
		[]*dotparser.Node{
			newNode("start", strAttr("shape", "Mdiamond")),
			newNode("middle", strAttr("type", "mock")),
			newNode("exit", strAttr("shape", "Msquare")),
		},
		[]*dotparser.Edge{
			newEdge("start", "middle"),
			newEdge("middle", "exit"),
		},
		nil,
	)

	result, err := Run(graph, &RunConfig{
		Registry: registry,
		StartAt:  "middle",
	})

	require.NoError(t, err)
	// Should skip "start" and begin at "middle"
	assert.Equal(t, []string{"middle"}, result.CompletedNodes)
}

func TestRun_StartAtInvalidNodeErrors(t *testing.T) {
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

	_, err := Run(graph, &RunConfig{StartAt: "nonexistent"})

	require.Error(t, err)
	assert.Contains(t, err.Error(), "start-at node")
	assert.Contains(t, err.Error(), "nonexistent")
}

// ---------- RunLoop tests (Spec Section 2.7 caller-side restart) ----------

func TestRunLoop_NoRestart(t *testing.T) {
	// RunLoop with a normal pipeline (no loop_restart edges) runs once
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

	result, err := RunLoop(graph, nil)

	require.NoError(t, err)
	assert.False(t, result.LoopRestart)
	assert.Equal(t, []string{"start"}, result.CompletedNodes)
}

func TestRunLoop_SingleRestart(t *testing.T) {
	// Pipeline restarts once via loop_restart edge, then completes normally
	// on the second run because the handler returns a different outcome.
	callCount := 0
	registry := DefaultRegistry()
	registry.Register("work", HandlerFunc(func(node *dotparser.Node, ctx *Context, graph *dotparser.Graph, logsRoot string) (*Outcome, error) {
		callCount++
		if callCount == 1 {
			return Success().WithContextUpdate("iteration", "1"), nil
		}
		// Second call: set context so condition routes to exit instead of restart
		return Success().WithPreferredLabel("done"), nil
	}))

	graph := newTestGraph(
		[]*dotparser.Node{
			newNode("start", strAttr("shape", "Mdiamond")),
			newNode("work", strAttr("type", "work")),
			newNode("exit", strAttr("shape", "Msquare")),
		},
		[]*dotparser.Edge{
			newEdge("start", "work"),
			// First edge: restart when preferred_label is not "done"
			{From: "work", To: "start", Attrs: []dotparser.Attr{
				boolAttr("loop_restart", true),
				strAttr("condition", "preferred_label!=done"),
			}},
			// Second edge: continue to exit when done
			{From: "work", To: "exit", Attrs: []dotparser.Attr{
				strAttr("condition", "preferred_label=done"),
			}},
		},
		nil,
	)

	result, err := RunLoop(graph, &RunConfig{Registry: registry})

	require.NoError(t, err)
	assert.False(t, result.LoopRestart)
	assert.Equal(t, 2, callCount)
}

func TestRunLoop_FreshLogDirectory(t *testing.T) {
	// Verify that each restart iteration gets a fresh log directory
	tmpDir := t.TempDir()
	var logDirs []string

	registry := DefaultRegistry()
	callCount := 0
	registry.Register("work", HandlerFunc(func(node *dotparser.Node, ctx *Context, graph *dotparser.Graph, logsRoot string) (*Outcome, error) {
		callCount++
		logDirs = append(logDirs, logsRoot)
		if callCount <= 2 {
			return Success(), nil
		}
		return Success().WithPreferredLabel("done"), nil
	}))

	graph := newTestGraph(
		[]*dotparser.Node{
			newNode("start", strAttr("shape", "Mdiamond")),
			newNode("work", strAttr("type", "work")),
			newNode("exit", strAttr("shape", "Msquare")),
		},
		[]*dotparser.Edge{
			newEdge("start", "work"),
			{From: "work", To: "start", Attrs: []dotparser.Attr{
				boolAttr("loop_restart", true),
				strAttr("condition", "preferred_label!=done"),
			}},
			{From: "work", To: "exit", Attrs: []dotparser.Attr{
				strAttr("condition", "preferred_label=done"),
			}},
		},
		nil,
	)

	result, err := RunLoop(graph, &RunConfig{
		Registry: registry,
		LogsRoot: tmpDir,
	})

	require.NoError(t, err)
	assert.False(t, result.LoopRestart)
	assert.Equal(t, 3, callCount)

	// Each restart should use a different log directory
	assert.NotEqual(t, logDirs[0], logDirs[1], "restart iterations should use different log dirs")
}

func TestRunLoop_MaxRestartsExceeded(t *testing.T) {
	// Always restart - should hit the max limit
	registry := DefaultRegistry()
	registry.Register("mock", &MockHandler{})

	graph := newTestGraph(
		[]*dotparser.Node{
			newNode("start", strAttr("shape", "Mdiamond")),
			newNode("work", strAttr("type", "mock")),
			newNode("exit", strAttr("shape", "Msquare")),
		},
		[]*dotparser.Edge{
			newEdge("start", "work"),
			newEdge("work", "start", boolAttr("loop_restart", true)),
		},
		nil,
	)

	_, err := RunLoop(graph, &RunConfig{
		Registry:        registry,
		MaxLoopRestarts: 3,
	})

	require.Error(t, err)
	assert.Contains(t, err.Error(), "max loop restarts")
}
