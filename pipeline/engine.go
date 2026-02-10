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
}

// Interviewer is the interface for human-in-the-loop interactions.
// It presents questions to humans and collects answers.
type Interviewer interface {
	// Ask presents a question and blocks until an answer is received.
	Ask(question *Question) (*Answer, error)
}

// Question represents a question to present to a human.
type Question struct {
	Text    string
	Type    QuestionType
	Options []Option
	Stage   string
}

// QuestionType represents the type of question.
type QuestionType string

const (
	QuestionMultipleChoice QuestionType = "multiple_choice"
	QuestionFreeform       QuestionType = "freeform"
)

// Option represents a choice in a multiple choice question.
type Option struct {
	Key   string
	Label string
}

// Answer represents a human's response to a question.
type Answer struct {
	Value   string
	Skipped bool
	Timeout bool
}

// Run executes a pipeline graph from start to completion.
func Run(graph *dotparser.Graph, config *RunConfig) (*RunResult, error) {
	if config == nil {
		config = &RunConfig{}
	}

	registry := config.Registry
	if registry == nil {
		registry = DefaultRegistry()
	}

	// Apply transforms before execution
	if len(config.Transforms) > 0 {
		graph = ApplyTransforms(graph, config.Transforms)
	}

	ctx := NewContext()
	mirrorGraphAttributes(graph, ctx)

	completedNodes := []string{}
	nodeOutcomes := make(map[string]*Outcome)

	// Write manifest.json at run initialization
	startTime := time.Now()
	if config.LogsRoot != "" {
		if err := writeManifest(config.LogsRoot, graph, startTime); err != nil {
			ctx.AppendLog(fmt.Sprintf("failed to write manifest: %v", err))
		}
	}

	// Find start node
	currentNode := findStartNode(graph)
	if currentNode == nil {
		return nil, errors.New("no start node found (shape=Mdiamond or id=start/Start)")
	}

	var lastOutcome *Outcome

	for {
		// Step 1: Check for terminal node
		if isTerminal(currentNode) {
			break
		}

		// Step 2: Execute node handler
		handler := registry.Resolve(currentNode)
		if handler == nil {
			return nil, fmt.Errorf("no handler found for node %q", currentNode.ID)
		}

		outcome, err := handler.Execute(currentNode, ctx, graph, config.LogsRoot)
		if err != nil {
			return nil, fmt.Errorf("handler execution failed for node %q: %w", currentNode.ID, err)
		}

		// Step 3: Record completion
		completedNodes = append(completedNodes, currentNode.ID)
		nodeOutcomes[currentNode.ID] = outcome
		lastOutcome = outcome

		// Step 4: Apply context updates from outcome
		if outcome.ContextUpdates != nil {
			ctx.ApplyUpdates(outcome.ContextUpdates)
		}
		ctx.Set("outcome", outcome.Status.String())
		if outcome.PreferredLabel != "" {
			ctx.Set("preferred_label", outcome.PreferredLabel)
		}

		// Step 5: Write status.json
		if config.LogsRoot != "" {
			if err := writeNodeStatus(config.LogsRoot, currentNode.ID, outcome); err != nil {
				// Log error but don't fail the pipeline
				ctx.AppendLog(fmt.Sprintf("failed to write status for %s: %v", currentNode.ID, err))
			}
		}

		// Step 6: Select next edge
		nextEdge := SelectEdge(currentNode, outcome, ctx, graph)
		if nextEdge == nil {
			if outcome.Status == StatusFail {
				return nil, fmt.Errorf("stage %q failed with no outgoing fail edge", currentNode.ID)
			}
			// No edge found, but not a failure - break naturally
			break
		}

		// Step 7: Advance to next node
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
		case dotparser.ValueDuration:
			ctx.Set(key, attr.Value.Duration)
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
