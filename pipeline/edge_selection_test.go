package pipeline

import (
	"testing"

	"github.com/martinemde/attractor/dotparser"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestSelectEdge_ConditionMatchWinsOverWeight(t *testing.T) {
	// Edge with lower weight but matching condition should win
	node := newNode("A")
	ctx := NewContext()
	ctx.Set("status", "ready")

	outcome := Success()

	graph := newTestGraph(
		[]*dotparser.Node{
			node,
			newNode("high_weight"),
			newNode("condition_match"),
		},
		[]*dotparser.Edge{
			newEdge("A", "high_weight", intAttr("weight", 100)),
			newEdge("A", "condition_match", intAttr("weight", 1), strAttr("condition", "status=ready")),
		},
		nil,
	)

	selected := SelectEdge(node, outcome, ctx, graph)

	require.NotNil(t, selected)
	assert.Equal(t, "condition_match", selected.To)
}

func TestSelectEdge_HighestWeightAmongUnconditional(t *testing.T) {
	node := newNode("A")
	ctx := NewContext()
	outcome := Success()

	graph := newTestGraph(
		[]*dotparser.Node{
			node,
			newNode("low"),
			newNode("medium"),
			newNode("high"),
		},
		[]*dotparser.Edge{
			newEdge("A", "low", intAttr("weight", 1)),
			newEdge("A", "medium", intAttr("weight", 5)),
			newEdge("A", "high", intAttr("weight", 10)),
		},
		nil,
	)

	selected := SelectEdge(node, outcome, ctx, graph)

	require.NotNil(t, selected)
	assert.Equal(t, "high", selected.To)
}

func TestSelectEdge_LexicalTiebreak(t *testing.T) {
	// When weights are equal, lexicographically first target wins
	node := newNode("A")
	ctx := NewContext()
	outcome := Success()

	graph := newTestGraph(
		[]*dotparser.Node{
			node,
			newNode("zebra"),
			newNode("apple"),
			newNode("banana"),
		},
		[]*dotparser.Edge{
			newEdge("A", "zebra", intAttr("weight", 5)),
			newEdge("A", "apple", intAttr("weight", 5)),
			newEdge("A", "banana", intAttr("weight", 5)),
		},
		nil,
	)

	selected := SelectEdge(node, outcome, ctx, graph)

	require.NotNil(t, selected)
	assert.Equal(t, "apple", selected.To) // Lexicographically first
}

func TestSelectEdge_PreferredLabelMatch(t *testing.T) {
	node := newNode("A")
	ctx := NewContext()
	outcome := Success().WithPreferredLabel("approve")

	graph := newTestGraph(
		[]*dotparser.Node{
			node,
			newNode("reject_node"),
			newNode("approve_node"),
		},
		[]*dotparser.Edge{
			newEdge("A", "reject_node", strAttr("label", "Reject"), intAttr("weight", 100)),
			newEdge("A", "approve_node", strAttr("label", "Approve"), intAttr("weight", 1)),
		},
		nil,
	)

	selected := SelectEdge(node, outcome, ctx, graph)

	require.NotNil(t, selected)
	assert.Equal(t, "approve_node", selected.To)
}

func TestSelectEdge_SuggestedNextIDs(t *testing.T) {
	node := newNode("A")
	ctx := NewContext()
	outcome := Success().WithSuggestedNextIDs("specific_target", "fallback")

	graph := newTestGraph(
		[]*dotparser.Node{
			node,
			newNode("default"),
			newNode("specific_target"),
			newNode("fallback"),
		},
		[]*dotparser.Edge{
			newEdge("A", "default", intAttr("weight", 100)),
			newEdge("A", "specific_target"),
			newEdge("A", "fallback"),
		},
		nil,
	)

	selected := SelectEdge(node, outcome, ctx, graph)

	require.NotNil(t, selected)
	assert.Equal(t, "specific_target", selected.To)
}

func TestSelectEdge_SuggestedNextIDsOrderMatters(t *testing.T) {
	node := newNode("A")
	ctx := NewContext()
	// First suggested ID takes priority
	outcome := Success().WithSuggestedNextIDs("first", "second")

	graph := newTestGraph(
		[]*dotparser.Node{
			node,
			newNode("first"),
			newNode("second"),
		},
		[]*dotparser.Edge{
			newEdge("A", "second", intAttr("weight", 100)),
			newEdge("A", "first"),
		},
		nil,
	)

	selected := SelectEdge(node, outcome, ctx, graph)

	require.NotNil(t, selected)
	assert.Equal(t, "first", selected.To)
}

func TestSelectEdge_NoEdges(t *testing.T) {
	node := newNode("A")
	ctx := NewContext()
	outcome := Success()

	graph := newTestGraph(
		[]*dotparser.Node{node},
		nil,
		nil,
	)

	selected := SelectEdge(node, outcome, ctx, graph)

	assert.Nil(t, selected)
}

func TestSelectEdge_DefaultWeight(t *testing.T) {
	// Edges without weight attribute default to 0
	node := newNode("A")
	ctx := NewContext()
	outcome := Success()

	graph := newTestGraph(
		[]*dotparser.Node{
			node,
			newNode("no_weight"),
			newNode("zero_weight"),
			newNode("positive_weight"),
		},
		[]*dotparser.Edge{
			newEdge("A", "no_weight"),                       // Default 0
			newEdge("A", "zero_weight", intAttr("weight", 0)), // Explicit 0
			newEdge("A", "positive_weight", intAttr("weight", 1)),
		},
		nil,
	)

	selected := SelectEdge(node, outcome, ctx, graph)

	require.NotNil(t, selected)
	assert.Equal(t, "positive_weight", selected.To)
}

func TestSelectEdge_ConditionMatchHighestWeight(t *testing.T) {
	// Among condition-matching edges, highest weight wins
	node := newNode("A")
	ctx := NewContext()
	ctx.Set("ready", "true")
	outcome := Success()

	graph := newTestGraph(
		[]*dotparser.Node{
			node,
			newNode("low_priority"),
			newNode("high_priority"),
		},
		[]*dotparser.Edge{
			newEdge("A", "low_priority", strAttr("condition", "ready=true"), intAttr("weight", 1)),
			newEdge("A", "high_priority", strAttr("condition", "ready=true"), intAttr("weight", 10)),
		},
		nil,
	)

	selected := SelectEdge(node, outcome, ctx, graph)

	require.NotNil(t, selected)
	assert.Equal(t, "high_priority", selected.To)
}

func TestSelectEdge_FallbackWhenNoConditionsMatch(t *testing.T) {
	// When all edges have conditions but none match, fall back to best by weight
	node := newNode("A")
	ctx := NewContext()
	ctx.Set("status", "unknown")
	outcome := Success()

	graph := newTestGraph(
		[]*dotparser.Node{
			node,
			newNode("cond_a"),
			newNode("cond_b"),
		},
		[]*dotparser.Edge{
			newEdge("A", "cond_a", strAttr("condition", "status=ready"), intAttr("weight", 1)),
			newEdge("A", "cond_b", strAttr("condition", "status=pending"), intAttr("weight", 10)),
		},
		nil,
	)

	selected := SelectEdge(node, outcome, ctx, graph)

	require.NotNil(t, selected)
	assert.Equal(t, "cond_b", selected.To) // Higher weight
}

func TestNormalizeLabel(t *testing.T) {
	tests := []struct {
		input    string
		expected string
	}{
		{"Approve", "approve"},
		{"  Approve  ", "approve"},
		{"[Y] Yes, deploy", "yes, deploy"},
		{"Y) Yes, deploy", "yes, deploy"},
		{"Y - Yes, deploy", "yes, deploy"},
		{"[A] Approve", "approve"},
		{"A) Approve", "approve"},
		{"A - Approve", "approve"},
		{"Simple Label", "simple label"},
		{"", ""},
	}

	for _, tt := range tests {
		t.Run(tt.input, func(t *testing.T) {
			result := normalizeLabel(tt.input)
			assert.Equal(t, tt.expected, result)
		})
	}
}

func TestParseAcceleratorKey(t *testing.T) {
	tests := []struct {
		input    string
		expected string
	}{
		{"[Y] Yes, deploy", "Y"},
		{"Y) Yes, deploy", "Y"},
		{"Y - Yes, deploy", "Y"},
		{"[A] Approve", "A"},
		{"[1] Option one", "1"},
		{"Simple", "S"},     // Falls back to first char
		{"approve", "A"},    // Falls back to first char
		{"", ""},
	}

	for _, tt := range tests {
		t.Run(tt.input, func(t *testing.T) {
			result := ParseAcceleratorKey(tt.input)
			assert.Equal(t, tt.expected, result)
		})
	}
}

func TestSelectEdge_PreferredLabelWithAccelerator(t *testing.T) {
	// Preferred label should match after stripping accelerator
	node := newNode("A")
	ctx := NewContext()
	outcome := Success().WithPreferredLabel("yes")

	graph := newTestGraph(
		[]*dotparser.Node{
			node,
			newNode("no_node"),
			newNode("yes_node"),
		},
		[]*dotparser.Edge{
			newEdge("A", "no_node", strAttr("label", "[N] No"), intAttr("weight", 100)),
			newEdge("A", "yes_node", strAttr("label", "[Y] Yes"), intAttr("weight", 1)),
		},
		nil,
	)

	selected := SelectEdge(node, outcome, ctx, graph)

	require.NotNil(t, selected)
	assert.Equal(t, "yes_node", selected.To)
}

func TestSelectEdge_PriorityOrder(t *testing.T) {
	// Verify the complete priority order:
	// 1. Condition match > 2. Preferred label > 3. Suggested IDs > 4. Weight > 5. Lexical

	t.Run("condition beats preferred label", func(t *testing.T) {
		node := newNode("A")
		ctx := NewContext()
		ctx.Set("use_condition", "true")
		outcome := Success().WithPreferredLabel("preferred")

		graph := newTestGraph(
			[]*dotparser.Node{
				node,
				newNode("preferred_target"),
				newNode("condition_target"),
			},
			[]*dotparser.Edge{
				newEdge("A", "preferred_target", strAttr("label", "Preferred")),
				newEdge("A", "condition_target", strAttr("condition", "use_condition=true")),
			},
			nil,
		)

		selected := SelectEdge(node, outcome, ctx, graph)

		require.NotNil(t, selected)
		assert.Equal(t, "condition_target", selected.To)
	})

	t.Run("preferred label beats suggested IDs", func(t *testing.T) {
		node := newNode("A")
		ctx := NewContext()
		outcome := Success().WithPreferredLabel("preferred").WithSuggestedNextIDs("suggested_target")

		graph := newTestGraph(
			[]*dotparser.Node{
				node,
				newNode("preferred_target"),
				newNode("suggested_target"),
			},
			[]*dotparser.Edge{
				newEdge("A", "preferred_target", strAttr("label", "Preferred")),
				newEdge("A", "suggested_target"),
			},
			nil,
		)

		selected := SelectEdge(node, outcome, ctx, graph)

		require.NotNil(t, selected)
		assert.Equal(t, "preferred_target", selected.To)
	})

	t.Run("suggested IDs beats weight", func(t *testing.T) {
		node := newNode("A")
		ctx := NewContext()
		outcome := Success().WithSuggestedNextIDs("suggested_target")

		graph := newTestGraph(
			[]*dotparser.Node{
				node,
				newNode("high_weight_target"),
				newNode("suggested_target"),
			},
			[]*dotparser.Edge{
				newEdge("A", "high_weight_target", intAttr("weight", 100)),
				newEdge("A", "suggested_target", intAttr("weight", 1)),
			},
			nil,
		)

		selected := SelectEdge(node, outcome, ctx, graph)

		require.NotNil(t, selected)
		assert.Equal(t, "suggested_target", selected.To)
	})
}
