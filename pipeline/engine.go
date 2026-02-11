package pipeline

import (
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"time"

	"github.com/martinemde/attractor/dotparser"
)

// RunConfig configures a pipeline execution run.
type RunConfig struct {
	// LogsRoot is the filesystem path for this run's log/artifact directory.
	LogsRoot string

	// Registry is the handler registry to use for resolving node handlers.
	// If nil, DefaultRegistry() is used.
	Registry *HandlerRegistry

	// Interviewer is the human interaction interface for wait.human nodes.
	// If nil, human gates will fail.
	Interviewer Interviewer

	// Transforms is a list of transforms to apply to the graph before execution.
	// Transforms are applied in order before any validation or execution.
	Transforms []Transform

	// Sleeper is used for delays during retries. If nil, DefaultSleeper is used.
	// This allows tests to inject a mock sleeper to avoid actual delays.
	Sleeper Sleeper

	// EventEmitter receives pipeline events for observability.
	// If nil, no events are emitted.
	EventEmitter *EventEmitter

	// PreToolHook is a shell command to run before tool calls.
	// Tool metadata is passed via environment variables.
	PreToolHook string

	// PostToolHook is a shell command to run after tool calls.
	// Tool metadata and result are passed via environment variables.
	PostToolHook string

	// StartAt overrides the start node. If set, execution begins at this node
	// instead of the default start node (shape=Mdiamond or id=start).
	// Used by RunLoop to restart from a loop_restart target.
	StartAt string

	// MaxLoopRestarts limits the number of loop restarts when using RunLoop.
	// If zero, DefaultMaxLoopRestarts is used.
	MaxLoopRestarts int
}

// RunResult contains the results of a pipeline execution.
type RunResult struct {
	// FinalOutcome is the outcome of the last executed node.
	FinalOutcome *Outcome

	// CompletedNodes is the list of node IDs that were executed, in order.
	CompletedNodes []string

	// Context is the final state of the pipeline context.
	Context *Context

	// NodeOutcomes maps node IDs to their execution outcomes.
	NodeOutcomes map[string]*Outcome

	// LoopRestart indicates that the pipeline should restart with a fresh log directory.
	// When true, the caller should create a new log directory and call Run again.
	// Per Spec Section 2.7: "When true, terminates the current run and re-launches
	// with a fresh log directory."
	LoopRestart bool

	// LoopRestartTarget is the node ID to restart from when LoopRestart is true.
	// This is the target node of the edge that had loop_restart=true.
	LoopRestartTarget string
}

// Interviewer is the interface for human-in-the-loop interactions.
// It presents questions to humans and collects answers.
type Interviewer interface {
	// Ask presents a question and blocks until an answer is received.
	Ask(question *Question) (*Answer, error)
}

// Question represents a question to present to a human.
type Question struct {
	Text           string         // The question to present to the human
	Type           QuestionType   // Determines the UI and valid answers
	Options        []Option       // For MULTIPLE_CHOICE type
	Default        *Answer        // Default if timeout/skip
	TimeoutSeconds float64        // Max wait time (0 means no timeout)
	Stage          string         // Originating stage name (for display)
	Metadata       map[string]any // Arbitrary key-value pairs
}

// QuestionType represents the type of question.
type QuestionType string

const (
	QuestionYesNo          QuestionType = "yes_no"          // Yes/no binary choice
	QuestionMultipleChoice QuestionType = "multiple_choice" // Select one from a list
	QuestionFreeform       QuestionType = "freeform"        // Free text input
	QuestionConfirmation   QuestionType = "confirmation"    // Yes/no confirmation
)

// Option represents a choice in a multiple choice question.
type Option struct {
	Key   string // Accelerator key (e.g., "Y", "A")
	Label string // Display text (e.g., "Yes, deploy to production")
}

// Common answer values for yes/no and confirmation questions.
const (
	AnswerYes = "yes"
	AnswerNo  = "no"
)

// Answer represents a human's response to a question.
type Answer struct {
	Value          string  // The selected value (e.g., AnswerYes, option key)
	SelectedOption *Option // The full selected option (for MULTIPLE_CHOICE)
	Text           string  // Free text response (for FREEFORM)
	Skipped        bool    // Human skipped the question
	Timeout        bool    // No response within timeout
}

// DefaultMaxLoopRestarts is the maximum number of loop restarts before giving up.
const DefaultMaxLoopRestarts = 100

// RunLoop executes a pipeline with loop restart support.
// When the pipeline signals a loop restart (via a loop_restart=true edge attribute),
// RunLoop creates a fresh log directory and re-runs the pipeline from the restart
// target node. Per Spec Section 2.7: "When true, terminates the current run and
// re-launches with a fresh log directory."
func RunLoop(graph *dotparser.Graph, config *RunConfig) (*RunResult, error) {
	if config == nil {
		config = &RunConfig{}
	}

	maxRestarts := config.MaxLoopRestarts
	if maxRestarts <= 0 {
		maxRestarts = DefaultMaxLoopRestarts
	}

	baseLogsRoot := config.LogsRoot

	for iteration := 0; iteration <= maxRestarts; iteration++ {
		// Create a fresh log directory for each restart iteration
		iterConfig := *config
		if baseLogsRoot != "" && iteration > 0 {
			iterConfig.LogsRoot = filepath.Join(baseLogsRoot, fmt.Sprintf("restart-%d", iteration))
		}

		result, err := Run(graph, &iterConfig)
		if err != nil {
			return result, err
		}

		if !result.LoopRestart {
			return result, nil
		}

		// Set up for restart from the target node
		config.StartAt = result.LoopRestartTarget
	}

	return nil, fmt.Errorf("max loop restarts (%d) exceeded", maxRestarts)
}

// Run executes a pipeline graph from start to completion.
func Run(graph *dotparser.Graph, config *RunConfig) (*RunResult, error) {
	if config == nil {
		config = &RunConfig{}
	}

	// Apply default transforms (StylesheetTransform, VariableExpansionTransform)
	// followed by any user-provided transforms. Per attractor-spec.md Section 8.5,
	// the stylesheet is applied after parsing and before validation.
	allTransforms := DefaultTransforms()
	allTransforms = append(allTransforms, config.Transforms...)
	graph = ApplyTransforms(graph, allTransforms)

	ctx := NewContext()
	mirrorGraphAttributes(graph, ctx)

	// Write manifest.json at run initialization
	startTime := time.Now()
	if config.LogsRoot != "" {
		if err := writeManifest(config.LogsRoot, graph, startTime); err != nil {
			ctx.AppendLog(fmt.Sprintf("failed to write manifest: %v", err))
		}
	}

	// Find start node: use StartAt override or default resolution
	var currentNode *dotparser.Node
	if config.StartAt != "" {
		currentNode = graph.NodeByID(config.StartAt)
		if currentNode == nil {
			return nil, fmt.Errorf("start-at node %q not found in graph", config.StartAt)
		}
	} else {
		currentNode = findStartNode(graph)
		if currentNode == nil {
			return nil, errors.New("no start node found (shape=Mdiamond or id=start/Start)")
		}
	}

	// Generate a unique run ID
	runID := fmt.Sprintf("run-%d", startTime.UnixNano())

	// Emit pipeline started event
	if config.EventEmitter != nil {
		config.EventEmitter.Emit(PipelineStartedEvent(graph.Name, runID))
	}

	completedNodes := []string{}

	result, err := runFromState(graph, config, ctx, completedNodes, currentNode, startTime)

	// Emit pipeline completion event
	if config.EventEmitter != nil {
		duration := time.Since(startTime)
		if err != nil {
			config.EventEmitter.Emit(PipelineFailedEvent(err.Error(), duration))
		} else {
			artifactCount := len(result.CompletedNodes)
			config.EventEmitter.Emit(PipelineCompletedEvent(duration, artifactCount))
		}
	}

	return result, err
}

// executeWithRetry executes a handler with retry logic.
// It handles RETRY outcomes and handler errors according to the retry policy.
func executeWithRetry(
	handler Handler,
	node *dotparser.Node,
	ctx *Context,
	graph *dotparser.Graph,
	logsRoot string,
	policy *RetryPolicy,
	sleeper Sleeper,
) *Outcome {
	maxAttempts := max(policy.MaxAttempts, 1)

	for attempt := 1; attempt <= maxAttempts; attempt++ {
		// Execute the handler
		outcome, err := handler.Execute(node, ctx, graph, logsRoot)

		// Handle execution errors
		if err != nil {
			if policy.ShouldRetry != nil && !policy.ShouldRetry(err) {
				// Error is not retryable
				return Fail(err.Error())
			}
			if attempt < maxAttempts {
				// Retryable error, try again after delay
				IncrementRetryCount(ctx, node.ID)
				delay := policy.Backoff.DelayForAttempt(attempt)
				sleeper.Sleep(delay)
				continue
			}
			// Out of retries
			return Fail(fmt.Sprintf("max retries exceeded: %v", err))
		}

		// Handle nil outcome - return nil for auto_status handling in runFromState
		if outcome == nil {
			return nil
		}

		// Handle outcome-based routing
		switch outcome.Status {
		case StatusSuccess, StatusPartialSuccess:
			// Success - reset retry counter and return
			ResetRetryCount(ctx, node.ID)
			return outcome

		case StatusRetry:
			if attempt < maxAttempts {
				// Handler requested retry
				IncrementRetryCount(ctx, node.ID)
				delay := policy.Backoff.DelayForAttempt(attempt)
				sleeper.Sleep(delay)
				continue
			}
			// Out of retries
			if allowPartial, ok := node.Attr("allow_partial"); ok && allowPartial.Bool {
				return PartialSuccess("retries exhausted, partial accepted")
			}
			return Fail("max retries exceeded")

		case StatusFail:
			// Immediate failure, no retries for explicit FAIL
			return outcome

		default:
			// Unknown status, treat as success
			return outcome
		}
	}

	// Should not reach here, but return failure as fallback
	return Fail("max retries exceeded")
}

// selectNextEdgeWithFailureRouting selects the next edge using edge selection
// with special handling for FAIL outcomes per Section 3.7.
//
// For FAIL outcomes, the routing order is:
// 1. Fail edge (condition="outcome=fail")
// 2. retry_target attribute on node
// 3. fallback_retry_target attribute on node
// 4. Graph-level retry_target / fallback_retry_target
// 5. Pipeline termination (returns nil)
//
// For non-FAIL outcomes, standard edge selection is used which includes
// condition matching, preferred label, suggested next IDs, and weight-based
// selection of unconditional edges.
func selectNextEdgeWithFailureRouting(node *dotparser.Node, outcome *Outcome, ctx *Context, graph *dotparser.Graph) *dotparser.Edge {
	// For FAIL outcomes, use failure routing order (Section 3.7)
	if outcome.Status == StatusFail {
		edges := graph.EdgesFrom(node.ID)

		// Step 1: Look for fail edge (condition that matches FAIL outcome)
		for _, e := range edges {
			if cond, ok := e.Attr("condition"); ok && cond.Str != "" {
				if EvaluateCondition(cond.Str, outcome, nil) {
					return e
				}
			}
		}

		// Steps 2-4: Check retry targets (node and graph level)
		retryTarget := getRetryTarget(node, graph)
		if retryTarget != "" {
			targetNode := graph.NodeByID(retryTarget)
			if targetNode != nil {
				// Create a synthetic edge to the retry target
				return &dotparser.Edge{
					From: node.ID,
					To:   retryTarget,
				}
			}
		}

		// Step 5: No failure route found - return nil for termination
		return nil
	}

	// For non-FAIL outcomes, use standard edge selection
	return SelectEdge(node, outcome, ctx, graph)
}

// checkGoalGates checks if all goal gates in the visited nodes are satisfied.
// Returns (true, nil) if all gates are satisfied or no goal gates exist.
// Returns (false, failedNode) if a goal gate has a non-success outcome.
func checkGoalGates(graph *dotparser.Graph, nodeOutcomes map[string]*Outcome) (bool, *dotparser.Node) {
	for nodeID, outcome := range nodeOutcomes {
		node := graph.NodeByID(nodeID)
		if node == nil {
			continue
		}

		// Check if this node is a goal gate
		if goalGateAttr, ok := node.Attr("goal_gate"); ok && goalGateAttr.Bool {
			// Goal gate must have a success outcome
			if outcome.Status != StatusSuccess && outcome.Status != StatusPartialSuccess {
				return false, node
			}
		}
	}
	return true, nil
}

// getRetryTarget returns the retry target for a node.
// Resolution order:
//  1. Node attribute `retry_target`
//  2. Node attribute `fallback_retry_target`
//  3. Graph attribute `retry_target`
//  4. Graph attribute `fallback_retry_target`
func getRetryTarget(node *dotparser.Node, graph *dotparser.Graph) string {
	// Check node-level retry_target
	if rt, ok := node.Attr("retry_target"); ok && rt.Str != "" {
		return rt.Str
	}

	// Check node-level fallback_retry_target
	if frt, ok := node.Attr("fallback_retry_target"); ok && frt.Str != "" {
		return frt.Str
	}

	// Check graph-level retry_target
	if graph != nil {
		if rt, ok := graph.GraphAttr("retry_target"); ok && rt.Str != "" {
			return rt.Str
		}

		// Check graph-level fallback_retry_target
		if frt, ok := graph.GraphAttr("fallback_retry_target"); ok && frt.Str != "" {
			return frt.Str
		}
	}

	return ""
}

// findStartNode locates the pipeline entry point.
// Resolution order: (1) shape=Mdiamond, (2) id="start" or "Start"
func findStartNode(graph *dotparser.Graph) *dotparser.Node {
	// First, look for shape=Mdiamond
	for _, node := range graph.Nodes {
		if shape, ok := node.Attr("shape"); ok {
			if shape.Str == "Mdiamond" {
				return node
			}
		}
	}

	// Fallback to ID-based lookup
	if node := graph.NodeByID("start"); node != nil {
		return node
	}
	if node := graph.NodeByID("Start"); node != nil {
		return node
	}

	return nil
}

// isTerminal returns true if the node is a pipeline exit point.
func isTerminal(node *dotparser.Node) bool {
	if shape, ok := node.Attr("shape"); ok {
		return shape.Str == "Msquare"
	}
	return false
}

// mirrorGraphAttributes copies graph-level attributes into the context.
// Attributes are prefixed with "graph." (e.g., goal becomes graph.goal).
func mirrorGraphAttributes(graph *dotparser.Graph, ctx *Context) {
	for _, attr := range graph.GraphAttrs {
		key := "graph." + attr.Key
		// Store the appropriate typed value
		switch attr.Value.Kind {
		case dotparser.ValueString:
			ctx.Set(key, attr.Value.Str)
		case dotparser.ValueInt:
			ctx.Set(key, attr.Value.Int)
		case dotparser.ValueFloat:
			ctx.Set(key, attr.Value.Float)
		case dotparser.ValueBool:
			ctx.Set(key, attr.Value.Bool)
		default:
			ctx.Set(key, attr.Value.Raw)
		}
	}
}

// writeNodeStatus writes the outcome to a status.json file in the node's log directory.
func writeNodeStatus(logsRoot, nodeID string, outcome *Outcome) error {
	if logsRoot == "" {
		return nil
	}

	nodeDir := filepath.Join(logsRoot, nodeID)
	if err := os.MkdirAll(nodeDir, 0o755); err != nil {
		return fmt.Errorf("failed to create node directory: %w", err)
	}

	statusFile := filepath.Join(nodeDir, "status.json")

	// Create a serializable status structure
	status := map[string]any{
		"status": outcome.Status.String(),
	}
	if outcome.Notes != "" {
		status["notes"] = outcome.Notes
	}
	if outcome.FailureReason != "" {
		status["failure_reason"] = outcome.FailureReason
	}
	if outcome.PreferredLabel != "" {
		status["preferred_label"] = outcome.PreferredLabel
	}
	if len(outcome.SuggestedNextIDs) > 0 {
		status["suggested_next_ids"] = outcome.SuggestedNextIDs
	}
	if len(outcome.ContextUpdates) > 0 {
		status["context_updates"] = outcome.ContextUpdates
	}

	data, err := json.MarshalIndent(status, "", "  ")
	if err != nil {
		return fmt.Errorf("failed to marshal status: %w", err)
	}

	if err := os.WriteFile(statusFile, data, 0o644); err != nil {
		return fmt.Errorf("failed to write status file: %w", err)
	}

	return nil
}

// writeManifest writes the manifest.json file with pipeline metadata.
func writeManifest(logsRoot string, graph *dotparser.Graph, startTime time.Time) error {
	if err := os.MkdirAll(logsRoot, 0o755); err != nil {
		return fmt.Errorf("failed to create logs directory: %w", err)
	}

	manifest := map[string]any{
		"name":       graph.Name,
		"start_time": startTime.Format(time.RFC3339),
	}

	// Include goal if present
	if goalAttr, ok := graph.GraphAttr("goal"); ok {
		manifest["goal"] = goalAttr.Str
	}

	data, err := json.MarshalIndent(manifest, "", "  ")
	if err != nil {
		return fmt.Errorf("failed to marshal manifest: %w", err)
	}

	manifestFile := filepath.Join(logsRoot, "manifest.json")
	if err := os.WriteFile(manifestFile, data, 0o644); err != nil {
		return fmt.Errorf("failed to write manifest file: %w", err)
	}

	return nil
}
