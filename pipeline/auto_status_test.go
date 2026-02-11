package pipeline

import (
	"testing"

	"github.com/martinemde/attractor/dotparser"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// NilOutcomeHandler returns nil outcome without error to simulate a handler
// that writes no status.
type NilOutcomeHandler struct {
	Calls int
}

func (h *NilOutcomeHandler) Execute(node *dotparser.Node, ctx *Context, graph *dotparser.Graph, logsRoot string) (*Outcome, error) {
	h.Calls++
	return nil, nil // No status written
}

// EmptyStatusHandler returns an outcome with empty status (zero value).
type EmptyStatusHandler struct {
	Calls int
}

func (h *EmptyStatusHandler) Execute(node *dotparser.Node, ctx *Context, graph *dotparser.Graph, logsRoot string) (*Outcome, error) {
	h.Calls++
	return &Outcome{}, nil // Empty status (zero value)
}

func TestAutoStatus_TrueWithNilOutcome_GeneratesSuccess(t *testing.T) {
	// When auto_status=true and handler returns nil outcome, auto-generate SUCCESS
	handler := &NilOutcomeHandler{}

	registry := DefaultRegistry()
	registry.Register("nil_outcome", handler)

	graph := newTestGraph(
		[]*dotparser.Node{
			newNode("start", strAttr("shape", "Mdiamond")),
			newNode("task", strAttr("type", "nil_outcome"), boolAttr("auto_status", true)),
			newNode("exit", strAttr("shape", "Msquare")),
		},
		[]*dotparser.Edge{
			newEdge("start", "task"),
			newEdge("task", "exit"),
		},
		nil,
	)

	result, err := Run(graph, &RunConfig{Registry: registry})

	require.NoError(t, err)
	assert.Equal(t, []string{"start", "task"}, result.CompletedNodes)
	assert.Equal(t, 1, handler.Calls)

	// Verify outcome was auto-generated as SUCCESS
	taskOutcome := result.NodeOutcomes["task"]
	require.NotNil(t, taskOutcome)
	assert.Equal(t, StatusSuccess, taskOutcome.Status)
	assert.Contains(t, taskOutcome.Notes, "auto_status")
}

func TestAutoStatus_TrueWithEmptyStatus_GeneratesSuccess(t *testing.T) {
	// When auto_status=true and handler returns outcome with empty status, auto-generate SUCCESS
	handler := &EmptyStatusHandler{}

	registry := DefaultRegistry()
	registry.Register("empty_status", handler)

	graph := newTestGraph(
		[]*dotparser.Node{
			newNode("start", strAttr("shape", "Mdiamond")),
			newNode("task", strAttr("type", "empty_status"), boolAttr("auto_status", true)),
			newNode("exit", strAttr("shape", "Msquare")),
		},
		[]*dotparser.Edge{
			newEdge("start", "task"),
			newEdge("task", "exit"),
		},
		nil,
	)

	result, err := Run(graph, &RunConfig{Registry: registry})

	require.NoError(t, err)
	assert.Equal(t, []string{"start", "task"}, result.CompletedNodes)
	assert.Equal(t, 1, handler.Calls)

	// Verify outcome was auto-generated as SUCCESS
	taskOutcome := result.NodeOutcomes["task"]
	require.NotNil(t, taskOutcome)
	assert.Equal(t, StatusSuccess, taskOutcome.Status)
	assert.Contains(t, taskOutcome.Notes, "auto_status")
}

func TestAutoStatus_FalseWithNilOutcome_Panics(t *testing.T) {
	// When auto_status=false (default) and handler returns nil, the nil outcome
	// causes a panic when the engine tries to access outcome.Status.
	// This test documents that handlers should always return an outcome when
	// auto_status is not enabled.
	handler := &NilOutcomeHandler{}

	registry := DefaultRegistry()
	registry.Register("nil_outcome", handler)

	graph := newTestGraph(
		[]*dotparser.Node{
			newNode("start", strAttr("shape", "Mdiamond")),
			newNode("task", strAttr("type", "nil_outcome")), // No auto_status (defaults to false)
			newNode("exit", strAttr("shape", "Msquare")),
		},
		[]*dotparser.Edge{
			newEdge("start", "task"),
			newEdge("task", "exit"),
		},
		nil,
	)

	// Without auto_status, nil outcome causes a panic when accessing outcome.Status
	// This is expected behavior - handlers must return valid outcomes unless auto_status=true
	assert.Panics(t, func() {
		Run(graph, &RunConfig{Registry: registry})
	})
}

func TestAutoStatus_TrueWithExplicitFailStatus_UsesExplicitStatus(t *testing.T) {
	// When auto_status=true but handler returns explicit FAIL status, use the explicit status
	mockHandler := &MockHandler{
		Outcomes: []*Outcome{
			Fail("explicit failure"),
		},
	}

	registry := DefaultRegistry()
	registry.Register("mock", mockHandler)

	graph := newTestGraph(
		[]*dotparser.Node{
			newNode("start", strAttr("shape", "Mdiamond")),
			newNode("task", strAttr("type", "mock"), boolAttr("auto_status", true)),
			newNode("exit", strAttr("shape", "Msquare")),
			newNode("fail_exit", strAttr("shape", "Msquare")),
		},
		[]*dotparser.Edge{
			newEdge("start", "task"),
			newEdge("task", "exit"),
			newEdge("task", "fail_exit", strAttr("condition", "outcome=fail")),
		},
		nil,
	)

	result, err := Run(graph, &RunConfig{Registry: registry})

	require.NoError(t, err) // Should follow fail edge to fail_exit
	assert.Equal(t, []string{"start", "task"}, result.CompletedNodes)

	// Verify explicit status was used, not auto-generated
	taskOutcome := result.NodeOutcomes["task"]
	require.NotNil(t, taskOutcome)
	assert.Equal(t, StatusFail, taskOutcome.Status)
	assert.Equal(t, "explicit failure", taskOutcome.FailureReason)
}

func TestAutoStatus_TrueWithExplicitSuccessStatus_UsesExplicitSuccess(t *testing.T) {
	// When auto_status=true and handler returns SUCCESS, use the handler's outcome
	mockHandler := &MockHandler{
		Outcomes: []*Outcome{
			Success().WithNotes("handler success"),
		},
	}

	registry := DefaultRegistry()
	registry.Register("mock", mockHandler)

	graph := newTestGraph(
		[]*dotparser.Node{
			newNode("start", strAttr("shape", "Mdiamond")),
			newNode("task", strAttr("type", "mock"), boolAttr("auto_status", true)),
			newNode("exit", strAttr("shape", "Msquare")),
		},
		[]*dotparser.Edge{
			newEdge("start", "task"),
			newEdge("task", "exit"),
		},
		nil,
	)

	result, err := Run(graph, &RunConfig{Registry: registry})

	require.NoError(t, err)
	assert.Equal(t, []string{"start", "task"}, result.CompletedNodes)

	// Verify handler's outcome was used (not auto-generated)
	taskOutcome := result.NodeOutcomes["task"]
	require.NotNil(t, taskOutcome)
	assert.Equal(t, StatusSuccess, taskOutcome.Status)
	assert.Equal(t, "handler success", taskOutcome.Notes)
}

func TestAutoStatus_TrueWithPartialSuccess_UsesExplicitStatus(t *testing.T) {
	// When auto_status=true and handler returns PARTIAL_SUCCESS, use the handler's outcome
	mockHandler := &MockHandler{
		Outcomes: []*Outcome{
			PartialSuccess("partial work done"),
		},
	}

	registry := DefaultRegistry()
	registry.Register("mock", mockHandler)

	graph := newTestGraph(
		[]*dotparser.Node{
			newNode("start", strAttr("shape", "Mdiamond")),
			newNode("task", strAttr("type", "mock"), boolAttr("auto_status", true)),
			newNode("exit", strAttr("shape", "Msquare")),
		},
		[]*dotparser.Edge{
			newEdge("start", "task"),
			newEdge("task", "exit"),
		},
		nil,
	)

	result, err := Run(graph, &RunConfig{Registry: registry})

	require.NoError(t, err)
	assert.Equal(t, []string{"start", "task"}, result.CompletedNodes)

	// Verify handler's outcome was used (not auto-generated)
	taskOutcome := result.NodeOutcomes["task"]
	require.NotNil(t, taskOutcome)
	assert.Equal(t, StatusPartialSuccess, taskOutcome.Status)
	assert.Equal(t, "partial work done", taskOutcome.Notes)
}

func TestApplyAutoStatus_Unit(t *testing.T) {
	// Unit tests for the applyAutoStatus function

	t.Run("auto_status not set returns outcome unchanged", func(t *testing.T) {
		node := newNode("test")
		outcome := Success()
		result := applyAutoStatus(node, outcome)
		assert.Same(t, outcome, result)
	})

	t.Run("auto_status=false returns outcome unchanged", func(t *testing.T) {
		node := newNode("test", boolAttr("auto_status", false))
		outcome := Success()
		result := applyAutoStatus(node, outcome)
		assert.Same(t, outcome, result)
	})

	t.Run("auto_status=true with nil outcome generates success", func(t *testing.T) {
		node := newNode("test", boolAttr("auto_status", true))
		result := applyAutoStatus(node, nil)
		require.NotNil(t, result)
		assert.Equal(t, StatusSuccess, result.Status)
		assert.Contains(t, result.Notes, "auto_status")
	})

	t.Run("auto_status=true with empty status generates success", func(t *testing.T) {
		node := newNode("test", boolAttr("auto_status", true))
		result := applyAutoStatus(node, &Outcome{})
		require.NotNil(t, result)
		assert.Equal(t, StatusSuccess, result.Status)
		assert.Contains(t, result.Notes, "auto_status")
	})

	t.Run("auto_status=true with explicit success returns original", func(t *testing.T) {
		node := newNode("test", boolAttr("auto_status", true))
		outcome := Success().WithNotes("explicit")
		result := applyAutoStatus(node, outcome)
		assert.Same(t, outcome, result)
	})

	t.Run("auto_status=true with explicit fail returns original", func(t *testing.T) {
		node := newNode("test", boolAttr("auto_status", true))
		outcome := Fail("explicit failure")
		result := applyAutoStatus(node, outcome)
		assert.Same(t, outcome, result)
	})
}
