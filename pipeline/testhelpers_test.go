package pipeline

import (
	"time"

	"github.com/martinemde/attractor/dotparser"
)

// noopSleeper is a Sleeper that doesn't actually sleep (for tests).
type noopSleeper struct {
	calls    int
	totalDur time.Duration
}

func (s *noopSleeper) Sleep(d time.Duration) {
	s.calls++
	s.totalDur += d
}

// recordingHandler records calls and returns a configurable outcome.
type recordingHandler struct {
	calls    []*dotparser.Node
	outcomes []*Outcome // one per call, cycles if exhausted
	idx      int
}

func newRecordingHandler(outcomes ...*Outcome) *recordingHandler {
	return &recordingHandler{outcomes: outcomes}
}

func (h *recordingHandler) Execute(node *dotparser.Node, ctx *Context, graph *dotparser.Graph, logsRoot string) (*Outcome, error) {
	h.calls = append(h.calls, node)
	if len(h.outcomes) == 0 {
		return &Outcome{Status: StatusSuccess}, nil
	}
	outcome := h.outcomes[h.idx%len(h.outcomes)]
	h.idx++
	return outcome, nil
}

// failingHandler always returns an error.
type failingHandler struct {
	err error
}

func (h *failingHandler) Execute(_ *dotparser.Node, _ *Context, _ *dotparser.Graph, _ string) (*Outcome, error) {
	return nil, h.err
}

// makeAttr creates a string attribute.
func makeAttr(key, value string) dotparser.Attr {
	return dotparser.Attr{
		Key:   key,
		Value: dotparser.Value{Kind: dotparser.ValueString, Str: value, Raw: value},
	}
}

// makeIntAttr creates an integer attribute.
func makeIntAttr(key string, value int64) dotparser.Attr {
	return dotparser.Attr{
		Key:   key,
		Value: dotparser.Value{Kind: dotparser.ValueInt, Int: value, Raw: ""},
	}
}

// makeBoolAttr creates a boolean attribute.
func makeBoolAttr(key string, value bool) dotparser.Attr {
	return dotparser.Attr{
		Key:   key,
		Value: dotparser.Value{Kind: dotparser.ValueBool, Bool: value, Raw: ""},
	}
}

// makeNode creates a node with the given ID and attributes.
func makeNode(id string, attrs ...dotparser.Attr) *dotparser.Node {
	return &dotparser.Node{ID: id, Attrs: attrs}
}

// makeEdge creates an edge from->to with the given attributes.
func makeEdge(from, to string, attrs ...dotparser.Attr) *dotparser.Edge {
	return &dotparser.Edge{From: from, To: to, Attrs: attrs}
}

// linearGraph builds:  start(Mdiamond) -> nodes... -> exit(Msquare)
func linearGraph(nodeIDs ...string) *dotparser.Graph {
	g := &dotparser.Graph{Name: "test"}
	startNode := makeNode("start", makeAttr("shape", "Mdiamond"))
	exitNode := makeNode("exit", makeAttr("shape", "Msquare"))
	g.Nodes = append(g.Nodes, startNode)

	var nodes []*dotparser.Node
	for _, id := range nodeIDs {
		n := makeNode(id, makeAttr("shape", "box"))
		nodes = append(nodes, n)
		g.Nodes = append(g.Nodes, n)
	}
	g.Nodes = append(g.Nodes, exitNode)

	// Build edges: start -> first -> ... -> last -> exit
	allIDs := []string{"start"}
	allIDs = append(allIDs, nodeIDs...)
	allIDs = append(allIDs, "exit")
	for i := 0; i < len(allIDs)-1; i++ {
		g.Edges = append(g.Edges, makeEdge(allIDs[i], allIDs[i+1]))
	}
	return g
}
