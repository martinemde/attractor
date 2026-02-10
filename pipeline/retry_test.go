package pipeline

import (
	"errors"
	"testing"
	"time"

	"github.com/martinemde/attractor/dotparser"
	"github.com/stretchr/testify/assert"
)

func TestBackoffConfig_DelayForAttempt(t *testing.T) {
	bc := BackoffConfig{
		InitialDelay: 200 * time.Millisecond,
		Factor:       2.0,
		MaxDelay:     60 * time.Second,
		Jitter:       false,
	}

	// Without jitter, delays should be deterministic
	assert.Equal(t, 200*time.Millisecond, bc.DelayForAttempt(1))
	assert.Equal(t, 400*time.Millisecond, bc.DelayForAttempt(2))
	assert.Equal(t, 800*time.Millisecond, bc.DelayForAttempt(3))
	assert.Equal(t, 1600*time.Millisecond, bc.DelayForAttempt(4))
}

func TestBackoffConfig_MaxDelayCap(t *testing.T) {
	bc := BackoffConfig{
		InitialDelay: 30 * time.Second,
		Factor:       3.0,
		MaxDelay:     60 * time.Second,
		Jitter:       false,
	}

	// attempt=2: 30s * 3.0 = 90s, capped to 60s
	assert.Equal(t, 60*time.Second, bc.DelayForAttempt(2))
}

func TestBackoffConfig_Jitter(t *testing.T) {
	bc := BackoffConfig{
		InitialDelay: 1 * time.Second,
		Factor:       1.0,
		MaxDelay:     60 * time.Second,
		Jitter:       true,
	}

	// With jitter, delay should be in range [0.5s, 1.5s]
	for i := 0; i < 100; i++ {
		d := bc.DelayForAttempt(1)
		assert.GreaterOrEqual(t, d, 500*time.Millisecond)
		assert.LessOrEqual(t, d, 1500*time.Millisecond)
	}
}

func TestBackoffConfig_ZeroInitialDelay(t *testing.T) {
	bc := BackoffConfig{}
	assert.Equal(t, time.Duration(0), bc.DelayForAttempt(1))
}

func TestBuildRetryPolicy_NodeAttribute(t *testing.T) {
	node := makeNode("n", makeIntAttr("max_retries", 3))
	graph := &dotparser.Graph{}

	policy := BuildRetryPolicy(node, graph)
	assert.Equal(t, 4, policy.MaxAttempts) // max_retries=3 -> 4 attempts
}

func TestBuildRetryPolicy_GraphDefault(t *testing.T) {
	node := makeNode("n")
	graph := &dotparser.Graph{
		GraphAttrs: []dotparser.Attr{makeIntAttr("default_max_retry", 5)},
	}

	policy := BuildRetryPolicy(node, graph)
	assert.Equal(t, 6, policy.MaxAttempts) // default_max_retry=5 -> 6 attempts
}

func TestBuildRetryPolicy_NoRetries(t *testing.T) {
	node := makeNode("n")
	graph := &dotparser.Graph{}

	policy := BuildRetryPolicy(node, graph)
	assert.Equal(t, 1, policy.MaxAttempts) // default: 0 retries -> 1 attempt
}

func TestExecuteWithRetry_SuccessOnFirstAttempt(t *testing.T) {
	handler := newRecordingHandler(&Outcome{Status: StatusSuccess, Notes: "ok"})
	sleeper := &noopSleeper{}
	counters := make(map[string]int)

	outcome := ExecuteWithRetry(handler, makeNode("n"), NewContext(), &dotparser.Graph{}, "", RetryPolicy{MaxAttempts: 3}, counters, sleeper)
	assert.Equal(t, StatusSuccess, outcome.Status)
	assert.Len(t, handler.calls, 1)
	assert.Equal(t, 0, sleeper.calls) // no sleep needed
}

func TestExecuteWithRetry_RetryThenSuccess(t *testing.T) {
	handler := newRecordingHandler(
		&Outcome{Status: StatusRetry},
		&Outcome{Status: StatusRetry},
		&Outcome{Status: StatusSuccess, Notes: "finally"},
	)
	sleeper := &noopSleeper{}
	counters := make(map[string]int)
	policy := RetryPolicy{
		MaxAttempts: 5,
		Backoff:     BackoffConfig{InitialDelay: 100 * time.Millisecond, Factor: 2.0, MaxDelay: 60 * time.Second, Jitter: false},
	}

	outcome := ExecuteWithRetry(handler, makeNode("n"), NewContext(), &dotparser.Graph{}, "", policy, counters, sleeper)
	assert.Equal(t, StatusSuccess, outcome.Status)
	assert.Len(t, handler.calls, 3)
	assert.Equal(t, 2, sleeper.calls)
}

func TestExecuteWithRetry_RetriesExhausted(t *testing.T) {
	handler := newRecordingHandler(
		&Outcome{Status: StatusRetry},
		&Outcome{Status: StatusRetry},
		&Outcome{Status: StatusRetry},
	)
	sleeper := &noopSleeper{}
	counters := make(map[string]int)
	policy := RetryPolicy{
		MaxAttempts: 3,
		Backoff:     BackoffConfig{InitialDelay: 100 * time.Millisecond, Factor: 1.0, MaxDelay: 60 * time.Second, Jitter: false},
	}

	outcome := ExecuteWithRetry(handler, makeNode("n"), NewContext(), &dotparser.Graph{}, "", policy, counters, sleeper)
	assert.Equal(t, StatusFail, outcome.Status)
	assert.Contains(t, outcome.FailureReason, "max retries exceeded")
}

func TestExecuteWithRetry_AllowPartialOnExhaustion(t *testing.T) {
	handler := newRecordingHandler(
		&Outcome{Status: StatusRetry},
	)
	sleeper := &noopSleeper{}
	counters := make(map[string]int)
	node := makeNode("n", makeBoolAttr("allow_partial", true))
	policy := RetryPolicy{MaxAttempts: 1}

	outcome := ExecuteWithRetry(handler, node, NewContext(), &dotparser.Graph{}, "", policy, counters, sleeper)
	assert.Equal(t, StatusPartialSuccess, outcome.Status)
	assert.Contains(t, outcome.Notes, "retries exhausted, partial accepted")
}

func TestExecuteWithRetry_HandlerError(t *testing.T) {
	handler := &failingHandler{err: errors.New("boom")}
	sleeper := &noopSleeper{}
	counters := make(map[string]int)
	policy := RetryPolicy{MaxAttempts: 1, ShouldRetry: func(err error) bool { return true }}

	outcome := ExecuteWithRetry(handler, makeNode("n"), NewContext(), &dotparser.Graph{}, "", policy, counters, sleeper)
	assert.Equal(t, StatusFail, outcome.Status)
	assert.Equal(t, "boom", outcome.FailureReason)
}

func TestExecuteWithRetry_HandlerErrorWithRetry(t *testing.T) {
	callCount := 0
	handler := HandlerFunc(func(node *dotparser.Node, ctx *Context, graph *dotparser.Graph, logsRoot string) (*Outcome, error) {
		callCount++
		if callCount < 3 {
			return nil, errors.New("transient")
		}
		return &Outcome{Status: StatusSuccess}, nil
	})
	sleeper := &noopSleeper{}
	counters := make(map[string]int)
	policy := RetryPolicy{
		MaxAttempts: 5,
		Backoff:     BackoffConfig{InitialDelay: 100 * time.Millisecond, Factor: 1.0, MaxDelay: 60 * time.Second, Jitter: false},
		ShouldRetry: func(err error) bool { return true },
	}

	outcome := ExecuteWithRetry(handler, makeNode("n"), NewContext(), &dotparser.Graph{}, "", policy, counters, sleeper)
	assert.Equal(t, StatusSuccess, outcome.Status)
	assert.Equal(t, 3, callCount)
}

func TestExecuteWithRetry_ImmediateFail(t *testing.T) {
	handler := newRecordingHandler(&Outcome{Status: StatusFail, FailureReason: "permanent error"})
	sleeper := &noopSleeper{}
	counters := make(map[string]int)
	policy := RetryPolicy{MaxAttempts: 5}

	outcome := ExecuteWithRetry(handler, makeNode("n"), NewContext(), &dotparser.Graph{}, "", policy, counters, sleeper)
	assert.Equal(t, StatusFail, outcome.Status)
	assert.Len(t, handler.calls, 1) // No retry on FAIL status
}

func TestPresetPolicies(t *testing.T) {
	assert.Equal(t, 1, RetryNone.MaxAttempts)
	assert.Equal(t, 5, RetryStandard.MaxAttempts)
	assert.Equal(t, 200*time.Millisecond, RetryStandard.Backoff.InitialDelay)
	assert.Equal(t, 5, RetryAggressive.MaxAttempts)
	assert.Equal(t, 500*time.Millisecond, RetryAggressive.Backoff.InitialDelay)
	assert.Equal(t, 3, RetryLinear.MaxAttempts)
	assert.Equal(t, 1.0, RetryLinear.Backoff.Factor)
	assert.Equal(t, 3, RetryPatient.MaxAttempts)
	assert.Equal(t, 2*time.Second, RetryPatient.Backoff.InitialDelay)
}
