package pipeline

import (
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
		{Key: "manager.poll_interval", Value: dotparser.Value{Kind: dotparser.ValueDuration, Duration: 100 * time.Millisecond}},
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
	handler := NewManagerLoopHandler()
	sleeper := &MockSleeper{}
	handler.Sleeper = sleeper

	graph := newTestGraph(nil, nil, []dotparser.Attr{
		strAttr("stack.child_dotfile", "/path/to/child.dot"),
	})

	node := newNode("manager")
	node.Attrs = []dotparser.Attr{
		{Key: "manager.max_cycles", Value: dotparser.Value{Kind: dotparser.ValueInt, Int: 1}},
		{Key: "manager.actions", Value: dotparser.Value{Kind: dotparser.ValueString, Str: "observe"}},
	}

	ctx := NewContext()
	// Don't pre-set child status - let autostart set it

	_, err := handler.Execute(node, ctx, graph, "")
	require.NoError(t, err)

	// Autostart should have set the child status to "running"
	status := ctx.GetString("stack.child.status", "")
	assert.Equal(t, "running", status)

	// Should have set the dotfile path
	dotfile := ctx.GetString("stack.child.dotfile", "")
	assert.Equal(t, "/path/to/child.dot", dotfile)
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
