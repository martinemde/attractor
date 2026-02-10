package pipeline

import (
	"github.com/martinemde/attractor/dotparser"
)

// ConditionalHandler handles diamond-shaped conditional routing nodes.
// The handler itself is a no-op that returns SUCCESS; actual routing is handled
// by the execution engine's edge selection algorithm, which evaluates conditions
// on outgoing edges.
type ConditionalHandler struct{}

// Execute returns SUCCESS immediately with notes indicating the conditional was evaluated.
// The routing logic is in the engine (where it can be deterministic and inspectable)
// rather than in this handler.
func (h *ConditionalHandler) Execute(node *dotparser.Node, ctx *Context, graph *dotparser.Graph, logsRoot string) (*Outcome, error) {
	return Success().WithNotes("Conditional node evaluated: " + node.ID), nil
}
