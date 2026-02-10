package pipeline

import (
	"testing"

	"github.com/martinemde/attractor/dotparser"
	"github.com/stretchr/testify/assert"
)

func TestSelectEdge_NoEdges(t *testing.T) {
	node := makeNode("a")
	graph := &dotparser.Graph{Nodes: []*dotparser.Node{node}}
	result := SelectEdge(node, &Outcome{Status: StatusSuccess}, NewContext(), graph)
	assert.Nil(t, result)
}

func TestSelectEdge_SingleUnconditionalEdge(t *testing.T) {
	node := makeNode("a")
	edge := makeEdge("a", "b")
	graph := &dotparser.Graph{
		Nodes: []*dotparser.Node{node, makeNode("b")},
		Edges: []*dotparser.Edge{edge},
	}
	result := SelectEdge(node, &Outcome{Status: StatusSuccess}, NewContext(), graph)
	assert.Equal(t, edge, result)
}

func TestSelectEdge_ConditionMatching(t *testing.T) {
	node := makeNode("a")
	successEdge := makeEdge("a", "pass", makeAttr("condition", "outcome=success"))
	failEdge := makeEdge("a", "fail", makeAttr("condition", "outcome=fail"))
	graph := &dotparser.Graph{
		Nodes: []*dotparser.Node{node},
		Edges: []*dotparser.Edge{successEdge, failEdge},
	}

	// Success outcome -> success edge (context has outcome=success)
	ctx := NewContext()
	ctx.Set("outcome", "success")
	result := SelectEdge(node, &Outcome{Status: StatusSuccess}, ctx, graph)
	assert.Equal(t, "pass", result.To)

	// Fail outcome -> fail edge (context has outcome=fail)
	ctx = NewContext()
	ctx.Set("outcome", "fail")
	result = SelectEdge(node, &Outcome{Status: StatusFail}, ctx, graph)
	assert.Equal(t, "fail", result.To)
}

func TestSelectEdge_ConditionNotEquals(t *testing.T) {
	node := makeNode("a")
	edge := makeEdge("a", "retry", makeAttr("condition", "outcome!=success"))
	graph := &dotparser.Graph{
		Nodes: []*dotparser.Node{node},
		Edges: []*dotparser.Edge{edge},
	}

	ctx := NewContext()
	ctx.Set("outcome", "fail")
	result := SelectEdge(node, &Outcome{Status: StatusFail}, ctx, graph)
	assert.Equal(t, "retry", result.To)

	ctx = NewContext()
	ctx.Set("outcome", "success")
	result = SelectEdge(node, &Outcome{Status: StatusSuccess}, ctx, graph)
	// condition "outcome!=success" is false, no unconditional edges -> fallback
	assert.Equal(t, "retry", result.To)
}

func TestSelectEdge_PreferredLabel(t *testing.T) {
	node := makeNode("gate")
	approveEdge := makeEdge("gate", "approved", makeAttr("label", "[A] Approve"))
	rejectEdge := makeEdge("gate", "rejected", makeAttr("label", "[R] Reject"))
	graph := &dotparser.Graph{
		Nodes: []*dotparser.Node{node},
		Edges: []*dotparser.Edge{approveEdge, rejectEdge},
	}

	result := SelectEdge(node, &Outcome{Status: StatusSuccess, PreferredLabel: "approve"}, NewContext(), graph)
	assert.Equal(t, "approved", result.To)

	result = SelectEdge(node, &Outcome{Status: StatusSuccess, PreferredLabel: "reject"}, NewContext(), graph)
	assert.Equal(t, "rejected", result.To)
}

func TestSelectEdge_SuggestedNextIDs(t *testing.T) {
	node := makeNode("fork")
	edgeA := makeEdge("fork", "path_a")
	edgeB := makeEdge("fork", "path_b")
	graph := &dotparser.Graph{
		Nodes: []*dotparser.Node{node},
		Edges: []*dotparser.Edge{edgeA, edgeB},
	}

	result := SelectEdge(node, &Outcome{Status: StatusSuccess, SuggestedNextIDs: []string{"path_b"}}, NewContext(), graph)
	assert.Equal(t, "path_b", result.To)
}

func TestSelectEdge_WeightTiebreak(t *testing.T) {
	node := makeNode("fork")
	lowEdge := makeEdge("fork", "low", makeIntAttr("weight", 1))
	highEdge := makeEdge("fork", "high", makeIntAttr("weight", 10))
	graph := &dotparser.Graph{
		Nodes: []*dotparser.Node{node},
		Edges: []*dotparser.Edge{lowEdge, highEdge},
	}

	result := SelectEdge(node, &Outcome{Status: StatusSuccess}, NewContext(), graph)
	assert.Equal(t, "high", result.To)
}

func TestSelectEdge_LexicalTiebreak(t *testing.T) {
	node := makeNode("fork")
	edgeB := makeEdge("fork", "beta")
	edgeA := makeEdge("fork", "alpha")
	graph := &dotparser.Graph{
		Nodes: []*dotparser.Node{node},
		Edges: []*dotparser.Edge{edgeB, edgeA},
	}

	result := SelectEdge(node, &Outcome{Status: StatusSuccess}, NewContext(), graph)
	assert.Equal(t, "alpha", result.To) // alpha < beta lexically
}

func TestSelectEdge_ConditionPriorityOverPreferredLabel(t *testing.T) {
	node := makeNode("a")
	condEdge := makeEdge("a", "cond_match", makeAttr("condition", "outcome=success"))
	labelEdge := makeEdge("a", "label_match", makeAttr("label", "go here"))
	graph := &dotparser.Graph{
		Nodes: []*dotparser.Node{node},
		Edges: []*dotparser.Edge{condEdge, labelEdge},
	}

	// Condition matches take priority over preferred label
	ctx := NewContext()
	ctx.Set("outcome", "success")
	result := SelectEdge(node, &Outcome{Status: StatusSuccess, PreferredLabel: "go here"}, ctx, graph)
	assert.Equal(t, "cond_match", result.To)
}

func TestSelectEdge_ContextCondition(t *testing.T) {
	node := makeNode("a")
	edge := makeEdge("a", "b", makeAttr("condition", "mode=deploy"))
	graph := &dotparser.Graph{
		Nodes: []*dotparser.Node{node},
		Edges: []*dotparser.Edge{edge},
	}

	ctx := NewContext()
	ctx.Set("mode", "deploy")
	result := SelectEdge(node, &Outcome{Status: StatusSuccess}, ctx, graph)
	assert.Equal(t, "b", result.To)
}

func TestNormalizeLabel(t *testing.T) {
	tests := []struct {
		input    string
		expected string
	}{
		{"[A] Approve", "approve"},
		{"[R] Reject", "reject"},
		{"Y) Yes, deploy", "yes, deploy"},
		{"N - No, cancel", "no, cancel"},
		{"  Approve  ", "approve"},
		{"approve", "approve"},
		{"APPROVE", "approve"},
	}
	for _, tt := range tests {
		assert.Equal(t, tt.expected, normalizeLabel(tt.input), "normalizeLabel(%q)", tt.input)
	}
}

func TestEvaluateCondition(t *testing.T) {
	// evaluateCondition resolves all keys from context, including "outcome"
	t.Run("outcome conditions from context", func(t *testing.T) {
		ctx := NewContext()
		ctx.Set("outcome", "success")
		assert.True(t, evaluateCondition("outcome=success", &Outcome{}, ctx))
		assert.False(t, evaluateCondition("outcome=fail", &Outcome{}, ctx))
		assert.False(t, evaluateCondition("outcome!=success", &Outcome{}, ctx))

		ctx.Set("outcome", "fail")
		assert.True(t, evaluateCondition("outcome=fail", &Outcome{}, ctx))
		assert.True(t, evaluateCondition("outcome!=success", &Outcome{}, ctx))
		assert.False(t, evaluateCondition("outcome=success", &Outcome{}, ctx))
	})

	t.Run("arbitrary context keys", func(t *testing.T) {
		ctx := NewContext()
		ctx.Set("mode", "test")
		assert.True(t, evaluateCondition("mode=test", &Outcome{}, ctx))
		assert.False(t, evaluateCondition("mode=production", &Outcome{}, ctx))
		assert.True(t, evaluateCondition("mode!=production", &Outcome{}, ctx))
	})
}
