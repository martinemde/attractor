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
