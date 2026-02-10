package pipeline

import (
	"time"

	"github.com/martinemde/attractor/dotparser"
)

// StageStatus represents the result status of executing a node handler.
type StageStatus string

const (
	StatusSuccess        StageStatus = "success"
	StatusPartialSuccess StageStatus = "partial_success"
	StatusRetry          StageStatus = "retry"
	StatusFail           StageStatus = "fail"
	StatusSkipped        StageStatus = "skipped"
)

// IsSuccess returns true for SUCCESS and PARTIAL_SUCCESS.
func (s StageStatus) IsSuccess() bool {
	return s == StatusSuccess || s == StatusPartialSuccess
}

// Outcome is the result of executing a node handler. It drives routing
// decisions and state updates.
type Outcome struct {
	Status           StageStatus       // SUCCESS, FAIL, PARTIAL_SUCCESS, RETRY, SKIPPED
	PreferredLabel   string            // which edge label to follow (optional)
	SuggestedNextIDs []string          // explicit next node IDs (optional)
	ContextUpdates   map[string]string // key-value pairs to merge into context
	Notes            string            // human-readable execution summary
	FailureReason    string            // reason for failure (when status is FAIL or RETRY)
}

// Handler is the interface that all node handlers must implement.
// The execution engine dispatches to the appropriate handler based on the
// node's type attribute or shape-based resolution.
type Handler interface {
	Execute(node *dotparser.Node, ctx *Context, graph *dotparser.Graph, logsRoot string) (*Outcome, error)
}

// HandlerFunc adapts an ordinary function to the Handler interface.
type HandlerFunc func(node *dotparser.Node, ctx *Context, graph *dotparser.Graph, logsRoot string) (*Outcome, error)

func (f HandlerFunc) Execute(node *dotparser.Node, ctx *Context, graph *dotparser.Graph, logsRoot string) (*Outcome, error) {
	return f(node, ctx, graph, logsRoot)
}

// ShapeToHandlerType maps DOT shape names to handler type strings.
// See spec Section 2.8.
var ShapeToHandlerType = map[string]string{
	"Mdiamond":      "start",
	"Msquare":       "exit",
	"box":           "codergen",
	"hexagon":       "wait.human",
	"diamond":       "conditional",
	"component":     "parallel",
	"tripleoctagon": "parallel.fan_in",
	"parallelogram": "tool",
	"house":         "stack.manager_loop",
}

// RetryPolicy controls how many times a node is retried and how delays
// are calculated between attempts.
type RetryPolicy struct {
	MaxAttempts int           // minimum 1 (1 means no retries)
	Backoff     BackoffConfig // delay calculation between retries
	ShouldRetry func(error) bool
}

// BackoffConfig controls delay calculation between retry attempts.
type BackoffConfig struct {
	InitialDelay time.Duration // first retry delay (default 200ms)
	Factor       float64       // multiplier for subsequent delays (default 2.0)
	MaxDelay     time.Duration // cap on delay (default 60s)
	Jitter       bool          // add random jitter (default true)
}

// Preset retry policies per spec Section 3.6.
var (
	RetryNone = RetryPolicy{
		MaxAttempts: 1,
	}
	RetryStandard = RetryPolicy{
		MaxAttempts: 5,
		Backoff: BackoffConfig{
			InitialDelay: 200 * time.Millisecond,
			Factor:       2.0,
			MaxDelay:     60 * time.Second,
			Jitter:       true,
		},
	}
	RetryAggressive = RetryPolicy{
		MaxAttempts: 5,
		Backoff: BackoffConfig{
			InitialDelay: 500 * time.Millisecond,
			Factor:       2.0,
			MaxDelay:     60 * time.Second,
			Jitter:       true,
		},
	}
	RetryLinear = RetryPolicy{
		MaxAttempts: 3,
		Backoff: BackoffConfig{
			InitialDelay: 500 * time.Millisecond,
			Factor:       1.0,
			MaxDelay:     60 * time.Second,
			Jitter:       true,
		},
	}
	RetryPatient = RetryPolicy{
		MaxAttempts: 3,
		Backoff: BackoffConfig{
			InitialDelay: 2 * time.Second,
			Factor:       3.0,
			MaxDelay:     60 * time.Second,
			Jitter:       true,
		},
	}
)

// Checkpoint is a serializable snapshot of execution state, saved after
// each node completes. Enables crash recovery and resume.
type Checkpoint struct {
	Timestamp      time.Time         `json:"timestamp"`
	CurrentNode    string            `json:"current_node"`
	CompletedNodes []string          `json:"completed_nodes"`
	NodeRetries    map[string]int    `json:"node_retries"`
	ContextValues  map[string]string `json:"context"`
	Logs           []string          `json:"logs"`
}

// RunConfig holds the configuration for a pipeline run.
type RunConfig struct {
	LogsRoot string // filesystem path for this run's log/artifact directory
}
