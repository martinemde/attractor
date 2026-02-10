package pipeline

import (
	"fmt"
	"math"
	"math/rand/v2"
	"time"

	"github.com/martinemde/attractor/dotparser"
)

// RetryPolicy configures the retry behavior for a node.
type RetryPolicy struct {
	// MaxAttempts is the maximum number of execution attempts.
	// Minimum value is 1 (1 = no retries, just the initial attempt).
	MaxAttempts int

	// Backoff configures the delay between retry attempts.
	Backoff BackoffConfig

	// ShouldRetry is a predicate that determines whether an error is retryable.
	// If nil, all errors are considered retryable.
	ShouldRetry func(error) bool
}

// BackoffConfig configures exponential backoff with optional jitter.
type BackoffConfig struct {
	// InitialDelayMs is the first retry delay in milliseconds.
	// Default: 200
	InitialDelayMs int

	// BackoffFactor is the multiplier for subsequent delays.
	// Default: 2.0
	BackoffFactor float64

	// MaxDelayMs is the maximum delay in milliseconds.
	// Default: 60000 (60 seconds)
	MaxDelayMs int

	// Jitter adds random variance to prevent thundering herd.
	// When true, delay is multiplied by a random factor in [0.5, 1.5).
	// Default: true
	Jitter bool
}

// DelayForAttempt calculates the delay before the given attempt.
// Attempt is 1-indexed (first retry is attempt=1).
func (c BackoffConfig) DelayForAttempt(attempt int) time.Duration {
	if attempt < 1 {
		attempt = 1
	}

	initialDelay := c.InitialDelayMs
	if initialDelay <= 0 {
		initialDelay = 200
	}

	factor := c.BackoffFactor
	if factor <= 0 {
		factor = 2.0
	}

	maxDelay := c.MaxDelayMs
	if maxDelay <= 0 {
		maxDelay = 60000
	}

	// Calculate delay: initial_delay * (factor ^ (attempt - 1))
	delay := float64(initialDelay) * math.Pow(factor, float64(attempt-1))

	// Cap at max delay
	if delay > float64(maxDelay) {
		delay = float64(maxDelay)
	}

	// Apply jitter: multiply by random value in [0.5, 1.5)
	if c.Jitter {
		jitterFactor := 0.5 + rand.Float64() // [0.5, 1.5)
		delay *= jitterFactor
	}

	return time.Duration(delay) * time.Millisecond
}

// NoRetryPolicy returns a policy that does not retry.
// MaxAttempts=1 means only the initial attempt, no retries.
func NoRetryPolicy() *RetryPolicy {
	return &RetryPolicy{
		MaxAttempts: 1,
		Backoff:     BackoffConfig{},
		ShouldRetry: nil,
	}
}

// StandardRetryPolicy returns a general-purpose retry policy.
// MaxAttempts=5, 200ms initial delay, factor=2.0, jitter=true.
// Delays: 200, 400, 800, 1600, 3200ms (before jitter).
func StandardRetryPolicy() *RetryPolicy {
	return &RetryPolicy{
		MaxAttempts: 5,
		Backoff: BackoffConfig{
			InitialDelayMs: 200,
			BackoffFactor:  2.0,
			MaxDelayMs:     60000,
			Jitter:         true,
		},
		ShouldRetry: DefaultShouldRetry,
	}
}

// AggressiveRetryPolicy returns a policy for unreliable operations.
// MaxAttempts=5, 500ms initial delay, factor=2.0, jitter=true.
// Delays: 500, 1000, 2000, 4000, 8000ms (before jitter).
func AggressiveRetryPolicy() *RetryPolicy {
	return &RetryPolicy{
		MaxAttempts: 5,
		Backoff: BackoffConfig{
			InitialDelayMs: 500,
			BackoffFactor:  2.0,
			MaxDelayMs:     60000,
			Jitter:         true,
		},
		ShouldRetry: DefaultShouldRetry,
	}
}

// LinearRetryPolicy returns a policy with fixed delay between attempts.
// MaxAttempts=3, 500ms initial delay, factor=1.0, jitter=true.
// Delays: 500, 500, 500ms (before jitter).
func LinearRetryPolicy() *RetryPolicy {
	return &RetryPolicy{
		MaxAttempts: 3,
		Backoff: BackoffConfig{
			InitialDelayMs: 500,
			BackoffFactor:  1.0,
			MaxDelayMs:     60000,
			Jitter:         true,
		},
		ShouldRetry: DefaultShouldRetry,
	}
}

// PatientRetryPolicy returns a policy for long-running operations.
// MaxAttempts=3, 2000ms initial delay, factor=3.0, jitter=true.
// Delays: 2000, 6000, 18000ms (before jitter).
func PatientRetryPolicy() *RetryPolicy {
	return &RetryPolicy{
		MaxAttempts: 3,
		Backoff: BackoffConfig{
			InitialDelayMs: 2000,
			BackoffFactor:  3.0,
			MaxDelayMs:     60000,
			Jitter:         true,
		},
		ShouldRetry: DefaultShouldRetry,
	}
}

// DefaultShouldRetry is the default predicate for determining retryable errors.
// It returns true for all errors, allowing retry logic to proceed.
// More sophisticated implementations could check for specific error types
// like network errors, rate limits (HTTP 429), or server errors (HTTP 5xx).
func DefaultShouldRetry(err error) bool {
	return err != nil
}

// BuildRetryPolicy constructs a RetryPolicy from node and graph attributes.
// Resolution order:
//  1. Node attribute `max_retries` -> MaxAttempts = max_retries + 1
//  2. Graph attribute `default_max_retry` -> MaxAttempts = default_max_retry + 1
//  3. Built-in default: 0 retries (MaxAttempts = 1)
func BuildRetryPolicy(node *dotparser.Node, graph *dotparser.Graph) *RetryPolicy {
	maxAttempts := 1 // Default: no retries

	// Check node attribute first
	if maxRetriesAttr, ok := node.Attr("max_retries"); ok {
		maxAttempts = int(maxRetriesAttr.Int) + 1
	} else if graph != nil {
		// Fall back to graph-level default
		if defaultMaxRetry, ok := graph.GraphAttr("default_max_retry"); ok {
			maxAttempts = int(defaultMaxRetry.Int) + 1
		}
	}

	// Ensure minimum of 1 attempt
	if maxAttempts < 1 {
		maxAttempts = 1
	}

	return &RetryPolicy{
		MaxAttempts: maxAttempts,
		Backoff: BackoffConfig{
			InitialDelayMs: 200,
			BackoffFactor:  2.0,
			MaxDelayMs:     60000,
			Jitter:         true,
		},
		ShouldRetry: DefaultShouldRetry,
	}
}

// retryCountKey returns the context key for tracking retry count for a node.
func retryCountKey(nodeID string) string {
	return fmt.Sprintf("internal.retry_count.%s", nodeID)
}

// GetRetryCount retrieves the current retry count for a node from context.
func GetRetryCount(ctx *Context, nodeID string) int {
	if v, ok := ctx.Get(retryCountKey(nodeID)); ok {
		if count, ok := v.(int); ok {
			return count
		}
	}
	return 0
}

// SetRetryCount sets the retry count for a node in context.
func SetRetryCount(ctx *Context, nodeID string, count int) {
	ctx.Set(retryCountKey(nodeID), count)
}

// IncrementRetryCount increments and returns the new retry count for a node.
func IncrementRetryCount(ctx *Context, nodeID string) int {
	count := GetRetryCount(ctx, nodeID) + 1
	SetRetryCount(ctx, nodeID, count)
	return count
}

// ResetRetryCount resets the retry count for a node to zero.
func ResetRetryCount(ctx *Context, nodeID string) {
	ctx.Set(retryCountKey(nodeID), 0)
}

// Sleeper is an interface for sleeping, allowing tests to inject a mock.
type Sleeper interface {
	Sleep(d time.Duration)
}

// RealSleeper implements Sleeper using time.Sleep.
type RealSleeper struct{}

// Sleep pauses execution for the specified duration.
func (RealSleeper) Sleep(d time.Duration) {
	time.Sleep(d)
}

// DefaultSleeper is the default Sleeper used by the engine.
var DefaultSleeper Sleeper = RealSleeper{}
