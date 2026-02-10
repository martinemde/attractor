package pipeline

import (
	"regexp"
	"sort"
	"strings"

	"github.com/martinemde/attractor/dotparser"
)

// SelectEdge implements the five-step deterministic edge selection algorithm
// from spec Section 3.3. Returns nil if no edges are available.
func SelectEdge(node *dotparser.Node, outcome *Outcome, ctx *Context, graph *dotparser.Graph) *dotparser.Edge {
	edges := graph.EdgesFrom(node.ID)
	if len(edges) == 0 {
		return nil
	}

	// Step 1: Condition-matching edges
	var conditionMatched []*dotparser.Edge
	for _, edge := range edges {
		if cond, ok := edge.Attr("condition"); ok && cond.Str != "" {
			if evaluateCondition(cond.Str, outcome, ctx) {
				conditionMatched = append(conditionMatched, edge)
			}
		}
	}
	if len(conditionMatched) > 0 {
		return bestByWeightThenLexical(conditionMatched)
	}

	// Step 2: Preferred label match
	if outcome.PreferredLabel != "" {
		normalizedPref := normalizeLabel(outcome.PreferredLabel)
		for _, edge := range edges {
			if lbl, ok := edge.Attr("label"); ok {
				if normalizeLabel(lbl.Str) == normalizedPref {
					return edge
				}
			}
		}
	}

	// Step 3: Suggested next IDs
	if len(outcome.SuggestedNextIDs) > 0 {
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
		if cond, ok := edge.Attr("condition"); !ok || cond.Str == "" {
			unconditional = append(unconditional, edge)
		}
	}
	if len(unconditional) > 0 {
		return bestByWeightThenLexical(unconditional)
	}

	// Fallback: any edge
	return bestByWeightThenLexical(edges)
}

// bestByWeightThenLexical sorts edges by weight descending, then by target
// node ID ascending, and returns the first.
func bestByWeightThenLexical(edges []*dotparser.Edge) *dotparser.Edge {
	if len(edges) == 1 {
		return edges[0]
	}
	sort.SliceStable(edges, func(i, j int) bool {
		wi := edgeWeight(edges[i])
		wj := edgeWeight(edges[j])
		if wi != wj {
			return wi > wj // higher weight first
		}
		return edges[i].To < edges[j].To // lexical ascending
	})
	return edges[0]
}

// edgeWeight returns the weight attribute of an edge, defaulting to 0.
func edgeWeight(e *dotparser.Edge) int64 {
	if w, ok := e.Attr("weight"); ok {
		return w.Int
	}
	return 0
}

// acceleratorPrefixRe matches accelerator key prefixes like "[Y] ", "Y) ", "Y - ".
var acceleratorPrefixRe = regexp.MustCompile(`^(?:\[.\]\s*|.\)\s*|.\s*-\s*)`)

// normalizeLabel normalizes a label for comparison: lowercase, trim whitespace,
// strip accelerator prefixes.
func normalizeLabel(label string) string {
	s := strings.ToLower(strings.TrimSpace(label))
	s = acceleratorPrefixRe.ReplaceAllString(s, "")
	return strings.TrimSpace(s)
}

// evaluateCondition evaluates a simple condition expression against the
// outcome and context. Supports the following forms:
//
//	"outcome=success"  / "outcome=fail" / "outcome!=success"
//	"key=value"        / "key!=value"
//
// This is a minimal evaluator sufficient for core engine routing.
// A full condition expression language (spec Section 10) can extend this.
func evaluateCondition(condition string, outcome *Outcome, ctx *Context) bool {
	condition = strings.TrimSpace(condition)

	// Handle != operator
	if parts := strings.SplitN(condition, "!=", 2); len(parts) == 2 {
		key := strings.TrimSpace(parts[0])
		val := strings.TrimSpace(parts[1])
		return resolveConditionValue(key, outcome, ctx) != val
	}

	// Handle = operator
	if parts := strings.SplitN(condition, "=", 2); len(parts) == 2 {
		key := strings.TrimSpace(parts[0])
		val := strings.TrimSpace(parts[1])
		return resolveConditionValue(key, outcome, ctx) == val
	}

	return false
}

// resolveConditionValue resolves the left-hand side of a condition expression.
// All keys (including "outcome") are resolved from the context. This ensures
// that condition expressions on diamond/conditional routing nodes reference
// the prior node's outcome rather than the conditional handler's own result
// (which is always SUCCESS per spec Section 4.7).
func resolveConditionValue(key string, outcome *Outcome, ctx *Context) string {
	return ctx.GetString(key)
}
