package pipeline

import (
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/martinemde/attractor/dotparser"
)

// Checkpoint is a serializable snapshot of execution state, saved after each
// node completes. Enables crash recovery and resume.
type Checkpoint struct {
	// Timestamp is when this checkpoint was created.
	Timestamp time.Time `json:"timestamp"`

	// CurrentNode is the ID of the last completed node.
	CurrentNode string `json:"current_node"`

	// CompletedNodes is the list of all completed node IDs in order.
	CompletedNodes []string `json:"completed_nodes"`

	// NodeRetries maps node IDs to their retry counters.
	NodeRetries map[string]int `json:"node_retries"`

	// ContextValues is a serialized snapshot of the context.
	ContextValues map[string]any `json:"context"`

	// Logs are the run log entries.
	Logs []string `json:"logs"`
}

// CheckpointFileName is the name of the checkpoint file within a logs directory.
const CheckpointFileName = "checkpoint.json"

// ErrCheckpointNotFound is returned when no checkpoint file exists.
var ErrCheckpointNotFound = errors.New("checkpoint not found")

// SaveCheckpoint serializes a checkpoint to JSON and writes it to the logs directory.
func SaveCheckpoint(cp *Checkpoint, logsRoot string) error {
	if logsRoot == "" {
		return nil
	}

	if err := os.MkdirAll(logsRoot, 0o755); err != nil {
		return fmt.Errorf("failed to create logs directory: %w", err)
	}

	data, err := json.MarshalIndent(cp, "", "  ")
	if err != nil {
		return fmt.Errorf("failed to marshal checkpoint: %w", err)
	}

	checkpointPath := filepath.Join(logsRoot, CheckpointFileName)
	if err := os.WriteFile(checkpointPath, data, 0o644); err != nil {
		return fmt.Errorf("failed to write checkpoint: %w", err)
	}

	return nil
}

// LoadCheckpoint reads and deserializes a checkpoint from the logs directory.
func LoadCheckpoint(logsRoot string) (*Checkpoint, error) {
	if logsRoot == "" {
		return nil, ErrCheckpointNotFound
	}

	checkpointPath := filepath.Join(logsRoot, CheckpointFileName)
	data, err := os.ReadFile(checkpointPath)
	if err != nil {
		if os.IsNotExist(err) {
			return nil, ErrCheckpointNotFound
		}
		return nil, fmt.Errorf("failed to read checkpoint: %w", err)
	}

	var cp Checkpoint
	if err := json.Unmarshal(data, &cp); err != nil {
		return nil, fmt.Errorf("failed to unmarshal checkpoint: %w", err)
	}

	return &cp, nil
}

// buildCheckpoint creates a checkpoint from the current execution state.
func buildCheckpoint(
	currentNode string,
	completedNodes []string,
	ctx *Context,
) *Checkpoint {
	// Extract retry counters from context
	nodeRetries := extractRetryCounters(ctx)

	return &Checkpoint{
		Timestamp:      time.Now(),
		CurrentNode:    currentNode,
		CompletedNodes: append([]string(nil), completedNodes...),
		NodeRetries:    nodeRetries,
		ContextValues:  ctx.Snapshot(),
		Logs:           ctx.Logs(),
	}
}

// extractRetryCounters extracts retry counters from context.
// Keys are in the form "internal.retry_count.<node_id>".
func extractRetryCounters(ctx *Context) map[string]int {
	retries := make(map[string]int)
	snapshot := ctx.Snapshot()

	for key, value := range snapshot {
		nodeID, found := strings.CutPrefix(key, "internal.retry_count.")
		if found {
			if count, ok := value.(int); ok {
				retries[nodeID] = count
			}
		}
	}

	return retries
}

// restoreFromCheckpoint restores execution state from a checkpoint.
// It returns the restored context, completed nodes, and the next node to execute.
func restoreFromCheckpoint(cp *Checkpoint, graph *dotparser.Graph) (*Context, []string, *dotparser.Node, error) {
	// Restore context
	ctx := NewContext()
	ctx.ApplyUpdates(cp.ContextValues)

	// Restore logs
	for _, log := range cp.Logs {
		ctx.AppendLog(log)
	}

	// Restore retry counters
	for nodeID, count := range cp.NodeRetries {
		SetRetryCount(ctx, nodeID, count)
	}

	// Set fidelity degradation flag per spec Section 5.3
	ctx.Set("internal.resume_fidelity_degraded", true)

	// Find the next node to execute (the one after current_node)
	nextNode, err := findNextNodeAfter(cp.CurrentNode, graph)
	if err != nil {
		return nil, nil, nil, err
	}

	return ctx, cp.CompletedNodes, nextNode, nil
}

// findNextNodeAfter finds the next node in the graph after the given node ID.
// It follows the first outgoing edge from the current node.
func findNextNodeAfter(currentNodeID string, graph *dotparser.Graph) (*dotparser.Node, error) {
	edges := graph.EdgesFrom(currentNodeID)
	if len(edges) == 0 {
		// Current node was terminal or had no outgoing edges
		// Return the current node to re-evaluate terminal condition
		currentNode := graph.NodeByID(currentNodeID)
		if currentNode == nil {
			return nil, fmt.Errorf("checkpoint node %q not found in graph", currentNodeID)
		}
		return currentNode, nil
	}

	// Follow the first edge to the next node
	nextNodeID := edges[0].To
	nextNode := graph.NodeByID(nextNodeID)
	if nextNode == nil {
		return nil, fmt.Errorf("next node %q not found in graph", nextNodeID)
	}

	return nextNode, nil
}

// Resume loads a checkpoint and resumes pipeline execution from where it left off.
func Resume(graph *dotparser.Graph, config *RunConfig) (*RunResult, error) {
	if config == nil {
		config = &RunConfig{}
	}

	// Load checkpoint
	cp, err := LoadCheckpoint(config.LogsRoot)
	if err != nil {
		return nil, fmt.Errorf("failed to load checkpoint: %w", err)
	}

	// Restore state from checkpoint
	ctx, completedNodes, nextNode, err := restoreFromCheckpoint(cp, graph)
	if err != nil {
		return nil, fmt.Errorf("failed to restore from checkpoint: %w", err)
	}

	// Continue execution from the restored state
	return runFromState(graph, config, ctx, completedNodes, nextNode)
}

// runFromState executes the pipeline from a given state.
// This is the shared inner loop used by both Run() and Resume().
func runFromState(
	graph *dotparser.Graph,
	config *RunConfig,
	ctx *Context,
	completedNodes []string,
	currentNode *dotparser.Node,
) (*RunResult, error) {
	registry := config.Registry
	if registry == nil {
		registry = DefaultRegistry()
	}

	sleeper := config.Sleeper
	if sleeper == nil {
		sleeper = DefaultSleeper
	}

	nodeOutcomes := make(map[string]*Outcome)

	var lastOutcome *Outcome

	for {
		// Step 1: Check for terminal node with goal gate enforcement
		if isTerminal(currentNode) {
			gateOK, failedGate := checkGoalGates(graph, nodeOutcomes)
			if !gateOK && failedGate != nil {
				// Goal gate unsatisfied, try to find retry target
				retryTarget := getRetryTarget(failedGate, graph)
				if retryTarget != "" {
					nextNode := graph.NodeByID(retryTarget)
					if nextNode != nil {
						currentNode = nextNode
						continue
					}
				}
				return nil, fmt.Errorf("goal gate %q unsatisfied and no retry target available", failedGate.ID)
			}
			break
		}

		// Step 2: Resolve handler
		handler := registry.Resolve(currentNode)
		if handler == nil {
			return nil, fmt.Errorf("no handler found for node %q", currentNode.ID)
		}

		// Step 3: Execute node handler with retry policy
		retryPolicy := BuildRetryPolicy(currentNode, graph)
		outcome := executeWithRetry(handler, currentNode, ctx, graph, config.LogsRoot, retryPolicy, sleeper)

		// Step 4: Record completion
		completedNodes = append(completedNodes, currentNode.ID)
		nodeOutcomes[currentNode.ID] = outcome
		lastOutcome = outcome

		// Step 5: Apply context updates from outcome
		if outcome.ContextUpdates != nil {
			ctx.ApplyUpdates(outcome.ContextUpdates)
		}
		ctx.Set("outcome", outcome.Status.String())
		if outcome.PreferredLabel != "" {
			ctx.Set("preferred_label", outcome.PreferredLabel)
		}

		// Step 6: Write status.json
		if config.LogsRoot != "" {
			if err := writeNodeStatus(config.LogsRoot, currentNode.ID, outcome); err != nil {
				// Log error but don't fail the pipeline
				ctx.AppendLog(fmt.Sprintf("failed to write status for %s: %v", currentNode.ID, err))
			}
		}

		// Step 7: Save checkpoint after each node completes
		if config.LogsRoot != "" {
			cp := buildCheckpoint(currentNode.ID, completedNodes, ctx)
			if err := SaveCheckpoint(cp, config.LogsRoot); err != nil {
				ctx.AppendLog(fmt.Sprintf("failed to save checkpoint: %v", err))
			}
		}

		// Clear fidelity degradation flag after first resumed node completes
		ctx.Set("internal.resume_fidelity_degraded", false)

		// Step 8: Select next edge with failure routing
		nextEdge := selectNextEdgeWithFailureRouting(currentNode, outcome, ctx, graph)
		if nextEdge == nil {
			if outcome.Status == StatusFail {
				return nil, fmt.Errorf("stage %q failed with no outgoing fail edge and no retry target", currentNode.ID)
			}
			// No edge found, but not a failure - break naturally
			break
		}

		// Step 9: Advance to next node
		nextNode := graph.NodeByID(nextEdge.To)
		if nextNode == nil {
			return nil, fmt.Errorf("edge target node %q not found", nextEdge.To)
		}
		currentNode = nextNode
	}

	return &RunResult{
		FinalOutcome:   lastOutcome,
		CompletedNodes: completedNodes,
		Context:        ctx,
		NodeOutcomes:   nodeOutcomes,
	}, nil
}
