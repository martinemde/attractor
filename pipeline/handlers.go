package pipeline

import (
	"github.com/martinemde/attractor/dotparser"
)

// StartHandler is a no-op handler for the pipeline entry point.
// Returns SUCCESS immediately without performing any work.
// Every graph must have exactly one start node (shape=Mdiamond).
type StartHandler struct{}

func (h *StartHandler) Execute(_ *dotparser.Node, _ *Context, _ *dotparser.Graph, _ string) (*Outcome, error) {
	return &Outcome{Status: StatusSuccess}, nil
}

// ExitHandler is a no-op handler for the pipeline exit point.
// Returns SUCCESS immediately. Goal gate enforcement is handled by the
// execution engine (Section 3.4), not by this handler.
// Every graph must have exactly one exit node (shape=Msquare).
type ExitHandler struct{}

func (h *ExitHandler) Execute(_ *dotparser.Node, _ *Context, _ *dotparser.Graph, _ string) (*Outcome, error) {
	return &Outcome{Status: StatusSuccess}, nil
}
