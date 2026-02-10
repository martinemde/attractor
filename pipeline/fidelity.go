package pipeline

import (
	"github.com/martinemde/attractor/dotparser"
)

// FidelityMode controls how much prior conversation and state is carried
// into the next node's LLM session.
type FidelityMode string

const (
	// FidelityFull preserves full conversation history (reuses same thread).
	FidelityFull FidelityMode = "full"

	// FidelityTruncate uses minimal context: only graph goal and run ID.
	FidelityTruncate FidelityMode = "truncate"

	// FidelityCompact uses a structured bullet-point summary.
	FidelityCompact FidelityMode = "compact"

	// FidelitySummaryLow uses a brief textual summary (~600 tokens).
	FidelitySummaryLow FidelityMode = "summary:low"

	// FidelitySummaryMedium uses moderate detail (~1500 tokens).
	FidelitySummaryMedium FidelityMode = "summary:medium"

	// FidelitySummaryHigh uses detailed summary (~3000 tokens).
	FidelitySummaryHigh FidelityMode = "summary:high"
)

// DefaultFidelity is the default fidelity mode when none is specified.
const DefaultFidelity = FidelityCompact

// validFidelityModes is the set of all valid fidelity mode strings.
var validFidelityModes = map[string]bool{
	string(FidelityFull):          true,
	string(FidelityTruncate):      true,
	string(FidelityCompact):       true,
	string(FidelitySummaryLow):    true,
	string(FidelitySummaryMedium): true,
	string(FidelitySummaryHigh):   true,
}

// IsValidFidelity returns true if the given mode string is a valid fidelity mode.
func IsValidFidelity(mode string) bool {
	return validFidelityModes[mode]
}

// ResolveFidelity determines the fidelity mode for a node based on the precedence:
// 1. Edge fidelity attribute (on the incoming edge)
// 2. Target node fidelity attribute
// 3. Graph default_fidelity attribute
// 4. Default: compact
func ResolveFidelity(edge *dotparser.Edge, node *dotparser.Node, graph *dotparser.Graph) FidelityMode {
	// 1. Check edge fidelity (highest precedence)
	if edge != nil {
		if fidelityAttr, ok := edge.Attr("fidelity"); ok && fidelityAttr.Str != "" {
			if IsValidFidelity(fidelityAttr.Str) {
				return FidelityMode(fidelityAttr.Str)
			}
		}
	}

	// 2. Check node fidelity
	if node != nil {
		if fidelityAttr, ok := node.Attr("fidelity"); ok && fidelityAttr.Str != "" {
			if IsValidFidelity(fidelityAttr.Str) {
				return FidelityMode(fidelityAttr.Str)
			}
		}
	}

	// 3. Check graph default_fidelity
	if graph != nil {
		if defaultFidelity, ok := graph.GraphAttr("default_fidelity"); ok && defaultFidelity.Str != "" {
			if IsValidFidelity(defaultFidelity.Str) {
				return FidelityMode(defaultFidelity.Str)
			}
		}
	}

	// 4. Default to compact
	return DefaultFidelity
}

// ResolveThread determines the thread ID for session reuse when fidelity is "full".
// Resolution order:
// 1. Target node thread_id attribute
// 2. Edge thread_id attribute
// 3. Graph-level default thread (graph attribute "default_thread")
// 4. Derived class from enclosing subgraph (if applicable)
// 5. Fallback: previous node ID
func ResolveThread(edge *dotparser.Edge, node *dotparser.Node, graph *dotparser.Graph, previousNodeID string) string {
	// 1. Check node thread_id (highest precedence)
	if node != nil {
		if threadAttr, ok := node.Attr("thread_id"); ok && threadAttr.Str != "" {
			return threadAttr.Str
		}
	}

	// 2. Check edge thread_id
	if edge != nil {
		if threadAttr, ok := edge.Attr("thread_id"); ok && threadAttr.Str != "" {
			return threadAttr.Str
		}
	}

	// 3. Check graph default_thread
	if graph != nil {
		if defaultThread, ok := graph.GraphAttr("default_thread"); ok && defaultThread.Str != "" {
			return defaultThread.Str
		}
	}

	// 4. Derived class from enclosing subgraph
	// NOTE: Subgraph class derivation would require additional AST support.
	// For now, we skip this step as the dotparser doesn't expose subgraph membership.

	// 5. Fallback to previous node ID
	return previousNodeID
}
