package pipeline

import (
	"regexp"
	"slices"
	"strings"

	"github.com/martinemde/attractor/dotparser"
)

// acceleratorPattern matches edge label accelerator prefixes like [Y], Y), Y -
var acceleratorPattern = regexp.MustCompile(`^(?:\[([A-Za-z0-9])\]\s*|([A-Za-z0-9])\)\s*|([A-Za-z0-9])\s*-\s*)`)

// SelectEdge selects the next edge from the node's outgoing edges.
// The selection is deterministic and follows a five-step priority order:
//
//  1. Condition-matching edges: Edges whose condition evaluates to true
//  2. Preferred label match: Edge whose label matches outcome.PreferredLabel
//  3. Suggested next IDs: Edge whose target node appears in outcome.SuggestedNextIDs
//  4. Highest weight: Among unconditional edges, the one with highest weight
//  5. Lexical tiebreak: Target node ID that comes first lexicographically
func SelectEdge(node *dotparser.Node, outcome *Outcome, ctx *Context, graph *dotparser.Graph) *dotparser.Edge {
	edges := graph.EdgesFrom(node.ID)
	if len(edges) == 0 {
		return nil
	}

	// Step 1: Condition matching
	var conditionMatched []*dotparser.Edge
	for _, edge := range edges {
		if condition, ok := edge.Attr("condition"); ok && condition.Str != "" {
			if EvaluateCondition(condition.Str, outcome, ctx) {
				conditionMatched = append(conditionMatched, edge)
			}
		}
	}
	if len(conditionMatched) > 0 {
		return bestByWeightThenLexical(conditionMatched)
	}

	// Step 2: Preferred label match
	if outcome != nil && outcome.PreferredLabel != "" {
		normalizedPreferred := normalizeLabel(outcome.PreferredLabel)
		for _, edge := range edges {
			if label, ok := edge.Attr("label"); ok {
				if normalizeLabel(label.Str) == normalizedPreferred {
					return edge
				}
			}
		}
	}

	// Step 3: Suggested next IDs
	if outcome != nil && len(outcome.SuggestedNextIDs) > 0 {
		for _, suggestedID := range outcome.SuggestedNextIDs {
			for _, edge := range edges {
				if edge.To == suggestedID {
					return edge
				}
			}
		}
	}

	// Step 4 & 5: Weight with lexical tiebreak (unconditional edges only)
	var unconditional []*dotparser.Edge
	for _, edge := range edges {
		if condition, ok := edge.Attr("condition"); !ok || condition.Str == "" {
			unconditional = append(unconditional, edge)
		}
	}
	if len(unconditional) > 0 {
		return bestByWeightThenLexical(unconditional)
	}

	// Fallback: any edge (when all edges have conditions but none matched)
	return bestByWeightThenLexical(edges)
}

// bestByWeightThenLexical sorts edges by weight descending, then by target
// node ID ascending, and returns the first (best) one.
func bestByWeightThenLexical(edges []*dotparser.Edge) *dotparser.Edge {
	if len(edges) == 0 {
		return nil
	}
	if len(edges) == 1 {
		return edges[0]
	}

	// Create a copy to avoid modifying the original slice
	sorted := make([]*dotparser.Edge, len(edges))
	copy(sorted, edges)

	slices.SortFunc(sorted, func(a, b *dotparser.Edge) int {
		weightA := getEdgeWeight(a)
		weightB := getEdgeWeight(b)

		// Sort by weight descending (higher weight first)
		if weightA != weightB {
			if weightA > weightB {
				return -1
			}
			return 1
		}

		// Tiebreak by target node ID ascending (lexicographically)
		return strings.Compare(a.To, b.To)
	})

	return sorted[0]
}

// getEdgeWeight returns the weight attribute of an edge, defaulting to 0.
func getEdgeWeight(edge *dotparser.Edge) int {
	if weight, ok := edge.Attr("weight"); ok {
		return int(weight.Int)
	}
	return 0
}

// normalizeLabel normalizes a label for comparison.
// It lowercases, trims whitespace, and strips accelerator prefixes.
func normalizeLabel(label string) string {
	// Trim whitespace
	label = strings.TrimSpace(label)
	// Lowercase
	label = strings.ToLower(label)
	// Strip accelerator prefix patterns like [Y], Y), Y -
	label = acceleratorPattern.ReplaceAllString(label, "")
	// Trim again after stripping prefix
	label = strings.TrimSpace(label)
	return label
}

// ParseAcceleratorKey extracts the shortcut key from a label.
// Patterns: [K] Label, K) Label, K - Label, or first character.
func ParseAcceleratorKey(label string) string {
	matches := acceleratorPattern.FindStringSubmatch(label)
	if matches != nil {
		// Return the first non-empty capture group
		for i := 1; i < len(matches); i++ {
			if matches[i] != "" {
				return strings.ToUpper(matches[i])
			}
		}
	}
	// Fall back to first character
	label = strings.TrimSpace(label)
	if len(label) > 0 {
		return strings.ToUpper(string(label[0]))
	}
	return ""
}
