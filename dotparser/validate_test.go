package dotparser

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// --- helpers ---

func mustParse(t *testing.T, src string) *Graph {
	t.Helper()
	g, err := Parse([]byte(src))
	require.NoError(t, err)
	return g
}

func diagsByRule(diags []Diagnostic, rule string) []Diagnostic {
	var out []Diagnostic
	for _, d := range diags {
		if d.Rule == rule {
			out = append(out, d)
		}
	}
	return out
}

func hasRule(diags []Diagnostic, rule string) bool {
	return len(diagsByRule(diags, rule)) > 0
}

// validPipeline is a minimal valid pipeline used as a baseline.
const validPipeline = `
digraph Valid {
    graph [goal="test"]
    start [shape=Mdiamond]
    work  [shape=box, prompt="do work"]
    exit  [shape=Msquare]
    start -> work -> exit
}
`

// --- Validate / ValidateOrError API tests ---

func TestValidateCleanPipeline(t *testing.T) {
	g := mustParse(t, validPipeline)
	diags := Validate(g)
	for _, d := range diags {
		assert.NotEqual(t, Error, d.Severity, "unexpected error: %s", d)
	}
}

func TestValidateOrErrorReturnsNilOnClean(t *testing.T) {
	g := mustParse(t, validPipeline)
	diags, err := ValidateOrError(g)
	require.NoError(t, err)
	for _, d := range diags {
		assert.NotEqual(t, Error, d.Severity)
	}
}

func TestValidateOrErrorReturnsErrorOnBroken(t *testing.T) {
	g := mustParse(t, `digraph D { A -> B }`) // no start/exit
	_, err := ValidateOrError(g)
	require.Error(t, err)
	var ve *ValidationError
	require.ErrorAs(t, err, &ve)
	assert.Greater(t, len(ve.Diagnostics), 0)
}

func TestValidateWithCustomRule(t *testing.T) {
	g := mustParse(t, validPipeline)
	custom := &testRule{
		name: "custom_check",
		fn: func(g *Graph) []Diagnostic {
			return []Diagnostic{{
				Rule: "custom_check", Severity: Info, Message: "custom info",
			}}
		},
	}
	diags := Validate(g, custom)
	assert.True(t, hasRule(diags, "custom_check"))
}

// --- start_node tests ---

func TestStartNodeMissing(t *testing.T) {
	g := mustParse(t, `digraph D {
		work [shape=box]
		exit [shape=Msquare]
		work -> exit
	}`)
	diags := Validate(g)
	starts := diagsByRule(diags, "start_node")
	require.Len(t, starts, 1)
	assert.Equal(t, Error, starts[0].Severity)
}

func TestStartNodeMultiple(t *testing.T) {
	g := mustParse(t, `digraph D {
		s1 [shape=Mdiamond]
		s2 [shape=Mdiamond]
		exit [shape=Msquare]
		s1 -> exit
		s2 -> exit
	}`)
	diags := Validate(g)
	starts := diagsByRule(diags, "start_node")
	assert.Len(t, starts, 2)
	for _, d := range starts {
		assert.Equal(t, Error, d.Severity)
	}
}

func TestStartNodeByIDStart(t *testing.T) {
	// "start" ID without Mdiamond shape should be recognized
	g := mustParse(t, `digraph D {
		start [shape=box]
		exit [shape=Msquare]
		start -> exit
	}`)
	diags := Validate(g)
	starts := diagsByRule(diags, "start_node")
	assert.Empty(t, starts)
}

func TestStartNodeByIDCapitalized(t *testing.T) {
	g := mustParse(t, `digraph D {
		Start [shape=box]
		exit [shape=Msquare]
		Start -> exit
	}`)
	diags := Validate(g)
	starts := diagsByRule(diags, "start_node")
	assert.Empty(t, starts)
}

// --- terminal_node tests ---

func TestTerminalNodeMissing(t *testing.T) {
	g := mustParse(t, `digraph D {
		start [shape=Mdiamond]
		work [shape=box]
		start -> work
	}`)
	diags := Validate(g)
	terms := diagsByRule(diags, "terminal_node")
	require.Len(t, terms, 1)
	assert.Equal(t, Error, terms[0].Severity)
}

func TestTerminalNodeByIDExit(t *testing.T) {
	g := mustParse(t, `digraph D {
		start [shape=Mdiamond]
		exit [shape=box]
		start -> exit
	}`)
	diags := Validate(g)
	terms := diagsByRule(diags, "terminal_node")
	assert.Empty(t, terms)
}

func TestTerminalNodeByIDEnd(t *testing.T) {
	g := mustParse(t, `digraph D {
		start [shape=Mdiamond]
		end [shape=box]
		start -> end
	}`)
	diags := Validate(g)
	terms := diagsByRule(diags, "terminal_node")
	assert.Empty(t, terms)
}

// --- edge_target_exists tests ---

func TestEdgeTargetExists_AllValid(t *testing.T) {
	g := mustParse(t, validPipeline)
	diags := Validate(g)
	assert.False(t, hasRule(diags, "edge_target_exists"))
}

// The parser auto-creates nodes from edges, so this rule fires in unusual cases.
// We construct a graph manually to test the logic.
func TestEdgeTargetExists_MissingTarget(t *testing.T) {
	g := &Graph{
		Name: "test",
		Nodes: []*Node{
			{ID: "A"},
		},
		Edges: []*Edge{
			{From: "A", To: "B"}, // B does not exist
		},
	}
	rule := edgeTargetExistsRule{}
	diags := rule.Apply(g)
	require.Len(t, diags, 1)
	assert.Equal(t, Error, diags[0].Severity)
	assert.NotNil(t, diags[0].Edge)
	assert.Equal(t, "B", diags[0].Edge.To)
}

func TestEdgeTargetExists_MissingSource(t *testing.T) {
	g := &Graph{
		Name: "test",
		Nodes: []*Node{
			{ID: "B"},
		},
		Edges: []*Edge{
			{From: "A", To: "B"}, // A does not exist
		},
	}
	rule := edgeTargetExistsRule{}
	diags := rule.Apply(g)
	require.Len(t, diags, 1)
	assert.Contains(t, diags[0].Message, "A")
}

// --- start_no_incoming tests ---

func TestStartNoIncoming_Clean(t *testing.T) {
	g := mustParse(t, validPipeline)
	diags := Validate(g)
	assert.False(t, hasRule(diags, "start_no_incoming"))
}

func TestStartNoIncoming_HasIncoming(t *testing.T) {
	g := mustParse(t, `digraph D {
		start [shape=Mdiamond]
		work  [shape=box, prompt="work"]
		exit  [shape=Msquare]
		start -> work -> exit
		work -> start
	}`)
	diags := Validate(g)
	sni := diagsByRule(diags, "start_no_incoming")
	require.Len(t, sni, 1)
	assert.Equal(t, Error, sni[0].Severity)
	assert.Equal(t, "start", sni[0].NodeID)
}

// --- exit_no_outgoing tests ---

func TestExitNoOutgoing_Clean(t *testing.T) {
	g := mustParse(t, validPipeline)
	diags := Validate(g)
	assert.False(t, hasRule(diags, "exit_no_outgoing"))
}

func TestExitNoOutgoing_HasOutgoing(t *testing.T) {
	g := mustParse(t, `digraph D {
		start [shape=Mdiamond]
		work  [shape=box, prompt="work"]
		exit  [shape=Msquare]
		start -> work -> exit
		exit -> work
	}`)
	diags := Validate(g)
	eno := diagsByRule(diags, "exit_no_outgoing")
	require.Len(t, eno, 1)
	assert.Equal(t, Error, eno[0].Severity)
	assert.Equal(t, "exit", eno[0].NodeID)
}

// --- reachability tests ---

func TestReachability_AllReachable(t *testing.T) {
	g := mustParse(t, validPipeline)
	diags := Validate(g)
	assert.False(t, hasRule(diags, "reachability"))
}

func TestReachability_UnreachableNode(t *testing.T) {
	g := mustParse(t, `digraph D {
		start  [shape=Mdiamond]
		work   [shape=box, prompt="work"]
		exit   [shape=Msquare]
		island [shape=box, prompt="unreachable"]
		start -> work -> exit
	}`)
	diags := Validate(g)
	reach := diagsByRule(diags, "reachability")
	require.Len(t, reach, 1)
	assert.Equal(t, Error, reach[0].Severity)
	assert.Equal(t, "island", reach[0].NodeID)
}

func TestReachability_MultipleUnreachable(t *testing.T) {
	g := mustParse(t, `digraph D {
		start  [shape=Mdiamond]
		exit   [shape=Msquare]
		orphan1 [shape=box]
		orphan2 [shape=box]
		start -> exit
	}`)
	diags := Validate(g)
	reach := diagsByRule(diags, "reachability")
	assert.Len(t, reach, 2)
}

func TestReachability_SkipsWhenNoStartNode(t *testing.T) {
	// If there's no start, reachability shouldn't produce spurious errors
	g := mustParse(t, `digraph D { A -> B }`)
	rule := reachabilityRule{}
	diags := rule.Apply(g)
	assert.Empty(t, diags)
}

// --- condition_syntax tests ---

func TestConditionSyntax_ValidExpressions(t *testing.T) {
	cases := []string{
		`outcome=success`,
		`outcome!=success`,
		`outcome=fail`,
		`preferred_label=Fix`,
		`outcome=success && context.tests_passed=true`,
		`context.loop_state!=exhausted`,
		`context.flag`,
	}
	for _, expr := range cases {
		t.Run(expr, func(t *testing.T) {
			err := validateConditionExpr(expr)
			assert.NoError(t, err)
		})
	}
}

func TestConditionSyntax_InvalidExpressions(t *testing.T) {
	cases := []struct {
		expr string
		desc string
	}{
		{"=value", "missing key"},
		{"key=", "missing value"},
		{"&&", "empty clauses"},
		{"outcome=success && ", "trailing empty clause"},
		{"context.=foo", "empty segment after context."},
	}
	for _, tc := range cases {
		t.Run(tc.desc, func(t *testing.T) {
			err := validateConditionExpr(tc.expr)
			assert.Error(t, err)
		})
	}
}

func TestConditionSyntax_OnEdge(t *testing.T) {
	g := mustParse(t, `digraph D {
		start [shape=Mdiamond]
		gate  [shape=diamond]
		ok    [shape=box, prompt="ok"]
		exit  [shape=Msquare]
		start -> gate
		gate -> ok [condition="=invalid"]
		ok -> exit
	}`)
	diags := Validate(g)
	cs := diagsByRule(diags, "condition_syntax")
	require.Len(t, cs, 1)
	assert.Equal(t, Error, cs[0].Severity)
	assert.NotNil(t, cs[0].Edge)
}

func TestConditionSyntax_ValidEdge(t *testing.T) {
	g := mustParse(t, `digraph D {
		start [shape=Mdiamond]
		gate  [shape=diamond]
		ok    [shape=box, prompt="ok"]
		exit  [shape=Msquare]
		start -> gate
		gate -> ok [condition="outcome=success"]
		ok -> exit
	}`)
	diags := Validate(g)
	assert.False(t, hasRule(diags, "condition_syntax"))
}

// --- stylesheet_syntax tests ---

func TestStylesheetSyntax_Valid(t *testing.T) {
	cases := []string{
		`* { llm_model: claude-sonnet-4-5; }`,
		`* { llm_model: claude-sonnet-4-5; llm_provider: anthropic; }`,
		`.code { llm_model: claude-opus-4-6; }`,
		`#node_id { reasoning_effort: high; }`,
		`* { llm_model: gpt-5.2; } .code { llm_model: claude-opus-4-6; }`,
	}
	for _, ss := range cases {
		t.Run(ss, func(t *testing.T) {
			err := validateStylesheet(ss)
			assert.NoError(t, err)
		})
	}
}

func TestStylesheetSyntax_Invalid(t *testing.T) {
	cases := []struct {
		ss   string
		desc string
	}{
		{"", "empty stylesheet"},
		{"* {", "unterminated block"},
		{"{ llm_model: foo; }", "missing selector"},
		{"* { unknown_prop: val; }", "unknown property"},
		{"* { llm_model; }", "missing colon"},
		{"* { llm_model: ; }", "empty value"},
	}
	for _, tc := range cases {
		t.Run(tc.desc, func(t *testing.T) {
			err := validateStylesheet(tc.ss)
			assert.Error(t, err)
		})
	}
}

func TestStylesheetSyntax_OnGraph(t *testing.T) {
	g := mustParse(t, `digraph D {
		graph [
			goal="test",
			model_stylesheet="* { bad_prop: val; }"
		]
		start [shape=Mdiamond]
		exit  [shape=Msquare]
		start -> exit
	}`)
	diags := Validate(g)
	ss := diagsByRule(diags, "stylesheet_syntax")
	require.Len(t, ss, 1)
	assert.Equal(t, Error, ss[0].Severity)
}

func TestStylesheetSyntax_NoStylesheet(t *testing.T) {
	g := mustParse(t, validPipeline)
	diags := Validate(g)
	assert.False(t, hasRule(diags, "stylesheet_syntax"))
}

// --- type_known tests ---

func TestTypeKnown_RecognizedTypes(t *testing.T) {
	g := mustParse(t, `digraph D {
		start [shape=Mdiamond]
		gate  [type="wait.human", shape=hexagon]
		exit  [shape=Msquare]
		start -> gate -> exit
	}`)
	diags := Validate(g)
	assert.False(t, hasRule(diags, "type_known"))
}

func TestTypeKnown_UnrecognizedType(t *testing.T) {
	g := mustParse(t, `digraph D {
		start [shape=Mdiamond]
		work  [type="custom_thing", prompt="work"]
		exit  [shape=Msquare]
		start -> work -> exit
	}`)
	diags := Validate(g)
	tk := diagsByRule(diags, "type_known")
	require.Len(t, tk, 1)
	assert.Equal(t, Warning, tk[0].Severity)
	assert.Equal(t, "work", tk[0].NodeID)
}

// --- fidelity_valid tests ---

func TestFidelityValid_ValidValues(t *testing.T) {
	for _, mode := range []string{"full", "truncate", "compact", "summary:low", "summary:medium", "summary:high"} {
		t.Run(mode, func(t *testing.T) {
			g := mustParse(t, `digraph D {
				start [shape=Mdiamond]
				work  [shape=box, prompt="work", fidelity="`+mode+`"]
				exit  [shape=Msquare]
				start -> work -> exit
			}`)
			diags := Validate(g)
			assert.False(t, hasRule(diags, "fidelity_valid"))
		})
	}
}

func TestFidelityValid_InvalidNodeFidelity(t *testing.T) {
	g := mustParse(t, `digraph D {
		start [shape=Mdiamond]
		work  [shape=box, prompt="work", fidelity="invalid"]
		exit  [shape=Msquare]
		start -> work -> exit
	}`)
	diags := Validate(g)
	fv := diagsByRule(diags, "fidelity_valid")
	require.Len(t, fv, 1)
	assert.Equal(t, Warning, fv[0].Severity)
}

func TestFidelityValid_InvalidGraphDefault(t *testing.T) {
	g := mustParse(t, `digraph D {
		graph [default_fidelity="bogus"]
		start [shape=Mdiamond]
		exit  [shape=Msquare]
		start -> exit
	}`)
	diags := Validate(g)
	fv := diagsByRule(diags, "fidelity_valid")
	require.Len(t, fv, 1)
	assert.Equal(t, Warning, fv[0].Severity)
}

func TestFidelityValid_InvalidEdgeFidelity(t *testing.T) {
	g := mustParse(t, `digraph D {
		start [shape=Mdiamond]
		work  [shape=box, prompt="work"]
		exit  [shape=Msquare]
		start -> work [fidelity="wrong"]
		work -> exit
	}`)
	diags := Validate(g)
	fv := diagsByRule(diags, "fidelity_valid")
	require.Len(t, fv, 1)
	assert.Equal(t, Warning, fv[0].Severity)
	assert.NotNil(t, fv[0].Edge)
}

// --- retry_target_exists tests ---

func TestRetryTargetExists_Valid(t *testing.T) {
	g := mustParse(t, `digraph D {
		start [shape=Mdiamond]
		work  [shape=box, prompt="work", retry_target="start"]
		exit  [shape=Msquare]
		start -> work -> exit
	}`)
	diags := Validate(g)
	assert.False(t, hasRule(diags, "retry_target_exists"))
}

func TestRetryTargetExists_MissingTarget(t *testing.T) {
	g := mustParse(t, `digraph D {
		start [shape=Mdiamond]
		work  [shape=box, prompt="work", retry_target="nonexistent"]
		exit  [shape=Msquare]
		start -> work -> exit
	}`)
	diags := Validate(g)
	rt := diagsByRule(diags, "retry_target_exists")
	require.Len(t, rt, 1)
	assert.Equal(t, Warning, rt[0].Severity)
	assert.Equal(t, "work", rt[0].NodeID)
}

func TestRetryTargetExists_GraphLevel(t *testing.T) {
	g := mustParse(t, `digraph D {
		graph [retry_target="phantom"]
		start [shape=Mdiamond]
		exit  [shape=Msquare]
		start -> exit
	}`)
	diags := Validate(g)
	rt := diagsByRule(diags, "retry_target_exists")
	require.Len(t, rt, 1)
	assert.Equal(t, Warning, rt[0].Severity)
}

func TestRetryTargetExists_FallbackRetryTarget(t *testing.T) {
	g := mustParse(t, `digraph D {
		start [shape=Mdiamond]
		work  [shape=box, prompt="work", fallback_retry_target="ghost"]
		exit  [shape=Msquare]
		start -> work -> exit
	}`)
	diags := Validate(g)
	rt := diagsByRule(diags, "retry_target_exists")
	require.Len(t, rt, 1)
	assert.Equal(t, Warning, rt[0].Severity)
}

// --- goal_gate_has_retry tests ---

func TestGoalGateHasRetry_WithRetryTarget(t *testing.T) {
	g := mustParse(t, `digraph D {
		start [shape=Mdiamond]
		work  [shape=box, prompt="work", goal_gate=true, retry_target="start"]
		exit  [shape=Msquare]
		start -> work -> exit
	}`)
	diags := Validate(g)
	assert.False(t, hasRule(diags, "goal_gate_has_retry"))
}

func TestGoalGateHasRetry_WithGraphRetry(t *testing.T) {
	g := mustParse(t, `digraph D {
		graph [retry_target="start"]
		start [shape=Mdiamond]
		work  [shape=box, prompt="work", goal_gate=true]
		exit  [shape=Msquare]
		start -> work -> exit
	}`)
	diags := Validate(g)
	assert.False(t, hasRule(diags, "goal_gate_has_retry"))
}

func TestGoalGateHasRetry_Missing(t *testing.T) {
	g := mustParse(t, `digraph D {
		start [shape=Mdiamond]
		work  [shape=box, prompt="work", goal_gate=true]
		exit  [shape=Msquare]
		start -> work -> exit
	}`)
	diags := Validate(g)
	gg := diagsByRule(diags, "goal_gate_has_retry")
	require.Len(t, gg, 1)
	assert.Equal(t, Warning, gg[0].Severity)
	assert.Equal(t, "work", gg[0].NodeID)
}

// --- prompt_on_llm_nodes tests ---

func TestPromptOnLLMNodes_HasPrompt(t *testing.T) {
	g := mustParse(t, validPipeline)
	diags := Validate(g)
	assert.False(t, hasRule(diags, "prompt_on_llm_nodes"))
}

func TestPromptOnLLMNodes_HasLabel(t *testing.T) {
	g := mustParse(t, `digraph D {
		start [shape=Mdiamond]
		work  [shape=box, label="Do the work"]
		exit  [shape=Msquare]
		start -> work -> exit
	}`)
	diags := Validate(g)
	assert.False(t, hasRule(diags, "prompt_on_llm_nodes"))
}

func TestPromptOnLLMNodes_Missing(t *testing.T) {
	g := mustParse(t, `digraph D {
		start [shape=Mdiamond]
		work  [shape=box]
		exit  [shape=Msquare]
		start -> work -> exit
	}`)
	diags := Validate(g)
	pn := diagsByRule(diags, "prompt_on_llm_nodes")
	require.Len(t, pn, 1)
	assert.Equal(t, Warning, pn[0].Severity)
	assert.Equal(t, "work", pn[0].NodeID)
}

func TestPromptOnLLMNodes_NonLLMNode(t *testing.T) {
	// diamond shape resolves to "conditional", not codergen
	g := mustParse(t, `digraph D {
		start [shape=Mdiamond]
		gate  [shape=diamond]
		exit  [shape=Msquare]
		start -> gate -> exit
	}`)
	diags := Validate(g)
	assert.False(t, hasRule(diags, "prompt_on_llm_nodes"))
}

// --- Integration: full pipeline from spec ---

func TestValidate_SpecSmokeTest(t *testing.T) {
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
	g := mustParse(t, src)
	diags, err := ValidateOrError(g)
	require.NoError(t, err)

	// Should have a warning about goal_gate without retry_target
	gg := diagsByRule(diags, "goal_gate_has_retry")
	assert.Len(t, gg, 1)
	assert.Equal(t, Warning, gg[0].Severity)
}

func TestValidate_BranchingWorkflow(t *testing.T) {
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
	g := mustParse(t, src)
	_, err := ValidateOrError(g)
	require.NoError(t, err)
}

func TestValidate_CompleteStylesheetPipeline(t *testing.T) {
	src := `
digraph Pipeline {
    graph [
        goal="Implement feature X",
        model_stylesheet="
            * { llm_model: claude-sonnet-4-5; llm_provider: anthropic; }
            .code { llm_model: claude-opus-4-6; llm_provider: anthropic; }
            #critical_review { llm_model: gpt-5-2; llm_provider: openai; reasoning_effort: high; }
        "
    ]

    start [shape=Mdiamond]
    exit  [shape=Msquare]

    plan            [label="Plan", prompt="plan", class="planning"]
    implement       [label="Implement", prompt="code", class="code"]
    critical_review [label="Critical Review", prompt="review", class="code"]

    start -> plan -> implement -> critical_review -> exit
}
`
	g := mustParse(t, src)
	_, err := ValidateOrError(g)
	require.NoError(t, err)
}

// --- Diagnostic.String() ---

func TestDiagnosticString(t *testing.T) {
	d := Diagnostic{
		Rule:     "test_rule",
		Severity: Error,
		Message:  "something broke",
		NodeID:   "my_node",
		Edge:     &EdgeRef{From: "A", To: "B"},
		Fix:      "fix it",
	}
	s := d.String()
	assert.Contains(t, s, "ERROR")
	assert.Contains(t, s, "test_rule")
	assert.Contains(t, s, "something broke")
	assert.Contains(t, s, "my_node")
	assert.Contains(t, s, "A -> B")
	assert.Contains(t, s, "fix it")
}

func TestSeverityString(t *testing.T) {
	assert.Equal(t, "ERROR", Error.String())
	assert.Equal(t, "WARNING", Warning.String())
	assert.Equal(t, "INFO", Info.String())
}

// --- Condition expression edge cases ---

func TestValidateConditionExpr_CompoundAnd(t *testing.T) {
	err := validateConditionExpr("outcome=success && context.tests_passed=true")
	assert.NoError(t, err)
}

func TestValidateConditionExpr_BareKey(t *testing.T) {
	err := validateConditionExpr("context.some_flag")
	assert.NoError(t, err)
}

func TestValidateConditionExpr_UnqualifiedKey(t *testing.T) {
	err := validateConditionExpr("my_flag=true")
	assert.NoError(t, err)
}

// --- Stylesheet edge cases ---

func TestStylesheet_MultipleRules(t *testing.T) {
	ss := `* { llm_model: a; } .code { llm_provider: openai; } #x { reasoning_effort: high; }`
	err := validateStylesheet(ss)
	assert.NoError(t, err)
}

func TestStylesheet_TrailingSemicolon(t *testing.T) {
	ss := `* { llm_model: val; }`
	err := validateStylesheet(ss)
	assert.NoError(t, err)
}

func TestStylesheet_NoTrailingSemicolon(t *testing.T) {
	ss := `* { llm_model: val }`
	err := validateStylesheet(ss)
	assert.NoError(t, err)
}

// --- Helper: test rule ---

type testRule struct {
	name string
	fn   func(*Graph) []Diagnostic
}

func (r *testRule) Name() string              { return r.name }
func (r *testRule) Apply(g *Graph) []Diagnostic { return r.fn(g) }
