package pipeline

import (
	"github.com/martinemde/attractor/dotparser"
)

// Handler is the interface for node execution handlers.
// Every node handler implements this interface. The execution engine dispatches
// to the appropriate handler based on the node's type attribute or shape-based
// resolution.
type Handler interface {
	// Execute runs the handler for the given node.
	//
	// Parameters:
	//   - node: The parsed Node with all its attributes
	//   - ctx: The shared key-value Context for the pipeline run (read/write)
	//   - graph: The full parsed Graph (for reading outgoing edges, etc.)
	//   - logsRoot: Filesystem path for this run's log/artifact directory
	//
	// Returns:
	//   - Outcome: The result of execution
	//   - error: Non-nil if a fatal error occurred
	Execute(node *dotparser.Node, ctx *Context, graph *dotparser.Graph, logsRoot string) (*Outcome, error)
}

// StartHandler is a no-op handler for the pipeline entry point.
// Returns SUCCESS immediately without performing any work.
type StartHandler struct{}

// Execute returns SUCCESS immediately.
func (h *StartHandler) Execute(node *dotparser.Node, ctx *Context, graph *dotparser.Graph, logsRoot string) (*Outcome, error) {
	return Success(), nil
}

// ExitHandler is a no-op handler for the pipeline exit point.
// Returns SUCCESS immediately. Goal gate enforcement is handled by the
// execution engine, not by this handler.
type ExitHandler struct{}

// Execute returns SUCCESS immediately.
func (h *ExitHandler) Execute(node *dotparser.Node, ctx *Context, graph *dotparser.Graph, logsRoot string) (*Outcome, error) {
	return Success(), nil
}
