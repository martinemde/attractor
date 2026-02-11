package pipeline

import (
	"testing"

	"github.com/martinemde/attractor/dotparser"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestParseStylesheet_UniversalSelector(t *testing.T) {
	source := `* { llm_model: claude-sonnet-4-5; }`

	stylesheet, err := ParseStylesheet(source)

	require.NoError(t, err)
	require.Len(t, stylesheet.Rules, 1)

	rule := stylesheet.Rules[0]
	assert.Equal(t, SelectorUniversal, rule.Selector.Type)
	assert.Equal(t, "", rule.Selector.Value)
	assert.Equal(t, 0, rule.Selector.Specificity())

	require.Len(t, rule.Declarations, 1)
	assert.Equal(t, "llm_model", rule.Declarations[0].Property)
	assert.Equal(t, "claude-sonnet-4-5", rule.Declarations[0].Value)
}

func TestParseStylesheet_ClassSelector(t *testing.T) {
	source := `.code { llm_model: claude-opus-4-6; }`

	stylesheet, err := ParseStylesheet(source)

	require.NoError(t, err)
	require.Len(t, stylesheet.Rules, 1)

	rule := stylesheet.Rules[0]
	assert.Equal(t, SelectorClass, rule.Selector.Type)
	assert.Equal(t, "code", rule.Selector.Value)
	assert.Equal(t, 2, rule.Selector.Specificity())

	require.Len(t, rule.Declarations, 1)
	assert.Equal(t, "llm_model", rule.Declarations[0].Property)
	assert.Equal(t, "claude-opus-4-6", rule.Declarations[0].Value)
}

func TestParseStylesheet_IDSelector(t *testing.T) {
	source := `#review { reasoning_effort: high; }`

	stylesheet, err := ParseStylesheet(source)

	require.NoError(t, err)
	require.Len(t, stylesheet.Rules, 1)

	rule := stylesheet.Rules[0]
	assert.Equal(t, SelectorID, rule.Selector.Type)
	assert.Equal(t, "review", rule.Selector.Value)
	assert.Equal(t, 3, rule.Selector.Specificity())

	require.Len(t, rule.Declarations, 1)
	assert.Equal(t, "reasoning_effort", rule.Declarations[0].Property)
	assert.Equal(t, "high", rule.Declarations[0].Value)
}

func TestParseStylesheet_MultipleRules(t *testing.T) {
	source := `
		* { llm_model: claude-sonnet-4-5; }
		.code { llm_model: claude-opus-4-6; }
		#critical { reasoning_effort: high; }
	`

	stylesheet, err := ParseStylesheet(source)

	require.NoError(t, err)
	require.Len(t, stylesheet.Rules, 3)

	assert.Equal(t, SelectorUniversal, stylesheet.Rules[0].Selector.Type)
	assert.Equal(t, SelectorClass, stylesheet.Rules[1].Selector.Type)
	assert.Equal(t, "code", stylesheet.Rules[1].Selector.Value)
	assert.Equal(t, SelectorID, stylesheet.Rules[2].Selector.Type)
	assert.Equal(t, "critical", stylesheet.Rules[2].Selector.Value)
}

func TestParseStylesheet_MultipleDeclarationsPerRule(t *testing.T) {
	source := `.advanced { llm_model: gpt-5; llm_provider: openai; reasoning_effort: high; }`

	stylesheet, err := ParseStylesheet(source)

	require.NoError(t, err)
	require.Len(t, stylesheet.Rules, 1)

	decls := stylesheet.Rules[0].Declarations
	require.Len(t, decls, 3)

	assert.Equal(t, "llm_model", decls[0].Property)
	assert.Equal(t, "gpt-5", decls[0].Value)
	assert.Equal(t, "llm_provider", decls[1].Property)
	assert.Equal(t, "openai", decls[1].Value)
	assert.Equal(t, "reasoning_effort", decls[2].Property)
	assert.Equal(t, "high", decls[2].Value)
}

func TestParseStylesheet_TrailingSemicolonOptional(t *testing.T) {
	// Without trailing semicolon
	source1 := `* { llm_model: model1 }`
	// With trailing semicolon
	source2 := `* { llm_model: model1; }`

	stylesheet1, err1 := ParseStylesheet(source1)
	stylesheet2, err2 := ParseStylesheet(source2)

	require.NoError(t, err1)
	require.NoError(t, err2)

	assert.Equal(t, stylesheet1.Rules[0].Declarations[0].Value, stylesheet2.Rules[0].Declarations[0].Value)
}

func TestParseStylesheet_InvalidProperty(t *testing.T) {
	source := `* { invalid_prop: value; }`

	_, err := ParseStylesheet(source)

	require.Error(t, err)
	assert.Contains(t, err.Error(), "invalid property")
}

func TestParseStylesheet_EmptyValue(t *testing.T) {
	source := `* { llm_model: ; }`

	_, err := ParseStylesheet(source)

	require.Error(t, err)
	assert.Contains(t, err.Error(), "empty value")
}

func TestParseStylesheet_MissingBrace(t *testing.T) {
	source := `* llm_model: value; }`

	_, err := ParseStylesheet(source)

	require.Error(t, err)
	assert.Contains(t, err.Error(), "expected '{'")
}

func TestParseStylesheet_MissingClosingBrace(t *testing.T) {
	source := `* { llm_model: value;`

	_, err := ParseStylesheet(source)

	require.Error(t, err)
	assert.Contains(t, err.Error(), "expected '}'")
}

func TestParseStylesheet_InvalidSelector(t *testing.T) {
	source := `@ { llm_model: value; }`

	_, err := ParseStylesheet(source)

	require.Error(t, err)
	assert.Contains(t, err.Error(), "expected selector")
}

func TestParseStylesheet_EmptyClassName(t *testing.T) {
	source := `. { llm_model: value; }`

	_, err := ParseStylesheet(source)

	require.Error(t, err)
	assert.Contains(t, err.Error(), "expected class name")
}

func TestParseStylesheet_EmptyNodeID(t *testing.T) {
	source := `# { llm_model: value; }`

	_, err := ParseStylesheet(source)

	require.Error(t, err)
	assert.Contains(t, err.Error(), "expected node ID")
}

func TestParseStylesheet_EmptySource(t *testing.T) {
	stylesheet, err := ParseStylesheet("")

	require.NoError(t, err)
	assert.Empty(t, stylesheet.Rules)
}

func TestParseStylesheet_WhitespaceOnly(t *testing.T) {
	stylesheet, err := ParseStylesheet("   \n\t  ")

	require.NoError(t, err)
	assert.Empty(t, stylesheet.Rules)
}

func TestNodeHasClass(t *testing.T) {
	tests := []struct {
		name      string
		classAttr string
		className string
		expected  bool
	}{
		{"single class match", "code", "code", true},
		{"single class no match", "code", "review", false},
		{"multiple classes first match", "code,review", "code", true},
		{"multiple classes second match", "code,review", "review", true},
		{"multiple classes no match", "code,review", "test", false},
		{"with spaces", "code, review, test", "review", true},
		{"empty class attr", "", "code", false},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			node := newNode("test")
			if tt.classAttr != "" {
				node.Attrs = append(node.Attrs, strAttr("class", tt.classAttr))
			}
			result := nodeHasClass(node, tt.className)
			assert.Equal(t, tt.expected, result)
		})
	}
}

func TestSelectorMatchesNode(t *testing.T) {
	tests := []struct {
		name     string
		selector StyleSelector
		node     *dotparser.Node
		expected bool
	}{
		{
			"universal matches any node",
			StyleSelector{Type: SelectorUniversal},
			newNode("anything"),
			true,
		},
		{
			"ID selector matches exact ID",
			StyleSelector{Type: SelectorID, Value: "mynode"},
			newNode("mynode"),
			true,
		},
		{
			"ID selector no match",
			StyleSelector{Type: SelectorID, Value: "mynode"},
			newNode("other"),
			false,
		},
		{
			"class selector matches node with class",
			StyleSelector{Type: SelectorClass, Value: "code"},
			newNode("n", strAttr("class", "code")),
			true,
		},
		{
			"class selector matches in list",
			StyleSelector{Type: SelectorClass, Value: "review"},
			newNode("n", strAttr("class", "code,review")),
			true,
		},
		{
			"class selector no match",
			StyleSelector{Type: SelectorClass, Value: "missing"},
			newNode("n", strAttr("class", "code")),
			false,
		},
		{
			"class selector no class attr",
			StyleSelector{Type: SelectorClass, Value: "code"},
			newNode("n"),
			false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := selectorMatchesNode(tt.selector, tt.node)
			assert.Equal(t, tt.expected, result)
		})
	}
}

func TestApplyStylesheetToNode_UniversalSelector(t *testing.T) {
	stylesheet := &Stylesheet{
		Rules: []StyleRule{
			{
				Selector: StyleSelector{Type: SelectorUniversal},
				Declarations: []StyleDeclaration{
					{Property: "llm_model", Value: "claude-sonnet-4-5"},
					{Property: "llm_provider", Value: "anthropic"},
				},
			},
		},
	}

	node := newNode("anynode")
	applyStylesheetToNode(node, stylesheet)

	model, ok := node.Attr("llm_model")
	require.True(t, ok)
	assert.Equal(t, "claude-sonnet-4-5", model.Str)

	provider, ok := node.Attr("llm_provider")
	require.True(t, ok)
	assert.Equal(t, "anthropic", provider.Str)
}

func TestApplyStylesheetToNode_ClassOverridesUniversal(t *testing.T) {
	stylesheet := &Stylesheet{
		Rules: []StyleRule{
			{
				Selector: StyleSelector{Type: SelectorUniversal},
				Declarations: []StyleDeclaration{
					{Property: "llm_model", Value: "default-model"},
				},
			},
			{
				Selector: StyleSelector{Type: SelectorClass, Value: "code"},
				Declarations: []StyleDeclaration{
					{Property: "llm_model", Value: "code-model"},
				},
			},
		},
	}

	// Node with class=code should get code-model
	codeNode := newNode("impl", strAttr("class", "code"))
	applyStylesheetToNode(codeNode, stylesheet)

	model, ok := codeNode.Attr("llm_model")
	require.True(t, ok)
	assert.Equal(t, "code-model", model.Str)

	// Node without class should get default-model
	otherNode := newNode("plan")
	applyStylesheetToNode(otherNode, stylesheet)

	model, ok = otherNode.Attr("llm_model")
	require.True(t, ok)
	assert.Equal(t, "default-model", model.Str)
}

func TestApplyStylesheetToNode_IDOverridesClass(t *testing.T) {
	stylesheet := &Stylesheet{
		Rules: []StyleRule{
			{
				Selector: StyleSelector{Type: SelectorClass, Value: "code"},
				Declarations: []StyleDeclaration{
					{Property: "llm_model", Value: "class-model"},
				},
			},
			{
				Selector: StyleSelector{Type: SelectorID, Value: "critical"},
				Declarations: []StyleDeclaration{
					{Property: "llm_model", Value: "id-model"},
				},
			},
		},
	}

	// Node with ID=critical AND class=code should get id-model (ID wins)
	node := newNode("critical", strAttr("class", "code"))
	applyStylesheetToNode(node, stylesheet)

	model, ok := node.Attr("llm_model")
	require.True(t, ok)
	assert.Equal(t, "id-model", model.Str)
}

func TestApplyStylesheetToNode_ExplicitAttributeNotOverridden(t *testing.T) {
	stylesheet := &Stylesheet{
		Rules: []StyleRule{
			{
				Selector: StyleSelector{Type: SelectorUniversal},
				Declarations: []StyleDeclaration{
					{Property: "llm_model", Value: "stylesheet-model"},
				},
			},
		},
	}

	// Node with explicit llm_model should NOT be overridden
	node := newNode("special", strAttr("llm_model", "explicit-model"))
	applyStylesheetToNode(node, stylesheet)

	model, ok := node.Attr("llm_model")
	require.True(t, ok)
	assert.Equal(t, "explicit-model", model.Str)
}

func TestApplyStylesheetToNode_LaterRulesOfEqualSpecificityWin(t *testing.T) {
	stylesheet := &Stylesheet{
		Rules: []StyleRule{
			{
				Selector: StyleSelector{Type: SelectorClass, Value: "a"},
				Declarations: []StyleDeclaration{
					{Property: "llm_model", Value: "first"},
				},
			},
			{
				Selector: StyleSelector{Type: SelectorClass, Value: "b"},
				Declarations: []StyleDeclaration{
					{Property: "llm_model", Value: "second"},
				},
			},
		},
	}

	// Node with both classes - later rule should win
	node := newNode("both", strAttr("class", "a,b"))
	applyStylesheetToNode(node, stylesheet)

	model, ok := node.Attr("llm_model")
	require.True(t, ok)
	assert.Equal(t, "second", model.Str)
}

func TestStylesheetTransform_AppliesFromGraphAttribute(t *testing.T) {
	graph := newTestGraph(
		[]*dotparser.Node{
			newNode("start", strAttr("shape", "Mdiamond")),
			newNode("plan", strAttr("class", "planning")),
			newNode("implement", strAttr("class", "code")),
			newNode("critical_review", strAttr("class", "code")),
			newNode("exit", strAttr("shape", "Msquare")),
		},
		[]*dotparser.Edge{
			newEdge("start", "plan"),
			newEdge("plan", "implement"),
			newEdge("implement", "critical_review"),
			newEdge("critical_review", "exit"),
		},
		[]dotparser.Attr{
			strAttr("model_stylesheet", `
				* { llm_model: claude-sonnet-4-5; llm_provider: anthropic; }
				.code { llm_model: claude-opus-4-6; }
				#critical_review { llm_model: gpt-5; llm_provider: openai; reasoning_effort: high; }
			`),
		},
	)

	transform := &StylesheetTransform{}
	graph = transform.Apply(graph)

	// plan gets universal rule
	plan := graph.NodeByID("plan")
	require.NotNil(t, plan)
	model, _ := plan.Attr("llm_model")
	assert.Equal(t, "claude-sonnet-4-5", model.Str)
	provider, _ := plan.Attr("llm_provider")
	assert.Equal(t, "anthropic", provider.Str)

	// implement gets .code rule
	implement := graph.NodeByID("implement")
	require.NotNil(t, implement)
	model, _ = implement.Attr("llm_model")
	assert.Equal(t, "claude-opus-4-6", model.Str)
	provider, _ = implement.Attr("llm_provider")
	assert.Equal(t, "anthropic", provider.Str)

	// critical_review gets #critical_review rule (highest specificity)
	critical := graph.NodeByID("critical_review")
	require.NotNil(t, critical)
	model, _ = critical.Attr("llm_model")
	assert.Equal(t, "gpt-5", model.Str)
	provider, _ = critical.Attr("llm_provider")
	assert.Equal(t, "openai", provider.Str)
	effort, _ := critical.Attr("reasoning_effort")
	assert.Equal(t, "high", effort.Str)
}

func TestStylesheetTransform_NoStylesheet(t *testing.T) {
	graph := newTestGraph(
		[]*dotparser.Node{
			newNode("start", strAttr("shape", "Mdiamond")),
			newNode("exit", strAttr("shape", "Msquare")),
		},
		[]*dotparser.Edge{
			newEdge("start", "exit"),
		},
		nil, // No graph attributes
	)

	transform := &StylesheetTransform{}
	result := transform.Apply(graph)

	// Graph should be unchanged
	assert.Equal(t, graph, result)
	// Nodes should not have llm_model added
	start := result.NodeByID("start")
	_, hasModel := start.Attr("llm_model")
	assert.False(t, hasModel)
}

func TestStylesheetTransform_InvalidStylesheetIgnored(t *testing.T) {
	graph := newTestGraph(
		[]*dotparser.Node{
			newNode("start", strAttr("shape", "Mdiamond")),
			newNode("work"),
			newNode("exit", strAttr("shape", "Msquare")),
		},
		[]*dotparser.Edge{
			newEdge("start", "work"),
			newEdge("work", "exit"),
		},
		[]dotparser.Attr{
			strAttr("model_stylesheet", `invalid { not: valid }`),
		},
	)

	transform := &StylesheetTransform{}
	result := transform.Apply(graph)

	// Graph should be unchanged when stylesheet is invalid
	work := result.NodeByID("work")
	_, hasModel := work.Attr("llm_model")
	assert.False(t, hasModel)
}

func TestStylesheetTransform_SpecificityOrder(t *testing.T) {
	// Test full specificity chain: universal < class < ID
	graph := newTestGraph(
		[]*dotparser.Node{
			newNode("universal_only"),
			newNode("class_only", strAttr("class", "myclass")),
			newNode("id_only"),
			newNode("all_match", strAttr("class", "myclass")),
		},
		nil,
		[]dotparser.Attr{
			strAttr("model_stylesheet", `
				* { llm_model: universal; reasoning_effort: low; }
				.myclass { llm_model: class; reasoning_effort: medium; }
				#all_match { llm_model: id; reasoning_effort: high; }
			`),
		},
	)

	transform := &StylesheetTransform{}
	graph = transform.Apply(graph)

	// universal_only: gets universal rule
	uNode := graph.NodeByID("universal_only")
	model, _ := uNode.Attr("llm_model")
	assert.Equal(t, "universal", model.Str)

	// class_only: class overrides universal
	cNode := graph.NodeByID("class_only")
	model, _ = cNode.Attr("llm_model")
	assert.Equal(t, "class", model.Str)

	// id_only: only universal matches (no class attr)
	iNode := graph.NodeByID("id_only")
	model, _ = iNode.Attr("llm_model")
	assert.Equal(t, "universal", model.Str)

	// all_match: ID beats class beats universal
	aNode := graph.NodeByID("all_match")
	model, _ = aNode.Attr("llm_model")
	assert.Equal(t, "id", model.Str)
	effort, _ := aNode.Attr("reasoning_effort")
	assert.Equal(t, "high", effort.Str)
}

func TestParseStylesheet_ShapeSelector(t *testing.T) {
	source := `box { llm_model: claude-opus-4-6; }`

	stylesheet, err := ParseStylesheet(source)

	require.NoError(t, err)
	require.Len(t, stylesheet.Rules, 1)

	rule := stylesheet.Rules[0]
	assert.Equal(t, SelectorShape, rule.Selector.Type)
	assert.Equal(t, "box", rule.Selector.Value)
	assert.Equal(t, 1, rule.Selector.Specificity())

	require.Len(t, rule.Declarations, 1)
	assert.Equal(t, "llm_model", rule.Declarations[0].Property)
	assert.Equal(t, "claude-opus-4-6", rule.Declarations[0].Value)
}

func TestSelectorMatchesNode_Shape(t *testing.T) {
	tests := []struct {
		name     string
		selector StyleSelector
		node     *dotparser.Node
		expected bool
	}{
		{
			"shape selector matches node with matching shape",
			StyleSelector{Type: SelectorShape, Value: "box"},
			newNode("n", strAttr("shape", "box")),
			true,
		},
		{
			"shape selector no match different shape",
			StyleSelector{Type: SelectorShape, Value: "box"},
			newNode("n", strAttr("shape", "diamond")),
			false,
		},
		{
			"shape selector no match no shape attr",
			StyleSelector{Type: SelectorShape, Value: "box"},
			newNode("n"),
			false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := selectorMatchesNode(tt.selector, tt.node)
			assert.Equal(t, tt.expected, result)
		})
	}
}

func TestApplyStylesheetToNode_ShapeSpecificity(t *testing.T) {
	// Test full specificity chain: universal(0) < shape(1) < class(2) < ID(3)
	graph := newTestGraph(
		[]*dotparser.Node{
			newNode("universal_only"),
			newNode("shape_only", strAttr("shape", "box")),
			newNode("class_only", strAttr("class", "myclass")),
			newNode("shape_and_class", strAttr("shape", "box"), strAttr("class", "myclass")),
			newNode("all_match", strAttr("shape", "box"), strAttr("class", "myclass")),
		},
		nil,
		[]dotparser.Attr{
			strAttr("model_stylesheet", `
				* { llm_model: universal; }
				box { llm_model: shape; }
				.myclass { llm_model: class; }
				#all_match { llm_model: id; }
			`),
		},
	)

	transform := &StylesheetTransform{}
	graph = transform.Apply(graph)

	// universal_only: gets universal rule
	uNode := graph.NodeByID("universal_only")
	model, _ := uNode.Attr("llm_model")
	assert.Equal(t, "universal", model.Str)

	// shape_only: shape overrides universal
	sNode := graph.NodeByID("shape_only")
	model, _ = sNode.Attr("llm_model")
	assert.Equal(t, "shape", model.Str)

	// class_only: class overrides universal (no shape attr so shape rule doesn't match)
	cNode := graph.NodeByID("class_only")
	model, _ = cNode.Attr("llm_model")
	assert.Equal(t, "class", model.Str)

	// shape_and_class: class(2) beats shape(1)
	scNode := graph.NodeByID("shape_and_class")
	model, _ = scNode.Attr("llm_model")
	assert.Equal(t, "class", model.Str)

	// all_match: ID(3) beats everything
	aNode := graph.NodeByID("all_match")
	model, _ = aNode.Attr("llm_model")
	assert.Equal(t, "id", model.Str)
}

func TestParseStylesheet_ClassNameWithHyphensAndNumbers(t *testing.T) {
	source := `.loop-a { llm_model: model1; }
	           .test-123 { llm_model: model2; }`

	stylesheet, err := ParseStylesheet(source)

	require.NoError(t, err)
	require.Len(t, stylesheet.Rules, 2)

	assert.Equal(t, "loop-a", stylesheet.Rules[0].Selector.Value)
	assert.Equal(t, "test-123", stylesheet.Rules[1].Selector.Value)
}

func TestParseStylesheet_IDWithUnderscores(t *testing.T) {
	source := `#critical_review { llm_model: model1; }`

	stylesheet, err := ParseStylesheet(source)

	require.NoError(t, err)
	require.Len(t, stylesheet.Rules, 1)
	assert.Equal(t, "critical_review", stylesheet.Rules[0].Selector.Value)
}
