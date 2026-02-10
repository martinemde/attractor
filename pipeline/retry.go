package pipeline

import (
	"math"
	"math/rand/v2"
	"strconv"
	"time"

	"github.com/martinemde/attractor/dotparser"
)

// Sleeper is an interface for sleeping, allowing tests to override delays.
type Sleeper interface {
	Sleep(d time.Duration)
}

// realSleeper implements Sleeper using time.Sleep.
type realSleeper struct{}

func (realSleeper) Sleep(d time.Duration) { time.Sleep(d) }

// DefaultSleeper is the production sleeper.
var DefaultSleeper Sleeper = realSleeper{}

// DelayForAttempt calculates the delay for a given retry attempt (1-indexed).
func (bc BackoffConfig) DelayForAttempt(attempt int) time.Duration {
	if bc.InitialDelay == 0 {
		return 0
	}
	delay := float64(bc.InitialDelay) * math.Pow(bc.Factor, float64(attempt-1))
	if delay > float64(bc.MaxDelay) {
		delay = float64(bc.MaxDelay)
	}
	if bc.Jitter {
		// jitter: delay * uniform(0.5, 1.5)
		delay = delay * (0.5 + rand.Float64())
	}
	return time.Duration(delay)
}

// BuildRetryPolicy constructs a RetryPolicy for a node from its attributes
// and graph-level defaults. Per spec Section 3.5:
//
//	1. Node attribute max_retries (if set)
//	2. Graph attribute default_max_retry (fallback)
//	3. Built-in default: 0 (no retries)
func BuildRetryPolicy(node *dotparser.Node, graph *dotparser.Graph) RetryPolicy {
	maxRetries := 0

	if v, ok := node.Attr("max_retries"); ok {
		maxRetries = int(v.Int)
	} else if v, ok := graph.GraphAttr("default_max_retry"); ok {
		maxRetries = int(v.Int)
	}

	if maxRetries < 0 {
		maxRetries = 0
	}

	return RetryPolicy{
		MaxAttempts: maxRetries + 1, // max_retries=3 means 4 total attempts
		Backoff: BackoffConfig{
			InitialDelay: 200 * time.Millisecond,
			Factor:       2.0,
			MaxDelay:     60 * time.Second,
			Jitter:       true,
		},
		ShouldRetry: DefaultShouldRetry,
	}
}

// DefaultShouldRetry returns true for errors that are considered transient.
func DefaultShouldRetry(_ error) bool {
	return true
}

// ExecuteWithRetry runs a handler with the configured retry policy.
// It handles RETRY outcomes, exceptions, and backoff delays.
// The retryCounters map tracks per-node retry counts for checkpointing.
func ExecuteWithRetry(
	handler Handler,
	node *dotparser.Node,
	ctx *Context,
	graph *dotparser.Graph,
	logsRoot string,
	policy RetryPolicy,
	retryCounters map[string]int,
	sleeper Sleeper,
) *Outcome {
	for attempt := 1; attempt <= policy.MaxAttempts; attempt++ {
		outcome, err := handler.Execute(node, ctx, graph, logsRoot)
		if err != nil {
			if policy.ShouldRetry != nil && policy.ShouldRetry(err) && attempt < policy.MaxAttempts {
				delay := policy.Backoff.DelayForAttempt(attempt)
				sleeper.Sleep(delay)
				continue
			}
			return &Outcome{Status: StatusFail, FailureReason: err.Error()}
		}

		if outcome.Status.IsSuccess() {
			delete(retryCounters, node.ID)
			return outcome
		}

		if outcome.Status == StatusRetry {
			if attempt < policy.MaxAttempts {
				retryCounters[node.ID]++
				ctx.Set("internal.retry_count."+node.ID, strconv.Itoa(retryCounters[node.ID]))
				delay := policy.Backoff.DelayForAttempt(attempt)
				sleeper.Sleep(delay)
				continue
			}
			// Retries exhausted
			allowPartial := false
			if v, ok := node.Attr("allow_partial"); ok {
				allowPartial = v.Bool
			}
			if allowPartial {
				return &Outcome{
					Status: StatusPartialSuccess,
					Notes:  "retries exhausted, partial accepted",
				}
			}
			return &Outcome{Status: StatusFail, FailureReason: "max retries exceeded"}
		}

		if outcome.Status == StatusFail {
			return outcome
		}

		// SKIPPED or other status: return as-is
		return outcome
	}

	return &Outcome{Status: StatusFail, FailureReason: "max retries exceeded"}
}
