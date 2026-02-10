package pipeline

import (
	"errors"
	"os"
	"path/filepath"
	"testing"

	"github.com/martinemde/attractor/dotparser"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func newTestEngine(defaultHandler Handler) *Engine {
	reg := NewDefaultRegistry(defaultHandler)
	e := NewEngine(reg)
	e.Sleeper = &noopSleeper{}
	return e
}

func TestEngine_SimpleLinearPipeline(t *testing.T) {
	handler := newRecordingHandler(
		&Outcome{Status: StatusSuccess, Notes: "step_a done"},
		&Outcome{Status: StatusSuccess, Notes: "step_b done"},
	)
	engine := newTestEngine(handler)

	graph := linearGraph("step_a", "step_b")
	outcome, err := engine.Run(graph, RunConfig{})

	require.NoError(t, err)
	require.NotNil(t, outcome)
	assert.Equal(t, StatusSuccess, outcome.Status)

	// The handler should have been called for step_a and step_b (not start/exit)
	assert.Len(t, handler.calls, 2)
	assert.Equal(t, "step_a", handler.calls[0].ID)
	assert.Equal(t, "step_b", handler.calls[1].ID)
}

func TestEngine_StartToExitDirectly(t *testing.T) {
	handler := newRecordingHandler()
	engine := newTestEngine(handler)

	graph := linearGraph() // start -> exit, no intermediate nodes
	outcome, err := engine.Run(graph, RunConfig{})

	require.NoError(t, err)
	require.NotNil(t, outcome)
	// No handler calls for intermediate nodes since there are none
	assert.Len(t, handler.calls, 0)
}

func TestEngine_NoStartNode(t *testing.T) {
	engine := newTestEngine(nil)
	graph := &dotparser.Graph{
		Name:  "test",
		Nodes: []*dotparser.Node{makeNode("some_node")},
	}
	_, err := engine.Run(graph, RunConfig{})
	assert.Error(t, err)
	assert.Contains(t, err.Error(), "no start node")
}

func TestEngine_ContextUpdatesFromOutcome(t *testing.T) {
	handler := newRecordingHandler(
		&Outcome{
			Status:         StatusSuccess,
			ContextUpdates: map[string]string{"result": "42"},
		},
	)
	engine := newTestEngine(handler)
	graph := linearGraph("step_a")

	outcome, err := engine.Run(graph, RunConfig{})
	require.NoError(t, err)
	assert.Equal(t, StatusSuccess, outcome.Status)
}

func TestEngine_GraphGoalMirrored(t *testing.T) {
	// Build a graph with a goal attribute and a handler that reads it from context
	var capturedCtx *Context
	handler := HandlerFunc(func(node *dotparser.Node, ctx *Context, graph *dotparser.Graph, logsRoot string) (*Outcome, error) {
		capturedCtx = ctx
		return &Outcome{Status: StatusSuccess}, nil
	})
	engine := newTestEngine(handler)

	graph := linearGraph("step_a")
	graph.GraphAttrs = []dotparser.Attr{makeAttr("goal", "Run all tests")}

	_, err := engine.Run(graph, RunConfig{})
	require.NoError(t, err)
	assert.Equal(t, "Run all tests", capturedCtx.GetString("graph.goal"))
}

func TestEngine_EdgeConditionRouting(t *testing.T) {
	// Graph: start -> check -> (condition: outcome=success) -> pass -> exit
	//                         (condition: outcome=fail) -> fail_node -> exit
	var visited []string
	handler := HandlerFunc(func(node *dotparser.Node, ctx *Context, graph *dotparser.Graph, logsRoot string) (*Outcome, error) {
		visited = append(visited, node.ID)
		if node.ID == "check" {
			return &Outcome{Status: StatusSuccess}, nil
		}
		return &Outcome{Status: StatusSuccess}, nil
	})
	engine := newTestEngine(handler)

	graph := &dotparser.Graph{
		Name: "test",
		Nodes: []*dotparser.Node{
			makeNode("start", makeAttr("shape", "Mdiamond")),
			makeNode("check", makeAttr("shape", "box")),
			makeNode("pass", makeAttr("shape", "box")),
			makeNode("fail_node", makeAttr("shape", "box")),
			makeNode("exit", makeAttr("shape", "Msquare")),
		},
		Edges: []*dotparser.Edge{
			makeEdge("start", "check"),
			makeEdge("check", "pass", makeAttr("condition", "outcome=success")),
			makeEdge("check", "fail_node", makeAttr("condition", "outcome=fail")),
			makeEdge("pass", "exit"),
			makeEdge("fail_node", "exit"),
		},
	}

	outcome, err := engine.Run(graph, RunConfig{})
	require.NoError(t, err)
	assert.Equal(t, StatusSuccess, outcome.Status)
	assert.Equal(t, []string{"check", "pass"}, visited)
}

func TestEngine_PreferredLabelRouting(t *testing.T) {
	var visited []string
	handler := HandlerFunc(func(node *dotparser.Node, ctx *Context, graph *dotparser.Graph, logsRoot string) (*Outcome, error) {
		visited = append(visited, node.ID)
		if node.ID == "gate" {
			return &Outcome{Status: StatusSuccess, PreferredLabel: "approve"}, nil
		}
		return &Outcome{Status: StatusSuccess}, nil
	})
	engine := newTestEngine(handler)

	graph := &dotparser.Graph{
		Name: "test",
		Nodes: []*dotparser.Node{
			makeNode("start", makeAttr("shape", "Mdiamond")),
			makeNode("gate", makeAttr("shape", "box")),
			makeNode("approved", makeAttr("shape", "box")),
			makeNode("rejected", makeAttr("shape", "box")),
			makeNode("exit", makeAttr("shape", "Msquare")),
		},
		Edges: []*dotparser.Edge{
			makeEdge("start", "gate"),
			makeEdge("gate", "approved", makeAttr("label", "[A] Approve")),
			makeEdge("gate", "rejected", makeAttr("label", "[R] Reject")),
			makeEdge("approved", "exit"),
			makeEdge("rejected", "exit"),
		},
	}

	outcome, err := engine.Run(graph, RunConfig{})
	require.NoError(t, err)
	assert.Equal(t, StatusSuccess, outcome.Status)
	assert.Equal(t, []string{"gate", "approved"}, visited)
}

func TestEngine_SuggestedNextIDRouting(t *testing.T) {
	var visited []string
	handler := HandlerFunc(func(node *dotparser.Node, ctx *Context, graph *dotparser.Graph, logsRoot string) (*Outcome, error) {
		visited = append(visited, node.ID)
		if node.ID == "router" {
			return &Outcome{Status: StatusSuccess, SuggestedNextIDs: []string{"path_b"}}, nil
		}
		return &Outcome{Status: StatusSuccess}, nil
	})
	engine := newTestEngine(handler)

	graph := &dotparser.Graph{
		Name: "test",
		Nodes: []*dotparser.Node{
			makeNode("start", makeAttr("shape", "Mdiamond")),
			makeNode("router", makeAttr("shape", "box")),
			makeNode("path_a", makeAttr("shape", "box")),
			makeNode("path_b", makeAttr("shape", "box")),
			makeNode("exit", makeAttr("shape", "Msquare")),
		},
		Edges: []*dotparser.Edge{
			makeEdge("start", "router"),
			makeEdge("router", "path_a"),
			makeEdge("router", "path_b"),
			makeEdge("path_a", "exit"),
			makeEdge("path_b", "exit"),
		},
	}

	outcome, err := engine.Run(graph, RunConfig{})
	require.NoError(t, err)
	assert.Equal(t, StatusSuccess, outcome.Status)
	assert.Equal(t, []string{"router", "path_b"}, visited)
}

func TestEngine_WeightBasedRouting(t *testing.T) {
	var visited []string
	handler := HandlerFunc(func(node *dotparser.Node, ctx *Context, graph *dotparser.Graph, logsRoot string) (*Outcome, error) {
		visited = append(visited, node.ID)
		return &Outcome{Status: StatusSuccess}, nil
	})
	engine := newTestEngine(handler)

	graph := &dotparser.Graph{
		Name: "test",
		Nodes: []*dotparser.Node{
			makeNode("start", makeAttr("shape", "Mdiamond")),
			makeNode("fork", makeAttr("shape", "box")),
			makeNode("low_priority", makeAttr("shape", "box")),
			makeNode("high_priority", makeAttr("shape", "box")),
			makeNode("exit", makeAttr("shape", "Msquare")),
		},
		Edges: []*dotparser.Edge{
			makeEdge("start", "fork"),
			makeEdge("fork", "low_priority", makeIntAttr("weight", 1)),
			makeEdge("fork", "high_priority", makeIntAttr("weight", 10)),
			makeEdge("low_priority", "exit"),
			makeEdge("high_priority", "exit"),
		},
	}

	outcome, err := engine.Run(graph, RunConfig{})
	require.NoError(t, err)
	assert.Equal(t, StatusSuccess, outcome.Status)
	assert.Equal(t, []string{"fork", "high_priority"}, visited)
}

func TestEngine_GoalGateEnforcement(t *testing.T) {
	callCount := 0
	handler := HandlerFunc(func(node *dotparser.Node, ctx *Context, graph *dotparser.Graph, logsRoot string) (*Outcome, error) {
		callCount++
		if node.ID == "critical" {
			// First call fails, second succeeds
			if callCount <= 2 { // step_a + first critical call
				return &Outcome{Status: StatusFail, FailureReason: "not ready"}, nil
			}
			return &Outcome{Status: StatusSuccess}, nil
		}
		return &Outcome{Status: StatusSuccess}, nil
	})
	engine := newTestEngine(handler)

	graph := &dotparser.Graph{
		Name: "test",
		Nodes: []*dotparser.Node{
			makeNode("start", makeAttr("shape", "Mdiamond")),
			makeNode("step_a", makeAttr("shape", "box")),
			makeNode("critical", makeAttr("shape", "box"), makeBoolAttr("goal_gate", true), makeAttr("retry_target", "critical")),
			makeNode("exit", makeAttr("shape", "Msquare")),
		},
		Edges: []*dotparser.Edge{
			makeEdge("start", "step_a"),
			makeEdge("step_a", "critical"),
			makeEdge("critical", "exit"),
		},
	}

	outcome, err := engine.Run(graph, RunConfig{})
	require.NoError(t, err)
	assert.Equal(t, StatusSuccess, outcome.Status)
	// Should have been called: step_a, critical(fail), critical(success)
	assert.Equal(t, 3, callCount)
}

func TestEngine_GoalGateNoRetryTarget(t *testing.T) {
	handler := HandlerFunc(func(node *dotparser.Node, ctx *Context, graph *dotparser.Graph, logsRoot string) (*Outcome, error) {
		if node.ID == "critical" {
			return &Outcome{Status: StatusFail}, nil
		}
		return &Outcome{Status: StatusSuccess}, nil
	})
	engine := newTestEngine(handler)

	graph := &dotparser.Graph{
		Name: "test",
		Nodes: []*dotparser.Node{
			makeNode("start", makeAttr("shape", "Mdiamond")),
			makeNode("critical", makeAttr("shape", "box"), makeBoolAttr("goal_gate", true)),
			makeNode("exit", makeAttr("shape", "Msquare")),
		},
		Edges: []*dotparser.Edge{
			makeEdge("start", "critical"),
			makeEdge("critical", "exit"),
		},
	}

	_, err := engine.Run(graph, RunConfig{})
	assert.Error(t, err)
	assert.Contains(t, err.Error(), "goal gate unsatisfied")
}

func TestEngine_FailWithNoOutgoingEdge(t *testing.T) {
	handler := HandlerFunc(func(node *dotparser.Node, ctx *Context, graph *dotparser.Graph, logsRoot string) (*Outcome, error) {
		return &Outcome{Status: StatusFail, FailureReason: "broken"}, nil
	})
	engine := newTestEngine(handler)

	// Node has no outgoing edges except to exit, but let's make it fail with no fail edge
	graph := &dotparser.Graph{
		Name: "test",
		Nodes: []*dotparser.Node{
			makeNode("start", makeAttr("shape", "Mdiamond")),
			makeNode("broken", makeAttr("shape", "box")),
		},
		Edges: []*dotparser.Edge{
			makeEdge("start", "broken"),
			// No outgoing edge from broken
		},
	}

	_, err := engine.Run(graph, RunConfig{})
	assert.Error(t, err)
	assert.Contains(t, err.Error(), "failed with no outgoing fail edge")
}

func TestEngine_HandlerError(t *testing.T) {
	handler := &failingHandler{err: errors.New("handler explosion")}
	engine := newTestEngine(handler)

	// Node with no outgoing edge: handler error should surface
	graph := &dotparser.Graph{
		Name: "test",
		Nodes: []*dotparser.Node{
			makeNode("start", makeAttr("shape", "Mdiamond")),
			makeNode("broken", makeAttr("shape", "box")),
		},
		Edges: []*dotparser.Edge{
			makeEdge("start", "broken"),
		},
	}
	_, err := engine.Run(graph, RunConfig{})
	assert.Error(t, err)
	assert.Contains(t, err.Error(), "failed with no outgoing fail edge")
}

func TestEngine_HandlerErrorWithEdge(t *testing.T) {
	// When a handler errors but there's an outgoing edge, the FAIL outcome
	// follows the edge and the pipeline completes.
	handler := &failingHandler{err: errors.New("handler explosion")}
	engine := newTestEngine(handler)

	graph := linearGraph("step_a")
	outcome, err := engine.Run(graph, RunConfig{})
	require.NoError(t, err)
	assert.Equal(t, StatusFail, outcome.Status)
}

func TestEngine_CheckpointWritten(t *testing.T) {
	handler := newRecordingHandler(&Outcome{Status: StatusSuccess})
	engine := newTestEngine(handler)

	logsRoot := t.TempDir()
	graph := linearGraph("step_a")
	_, err := engine.Run(graph, RunConfig{LogsRoot: logsRoot})
	require.NoError(t, err)

	// Verify checkpoint was written
	cpPath := filepath.Join(logsRoot, "checkpoint.json")
	_, err = os.Stat(cpPath)
	assert.NoError(t, err)

	// Verify it can be loaded
	cp, err := LoadCheckpoint(logsRoot)
	require.NoError(t, err)
	assert.Contains(t, cp.CompletedNodes, "step_a")
}

func TestEngine_FindStartNodeByShape(t *testing.T) {
	graph := &dotparser.Graph{
		Name: "test",
		Nodes: []*dotparser.Node{
			makeNode("entry", makeAttr("shape", "Mdiamond")),
		},
	}
	node, err := findStartNode(graph)
	require.NoError(t, err)
	assert.Equal(t, "entry", node.ID)
}

func TestEngine_FindStartNodeByID(t *testing.T) {
	graph := &dotparser.Graph{
		Name: "test",
		Nodes: []*dotparser.Node{
			makeNode("start"),
		},
	}
	node, err := findStartNode(graph)
	require.NoError(t, err)
	assert.Equal(t, "start", node.ID)
}

func TestEngine_IsTerminal(t *testing.T) {
	assert.True(t, isTerminal(makeNode("exit", makeAttr("shape", "Msquare"))))
	assert.True(t, isTerminal(makeNode("exit", makeAttr("type", "exit"))))
	assert.False(t, isTerminal(makeNode("step", makeAttr("shape", "box"))))
	assert.False(t, isTerminal(makeNode("step")))
}
