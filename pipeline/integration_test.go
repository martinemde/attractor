package pipeline

import (
	"testing"

	"github.com/martinemde/attractor/dotparser"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// Integration tests that parse real DOT source and run the engine.

func TestIntegration_SimpleLinearWorkflow(t *testing.T) {
	src := []byte(`digraph Simple {
		graph [goal="Run tests and report"]

		start [shape=Mdiamond, label="Start"]
		exit  [shape=Msquare, label="Exit"]

		run_tests [label="Run Tests", prompt="Run the test suite"]
		report    [label="Report", prompt="Summarize results"]

		start -> run_tests -> report -> exit
	}`)

	graph, err := dotparser.Parse(src)
	require.NoError(t, err)

	var visited []string
	handler := HandlerFunc(func(node *dotparser.Node, ctx *Context, g *dotparser.Graph, logsRoot string) (*Outcome, error) {
		visited = append(visited, node.ID)
		return &Outcome{
			Status:         StatusSuccess,
			ContextUpdates: map[string]string{"last_stage": node.ID},
		}, nil
	})

	engine := newTestEngine(handler)
	outcome, err := engine.Run(graph, RunConfig{})
	require.NoError(t, err)
	assert.Equal(t, StatusSuccess, outcome.Status)
	assert.Equal(t, []string{"run_tests", "report"}, visited)
}

func TestIntegration_BranchingWithConditions(t *testing.T) {
	src := []byte(`digraph Branch {
		graph [goal="Implement and validate"]
		node [shape=box]

		start     [shape=Mdiamond, label="Start"]
		exit      [shape=Msquare, label="Exit"]
		plan      [label="Plan"]
		implement [label="Implement"]
		validate  [label="Validate"]
		gate      [shape=diamond, label="Tests passing?"]

		start -> plan -> implement -> validate -> gate
		gate -> exit      [label="Yes", condition="outcome=success"]
		gate -> implement [label="No", condition="outcome!=success"]
	}`)

	graph, err := dotparser.Parse(src)
	require.NoError(t, err)

	callCount := 0
	var visited []string
	handler := HandlerFunc(func(node *dotparser.Node, ctx *Context, g *dotparser.Graph, logsRoot string) (*Outcome, error) {
		callCount++
		visited = append(visited, node.ID)
		// Simulate: first validate fails, second succeeds
		if node.ID == "validate" {
			if callCount <= 4 { // plan + implement + validate(1st) + gate(1st)
				return &Outcome{Status: StatusFail}, nil
			}
			return &Outcome{Status: StatusSuccess}, nil
		}
		return &Outcome{Status: StatusSuccess}, nil
	})

	engine := newTestEngine(handler)
	// Register a conditional handler for the diamond shape
	engine.Registry.Register("conditional", HandlerFunc(func(node *dotparser.Node, ctx *Context, g *dotparser.Graph, logsRoot string) (*Outcome, error) {
		callCount++
		visited = append(visited, node.ID)
		return &Outcome{Status: StatusSuccess}, nil
	}))

	outcome, err := engine.Run(graph, RunConfig{})
	require.NoError(t, err)
	assert.Equal(t, StatusSuccess, outcome.Status)
	// Flow: plan -> implement -> validate(fail) -> gate -> implement -> validate(success) -> gate -> exit
	assert.Equal(t, []string{"plan", "implement", "validate", "gate", "implement", "validate", "gate"}, visited)
}

func TestIntegration_HumanGateSimulated(t *testing.T) {
	src := []byte(`digraph Review {
		start [shape=Mdiamond, label="Start"]
		exit  [shape=Msquare, label="Exit"]

		review_gate [shape=hexagon, label="Review Changes", type="wait.human"]

		ship_it [label="Ship It"]
		fixes   [label="Fix Issues"]

		start -> review_gate
		review_gate -> ship_it [label="[A] Approve"]
		review_gate -> fixes   [label="[F] Fix"]
		ship_it -> exit
		fixes -> review_gate
	}`)

	graph, err := dotparser.Parse(src)
	require.NoError(t, err)

	var visited []string
	handler := HandlerFunc(func(node *dotparser.Node, ctx *Context, g *dotparser.Graph, logsRoot string) (*Outcome, error) {
		visited = append(visited, node.ID)
		return &Outcome{Status: StatusSuccess}, nil
	})

	engine := newTestEngine(handler)
	// Register a simulated human handler that always approves
	engine.Registry.Register("wait.human", HandlerFunc(func(node *dotparser.Node, ctx *Context, g *dotparser.Graph, logsRoot string) (*Outcome, error) {
		visited = append(visited, node.ID)
		return &Outcome{
			Status:         StatusSuccess,
			PreferredLabel: "approve",
		}, nil
	}))

	outcome, err := engine.Run(graph, RunConfig{})
	require.NoError(t, err)
	assert.Equal(t, StatusSuccess, outcome.Status)
	assert.Equal(t, []string{"review_gate", "ship_it"}, visited)
}

func TestIntegration_GoalGateWithGraphRetryTarget(t *testing.T) {
	src := []byte(`digraph GoalGate {
		graph [goal="Deploy safely", retry_target="validate"]

		start    [shape=Mdiamond]
		exit     [shape=Msquare]
		build    [label="Build"]
		validate [label="Validate", goal_gate=true]

		start -> build -> validate -> exit
	}`)

	graph, err := dotparser.Parse(src)
	require.NoError(t, err)

	callCount := 0
	handler := HandlerFunc(func(node *dotparser.Node, ctx *Context, g *dotparser.Graph, logsRoot string) (*Outcome, error) {
		callCount++
		if node.ID == "validate" {
			// Fail on first attempt, succeed on second
			if callCount <= 2 {
				return &Outcome{Status: StatusFail}, nil
			}
			return &Outcome{Status: StatusSuccess}, nil
		}
		return &Outcome{Status: StatusSuccess}, nil
	})

	engine := newTestEngine(handler)
	outcome, err := engine.Run(graph, RunConfig{})
	require.NoError(t, err)
	assert.Equal(t, StatusSuccess, outcome.Status)
}

func TestIntegration_CheckpointPersistence(t *testing.T) {
	src := []byte(`digraph Checkpoint {
		start [shape=Mdiamond]
		exit  [shape=Msquare]
		step  [label="Step"]

		start -> step -> exit
	}`)

	graph, err := dotparser.Parse(src)
	require.NoError(t, err)

	handler := HandlerFunc(func(node *dotparser.Node, ctx *Context, g *dotparser.Graph, logsRoot string) (*Outcome, error) {
		return &Outcome{Status: StatusSuccess, ContextUpdates: map[string]string{"done": "yes"}}, nil
	})

	engine := newTestEngine(handler)
	logsRoot := t.TempDir()
	outcome, err := engine.Run(graph, RunConfig{LogsRoot: logsRoot})
	require.NoError(t, err)
	assert.Equal(t, StatusSuccess, outcome.Status)

	cp, err := LoadCheckpoint(logsRoot)
	require.NoError(t, err)
	assert.Equal(t, "step", cp.CurrentNode)
	assert.Contains(t, cp.CompletedNodes, "start")
	assert.Contains(t, cp.CompletedNodes, "step")
	assert.Equal(t, "yes", cp.ContextValues["done"])
	assert.Equal(t, "success", cp.ContextValues["outcome"])
}

func TestIntegration_ExpandVariables(t *testing.T) {
	graph := &dotparser.Graph{
		GraphAttrs: []dotparser.Attr{makeAttr("goal", "Deploy safely")},
	}
	result := ExpandVariables("Complete the task: $goal", graph, NewContext())
	assert.Equal(t, "Complete the task: Deploy safely", result)
}

func TestStageStatus_IsSuccess(t *testing.T) {
	assert.True(t, StatusSuccess.IsSuccess())
	assert.True(t, StatusPartialSuccess.IsSuccess())
	assert.False(t, StatusFail.IsSuccess())
	assert.False(t, StatusRetry.IsSuccess())
	assert.False(t, StatusSkipped.IsSuccess())
}
