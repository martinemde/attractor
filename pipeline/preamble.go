package pipeline

import (
	"fmt"
	"sort"
	"strings"

	"github.com/martinemde/attractor/dotparser"
)

// PreambleGenerator generates context preamble text for LLM stages based on fidelity mode.
// Unlike AST transforms, this is applied at execution time since it depends on runtime state.
type PreambleGenerator struct{}

// GeneratePreamble creates context carryover text based on the resolved fidelity mode.
// For "full" fidelity, returns empty string (full conversation history is preserved).
// For other modes, generates appropriate summary text based on the current context and outcomes.
//
// Parameters:
//   - fidelity: The resolved fidelity mode for this node
//   - ctx: The current pipeline context
//   - graph: The parsed pipeline graph
//   - completedNodes: List of completed node IDs in execution order
//   - nodeOutcomes: Map of node IDs to their execution outcomes
//   - currentNodeID: ID of the node about to be executed
func (g *PreambleGenerator) GeneratePreamble(
	fidelity FidelityMode,
	ctx *Context,
	graph *dotparser.Graph,
	completedNodes []string,
	nodeOutcomes map[string]*Outcome,
	currentNodeID string,
) string {
	switch fidelity {
	case FidelityFull:
		// Full fidelity preserves the entire conversation; no preamble needed
		return ""

	case FidelityTruncate:
		return g.generateTruncatePreamble(ctx, graph)

	case FidelityCompact:
		return g.generateCompactPreamble(ctx, graph, completedNodes, nodeOutcomes)

	case FidelitySummaryLow:
		return g.generateSummaryPreamble(ctx, graph, completedNodes, nodeOutcomes, "low")

	case FidelitySummaryMedium:
		return g.generateSummaryPreamble(ctx, graph, completedNodes, nodeOutcomes, "medium")

	case FidelitySummaryHigh:
		return g.generateSummaryPreamble(ctx, graph, completedNodes, nodeOutcomes, "high")

	default:
		// Unknown fidelity; fall back to compact
		return g.generateCompactPreamble(ctx, graph, completedNodes, nodeOutcomes)
	}
}

// generateTruncatePreamble generates minimal context: only graph goal and run ID.
func (g *PreambleGenerator) generateTruncatePreamble(ctx *Context, graph *dotparser.Graph) string {
	var parts []string

	parts = append(parts, "## Context")
	parts = append(parts, "")

	// Include graph goal if present
	if goalAttr, ok := graph.GraphAttr("goal"); ok && goalAttr.Str != "" {
		parts = append(parts, fmt.Sprintf("**Goal:** %s", goalAttr.Str))
	}

	// Include graph name if present
	if graph.Name != "" {
		parts = append(parts, fmt.Sprintf("**Pipeline:** %s", graph.Name))
	}

	return strings.Join(parts, "\n")
}

// generateCompactPreamble generates a structured bullet-point summary:
// completed stages, outcomes, key context values.
func (g *PreambleGenerator) generateCompactPreamble(
	ctx *Context,
	graph *dotparser.Graph,
	completedNodes []string,
	nodeOutcomes map[string]*Outcome,
) string {
	var parts []string

	parts = append(parts, "## Context")
	parts = append(parts, "")

	// Include graph goal if present
	if goalAttr, ok := graph.GraphAttr("goal"); ok && goalAttr.Str != "" {
		parts = append(parts, fmt.Sprintf("**Goal:** %s", goalAttr.Str))
	}

	// Include pipeline name
	if graph.Name != "" {
		parts = append(parts, fmt.Sprintf("**Pipeline:** %s", graph.Name))
	}

	// Completed stages summary
	if len(completedNodes) > 0 {
		parts = append(parts, "")
		parts = append(parts, "### Completed Stages")
		for _, nodeID := range completedNodes {
			outcome, ok := nodeOutcomes[nodeID]
			if !ok {
				parts = append(parts, fmt.Sprintf("- %s", nodeID))
				continue
			}
			statusStr := outcome.Status.String()
			if outcome.Notes != "" {
				parts = append(parts, fmt.Sprintf("- %s: %s (%s)", nodeID, statusStr, outcome.Notes))
			} else {
				parts = append(parts, fmt.Sprintf("- %s: %s", nodeID, statusStr))
			}
		}
	}

	// Key context values (excluding internal values)
	keyVals := g.extractKeyContextValues(ctx)
	if len(keyVals) > 0 {
		parts = append(parts, "")
		parts = append(parts, "### Key Context Values")
		for _, kv := range keyVals {
			parts = append(parts, fmt.Sprintf("- **%s:** %s", kv.key, kv.value))
		}
	}

	return strings.Join(parts, "\n")
}

// generateSummaryPreamble generates a textual summary with varying detail levels.
// low: ~600 tokens, medium: ~1500 tokens, high: ~3000 tokens
func (g *PreambleGenerator) generateSummaryPreamble(
	ctx *Context,
	graph *dotparser.Graph,
	completedNodes []string,
	nodeOutcomes map[string]*Outcome,
	level string,
) string {
	var parts []string

	parts = append(parts, "## Pipeline Context Summary")
	parts = append(parts, "")

	// Include graph goal if present
	if goalAttr, ok := graph.GraphAttr("goal"); ok && goalAttr.Str != "" {
		parts = append(parts, fmt.Sprintf("**Goal:** %s", goalAttr.Str))
	}

	// Include pipeline name
	if graph.Name != "" {
		parts = append(parts, fmt.Sprintf("**Pipeline:** %s", graph.Name))
	}

	// Execution progress
	stageCount := len(completedNodes)
	successCount := 0
	failCount := 0
	for _, outcome := range nodeOutcomes {
		if outcome.Status.IsSuccess() {
			successCount++
		} else if outcome.Status == StatusFail {
			failCount++
		}
	}

	parts = append(parts, "")
	parts = append(parts, fmt.Sprintf("**Progress:** %d stages completed (%d successful, %d failed)",
		stageCount, successCount, failCount))

	// Determine how many recent stages to show based on level
	var recentStageCount int
	switch level {
	case "low":
		recentStageCount = 2
	case "medium":
		recentStageCount = 5
	case "high":
		recentStageCount = 10
	default:
		recentStageCount = 3
	}

	// Recent stage outcomes
	if len(completedNodes) > 0 {
		parts = append(parts, "")
		parts = append(parts, "### Recent Stage Outcomes")

		startIdx := 0
		if len(completedNodes) > recentStageCount {
			startIdx = len(completedNodes) - recentStageCount
		}

		for i := startIdx; i < len(completedNodes); i++ {
			nodeID := completedNodes[i]
			outcome, ok := nodeOutcomes[nodeID]
			if !ok {
				continue
			}

			node := graph.NodeByID(nodeID)
			label := nodeID
			if node != nil {
				if labelAttr, ok := node.Attr("label"); ok && labelAttr.Str != "" {
					label = labelAttr.Str
				}
			}

			statusStr := outcome.Status.String()
			switch level {
			case "low":
				parts = append(parts, fmt.Sprintf("- **%s:** %s", label, statusStr))
			case "medium":
				if outcome.Notes != "" {
					parts = append(parts, fmt.Sprintf("- **%s:** %s - %s", label, statusStr, outcome.Notes))
				} else if outcome.FailureReason != "" {
					parts = append(parts, fmt.Sprintf("- **%s:** %s - %s", label, statusStr, outcome.FailureReason))
				} else {
					parts = append(parts, fmt.Sprintf("- **%s:** %s", label, statusStr))
				}
			case "high":
				parts = append(parts, fmt.Sprintf("- **%s** (ID: %s): %s", label, nodeID, statusStr))
				if outcome.Notes != "" {
					parts = append(parts, fmt.Sprintf("  - Notes: %s", outcome.Notes))
				}
				if outcome.FailureReason != "" {
					parts = append(parts, fmt.Sprintf("  - Reason: %s", outcome.FailureReason))
				}
				if len(outcome.ContextUpdates) > 0 {
					parts = append(parts, "  - Context updates:")
					for k, v := range outcome.ContextUpdates {
						parts = append(parts, fmt.Sprintf("    - %s: %v", k, v))
					}
				}
			}
		}
	}

	// Context values (more comprehensive for higher levels)
	keyVals := g.extractKeyContextValues(ctx)
	if len(keyVals) > 0 {
		parts = append(parts, "")
		parts = append(parts, "### Active Context Values")

		maxVals := 5
		switch level {
		case "medium":
			maxVals = 10
		case "high":
			maxVals = 20
		}

		count := 0
		for _, kv := range keyVals {
			if count >= maxVals {
				remaining := len(keyVals) - maxVals
				if remaining > 0 {
					parts = append(parts, fmt.Sprintf("- ... and %d more values", remaining))
				}
				break
			}

			// For high level, show full values
			value := kv.value
			if level != "high" && len(value) > 100 {
				value = value[:100] + "..."
			}
			parts = append(parts, fmt.Sprintf("- **%s:** %s", kv.key, value))
			count++
		}
	}

	// For high level, include logs summary
	if level == "high" {
		logs := ctx.Logs()
		if len(logs) > 0 {
			parts = append(parts, "")
			parts = append(parts, "### Recent Logs")
			startIdx := 0
			if len(logs) > 10 {
				startIdx = len(logs) - 10
			}
			for i := startIdx; i < len(logs); i++ {
				parts = append(parts, fmt.Sprintf("- %s", logs[i]))
			}
		}
	}

	return strings.Join(parts, "\n")
}

// keyValue is a helper struct for sorted key-value output.
type keyValue struct {
	key   string
	value string
}

// extractKeyContextValues extracts user-facing context values, excluding internal keys.
func (g *PreambleGenerator) extractKeyContextValues(ctx *Context) []keyValue {
	snapshot := ctx.Snapshot()
	var result []keyValue

	for key, value := range snapshot {
		// Skip internal keys
		if strings.HasPrefix(key, "internal.") {
			continue
		}
		// Skip graph mirrored keys (handled separately)
		if strings.HasPrefix(key, "graph.") {
			continue
		}
		// Skip outcome key (it's transient)
		if key == "outcome" {
			continue
		}
		// Skip preferred_label (it's transient)
		if key == "preferred_label" {
			continue
		}

		result = append(result, keyValue{
			key:   key,
			value: toString(value),
		})
	}

	// Sort by key for deterministic output
	sort.Slice(result, func(i, j int) bool {
		return result[i].key < result[j].key
	})

	return result
}

// InjectPreambleIntoContext stores the generated preamble in the context
// under a well-known key so it can be used by handlers.
func InjectPreambleIntoContext(ctx *Context, preamble string) {
	ctx.Set(ContextKeyPreamble, preamble)
}

// GetPreambleFromContext retrieves the preamble from context.
// Returns empty string if no preamble is set.
func GetPreambleFromContext(ctx *Context) string {
	return ctx.GetString(ContextKeyPreamble, "")
}

// ContextKeyPreamble is the context key where the preamble is stored.
const ContextKeyPreamble = "internal.preamble"
