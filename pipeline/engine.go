package pipeline

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/martinemde/attractor/dotparser"
)

// Engine is the pipeline execution engine. It traverses a parsed DOT graph,
// executing node handlers and selecting edges according to the spec.
type Engine struct {
	Registry *HandlerRegistry
	Sleeper  Sleeper
}

// NewEngine creates an engine with the given handler registry.
func NewEngine(registry *HandlerRegistry) *Engine {
	return &Engine{
		Registry: registry,
		Sleeper:  DefaultSleeper,
	}
}

// Run executes a pipeline graph according to spec Section 3.2.
// It traverses from the start node, executing handlers, selecting edges,
// and enforcing goal gates until a terminal node is reached.
func (e *Engine) Run(graph *dotparser.Graph, config RunConfig) (*Outcome, error) {
	ctx := NewContext()
	mirrorGraphAttributes(graph, ctx)

	retryCounters := make(map[string]int)
	var completedNodes []string
	nodeOutcomes := make(map[string]*Outcome)

	startNode, err := findStartNode(graph)
	if err != nil {
		return nil, err
	}

	currentNode := startNode
	var lastOutcome *Outcome

	for {
		// Step 1: Check for terminal node
		if isTerminal(currentNode) {
			gateOK, failedGate := checkGoalGates(graph, nodeOutcomes)
			if !gateOK && failedGate != nil {
				retryTarget := getRetryTarget(failedGate, graph)
				if retryTarget != "" {
					targetNode := graph.NodeByID(retryTarget)
					if targetNode != nil {
						currentNode = targetNode
						continue
					}
				}
				return nil, fmt.Errorf("goal gate unsatisfied for node %q and no retry target", failedGate.ID)
			}
			break
		}

		// Step 2: Execute node handler with retry policy
		handler, err := e.Registry.Resolve(currentNode)
		if err != nil {
			return nil, fmt.Errorf("resolving handler for node %q: %w", currentNode.ID, err)
		}

		ctx.Set("current_node", currentNode.ID)

		retryPolicy := BuildRetryPolicy(currentNode, graph)
		outcome := ExecuteWithRetry(handler, currentNode, ctx, graph, config.LogsRoot, retryPolicy, retryCounters, e.sleeper())

		// Step 3: Record completion
		completedNodes = append(completedNodes, currentNode.ID)
		nodeOutcomes[currentNode.ID] = outcome
		lastOutcome = outcome

		// Step 4: Apply context updates from outcome.
		// For conditional/routing nodes (diamond shape), the handler is a
		// no-op that returns SUCCESS (spec Section 4.7). We preserve the
		// prior ctx["outcome"] so that condition expressions on outgoing
		// edges can route based on the previous stage's result.
		if outcome.ContextUpdates != nil {
			ctx.ApplyUpdates(outcome.ContextUpdates)
		}
		if !isConditionalNode(currentNode) {
			ctx.Set("outcome", string(outcome.Status))
		}
		if outcome.PreferredLabel != "" {
			ctx.Set("preferred_label", outcome.PreferredLabel)
		}

		// Step 5: Select next edge
		nextEdge := SelectEdge(currentNode, outcome, ctx, graph)

		// Step 6: Save checkpoint
		if config.LogsRoot != "" {
			cp := &Checkpoint{
				Timestamp:      time.Now(),
				CurrentNode:    currentNode.ID,
				CompletedNodes: completedNodes,
				NodeRetries:    copyRetryCounters(retryCounters),
				ContextValues:  ctx.Snapshot(),
				Logs:           ctx.Logs(),
			}
			_ = saveCheckpoint(cp, config.LogsRoot)
		}

		if nextEdge == nil {
			if outcome.Status == StatusFail {
				return nil, fmt.Errorf("stage %q failed with no outgoing fail edge: %s", currentNode.ID, outcome.FailureReason)
			}
			break
		}

		// Step 7: Handle loop_restart
		if v, ok := nextEdge.Attr("loop_restart"); ok && v.Bool {
			return e.Run(graph, config)
		}

		// Step 8: Advance to next node
		nextNode := graph.NodeByID(nextEdge.To)
		if nextNode == nil {
			return nil, fmt.Errorf("edge from %q targets unknown node %q", currentNode.ID, nextEdge.To)
		}
		currentNode = nextNode
	}

	return lastOutcome, nil
}

func (e *Engine) sleeper() Sleeper {
	if e.Sleeper != nil {
		return e.Sleeper
	}
	return DefaultSleeper
}

// findStartNode locates the start node in the graph per spec:
// (1) shape=Mdiamond, (2) id="start" or "Start".
func findStartNode(graph *dotparser.Graph) (*dotparser.Node, error) {
	// First: look for shape=Mdiamond
	for _, node := range graph.Nodes {
		if v, ok := node.Attr("shape"); ok && v.Str == "Mdiamond" {
			return node, nil
		}
	}
	// Second: look for id="start" or "Start"
	for _, candidate := range []string{"start", "Start"} {
		if n := graph.NodeByID(candidate); n != nil {
			return n, nil
		}
	}
	return nil, fmt.Errorf("no start node found (expected shape=Mdiamond or id=start)")
}

// isTerminal returns true if the node is a terminal/exit node.
// Terminal nodes have shape=Msquare or type="exit".
func isTerminal(node *dotparser.Node) bool {
	if v, ok := node.Attr("shape"); ok && v.Str == "Msquare" {
		return true
	}
	if v, ok := node.Attr("type"); ok && v.Str == "exit" {
		return true
	}
	return false
}

// isConditionalNode returns true if the node is a conditional routing point.
// Conditional nodes have shape=diamond or type="conditional".
func isConditionalNode(node *dotparser.Node) bool {
	if v, ok := node.Attr("shape"); ok && v.Str == "diamond" {
		return true
	}
	if v, ok := node.Attr("type"); ok && v.Str == "conditional" {
		return true
	}
	return false
}

// checkGoalGates checks all visited goal gate nodes. If any have a
// non-success outcome, returns (false, failedNode). Per spec Section 3.4.
func checkGoalGates(graph *dotparser.Graph, nodeOutcomes map[string]*Outcome) (bool, *dotparser.Node) {
	for nodeID, outcome := range nodeOutcomes {
		node := graph.NodeByID(nodeID)
		if node == nil {
			continue
		}
		if v, ok := node.Attr("goal_gate"); ok && v.Bool {
			if !outcome.Status.IsSuccess() {
				return false, node
			}
		}
	}
	return true, nil
}

// getRetryTarget returns the retry target for a failed goal gate node.
// Resolution order per spec Section 3.4:
//  1. Node retry_target
//  2. Node fallback_retry_target
//  3. Graph retry_target
//  4. Graph fallback_retry_target
func getRetryTarget(node *dotparser.Node, graph *dotparser.Graph) string {
	if v, ok := node.Attr("retry_target"); ok && v.Str != "" {
		return v.Str
	}
	if v, ok := node.Attr("fallback_retry_target"); ok && v.Str != "" {
		return v.Str
	}
	if v, ok := graph.GraphAttr("retry_target"); ok && v.Str != "" {
		return v.Str
	}
	if v, ok := graph.GraphAttr("fallback_retry_target"); ok && v.Str != "" {
		return v.Str
	}
	return ""
}

// mirrorGraphAttributes copies graph-level attributes into the context
// under the "graph." namespace.
func mirrorGraphAttributes(graph *dotparser.Graph, ctx *Context) {
	for _, attr := range graph.GraphAttrs {
		ctx.Set("graph."+attr.Key, attr.Value.Raw)
	}
	// Ensure graph.goal is set from the goal attribute
	if v, ok := graph.GraphAttr("goal"); ok {
		ctx.Set("graph.goal", v.Str)
	}
}

// saveCheckpoint writes a checkpoint to the logs root directory.
func saveCheckpoint(cp *Checkpoint, logsRoot string) error {
	if err := os.MkdirAll(logsRoot, 0o755); err != nil {
		return err
	}
	data, err := json.MarshalIndent(cp, "", "  ")
	if err != nil {
		return err
	}
	return os.WriteFile(filepath.Join(logsRoot, "checkpoint.json"), data, 0o644)
}

// LoadCheckpoint reads a checkpoint from a logs root directory.
func LoadCheckpoint(logsRoot string) (*Checkpoint, error) {
	data, err := os.ReadFile(filepath.Join(logsRoot, "checkpoint.json"))
	if err != nil {
		return nil, err
	}
	var cp Checkpoint
	if err := json.Unmarshal(data, &cp); err != nil {
		return nil, err
	}
	return &cp, nil
}

// copyRetryCounters returns a shallow copy of a retry counter map.
func copyRetryCounters(m map[string]int) map[string]int {
	out := make(map[string]int, len(m))
	for k, v := range m {
		out[k] = v
	}
	return out
}

// ExpandVariables performs simple $goal variable expansion in a string.
// This is the only built-in template variable per spec Section 4.5.
func ExpandVariables(s string, graph *dotparser.Graph, ctx *Context) string {
	if v, ok := graph.GraphAttr("goal"); ok {
		s = strings.ReplaceAll(s, "$goal", v.Str)
	}
	return s
}
