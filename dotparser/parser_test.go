package dotparser

import (
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestParseMinimalDigraph(t *testing.T) {
	g, err := Parse([]byte(`digraph G { }`))
	require.NoError(t, err)
	assert.Equal(t, "G", g.Name)
	assert.Empty(t, g.Nodes)
	assert.Empty(t, g.Edges)
}

func TestParseSimpleLinear(t *testing.T) {
	src := `
digraph Simple {
    graph [goal="Run tests and report"]
    rankdir=LR

    start [shape=Mdiamond, label="Start"]
    exit  [shape=Msquare, label="Exit"]

    run_tests [label="Run Tests", prompt="Run the test suite and report results"]
    report    [label="Report", prompt="Summarize the test results"]

    start -> run_tests -> report -> exit
}
`
	g, err := Parse([]byte(src))
	require.NoError(t, err)
	assert.Equal(t, "Simple", g.Name)
	assert.Len(t, g.Nodes, 4)
	assert.Len(t, g.Edges, 3) // chained edge produces 3 edges

	// Graph attrs
	goal, ok := g.GraphAttr("goal")
	assert.True(t, ok)
	assert.Equal(t, "Run tests and report", goal.Str)

	rankdir, ok := g.GraphAttr("rankdir")
	assert.True(t, ok)
	assert.Equal(t, "LR", rankdir.Str)

	// Node shapes
	startNode := g.NodeByID("start")
	require.NotNil(t, startNode)
	shape, ok := startNode.Attr("shape")
	assert.True(t, ok)
	assert.Equal(t, "Mdiamond", shape.Str)

	exitNode := g.NodeByID("exit")
	require.NotNil(t, exitNode)
	shape, ok = exitNode.Attr("shape")
	assert.True(t, ok)
	assert.Equal(t, "Msquare", shape.Str)

	// Edges
	assert.Equal(t, "start", g.Edges[0].From)
	assert.Equal(t, "run_tests", g.Edges[0].To)
	assert.Equal(t, "run_tests", g.Edges[1].From)
	assert.Equal(t, "report", g.Edges[1].To)
	assert.Equal(t, "report", g.Edges[2].From)
	assert.Equal(t, "exit", g.Edges[2].To)
}

func TestParseBranchingWorkflow(t *testing.T) {
	src := `
digraph Branch {
    graph [goal="Implement and validate a feature"]
    rankdir=LR
    node [shape=box, timeout=900s]

    start     [shape=Mdiamond, label="Start"]
    exit      [shape=Msquare, label="Exit"]
    plan      [label="Plan", prompt="Plan the implementation"]
    implement [label="Implement", prompt="Implement the plan"]
    validate  [label="Validate", prompt="Run tests"]
    gate      [shape=diamond, label="Tests passing?"]

    start -> plan -> implement -> validate -> gate
    gate -> exit      [label="Yes", condition="outcome=success"]
    gate -> implement [label="No", condition="outcome!=success"]
}
`
	g, err := Parse([]byte(src))
	require.NoError(t, err)
	assert.Len(t, g.Nodes, 6)
	assert.Len(t, g.Edges, 6) // 4 from chain + 2 explicit

	// gate node should have diamond shape
	gate := g.NodeByID("gate")
	require.NotNil(t, gate)
	shape, ok := gate.Attr("shape")
	assert.True(t, ok)
	assert.Equal(t, "diamond", shape.Str)

	// Check edge conditions
	gateEdges := g.EdgesFrom("gate")
	assert.Len(t, gateEdges, 2)

	// Find the exit edge
	var exitEdge, implEdge *Edge
	for _, e := range gateEdges {
		if e.To == "exit" {
			exitEdge = e
		} else if e.To == "implement" {
			implEdge = e
		}
	}
	require.NotNil(t, exitEdge)
	require.NotNil(t, implEdge)

	cond, ok := exitEdge.Attr("condition")
	assert.True(t, ok)
	assert.Equal(t, "outcome=success", cond.Str)

	cond, ok = implEdge.Attr("condition")
	assert.True(t, ok)
	assert.Equal(t, "outcome!=success", cond.Str)
}

func TestParseHumanGate(t *testing.T) {
	src := `
digraph Review {
    rankdir=LR

    start [shape=Mdiamond, label="Start"]
    exit  [shape=Msquare, label="Exit"]

    review_gate [
        shape=hexagon,
        label="Review Changes",
        type="wait.human"
    ]

    start -> review_gate
    review_gate -> ship_it [label="[A] Approve"]
    review_gate -> fixes   [label="[F] Fix"]
    ship_it -> exit
    fixes -> review_gate
}
`
	g, err := Parse([]byte(src))
	require.NoError(t, err)
	assert.Len(t, g.Nodes, 5) // start, exit, review_gate, ship_it, fixes

	rg := g.NodeByID("review_gate")
	require.NotNil(t, rg)
	shape, ok := rg.Attr("shape")
	assert.True(t, ok)
	assert.Equal(t, "hexagon", shape.Str)
	typ, ok := rg.Attr("type")
	assert.True(t, ok)
	assert.Equal(t, "wait.human", typ.Str)
}

func TestParseChainedEdges(t *testing.T) {
	src := `digraph D { A -> B -> C -> D [label="chain"] }`
	g, err := Parse([]byte(src))
	require.NoError(t, err)
	assert.Len(t, g.Edges, 3)

	for _, e := range g.Edges {
		lbl, ok := e.Attr("label")
		assert.True(t, ok, "edge %s->%s should have label", e.From, e.To)
		assert.Equal(t, "chain", lbl.Str)
	}

	assert.Equal(t, "A", g.Edges[0].From)
	assert.Equal(t, "B", g.Edges[0].To)
	assert.Equal(t, "B", g.Edges[1].From)
	assert.Equal(t, "C", g.Edges[1].To)
	assert.Equal(t, "C", g.Edges[2].From)
	assert.Equal(t, "D", g.Edges[2].To)
}

func TestParseNodeDefaults(t *testing.T) {
	src := `
digraph D {
    node [shape=box, timeout=900s]
    A [label="A"]
    B [label="B", timeout=1800s]
}
`
	g, err := Parse([]byte(src))
	require.NoError(t, err)

	a := g.NodeByID("A")
	require.NotNil(t, a)
	shape, ok := a.Attr("shape")
	assert.True(t, ok)
	assert.Equal(t, "box", shape.Str)
	timeout, ok := a.Attr("timeout")
	assert.True(t, ok)
	assert.Equal(t, 900*time.Second, timeout.Duration)

	b := g.NodeByID("B")
	require.NotNil(t, b)
	shape, ok = b.Attr("shape")
	assert.True(t, ok)
	assert.Equal(t, "box", shape.Str)
	timeout, ok = b.Attr("timeout")
	assert.True(t, ok)
	assert.Equal(t, 1800*time.Second, timeout.Duration)
}

func TestParseEdgeDefaults(t *testing.T) {
	src := `
digraph D {
    edge [weight=0]
    A -> B [label="ab"]
    C -> D
}
`
	g, err := Parse([]byte(src))
	require.NoError(t, err)
	assert.Len(t, g.Edges, 2)

	// Both edges should have weight=0
	for _, e := range g.Edges {
		w, ok := e.Attr("weight")
		assert.True(t, ok, "edge %s->%s should have weight", e.From, e.To)
		assert.Equal(t, int64(0), w.Int)
	}

	// First edge also has label
	lbl, ok := g.Edges[0].Attr("label")
	assert.True(t, ok)
	assert.Equal(t, "ab", lbl.Str)
}

func TestParseSubgraphFlattening(t *testing.T) {
	src := `
digraph D {
    subgraph cluster_loop {
        label = "Loop A"
        node [thread_id="loop-a", timeout=900s]

        Plan      [label="Plan next step"]
        Implement [label="Implement", timeout=1800s]
    }
}
`
	g, err := Parse([]byte(src))
	require.NoError(t, err)

	// Nodes should be in the top-level graph (flattened)
	assert.Len(t, g.Nodes, 2)

	plan := g.NodeByID("Plan")
	require.NotNil(t, plan)
	tid, ok := plan.Attr("thread_id")
	assert.True(t, ok)
	assert.Equal(t, "loop-a", tid.Str)
	timeout, ok := plan.Attr("timeout")
	assert.True(t, ok)
	assert.Equal(t, 900*time.Second, timeout.Duration)

	impl := g.NodeByID("Implement")
	require.NotNil(t, impl)
	tid, ok = impl.Attr("thread_id")
	assert.True(t, ok)
	assert.Equal(t, "loop-a", tid.Str)
	timeout, ok = impl.Attr("timeout")
	assert.True(t, ok)
	assert.Equal(t, 1800*time.Second, timeout.Duration)

	// Both should have derived class from subgraph label "Loop A" -> "loop-a"
	planClass, ok := plan.Attr("class")
	assert.True(t, ok)
	assert.Equal(t, "loop-a", planClass.Str)

	implClass, ok := impl.Attr("class")
	assert.True(t, ok)
	assert.Equal(t, "loop-a", implClass.Str)
}

func TestParseGraphAttrDecl(t *testing.T) {
	src := `digraph D { rankdir=LR }`
	g, err := Parse([]byte(src))
	require.NoError(t, err)

	v, ok := g.GraphAttr("rankdir")
	assert.True(t, ok)
	assert.Equal(t, "LR", v.Str)
}

func TestParseQualifiedKey(t *testing.T) {
	src := `digraph D { graph [stack.child_dotfile="sub.dot"] }`
	g, err := Parse([]byte(src))
	require.NoError(t, err)

	v, ok := g.GraphAttr("stack.child_dotfile")
	assert.True(t, ok)
	assert.Equal(t, "sub.dot", v.Str)
}

func TestParseMultilineAttrBlock(t *testing.T) {
	src := `
digraph D {
    review_gate [
        shape=hexagon,
        label="Review Changes",
        type="wait.human"
    ]
}
`
	g, err := Parse([]byte(src))
	require.NoError(t, err)

	n := g.NodeByID("review_gate")
	require.NotNil(t, n)

	shape, ok := n.Attr("shape")
	assert.True(t, ok)
	assert.Equal(t, "hexagon", shape.Str)

	lbl, ok := n.Attr("label")
	assert.True(t, ok)
	assert.Equal(t, "Review Changes", lbl.Str)

	typ, ok := n.Attr("type")
	assert.True(t, ok)
	assert.Equal(t, "wait.human", typ.Str)
}

func TestParseComments(t *testing.T) {
	src := `
// This is a comment
digraph D {
    /* block comment */
    A [label="A"] // inline comment
}
`
	g, err := Parse([]byte(src))
	require.NoError(t, err)
	assert.Len(t, g.Nodes, 1)
	assert.Equal(t, "A", g.Nodes[0].ID)
}

func TestParseOptionalSemicolons(t *testing.T) {
	src := `
digraph D {
    A [label="A"];
    B [label="B"]
    A -> B;
}
`
	g, err := Parse([]byte(src))
	require.NoError(t, err)
	assert.Len(t, g.Nodes, 2)
	assert.Len(t, g.Edges, 1)
}

func TestParseStringEscapes(t *testing.T) {
	src := `digraph D { A [label="line1\nline2", prompt="say \"hello\""] }`
	g, err := Parse([]byte(src))
	require.NoError(t, err)

	n := g.NodeByID("A")
	require.NotNil(t, n)

	lbl, ok := n.Attr("label")
	assert.True(t, ok)
	assert.Equal(t, "line1\nline2", lbl.Str)

	prompt, ok := n.Attr("prompt")
	assert.True(t, ok)
	assert.Equal(t, `say "hello"`, prompt.Str)
}

func TestParseValueTypes(t *testing.T) {
	src := `
digraph D {
    A [max_retries=3, goal_gate=true, timeout=900s, weight=0.5, label="hello"]
}
`
	g, err := Parse([]byte(src))
	require.NoError(t, err)

	n := g.NodeByID("A")
	require.NotNil(t, n)

	mr, ok := n.Attr("max_retries")
	assert.True(t, ok)
	assert.Equal(t, ValueInt, mr.Kind)
	assert.Equal(t, int64(3), mr.Int)

	gg, ok := n.Attr("goal_gate")
	assert.True(t, ok)
	assert.Equal(t, ValueBool, gg.Kind)
	assert.True(t, gg.Bool)

	to, ok := n.Attr("timeout")
	assert.True(t, ok)
	assert.Equal(t, ValueDuration, to.Kind)
	assert.Equal(t, 900*time.Second, to.Duration)

	w, ok := n.Attr("weight")
	assert.True(t, ok)
	assert.Equal(t, ValueFloat, w.Kind)
	assert.InDelta(t, 0.5, w.Float, 0.001)

	lbl, ok := n.Attr("label")
	assert.True(t, ok)
	assert.Equal(t, ValueString, lbl.Kind)
	assert.Equal(t, "hello", lbl.Str)
}

func TestParseClassAttribute(t *testing.T) {
	src := `digraph D { A [class="code,critical"] }`
	g, err := Parse([]byte(src))
	require.NoError(t, err)

	n := g.NodeByID("A")
	require.NotNil(t, n)
	cls, ok := n.Attr("class")
	assert.True(t, ok)
	assert.Equal(t, "code,critical", cls.Str)
}

func TestParseImplicitNodes(t *testing.T) {
	src := `digraph D { A -> B }`
	g, err := Parse([]byte(src))
	require.NoError(t, err)
	assert.Len(t, g.Nodes, 2)
	assert.NotNil(t, g.NodeByID("A"))
	assert.NotNil(t, g.NodeByID("B"))
}

func TestParseSmokeTestFromSpec(t *testing.T) {
	src := `
digraph test_pipeline {
    graph [goal="Create a hello world Python script"]

    start       [shape=Mdiamond]
    plan        [shape=box, prompt="Plan how to create a hello world script for: $goal"]
    implement   [shape=box, prompt="Write the code based on the plan", goal_gate=true]
    review      [shape=box, prompt="Review the code for correctness"]
    done        [shape=Msquare]

    start -> plan
    plan -> implement
    implement -> review [condition="outcome=success"]
    implement -> plan   [condition="outcome=fail", label="Retry"]
    review -> done      [condition="outcome=success"]
    review -> implement [condition="outcome=fail", label="Fix"]
}
`
	g, err := Parse([]byte(src))
	require.NoError(t, err)

	assert.Equal(t, "test_pipeline", g.Name)
	assert.Len(t, g.Nodes, 5)
	assert.Len(t, g.Edges, 6)

	goal, ok := g.GraphAttr("goal")
	assert.True(t, ok)
	assert.Equal(t, "Create a hello world Python script", goal.Str)

	// Verify goal_gate on implement
	impl := g.NodeByID("implement")
	require.NotNil(t, impl)
	gg, ok := impl.Attr("goal_gate")
	assert.True(t, ok)
	assert.True(t, gg.Bool)
}

func TestParseRejectsUndirectedEdge(t *testing.T) {
	// "--" is not a valid token, so "A -- B" should fail
	_, err := Parse([]byte(`digraph D { A -- B }`))
	require.Error(t, err)
}

func TestParseRejectsMultipleGraphs(t *testing.T) {
	_, err := Parse([]byte(`digraph A { } digraph B { }`))
	require.Error(t, err)
}

func TestParseRejectsStrictModifier(t *testing.T) {
	_, err := Parse([]byte(`strict digraph G { }`))
	require.Error(t, err)
	assert.IsType(t, &SyntaxError{}, err)
}

func TestParseEmptyAttrBlock(t *testing.T) {
	g, err := Parse([]byte(`digraph D { A [] }`))
	require.NoError(t, err)
	n := g.NodeByID("A")
	require.NotNil(t, n)
	assert.Empty(t, n.Attrs)
}

func TestParseNodeDefaultsOverrideScope(t *testing.T) {
	// Node defaults set before subgraph should not affect nodes after subgraph
	src := `
digraph D {
    node [shape=box]
    A [label="A"]
    subgraph cluster_inner {
        node [shape=diamond]
        B [label="B"]
    }
    C [label="C"]
}
`
	g, err := Parse([]byte(src))
	require.NoError(t, err)

	a := g.NodeByID("A")
	require.NotNil(t, a)
	shape, ok := a.Attr("shape")
	assert.True(t, ok)
	assert.Equal(t, "box", shape.Str)

	b := g.NodeByID("B")
	require.NotNil(t, b)
	shape, ok = b.Attr("shape")
	assert.True(t, ok)
	assert.Equal(t, "diamond", shape.Str)

	// C should use outer defaults (box), not inner (diamond)
	c := g.NodeByID("C")
	require.NotNil(t, c)
	shape, ok = c.Attr("shape")
	assert.True(t, ok)
	assert.Equal(t, "box", shape.Str)
}

func TestParseEdgesFrom(t *testing.T) {
	src := `
digraph D {
    A -> B
    A -> C
    B -> C
}
`
	g, err := Parse([]byte(src))
	require.NoError(t, err)

	fromA := g.EdgesFrom("A")
	assert.Len(t, fromA, 2)

	toC := g.EdgesTo("C")
	assert.Len(t, toC, 2)
}

func TestDeriveClassName(t *testing.T) {
	tests := []struct {
		label string
		want  string
	}{
		{"Loop A", "loop-a"},
		{"", ""},
		{"simple", "simple"},
		{"Multi Word Label", "multi-word-label"},
		{"cluster_inner", "inner"},
		{"Special!@#Chars", "specialchars"},
	}
	for _, tt := range tests {
		got := deriveClassName(tt.label)
		assert.Equal(t, tt.want, got, "label: %q", tt.label)
	}
}

func TestParseGraphAttrStmtAndDecl(t *testing.T) {
	// Both graph [key=value] and key=value at top level
	src := `
digraph D {
    graph [goal="test"]
    label = "My Pipeline"
    rankdir = LR
}
`
	g, err := Parse([]byte(src))
	require.NoError(t, err)

	goal, ok := g.GraphAttr("goal")
	assert.True(t, ok)
	assert.Equal(t, "test", goal.Str)

	lbl, ok := g.GraphAttr("label")
	assert.True(t, ok)
	assert.Equal(t, "My Pipeline", lbl.Str)

	rd, ok := g.GraphAttr("rankdir")
	assert.True(t, ok)
	assert.Equal(t, "LR", rd.Str)
}

func TestParseNodeReferencedBeforeDeclaration(t *testing.T) {
	// Edge references B before B is explicitly declared
	src := `
digraph D {
    A -> B
    B [label="Bee"]
}
`
	g, err := Parse([]byte(src))
	require.NoError(t, err)

	b := g.NodeByID("B")
	require.NotNil(t, b)
	lbl, ok := b.Attr("label")
	assert.True(t, ok)
	assert.Equal(t, "Bee", lbl.Str)
}
