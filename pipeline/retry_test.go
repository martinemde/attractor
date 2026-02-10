package pipeline

import (
	"errors"
	"sync/atomic"
	"testing"
	"time"

	"github.com/martinemde/attractor/dotparser"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// mockSleeper records sleep calls without actually sleeping
type mockSleeper struct {
	Calls []time.Duration
}

func (s *mockSleeper) Sleep(d time.Duration) {
	s.Calls = append(s.Calls, d)
}

func boolAttr(key string, value bool) dotparser.Attr {
	return dotparser.Attr{Key: key, Value: dotparser.Value{Kind: dotparser.ValueBool, Bool: value, Raw: ""}}
}

// --- Backoff Delay Calculation Tests ---

func TestBackoffConfig_DelayForAttempt_ExponentialGrowth(t *testing.T) {
	config := BackoffConfig{
		InitialDelayMs: 200,
		BackoffFactor:  2.0,
		MaxDelayMs:     60000,
		Jitter:         false, // Disable jitter for predictable tests
	}

	tests := []struct {
		attempt  int
		expected time.Duration
	}{
		{1, 200 * time.Millisecond},
		{2, 400 * time.Millisecond},
		{3, 800 * time.Millisecond},
		{4, 1600 * time.Millisecond},
		{5, 3200 * time.Millisecond},
	}

	for _, tt := range tests {
		delay := config.DelayForAttempt(tt.attempt)
		assert.Equal(t, tt.expected, delay, "attempt %d", tt.attempt)
	}
}

func TestBackoffConfig_DelayForAttempt_LinearGrowth(t *testing.T) {
	config := BackoffConfig{
		InitialDelayMs: 500,
		BackoffFactor:  1.0, // Linear = no growth
		MaxDelayMs:     60000,
		Jitter:         false,
	}

	// All attempts should have the same delay
	for attempt := 1; attempt <= 5; attempt++ {
		delay := config.DelayForAttempt(attempt)
		assert.Equal(t, 500*time.Millisecond, delay, "attempt %d", attempt)
	}
}

func TestBackoffConfig_DelayForAttempt_MaxDelayCap(t *testing.T) {
	config := BackoffConfig{
		InitialDelayMs: 10000,
		BackoffFactor:  10.0,
		MaxDelayMs:     30000, // Cap at 30 seconds
		Jitter:         false,
	}

	// First attempt: 10000ms (under cap)
	assert.Equal(t, 10000*time.Millisecond, config.DelayForAttempt(1))

	// Second attempt: 100000ms but capped at 30000ms
	assert.Equal(t, 30000*time.Millisecond, config.DelayForAttempt(2))

	// Third attempt: would be 1000000ms but capped
	assert.Equal(t, 30000*time.Millisecond, config.DelayForAttempt(3))
}

func TestBackoffConfig_DelayForAttempt_JitterBounds(t *testing.T) {
	config := BackoffConfig{
		InitialDelayMs: 1000,
		BackoffFactor:  1.0,
		MaxDelayMs:     60000,
		Jitter:         true,
	}

	// Run many times to test jitter bounds
	minExpected := 500 * time.Millisecond  // 1000 * 0.5
	maxExpected := 1500 * time.Millisecond // 1000 * 1.5

	for range 100 {
		delay := config.DelayForAttempt(1)
		assert.GreaterOrEqual(t, delay, minExpected, "delay should be at least 50%% of base")
		assert.Less(t, delay, maxExpected, "delay should be less than 150%% of base")
	}
}

func TestBackoffConfig_DelayForAttempt_DefaultValues(t *testing.T) {
	config := BackoffConfig{} // All zeros

	// Should use defaults: 200ms initial, 2.0 factor, 60000 max
	delay := config.DelayForAttempt(1)
	// With jitter (default false for zero value), should be exactly 200ms
	assert.Equal(t, 200*time.Millisecond, delay)
}

func TestBackoffConfig_DelayForAttempt_InvalidAttempt(t *testing.T) {
	config := BackoffConfig{
		InitialDelayMs: 100,
		BackoffFactor:  2.0,
		MaxDelayMs:     60000,
		Jitter:         false,
	}

	// Attempt 0 or negative should be treated as 1
	assert.Equal(t, 100*time.Millisecond, config.DelayForAttempt(0))
	assert.Equal(t, 100*time.Millisecond, config.DelayForAttempt(-1))
}

// --- Preset Policy Tests ---

func TestNoRetryPolicy(t *testing.T) {
	policy := NoRetryPolicy()

	assert.Equal(t, 1, policy.MaxAttempts)
}

func TestStandardRetryPolicy(t *testing.T) {
	policy := StandardRetryPolicy()

	assert.Equal(t, 5, policy.MaxAttempts)
	assert.Equal(t, 200, policy.Backoff.InitialDelayMs)
	assert.Equal(t, 2.0, policy.Backoff.BackoffFactor)
	assert.Equal(t, 60000, policy.Backoff.MaxDelayMs)
	assert.True(t, policy.Backoff.Jitter)
	assert.NotNil(t, policy.ShouldRetry)
}

func TestAggressiveRetryPolicy(t *testing.T) {
	policy := AggressiveRetryPolicy()

	assert.Equal(t, 5, policy.MaxAttempts)
	assert.Equal(t, 500, policy.Backoff.InitialDelayMs)
	assert.Equal(t, 2.0, policy.Backoff.BackoffFactor)
	assert.True(t, policy.Backoff.Jitter)
}

func TestLinearRetryPolicy(t *testing.T) {
	policy := LinearRetryPolicy()

	assert.Equal(t, 3, policy.MaxAttempts)
	assert.Equal(t, 500, policy.Backoff.InitialDelayMs)
	assert.Equal(t, 1.0, policy.Backoff.BackoffFactor)
	assert.True(t, policy.Backoff.Jitter)
}

func TestPatientRetryPolicy(t *testing.T) {
	policy := PatientRetryPolicy()

	assert.Equal(t, 3, policy.MaxAttempts)
	assert.Equal(t, 2000, policy.Backoff.InitialDelayMs)
	assert.Equal(t, 3.0, policy.Backoff.BackoffFactor)
	assert.True(t, policy.Backoff.Jitter)
}

// --- BuildRetryPolicy Tests ---

func TestBuildRetryPolicy_FromNodeAttribute(t *testing.T) {
	node := newNode("test", intAttr("max_retries", 2))
	graph := newTestGraph([]*dotparser.Node{node}, nil, nil)

	policy := BuildRetryPolicy(node, graph)

	// max_retries=2 means 3 total attempts (1 initial + 2 retries)
	assert.Equal(t, 3, policy.MaxAttempts)
}

func TestBuildRetryPolicy_FromGraphDefault(t *testing.T) {
	node := newNode("test") // No max_retries attribute
	graph := newTestGraph([]*dotparser.Node{node}, nil, []dotparser.Attr{
		intAttr("default_max_retry", 4),
	})

	policy := BuildRetryPolicy(node, graph)

	// default_max_retry=4 means 5 total attempts
	assert.Equal(t, 5, policy.MaxAttempts)
}

func TestBuildRetryPolicy_NodeOverridesGraph(t *testing.T) {
	node := newNode("test", intAttr("max_retries", 1))
	graph := newTestGraph([]*dotparser.Node{node}, nil, []dotparser.Attr{
		intAttr("default_max_retry", 10),
	})

	policy := BuildRetryPolicy(node, graph)

	// Node attribute takes precedence
	assert.Equal(t, 2, policy.MaxAttempts)
}

func TestBuildRetryPolicy_DefaultNoRetries(t *testing.T) {
	node := newNode("test") // No max_retries
	graph := newTestGraph([]*dotparser.Node{node}, nil, nil)

	policy := BuildRetryPolicy(node, graph)

	// Default is no retries (1 attempt)
	assert.Equal(t, 1, policy.MaxAttempts)
}

// --- Retry Counter Tests ---

func TestRetryCounter_TrackInContext(t *testing.T) {
	ctx := NewContext()

	// Initially zero
	assert.Equal(t, 0, GetRetryCount(ctx, "node1"))

	// Increment
	count := IncrementRetryCount(ctx, "node1")
	assert.Equal(t, 1, count)
	assert.Equal(t, 1, GetRetryCount(ctx, "node1"))

	// Increment again
	count = IncrementRetryCount(ctx, "node1")
	assert.Equal(t, 2, count)

	// Reset
	ResetRetryCount(ctx, "node1")
	assert.Equal(t, 0, GetRetryCount(ctx, "node1"))
}

func TestRetryCounter_SeparateNodes(t *testing.T) {
	ctx := NewContext()

	IncrementRetryCount(ctx, "node1")
	IncrementRetryCount(ctx, "node1")
	IncrementRetryCount(ctx, "node2")

	assert.Equal(t, 2, GetRetryCount(ctx, "node1"))
	assert.Equal(t, 1, GetRetryCount(ctx, "node2"))
	assert.Equal(t, 0, GetRetryCount(ctx, "node3"))
}

// --- Execute With Retry Integration Tests ---

// RetryingHandler returns RETRY for the first N calls, then SUCCESS
type RetryingHandler struct {
	retryCount   int // Number of RETRY responses before SUCCESS
	currentCalls int32
}

func (h *RetryingHandler) Execute(node *dotparser.Node, ctx *Context, graph *dotparser.Graph, logsRoot string) (*Outcome, error) {
	calls := atomic.AddInt32(&h.currentCalls, 1)
	if int(calls) <= h.retryCount {
		return Retry("temporary failure"), nil
	}
	return Success().WithNotes("eventually succeeded"), nil
}

// ErroringHandler returns an error for the first N calls, then SUCCESS
type ErroringHandler struct {
	errorCount   int
	currentCalls int32
}

func (h *ErroringHandler) Execute(node *dotparser.Node, ctx *Context, graph *dotparser.Graph, logsRoot string) (*Outcome, error) {
	calls := atomic.AddInt32(&h.currentCalls, 1)
	if int(calls) <= h.errorCount {
		return nil, errors.New("handler error")
	}
	return Success().WithNotes("eventually succeeded"), nil
}

func TestRun_RetryOnRetryOutcome(t *testing.T) {
	sleeper := &mockSleeper{}

	handler := &RetryingHandler{retryCount: 2}
	registry := DefaultRegistry()
	registry.Register("retrying", handler)

	graph := newTestGraph(
		[]*dotparser.Node{
			newNode("start", strAttr("shape", "Mdiamond")),
			newNode("A", strAttr("type", "retrying"), intAttr("max_retries", 3)),
			newNode("exit", strAttr("shape", "Msquare")),
		},
		[]*dotparser.Edge{
			newEdge("start", "A"),
			newEdge("A", "exit"),
		},
		nil,
	)

	result, err := Run(graph, &RunConfig{
		Registry: registry,
		Sleeper:  sleeper,
	})

	require.NoError(t, err)
	assert.Equal(t, StatusSuccess, result.FinalOutcome.Status)
	assert.Equal(t, int32(3), handler.currentCalls) // 2 retries + 1 success
	assert.Len(t, sleeper.Calls, 2)                 // 2 sleep calls for retries
}

func TestRun_RetryOnHandlerError(t *testing.T) {
	sleeper := &mockSleeper{}

	handler := &ErroringHandler{errorCount: 1}
	registry := DefaultRegistry()
	registry.Register("erroring", handler)

	graph := newTestGraph(
		[]*dotparser.Node{
			newNode("start", strAttr("shape", "Mdiamond")),
			newNode("A", strAttr("type", "erroring"), intAttr("max_retries", 2)),
			newNode("exit", strAttr("shape", "Msquare")),
		},
		[]*dotparser.Edge{
			newEdge("start", "A"),
			newEdge("A", "exit"),
		},
		nil,
	)

	result, err := Run(graph, &RunConfig{
		Registry: registry,
		Sleeper:  sleeper,
	})

	require.NoError(t, err)
	assert.Equal(t, StatusSuccess, result.FinalOutcome.Status)
	assert.Equal(t, int32(2), handler.currentCalls) // 1 error + 1 success
	assert.Len(t, sleeper.Calls, 1)                 // 1 sleep call for retry
}

func TestRun_RetryExhaustedReturnsFail(t *testing.T) {
	sleeper := &mockSleeper{}

	handler := &RetryingHandler{retryCount: 10} // More than max retries
	registry := DefaultRegistry()
	registry.Register("retrying", handler)

	graph := newTestGraph(
		[]*dotparser.Node{
			newNode("start", strAttr("shape", "Mdiamond")),
			newNode("A", strAttr("type", "retrying"), intAttr("max_retries", 2)),
			newNode("exit", strAttr("shape", "Msquare")),
		},
		[]*dotparser.Edge{
			newEdge("start", "A"),
			newEdge("A", "exit", strAttr("condition", "outcome=fail")), // Fail edge to allow exit
		},
		nil,
	)

	result, err := Run(graph, &RunConfig{
		Registry: registry,
		Sleeper:  sleeper,
	})

	require.NoError(t, err) // No error because there's a fail edge
	assert.Equal(t, StatusFail, result.FinalOutcome.Status)
	assert.Contains(t, result.FinalOutcome.FailureReason, "max retries exceeded")
	assert.Equal(t, int32(3), handler.currentCalls) // 3 total attempts (1 + 2 retries)
	assert.Len(t, sleeper.Calls, 2)                 // 2 sleep calls
}

func TestRun_RetryExhaustedWithAllowPartial(t *testing.T) {
	sleeper := &mockSleeper{}

	handler := &RetryingHandler{retryCount: 10}
	registry := DefaultRegistry()
	registry.Register("retrying", handler)

	graph := newTestGraph(
		[]*dotparser.Node{
			newNode("start", strAttr("shape", "Mdiamond")),
			newNode("A",
				strAttr("type", "retrying"),
				intAttr("max_retries", 1),
				boolAttr("allow_partial", true),
			),
			newNode("exit", strAttr("shape", "Msquare")),
		},
		[]*dotparser.Edge{
			newEdge("start", "A"),
			newEdge("A", "exit"),
		},
		nil,
	)

	result, err := Run(graph, &RunConfig{
		Registry: registry,
		Sleeper:  sleeper,
	})

	require.NoError(t, err)
	assert.Equal(t, StatusPartialSuccess, result.FinalOutcome.Status)
	assert.Contains(t, result.FinalOutcome.Notes, "retries exhausted")
}

func TestRun_NodeMaxRetriesGivesTotalAttempts(t *testing.T) {
	sleeper := &mockSleeper{}

	callCount := int32(0)
	handler := HandlerFunc(func(node *dotparser.Node, ctx *Context, graph *dotparser.Graph, logsRoot string) (*Outcome, error) {
		atomic.AddInt32(&callCount, 1)
		return Retry("keep retrying"), nil
	})

	registry := DefaultRegistry()
	registry.Register("counting", handler)

	// max_retries=2 means 3 total attempts
	graph := newTestGraph(
		[]*dotparser.Node{
			newNode("start", strAttr("shape", "Mdiamond")),
			newNode("A", strAttr("type", "counting"), intAttr("max_retries", 2)),
			newNode("exit", strAttr("shape", "Msquare")),
		},
		[]*dotparser.Edge{
			newEdge("start", "A"),
			newEdge("A", "exit", strAttr("condition", "outcome=fail")), // Fail edge to allow exit
		},
		nil,
	)

	result, err := Run(graph, &RunConfig{
		Registry: registry,
		Sleeper:  sleeper,
	})

	require.NoError(t, err)
	assert.Equal(t, int32(3), callCount, "should have 3 total attempts (1 initial + 2 retries)")
	assert.Equal(t, StatusFail, result.FinalOutcome.Status) // Retries exhausted = fail
}

func TestRun_RetryCounterTrackedInContext(t *testing.T) {
	sleeper := &mockSleeper{}

	var capturedCtx *Context
	callCount := int32(0)
	handler := HandlerFunc(func(node *dotparser.Node, ctx *Context, graph *dotparser.Graph, logsRoot string) (*Outcome, error) {
		count := atomic.AddInt32(&callCount, 1)
		capturedCtx = ctx.Clone()
		if count < 3 {
			return Retry("retry"), nil
		}
		return Success(), nil
	})

	registry := DefaultRegistry()
	registry.Register("tracking", handler)

	graph := newTestGraph(
		[]*dotparser.Node{
			newNode("start", strAttr("shape", "Mdiamond")),
			newNode("A", strAttr("type", "tracking"), intAttr("max_retries", 3)),
			newNode("exit", strAttr("shape", "Msquare")),
		},
		[]*dotparser.Edge{
			newEdge("start", "A"),
			newEdge("A", "exit"),
		},
		nil,
	)

	result, err := Run(graph, &RunConfig{
		Registry: registry,
		Sleeper:  sleeper,
	})

	require.NoError(t, err)
	assert.Equal(t, StatusSuccess, result.FinalOutcome.Status)

	// On success, retry count should be reset to 0
	assert.Equal(t, 0, GetRetryCount(result.Context, "A"))
	// But we captured context during the retries
	assert.NotNil(t, capturedCtx)
}

// --- Goal Gate Enforcement Tests ---

func TestRun_GoalGateBlocksExitWhenUnsatisfied(t *testing.T) {
	// Handler that always fails
	handler := &MockHandler{
		Outcomes: []*Outcome{
			Fail("critical failure"),
		},
	}

	registry := DefaultRegistry()
	registry.Register("failing", handler)

	// Use a fail edge so the pipeline can reach the exit node, where goal gate
	// enforcement will kick in
	graph := newTestGraph(
		[]*dotparser.Node{
			newNode("start", strAttr("shape", "Mdiamond")),
			newNode("critical",
				strAttr("type", "failing"),
				boolAttr("goal_gate", true),
			),
			newNode("exit", strAttr("shape", "Msquare")),
		},
		[]*dotparser.Edge{
			newEdge("start", "critical"),
			newEdge("critical", "exit", strAttr("condition", "outcome=fail")), // Fail edge to reach exit
		},
		nil,
	)

	_, err := Run(graph, &RunConfig{Registry: registry})

	// Should fail because goal gate is unsatisfied at exit and no retry target
	require.Error(t, err)
	assert.Contains(t, err.Error(), "goal gate")
}

func TestRun_GoalGateAllowsExitWhenSatisfied(t *testing.T) {
	handler := &MockHandler{
		Outcomes: []*Outcome{
			Success().WithNotes("gate passed"),
		},
	}

	registry := DefaultRegistry()
	registry.Register("passing", handler)

	graph := newTestGraph(
		[]*dotparser.Node{
			newNode("start", strAttr("shape", "Mdiamond")),
			newNode("critical",
				strAttr("type", "passing"),
				boolAttr("goal_gate", true),
			),
			newNode("exit", strAttr("shape", "Msquare")),
		},
		[]*dotparser.Edge{
			newEdge("start", "critical"),
			newEdge("critical", "exit"),
		},
		nil,
	)

	result, err := Run(graph, &RunConfig{Registry: registry})

	require.NoError(t, err)
	assert.Equal(t, StatusSuccess, result.FinalOutcome.Status)
}

func TestRun_GoalGatePartialSuccessAllowed(t *testing.T) {
	handler := &MockHandler{
		Outcomes: []*Outcome{
			PartialSuccess("mostly done"),
		},
	}

	registry := DefaultRegistry()
	registry.Register("partial", handler)

	graph := newTestGraph(
		[]*dotparser.Node{
			newNode("start", strAttr("shape", "Mdiamond")),
			newNode("critical",
				strAttr("type", "partial"),
				boolAttr("goal_gate", true),
			),
			newNode("exit", strAttr("shape", "Msquare")),
		},
		[]*dotparser.Edge{
			newEdge("start", "critical"),
			newEdge("critical", "exit"),
		},
		nil,
	)

	result, err := Run(graph, &RunConfig{Registry: registry})

	require.NoError(t, err)
	assert.Equal(t, StatusPartialSuccess, result.FinalOutcome.Status)
}

func TestRun_GoalGateJumpsToRetryTarget(t *testing.T) {
	sleeper := &mockSleeper{}
	callCount := int32(0)

	handler := HandlerFunc(func(node *dotparser.Node, ctx *Context, graph *dotparser.Graph, logsRoot string) (*Outcome, error) {
		count := atomic.AddInt32(&callCount, 1)
		if count == 1 {
			return Fail("first attempt fails"), nil
		}
		return Success(), nil
	})

	registry := DefaultRegistry()
	registry.Register("retryable", handler)

	graph := newTestGraph(
		[]*dotparser.Node{
			newNode("start", strAttr("shape", "Mdiamond")),
			newNode("critical",
				strAttr("type", "retryable"),
				boolAttr("goal_gate", true),
				strAttr("retry_target", "critical"), // Retry itself
			),
			newNode("exit", strAttr("shape", "Msquare")),
		},
		[]*dotparser.Edge{
			newEdge("start", "critical"),
			newEdge("critical", "exit"),
		},
		nil,
	)

	result, err := Run(graph, &RunConfig{
		Registry: registry,
		Sleeper:  sleeper,
	})

	require.NoError(t, err)
	assert.Equal(t, StatusSuccess, result.FinalOutcome.Status)
	assert.Equal(t, int32(2), callCount, "should have been called twice due to goal gate retry")
}

func TestRun_GoalGateUsesGraphRetryTarget(t *testing.T) {
	sleeper := &mockSleeper{}
	callCount := int32(0)

	handler := HandlerFunc(func(node *dotparser.Node, ctx *Context, graph *dotparser.Graph, logsRoot string) (*Outcome, error) {
		count := atomic.AddInt32(&callCount, 1)
		if count == 1 {
			return Fail("first attempt fails"), nil
		}
		return Success(), nil
	})

	registry := DefaultRegistry()
	registry.Register("retryable", handler)

	graph := newTestGraph(
		[]*dotparser.Node{
			newNode("start", strAttr("shape", "Mdiamond")),
			newNode("critical",
				strAttr("type", "retryable"),
				boolAttr("goal_gate", true),
				// No node-level retry_target
			),
			newNode("exit", strAttr("shape", "Msquare")),
		},
		[]*dotparser.Edge{
			newEdge("start", "critical"),
			newEdge("critical", "exit"),
		},
		[]dotparser.Attr{
			strAttr("retry_target", "critical"), // Graph-level retry target
		},
	)

	result, err := Run(graph, &RunConfig{
		Registry: registry,
		Sleeper:  sleeper,
	})

	require.NoError(t, err)
	assert.Equal(t, StatusSuccess, result.FinalOutcome.Status)
}

func TestRun_GoalGateFailsWhenNoRetryTarget(t *testing.T) {
	handler := &MockHandler{
		Outcomes: []*Outcome{
			Fail("permanent failure"),
		},
	}

	registry := DefaultRegistry()
	registry.Register("failing", handler)

	// Use a fail edge so the pipeline can reach the exit node
	graph := newTestGraph(
		[]*dotparser.Node{
			newNode("start", strAttr("shape", "Mdiamond")),
			newNode("critical",
				strAttr("type", "failing"),
				boolAttr("goal_gate", true),
				// No retry_target
			),
			newNode("exit", strAttr("shape", "Msquare")),
		},
		[]*dotparser.Edge{
			newEdge("start", "critical"),
			newEdge("critical", "exit", strAttr("condition", "outcome=fail")), // Fail edge to reach exit
		},
		nil,
	)

	_, err := Run(graph, &RunConfig{Registry: registry})

	require.Error(t, err)
	assert.Contains(t, err.Error(), "goal gate")
	assert.Contains(t, err.Error(), "no retry target")
}

// --- Failure Routing Tests ---

func TestRun_FailEdgeTakenOnFail(t *testing.T) {
	handler := &MockHandler{
		Outcomes: []*Outcome{
			Fail("task failed"),
		},
	}

	recovery := &MockHandler{
		Outcomes: []*Outcome{
			Success().WithNotes("recovered"),
		},
	}

	registry := DefaultRegistry()
	registry.Register("failing", handler)
	registry.Register("recovery", recovery)

	graph := newTestGraph(
		[]*dotparser.Node{
			newNode("start", strAttr("shape", "Mdiamond")),
			newNode("task", strAttr("type", "failing")),
			newNode("recovery", strAttr("type", "recovery")),
			newNode("exit", strAttr("shape", "Msquare")),
		},
		[]*dotparser.Edge{
			newEdge("start", "task"),
			newEdge("task", "exit", strAttr("condition", "outcome=success")),
			newEdge("task", "recovery", strAttr("condition", "outcome=fail")),
			newEdge("recovery", "exit"),
		},
		nil,
	)

	result, err := Run(graph, &RunConfig{Registry: registry})

	require.NoError(t, err)
	assert.Contains(t, result.CompletedNodes, "recovery")
	assert.Equal(t, StatusSuccess, result.FinalOutcome.Status)
}

func TestRun_RetryTargetUsedWhenNoFailEdge(t *testing.T) {
	sleeper := &mockSleeper{}
	callCount := int32(0)

	handler := HandlerFunc(func(node *dotparser.Node, ctx *Context, graph *dotparser.Graph, logsRoot string) (*Outcome, error) {
		count := atomic.AddInt32(&callCount, 1)
		if count == 1 {
			return Fail("first attempt fails"), nil
		}
		return Success(), nil
	})

	registry := DefaultRegistry()
	registry.Register("retryable", handler)

	graph := newTestGraph(
		[]*dotparser.Node{
			newNode("start", strAttr("shape", "Mdiamond")),
			newNode("task",
				strAttr("type", "retryable"),
				strAttr("retry_target", "task"), // Retry itself
			),
			newNode("exit", strAttr("shape", "Msquare")),
		},
		[]*dotparser.Edge{
			newEdge("start", "task"),
			newEdge("task", "exit"), // No fail edge, just normal flow
		},
		nil,
	)

	result, err := Run(graph, &RunConfig{
		Registry: registry,
		Sleeper:  sleeper,
	})

	require.NoError(t, err)
	assert.Equal(t, StatusSuccess, result.FinalOutcome.Status)
	assert.Equal(t, int32(2), callCount)
}

func TestRun_FallbackRetryTargetUsed(t *testing.T) {
	sleeper := &mockSleeper{}
	callCount := int32(0)

	handler := HandlerFunc(func(node *dotparser.Node, ctx *Context, graph *dotparser.Graph, logsRoot string) (*Outcome, error) {
		count := atomic.AddInt32(&callCount, 1)
		if count == 1 {
			return Fail("first attempt fails"), nil
		}
		return Success(), nil
	})

	registry := DefaultRegistry()
	registry.Register("retryable", handler)

	graph := newTestGraph(
		[]*dotparser.Node{
			newNode("start", strAttr("shape", "Mdiamond")),
			newNode("task",
				strAttr("type", "retryable"),
				strAttr("fallback_retry_target", "task"), // Use fallback
			),
			newNode("exit", strAttr("shape", "Msquare")),
		},
		[]*dotparser.Edge{
			newEdge("start", "task"),
			newEdge("task", "exit"),
		},
		nil,
	)

	result, err := Run(graph, &RunConfig{
		Registry: registry,
		Sleeper:  sleeper,
	})

	require.NoError(t, err)
	assert.Equal(t, StatusSuccess, result.FinalOutcome.Status)
	assert.Equal(t, int32(2), callCount)
}

// --- Integration Test: Pipeline With Retry That Eventually Succeeds ---

func TestRun_IntegrationRetryEventuallySucceeds(t *testing.T) {
	sleeper := &mockSleeper{}

	// Simulates a flaky service that fails twice then succeeds
	callCount := int32(0)
	flakyHandler := HandlerFunc(func(node *dotparser.Node, ctx *Context, graph *dotparser.Graph, logsRoot string) (*Outcome, error) {
		count := atomic.AddInt32(&callCount, 1)
		switch count {
		case 1:
			return nil, errors.New("network timeout")
		case 2:
			return Retry("rate limited"), nil
		default:
			return Success().WithContextUpdate("result", "data"), nil
		}
	})

	registry := DefaultRegistry()
	registry.Register("flaky", flakyHandler)

	graph := newTestGraph(
		[]*dotparser.Node{
			newNode("start", strAttr("shape", "Mdiamond")),
			newNode("fetch",
				strAttr("type", "flaky"),
				intAttr("max_retries", 5),
				boolAttr("goal_gate", true),
			),
			newNode("process", strAttr("type", "start")), // Use start handler as no-op
			newNode("exit", strAttr("shape", "Msquare")),
		},
		[]*dotparser.Edge{
			newEdge("start", "fetch"),
			newEdge("fetch", "process"),
			newEdge("process", "exit"),
		},
		nil,
	)

	result, err := Run(graph, &RunConfig{
		Registry: registry,
		Sleeper:  sleeper,
	})

	require.NoError(t, err)
	assert.Equal(t, []string{"start", "fetch", "process"}, result.CompletedNodes)
	assert.Equal(t, StatusSuccess, result.FinalOutcome.Status)
	assert.Equal(t, int32(3), callCount, "should have 3 calls: error, retry, success")
	assert.Len(t, sleeper.Calls, 2, "should have 2 sleeps before successful attempt")

	// Verify context was updated on success
	val, ok := result.Context.Get("result")
	assert.True(t, ok)
	assert.Equal(t, "data", val)
}

// --- checkGoalGates Unit Tests ---

func TestCheckGoalGates_NoGoalGates(t *testing.T) {
	graph := newTestGraph(
		[]*dotparser.Node{
			newNode("A"),
			newNode("B"),
		},
		nil,
		nil,
	)

	outcomes := map[string]*Outcome{
		"A": Success(),
		"B": Fail("failed"),
	}

	ok, failed := checkGoalGates(graph, outcomes)
	assert.True(t, ok)
	assert.Nil(t, failed)
}

func TestCheckGoalGates_AllSatisfied(t *testing.T) {
	graph := newTestGraph(
		[]*dotparser.Node{
			newNode("A", boolAttr("goal_gate", true)),
			newNode("B", boolAttr("goal_gate", true)),
		},
		nil,
		nil,
	)

	outcomes := map[string]*Outcome{
		"A": Success(),
		"B": PartialSuccess("partial"),
	}

	ok, failed := checkGoalGates(graph, outcomes)
	assert.True(t, ok)
	assert.Nil(t, failed)
}

func TestCheckGoalGates_OneFailed(t *testing.T) {
	graph := newTestGraph(
		[]*dotparser.Node{
			newNode("A", boolAttr("goal_gate", true)),
			newNode("B", boolAttr("goal_gate", true)),
		},
		nil,
		nil,
	)

	outcomes := map[string]*Outcome{
		"A": Success(),
		"B": Fail("failed"),
	}

	ok, failed := checkGoalGates(graph, outcomes)
	assert.False(t, ok)
	assert.NotNil(t, failed)
	assert.Equal(t, "B", failed.ID)
}

// --- getRetryTarget Unit Tests ---

func TestGetRetryTarget_NodeLevel(t *testing.T) {
	node := newNode("test",
		strAttr("retry_target", "node_target"),
		strAttr("fallback_retry_target", "node_fallback"),
	)
	graph := newTestGraph([]*dotparser.Node{node}, nil, []dotparser.Attr{
		strAttr("retry_target", "graph_target"),
	})

	target := getRetryTarget(node, graph)
	assert.Equal(t, "node_target", target)
}

func TestGetRetryTarget_NodeFallback(t *testing.T) {
	node := newNode("test",
		strAttr("fallback_retry_target", "node_fallback"),
	)
	graph := newTestGraph([]*dotparser.Node{node}, nil, []dotparser.Attr{
		strAttr("retry_target", "graph_target"),
	})

	target := getRetryTarget(node, graph)
	assert.Equal(t, "node_fallback", target)
}

func TestGetRetryTarget_GraphLevel(t *testing.T) {
	node := newNode("test")
	graph := newTestGraph([]*dotparser.Node{node}, nil, []dotparser.Attr{
		strAttr("retry_target", "graph_target"),
	})

	target := getRetryTarget(node, graph)
	assert.Equal(t, "graph_target", target)
}

func TestGetRetryTarget_GraphFallback(t *testing.T) {
	node := newNode("test")
	graph := newTestGraph([]*dotparser.Node{node}, nil, []dotparser.Attr{
		strAttr("fallback_retry_target", "graph_fallback"),
	})

	target := getRetryTarget(node, graph)
	assert.Equal(t, "graph_fallback", target)
}

func TestGetRetryTarget_NoTarget(t *testing.T) {
	node := newNode("test")
	graph := newTestGraph([]*dotparser.Node{node}, nil, nil)

	target := getRetryTarget(node, graph)
	assert.Equal(t, "", target)
}
