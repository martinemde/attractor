package pipeline

import (
	"testing"

	"github.com/martinemde/attractor/dotparser"
)

// Helper to create a minimal valid graph for testing.
func validGraph() *dotparser.Graph {
	return &dotparser.Graph{
		Name: "TestGraph",
		Nodes: []*dotparser.Node{
			{ID: "start", Attrs: []dotparser.Attr{{Key: "shape", Value: dotparser.Value{Kind: dotparser.ValueString, Str: "Mdiamond"}}}},
			{ID: "work", Attrs: []dotparser.Attr{{Key: "label", Value: dotparser.Value{Kind: dotparser.ValueString, Str: "Do work"}}}},
			{ID: "exit", Attrs: []dotparser.Attr{{Key: "shape", Value: dotparser.Value{Kind: dotparser.ValueString, Str: "Msquare"}}}},
		},
		Edges: []*dotparser.Edge{
			{From: "start", To: "work"},
			{From: "work", To: "exit"},
		},
	}
}

// ---------- start_node rule tests ----------

func TestStartNodeRule_MissingStartNode(t *testing.T) {
	graph := &dotparser.Graph{
		Name: "NoStart",
		Nodes: []*dotparser.Node{
			{ID: "work"},
			{ID: "exit", Attrs: []dotparser.Attr{{Key: "shape", Value: dotparser.Value{Kind: dotparser.ValueString, Str: "Msquare"}}}},
		},
	}

	rule := &StartNodeRule{}
	diagnostics := rule.Apply(graph)

	if len(diagnostics) != 1 {
		t.Fatalf("expected 1 diagnostic, got %d", len(diagnostics))
	}
	if diagnostics[0].Rule != "start_node" {
		t.Errorf("expected rule 'start_node', got %q", diagnostics[0].Rule)
	}
	if diagnostics[0].Severity != SeverityError {
		t.Errorf("expected severity ERROR, got %v", diagnostics[0].Severity)
	}
}

func TestStartNodeRule_MultipleStartNodes(t *testing.T) {
	graph := &dotparser.Graph{
		Name: "MultiStart",
		Nodes: []*dotparser.Node{
			{ID: "start1", Attrs: []dotparser.Attr{{Key: "shape", Value: dotparser.Value{Kind: dotparser.ValueString, Str: "Mdiamond"}}}},
			{ID: "start2", Attrs: []dotparser.Attr{{Key: "shape", Value: dotparser.Value{Kind: dotparser.ValueString, Str: "Mdiamond"}}}},
			{ID: "exit", Attrs: []dotparser.Attr{{Key: "shape", Value: dotparser.Value{Kind: dotparser.ValueString, Str: "Msquare"}}}},
		},
	}

	rule := &StartNodeRule{}
	diagnostics := rule.Apply(graph)

	if len(diagnostics) != 1 {
		t.Fatalf("expected 1 diagnostic, got %d", len(diagnostics))
	}
	if diagnostics[0].Severity != SeverityError {
		t.Errorf("expected severity ERROR, got %v", diagnostics[0].Severity)
	}
}

func TestStartNodeRule_ValidByShape(t *testing.T) {
	graph := validGraph()
	rule := &StartNodeRule{}
	diagnostics := rule.Apply(graph)

	if len(diagnostics) != 0 {
		t.Errorf("expected no diagnostics, got %d: %v", len(diagnostics), diagnostics)
	}
}

func TestStartNodeRule_ValidByID(t *testing.T) {
	graph := &dotparser.Graph{
		Name: "StartByID",
		Nodes: []*dotparser.Node{
			{ID: "start"},
			{ID: "exit", Attrs: []dotparser.Attr{{Key: "shape", Value: dotparser.Value{Kind: dotparser.ValueString, Str: "Msquare"}}}},
		},
		Edges: []*dotparser.Edge{
			{From: "start", To: "exit"},
		},
	}

	rule := &StartNodeRule{}
	diagnostics := rule.Apply(graph)

	if len(diagnostics) != 0 {
		t.Errorf("expected no diagnostics, got %d: %v", len(diagnostics), diagnostics)
	}
}

func TestStartNodeRule_ValidByIDCapitalized(t *testing.T) {
	graph := &dotparser.Graph{
		Name: "StartByIDCap",
		Nodes: []*dotparser.Node{
			{ID: "Start"},
			{ID: "exit", Attrs: []dotparser.Attr{{Key: "shape", Value: dotparser.Value{Kind: dotparser.ValueString, Str: "Msquare"}}}},
		},
		Edges: []*dotparser.Edge{
			{From: "Start", To: "exit"},
		},
	}

	rule := &StartNodeRule{}
	diagnostics := rule.Apply(graph)

	if len(diagnostics) != 0 {
		t.Errorf("expected no diagnostics, got %d: %v", len(diagnostics), diagnostics)
	}
}

// ---------- terminal_node rule tests ----------

func TestTerminalNodeRule_MissingExitNode(t *testing.T) {
	graph := &dotparser.Graph{
		Name: "NoExit",
		Nodes: []*dotparser.Node{
			{ID: "start", Attrs: []dotparser.Attr{{Key: "shape", Value: dotparser.Value{Kind: dotparser.ValueString, Str: "Mdiamond"}}}},
			{ID: "work"},
		},
	}

	rule := &TerminalNodeRule{}
	diagnostics := rule.Apply(graph)

	if len(diagnostics) != 1 {
		t.Fatalf("expected 1 diagnostic, got %d", len(diagnostics))
	}
	if diagnostics[0].Rule != "terminal_node" {
		t.Errorf("expected rule 'terminal_node', got %q", diagnostics[0].Rule)
	}
	if diagnostics[0].Severity != SeverityError {
		t.Errorf("expected severity ERROR, got %v", diagnostics[0].Severity)
	}
}

func TestTerminalNodeRule_ValidByShape(t *testing.T) {
	graph := validGraph()
	rule := &TerminalNodeRule{}
	diagnostics := rule.Apply(graph)

	if len(diagnostics) != 0 {
		t.Errorf("expected no diagnostics, got %d: %v", len(diagnostics), diagnostics)
	}
}

func TestTerminalNodeRule_ValidByIDExit(t *testing.T) {
	graph := &dotparser.Graph{
		Name: "ExitByID",
		Nodes: []*dotparser.Node{
			{ID: "start", Attrs: []dotparser.Attr{{Key: "shape", Value: dotparser.Value{Kind: dotparser.ValueString, Str: "Mdiamond"}}}},
			{ID: "exit"},
		},
	}

	rule := &TerminalNodeRule{}
	diagnostics := rule.Apply(graph)

	if len(diagnostics) != 0 {
		t.Errorf("expected no diagnostics, got %d: %v", len(diagnostics), diagnostics)
	}
}

func TestTerminalNodeRule_ValidByIDEnd(t *testing.T) {
	graph := &dotparser.Graph{
		Name: "EndByID",
		Nodes: []*dotparser.Node{
			{ID: "start", Attrs: []dotparser.Attr{{Key: "shape", Value: dotparser.Value{Kind: dotparser.ValueString, Str: "Mdiamond"}}}},
			{ID: "end"},
		},
	}

	rule := &TerminalNodeRule{}
	diagnostics := rule.Apply(graph)

	if len(diagnostics) != 0 {
		t.Errorf("expected no diagnostics, got %d: %v", len(diagnostics), diagnostics)
	}
}

// ---------- reachability rule tests ----------

func TestReachabilityRule_UnreachableNode(t *testing.T) {
	graph := &dotparser.Graph{
		Name: "Unreachable",
		Nodes: []*dotparser.Node{
			{ID: "start", Attrs: []dotparser.Attr{{Key: "shape", Value: dotparser.Value{Kind: dotparser.ValueString, Str: "Mdiamond"}}}},
			{ID: "work"},
			{ID: "orphan"},
			{ID: "exit", Attrs: []dotparser.Attr{{Key: "shape", Value: dotparser.Value{Kind: dotparser.ValueString, Str: "Msquare"}}}},
		},
		Edges: []*dotparser.Edge{
			{From: "start", To: "work"},
			{From: "work", To: "exit"},
		},
	}

	rule := &ReachabilityRule{}
	diagnostics := rule.Apply(graph)

	if len(diagnostics) != 1 {
		t.Fatalf("expected 1 diagnostic, got %d", len(diagnostics))
	}
	if diagnostics[0].Rule != "reachability" {
		t.Errorf("expected rule 'reachability', got %q", diagnostics[0].Rule)
	}
	if diagnostics[0].NodeID != "orphan" {
		t.Errorf("expected node 'orphan', got %q", diagnostics[0].NodeID)
	}
	if diagnostics[0].Severity != SeverityError {
		t.Errorf("expected severity ERROR, got %v", diagnostics[0].Severity)
	}
}

func TestReachabilityRule_AllReachable(t *testing.T) {
	graph := validGraph()
	rule := &ReachabilityRule{}
	diagnostics := rule.Apply(graph)

	if len(diagnostics) != 0 {
		t.Errorf("expected no diagnostics, got %d: %v", len(diagnostics), diagnostics)
	}
}

// ---------- edge_target_exists rule tests ----------

func TestEdgeTargetExistsRule_MissingTarget(t *testing.T) {
	graph := &dotparser.Graph{
		Name: "BadEdge",
		Nodes: []*dotparser.Node{
			{ID: "start", Attrs: []dotparser.Attr{{Key: "shape", Value: dotparser.Value{Kind: dotparser.ValueString, Str: "Mdiamond"}}}},
			{ID: "exit", Attrs: []dotparser.Attr{{Key: "shape", Value: dotparser.Value{Kind: dotparser.ValueString, Str: "Msquare"}}}},
		},
		Edges: []*dotparser.Edge{
			{From: "start", To: "nonexistent"},
		},
	}

	rule := &EdgeTargetExistsRule{}
	diagnostics := rule.Apply(graph)

	if len(diagnostics) != 1 {
		t.Fatalf("expected 1 diagnostic, got %d", len(diagnostics))
	}
	if diagnostics[0].Rule != "edge_target_exists" {
		t.Errorf("expected rule 'edge_target_exists', got %q", diagnostics[0].Rule)
	}
	if diagnostics[0].Severity != SeverityError {
		t.Errorf("expected severity ERROR, got %v", diagnostics[0].Severity)
	}
}

func TestEdgeTargetExistsRule_MissingSource(t *testing.T) {
	graph := &dotparser.Graph{
		Name: "BadEdgeSource",
		Nodes: []*dotparser.Node{
			{ID: "start", Attrs: []dotparser.Attr{{Key: "shape", Value: dotparser.Value{Kind: dotparser.ValueString, Str: "Mdiamond"}}}},
			{ID: "exit", Attrs: []dotparser.Attr{{Key: "shape", Value: dotparser.Value{Kind: dotparser.ValueString, Str: "Msquare"}}}},
		},
		Edges: []*dotparser.Edge{
			{From: "nonexistent", To: "exit"},
		},
	}

	rule := &EdgeTargetExistsRule{}
	diagnostics := rule.Apply(graph)

	if len(diagnostics) != 1 {
		t.Fatalf("expected 1 diagnostic, got %d", len(diagnostics))
	}
}

func TestEdgeTargetExistsRule_Valid(t *testing.T) {
	graph := validGraph()
	rule := &EdgeTargetExistsRule{}
	diagnostics := rule.Apply(graph)

	if len(diagnostics) != 0 {
		t.Errorf("expected no diagnostics, got %d: %v", len(diagnostics), diagnostics)
	}
}

// ---------- start_no_incoming rule tests ----------

func TestStartNoIncomingRule_HasIncoming(t *testing.T) {
	graph := &dotparser.Graph{
		Name: "StartHasIncoming",
		Nodes: []*dotparser.Node{
			{ID: "start", Attrs: []dotparser.Attr{{Key: "shape", Value: dotparser.Value{Kind: dotparser.ValueString, Str: "Mdiamond"}}}},
			{ID: "work"},
			{ID: "exit", Attrs: []dotparser.Attr{{Key: "shape", Value: dotparser.Value{Kind: dotparser.ValueString, Str: "Msquare"}}}},
		},
		Edges: []*dotparser.Edge{
			{From: "start", To: "work"},
			{From: "work", To: "start"}, // Bad: incoming to start
			{From: "work", To: "exit"},
		},
	}

	rule := &StartNoIncomingRule{}
	diagnostics := rule.Apply(graph)

	if len(diagnostics) != 1 {
		t.Fatalf("expected 1 diagnostic, got %d", len(diagnostics))
	}
	if diagnostics[0].Rule != "start_no_incoming" {
		t.Errorf("expected rule 'start_no_incoming', got %q", diagnostics[0].Rule)
	}
	if diagnostics[0].Severity != SeverityError {
		t.Errorf("expected severity ERROR, got %v", diagnostics[0].Severity)
	}
}

func TestStartNoIncomingRule_Valid(t *testing.T) {
	graph := validGraph()
	rule := &StartNoIncomingRule{}
	diagnostics := rule.Apply(graph)

	if len(diagnostics) != 0 {
		t.Errorf("expected no diagnostics, got %d: %v", len(diagnostics), diagnostics)
	}
}

// ---------- exit_no_outgoing rule tests ----------

func TestExitNoOutgoingRule_HasOutgoing(t *testing.T) {
	graph := &dotparser.Graph{
		Name: "ExitHasOutgoing",
		Nodes: []*dotparser.Node{
			{ID: "start", Attrs: []dotparser.Attr{{Key: "shape", Value: dotparser.Value{Kind: dotparser.ValueString, Str: "Mdiamond"}}}},
			{ID: "work"},
			{ID: "exit", Attrs: []dotparser.Attr{{Key: "shape", Value: dotparser.Value{Kind: dotparser.ValueString, Str: "Msquare"}}}},
		},
		Edges: []*dotparser.Edge{
			{From: "start", To: "work"},
			{From: "work", To: "exit"},
			{From: "exit", To: "work"}, // Bad: outgoing from exit
		},
	}

	rule := &ExitNoOutgoingRule{}
	diagnostics := rule.Apply(graph)

	if len(diagnostics) != 1 {
		t.Fatalf("expected 1 diagnostic, got %d", len(diagnostics))
	}
	if diagnostics[0].Rule != "exit_no_outgoing" {
		t.Errorf("expected rule 'exit_no_outgoing', got %q", diagnostics[0].Rule)
	}
	if diagnostics[0].Severity != SeverityError {
		t.Errorf("expected severity ERROR, got %v", diagnostics[0].Severity)
	}
}

func TestExitNoOutgoingRule_Valid(t *testing.T) {
	graph := validGraph()
	rule := &ExitNoOutgoingRule{}
	diagnostics := rule.Apply(graph)

	if len(diagnostics) != 0 {
		t.Errorf("expected no diagnostics, got %d: %v", len(diagnostics), diagnostics)
	}
}

// ---------- condition_syntax rule tests ----------

func TestConditionSyntaxRule_InvalidCondition(t *testing.T) {
	graph := &dotparser.Graph{
		Name: "BadCondition",
		Nodes: []*dotparser.Node{
			{ID: "start", Attrs: []dotparser.Attr{{Key: "shape", Value: dotparser.Value{Kind: dotparser.ValueString, Str: "Mdiamond"}}}},
			{ID: "exit", Attrs: []dotparser.Attr{{Key: "shape", Value: dotparser.Value{Kind: dotparser.ValueString, Str: "Msquare"}}}},
		},
		Edges: []*dotparser.Edge{
			{
				From: "start",
				To:   "exit",
				Attrs: []dotparser.Attr{
					{Key: "condition", Value: dotparser.Value{Kind: dotparser.ValueString, Str: "=value"}}, // Missing key
				},
			},
		},
	}

	rule := &ConditionSyntaxRule{}
	diagnostics := rule.Apply(graph)

	if len(diagnostics) != 1 {
		t.Fatalf("expected 1 diagnostic, got %d", len(diagnostics))
	}
	if diagnostics[0].Rule != "condition_syntax" {
		t.Errorf("expected rule 'condition_syntax', got %q", diagnostics[0].Rule)
	}
	if diagnostics[0].Severity != SeverityError {
		t.Errorf("expected severity ERROR, got %v", diagnostics[0].Severity)
	}
}

func TestConditionSyntaxRule_ValidCondition(t *testing.T) {
	graph := &dotparser.Graph{
		Name: "GoodCondition",
		Nodes: []*dotparser.Node{
			{ID: "start", Attrs: []dotparser.Attr{{Key: "shape", Value: dotparser.Value{Kind: dotparser.ValueString, Str: "Mdiamond"}}}},
			{ID: "exit", Attrs: []dotparser.Attr{{Key: "shape", Value: dotparser.Value{Kind: dotparser.ValueString, Str: "Msquare"}}}},
		},
		Edges: []*dotparser.Edge{
			{
				From: "start",
				To:   "exit",
				Attrs: []dotparser.Attr{
					{Key: "condition", Value: dotparser.Value{Kind: dotparser.ValueString, Str: "outcome=success && context.ready"}},
				},
			},
		},
	}

	rule := &ConditionSyntaxRule{}
	diagnostics := rule.Apply(graph)

	if len(diagnostics) != 0 {
		t.Errorf("expected no diagnostics, got %d: %v", len(diagnostics), diagnostics)
	}
}

func TestConditionSyntaxRule_EmptyCondition(t *testing.T) {
	graph := validGraph()
	rule := &ConditionSyntaxRule{}
	diagnostics := rule.Apply(graph)

	if len(diagnostics) != 0 {
		t.Errorf("expected no diagnostics, got %d: %v", len(diagnostics), diagnostics)
	}
}

// ---------- type_known rule tests ----------

func TestTypeKnownRule_UnknownType(t *testing.T) {
	graph := &dotparser.Graph{
		Name: "UnknownType",
		Nodes: []*dotparser.Node{
			{ID: "start", Attrs: []dotparser.Attr{{Key: "shape", Value: dotparser.Value{Kind: dotparser.ValueString, Str: "Mdiamond"}}}},
			{ID: "work", Attrs: []dotparser.Attr{
				{Key: "type", Value: dotparser.Value{Kind: dotparser.ValueString, Str: "unknown_handler"}},
			}},
			{ID: "exit", Attrs: []dotparser.Attr{{Key: "shape", Value: dotparser.Value{Kind: dotparser.ValueString, Str: "Msquare"}}}},
		},
		Edges: []*dotparser.Edge{
			{From: "start", To: "work"},
			{From: "work", To: "exit"},
		},
	}

	rule := &TypeKnownRule{}
	diagnostics := rule.Apply(graph)

	if len(diagnostics) != 1 {
		t.Fatalf("expected 1 diagnostic, got %d", len(diagnostics))
	}
	if diagnostics[0].Rule != "type_known" {
		t.Errorf("expected rule 'type_known', got %q", diagnostics[0].Rule)
	}
	if diagnostics[0].Severity != SeverityWarning {
		t.Errorf("expected severity WARNING, got %v", diagnostics[0].Severity)
	}
}

func TestTypeKnownRule_KnownType(t *testing.T) {
	graph := &dotparser.Graph{
		Name: "KnownType",
		Nodes: []*dotparser.Node{
			{ID: "start", Attrs: []dotparser.Attr{{Key: "shape", Value: dotparser.Value{Kind: dotparser.ValueString, Str: "Mdiamond"}}}},
			{ID: "work", Attrs: []dotparser.Attr{
				{Key: "type", Value: dotparser.Value{Kind: dotparser.ValueString, Str: "codergen"}},
			}},
			{ID: "exit", Attrs: []dotparser.Attr{{Key: "shape", Value: dotparser.Value{Kind: dotparser.ValueString, Str: "Msquare"}}}},
		},
		Edges: []*dotparser.Edge{
			{From: "start", To: "work"},
			{From: "work", To: "exit"},
		},
	}

	rule := &TypeKnownRule{}
	diagnostics := rule.Apply(graph)

	if len(diagnostics) != 0 {
		t.Errorf("expected no diagnostics, got %d: %v", len(diagnostics), diagnostics)
	}
}

// ---------- fidelity_valid rule tests ----------

func TestFidelityValidRule_InvalidNodeFidelity(t *testing.T) {
	graph := &dotparser.Graph{
		Name: "BadFidelity",
		Nodes: []*dotparser.Node{
			{ID: "start", Attrs: []dotparser.Attr{{Key: "shape", Value: dotparser.Value{Kind: dotparser.ValueString, Str: "Mdiamond"}}}},
			{ID: "work", Attrs: []dotparser.Attr{
				{Key: "fidelity", Value: dotparser.Value{Kind: dotparser.ValueString, Str: "invalid_mode"}},
			}},
			{ID: "exit", Attrs: []dotparser.Attr{{Key: "shape", Value: dotparser.Value{Kind: dotparser.ValueString, Str: "Msquare"}}}},
		},
		Edges: []*dotparser.Edge{
			{From: "start", To: "work"},
			{From: "work", To: "exit"},
		},
	}

	rule := &FidelityValidRule{}
	diagnostics := rule.Apply(graph)

	if len(diagnostics) != 1 {
		t.Fatalf("expected 1 diagnostic, got %d", len(diagnostics))
	}
	if diagnostics[0].Rule != "fidelity_valid" {
		t.Errorf("expected rule 'fidelity_valid', got %q", diagnostics[0].Rule)
	}
	if diagnostics[0].Severity != SeverityWarning {
		t.Errorf("expected severity WARNING, got %v", diagnostics[0].Severity)
	}
}

func TestFidelityValidRule_ValidFidelity(t *testing.T) {
	for _, mode := range []string{"full", "truncate", "compact", "summary:low", "summary:medium", "summary:high"} {
		t.Run(mode, func(t *testing.T) {
			graph := &dotparser.Graph{
				Name: "GoodFidelity",
				Nodes: []*dotparser.Node{
					{ID: "start", Attrs: []dotparser.Attr{{Key: "shape", Value: dotparser.Value{Kind: dotparser.ValueString, Str: "Mdiamond"}}}},
					{ID: "work", Attrs: []dotparser.Attr{
						{Key: "fidelity", Value: dotparser.Value{Kind: dotparser.ValueString, Str: mode}},
					}},
					{ID: "exit", Attrs: []dotparser.Attr{{Key: "shape", Value: dotparser.Value{Kind: dotparser.ValueString, Str: "Msquare"}}}},
				},
			}

			rule := &FidelityValidRule{}
			diagnostics := rule.Apply(graph)

			if len(diagnostics) != 0 {
				t.Errorf("expected no diagnostics for mode %q, got %d: %v", mode, len(diagnostics), diagnostics)
			}
		})
	}
}

func TestFidelityValidRule_InvalidGraphFidelity(t *testing.T) {
	graph := &dotparser.Graph{
		Name: "BadGraphFidelity",
		GraphAttrs: []dotparser.Attr{
			{Key: "default_fidelity", Value: dotparser.Value{Kind: dotparser.ValueString, Str: "bad_mode"}},
		},
		Nodes: []*dotparser.Node{
			{ID: "start", Attrs: []dotparser.Attr{{Key: "shape", Value: dotparser.Value{Kind: dotparser.ValueString, Str: "Mdiamond"}}}},
			{ID: "exit", Attrs: []dotparser.Attr{{Key: "shape", Value: dotparser.Value{Kind: dotparser.ValueString, Str: "Msquare"}}}},
		},
	}

	rule := &FidelityValidRule{}
	diagnostics := rule.Apply(graph)

	if len(diagnostics) != 1 {
		t.Fatalf("expected 1 diagnostic, got %d", len(diagnostics))
	}
	if diagnostics[0].Severity != SeverityWarning {
		t.Errorf("expected severity WARNING, got %v", diagnostics[0].Severity)
	}
}

// ---------- retry_target_exists rule tests ----------

func TestRetryTargetExistsRule_MissingTarget(t *testing.T) {
	graph := &dotparser.Graph{
		Name: "BadRetryTarget",
		Nodes: []*dotparser.Node{
			{ID: "start", Attrs: []dotparser.Attr{{Key: "shape", Value: dotparser.Value{Kind: dotparser.ValueString, Str: "Mdiamond"}}}},
			{ID: "work", Attrs: []dotparser.Attr{
				{Key: "retry_target", Value: dotparser.Value{Kind: dotparser.ValueString, Str: "nonexistent"}},
			}},
			{ID: "exit", Attrs: []dotparser.Attr{{Key: "shape", Value: dotparser.Value{Kind: dotparser.ValueString, Str: "Msquare"}}}},
		},
	}

	rule := &RetryTargetExistsRule{}
	diagnostics := rule.Apply(graph)

	if len(diagnostics) != 1 {
		t.Fatalf("expected 1 diagnostic, got %d", len(diagnostics))
	}
	if diagnostics[0].Rule != "retry_target_exists" {
		t.Errorf("expected rule 'retry_target_exists', got %q", diagnostics[0].Rule)
	}
	if diagnostics[0].Severity != SeverityWarning {
		t.Errorf("expected severity WARNING, got %v", diagnostics[0].Severity)
	}
}

func TestRetryTargetExistsRule_ValidTarget(t *testing.T) {
	graph := &dotparser.Graph{
		Name: "GoodRetryTarget",
		Nodes: []*dotparser.Node{
			{ID: "start", Attrs: []dotparser.Attr{{Key: "shape", Value: dotparser.Value{Kind: dotparser.ValueString, Str: "Mdiamond"}}}},
			{ID: "work", Attrs: []dotparser.Attr{
				{Key: "retry_target", Value: dotparser.Value{Kind: dotparser.ValueString, Str: "start"}},
			}},
			{ID: "exit", Attrs: []dotparser.Attr{{Key: "shape", Value: dotparser.Value{Kind: dotparser.ValueString, Str: "Msquare"}}}},
		},
	}

	rule := &RetryTargetExistsRule{}
	diagnostics := rule.Apply(graph)

	if len(diagnostics) != 0 {
		t.Errorf("expected no diagnostics, got %d: %v", len(diagnostics), diagnostics)
	}
}

func TestRetryTargetExistsRule_GraphLevelMissing(t *testing.T) {
	graph := &dotparser.Graph{
		Name: "BadGraphRetryTarget",
		GraphAttrs: []dotparser.Attr{
			{Key: "retry_target", Value: dotparser.Value{Kind: dotparser.ValueString, Str: "nonexistent"}},
		},
		Nodes: []*dotparser.Node{
			{ID: "start", Attrs: []dotparser.Attr{{Key: "shape", Value: dotparser.Value{Kind: dotparser.ValueString, Str: "Mdiamond"}}}},
			{ID: "exit", Attrs: []dotparser.Attr{{Key: "shape", Value: dotparser.Value{Kind: dotparser.ValueString, Str: "Msquare"}}}},
		},
	}

	rule := &RetryTargetExistsRule{}
	diagnostics := rule.Apply(graph)

	if len(diagnostics) != 1 {
		t.Fatalf("expected 1 diagnostic, got %d", len(diagnostics))
	}
}

// ---------- goal_gate_has_retry rule tests ----------

func TestGoalGateHasRetryRule_NoRetry(t *testing.T) {
	graph := &dotparser.Graph{
		Name: "GoalGateNoRetry",
		Nodes: []*dotparser.Node{
			{ID: "start", Attrs: []dotparser.Attr{{Key: "shape", Value: dotparser.Value{Kind: dotparser.ValueString, Str: "Mdiamond"}}}},
			{ID: "work", Attrs: []dotparser.Attr{
				{Key: "goal_gate", Value: dotparser.Value{Kind: dotparser.ValueBool, Bool: true}},
			}},
			{ID: "exit", Attrs: []dotparser.Attr{{Key: "shape", Value: dotparser.Value{Kind: dotparser.ValueString, Str: "Msquare"}}}},
		},
	}

	rule := &GoalGateHasRetryRule{}
	diagnostics := rule.Apply(graph)

	if len(diagnostics) != 1 {
		t.Fatalf("expected 1 diagnostic, got %d", len(diagnostics))
	}
	if diagnostics[0].Rule != "goal_gate_has_retry" {
		t.Errorf("expected rule 'goal_gate_has_retry', got %q", diagnostics[0].Rule)
	}
	if diagnostics[0].Severity != SeverityWarning {
		t.Errorf("expected severity WARNING, got %v", diagnostics[0].Severity)
	}
}

func TestGoalGateHasRetryRule_HasNodeRetry(t *testing.T) {
	graph := &dotparser.Graph{
		Name: "GoalGateWithNodeRetry",
		Nodes: []*dotparser.Node{
			{ID: "start", Attrs: []dotparser.Attr{{Key: "shape", Value: dotparser.Value{Kind: dotparser.ValueString, Str: "Mdiamond"}}}},
			{ID: "work", Attrs: []dotparser.Attr{
				{Key: "goal_gate", Value: dotparser.Value{Kind: dotparser.ValueBool, Bool: true}},
				{Key: "retry_target", Value: dotparser.Value{Kind: dotparser.ValueString, Str: "start"}},
			}},
			{ID: "exit", Attrs: []dotparser.Attr{{Key: "shape", Value: dotparser.Value{Kind: dotparser.ValueString, Str: "Msquare"}}}},
		},
	}

	rule := &GoalGateHasRetryRule{}
	diagnostics := rule.Apply(graph)

	if len(diagnostics) != 0 {
		t.Errorf("expected no diagnostics, got %d: %v", len(diagnostics), diagnostics)
	}
}

func TestGoalGateHasRetryRule_HasGraphRetry(t *testing.T) {
	graph := &dotparser.Graph{
		Name: "GoalGateWithGraphRetry",
		GraphAttrs: []dotparser.Attr{
			{Key: "retry_target", Value: dotparser.Value{Kind: dotparser.ValueString, Str: "start"}},
		},
		Nodes: []*dotparser.Node{
			{ID: "start", Attrs: []dotparser.Attr{{Key: "shape", Value: dotparser.Value{Kind: dotparser.ValueString, Str: "Mdiamond"}}}},
			{ID: "work", Attrs: []dotparser.Attr{
				{Key: "goal_gate", Value: dotparser.Value{Kind: dotparser.ValueBool, Bool: true}},
			}},
			{ID: "exit", Attrs: []dotparser.Attr{{Key: "shape", Value: dotparser.Value{Kind: dotparser.ValueString, Str: "Msquare"}}}},
		},
	}

	rule := &GoalGateHasRetryRule{}
	diagnostics := rule.Apply(graph)

	if len(diagnostics) != 0 {
		t.Errorf("expected no diagnostics, got %d: %v", len(diagnostics), diagnostics)
	}
}

// ---------- prompt_on_llm_nodes rule tests ----------

func TestPromptOnLLMNodesRule_NoPromptOrLabel(t *testing.T) {
	graph := &dotparser.Graph{
		Name: "NoPrompt",
		Nodes: []*dotparser.Node{
			{ID: "start", Attrs: []dotparser.Attr{{Key: "shape", Value: dotparser.Value{Kind: dotparser.ValueString, Str: "Mdiamond"}}}},
			{ID: "work"}, // No shape, no prompt, no label - defaults to codergen
			{ID: "exit", Attrs: []dotparser.Attr{{Key: "shape", Value: dotparser.Value{Kind: dotparser.ValueString, Str: "Msquare"}}}},
		},
	}

	rule := &PromptOnLLMNodesRule{}
	diagnostics := rule.Apply(graph)

	if len(diagnostics) != 1 {
		t.Fatalf("expected 1 diagnostic, got %d", len(diagnostics))
	}
	if diagnostics[0].Rule != "prompt_on_llm_nodes" {
		t.Errorf("expected rule 'prompt_on_llm_nodes', got %q", diagnostics[0].Rule)
	}
	if diagnostics[0].Severity != SeverityWarning {
		t.Errorf("expected severity WARNING, got %v", diagnostics[0].Severity)
	}
}

func TestPromptOnLLMNodesRule_HasPrompt(t *testing.T) {
	graph := &dotparser.Graph{
		Name: "HasPrompt",
		Nodes: []*dotparser.Node{
			{ID: "start", Attrs: []dotparser.Attr{{Key: "shape", Value: dotparser.Value{Kind: dotparser.ValueString, Str: "Mdiamond"}}}},
			{ID: "work", Attrs: []dotparser.Attr{
				{Key: "prompt", Value: dotparser.Value{Kind: dotparser.ValueString, Str: "Do the work"}},
			}},
			{ID: "exit", Attrs: []dotparser.Attr{{Key: "shape", Value: dotparser.Value{Kind: dotparser.ValueString, Str: "Msquare"}}}},
		},
	}

	rule := &PromptOnLLMNodesRule{}
	diagnostics := rule.Apply(graph)

	if len(diagnostics) != 0 {
		t.Errorf("expected no diagnostics, got %d: %v", len(diagnostics), diagnostics)
	}
}

func TestPromptOnLLMNodesRule_HasLabel(t *testing.T) {
	graph := &dotparser.Graph{
		Name: "HasLabel",
		Nodes: []*dotparser.Node{
			{ID: "start", Attrs: []dotparser.Attr{{Key: "shape", Value: dotparser.Value{Kind: dotparser.ValueString, Str: "Mdiamond"}}}},
			{ID: "work", Attrs: []dotparser.Attr{
				{Key: "label", Value: dotparser.Value{Kind: dotparser.ValueString, Str: "Work step"}},
			}},
			{ID: "exit", Attrs: []dotparser.Attr{{Key: "shape", Value: dotparser.Value{Kind: dotparser.ValueString, Str: "Msquare"}}}},
		},
	}

	rule := &PromptOnLLMNodesRule{}
	diagnostics := rule.Apply(graph)

	if len(diagnostics) != 0 {
		t.Errorf("expected no diagnostics, got %d: %v", len(diagnostics), diagnostics)
	}
}

func TestPromptOnLLMNodesRule_NonLLMNode(t *testing.T) {
	graph := &dotparser.Graph{
		Name: "NonLLMNode",
		Nodes: []*dotparser.Node{
			{ID: "start", Attrs: []dotparser.Attr{{Key: "shape", Value: dotparser.Value{Kind: dotparser.ValueString, Str: "Mdiamond"}}}},
			{ID: "gate", Attrs: []dotparser.Attr{
				{Key: "shape", Value: dotparser.Value{Kind: dotparser.ValueString, Str: "hexagon"}},
			}}, // wait.human - not an LLM node, no prompt needed
			{ID: "exit", Attrs: []dotparser.Attr{{Key: "shape", Value: dotparser.Value{Kind: dotparser.ValueString, Str: "Msquare"}}}},
		},
	}

	rule := &PromptOnLLMNodesRule{}
	diagnostics := rule.Apply(graph)

	if len(diagnostics) != 0 {
		t.Errorf("expected no diagnostics for non-LLM node, got %d: %v", len(diagnostics), diagnostics)
	}
}

func TestPromptOnLLMNodesRule_BoxShapeNoPrompt(t *testing.T) {
	graph := &dotparser.Graph{
		Name: "BoxNoPrompt",
		Nodes: []*dotparser.Node{
			{ID: "start", Attrs: []dotparser.Attr{{Key: "shape", Value: dotparser.Value{Kind: dotparser.ValueString, Str: "Mdiamond"}}}},
			{ID: "work", Attrs: []dotparser.Attr{
				{Key: "shape", Value: dotparser.Value{Kind: dotparser.ValueString, Str: "box"}},
			}}, // box -> codergen
			{ID: "exit", Attrs: []dotparser.Attr{{Key: "shape", Value: dotparser.Value{Kind: dotparser.ValueString, Str: "Msquare"}}}},
		},
	}

	rule := &PromptOnLLMNodesRule{}
	diagnostics := rule.Apply(graph)

	if len(diagnostics) != 1 {
		t.Fatalf("expected 1 diagnostic, got %d", len(diagnostics))
	}
}

// ---------- Validate and ValidateOrError tests ----------

func TestValidate_ValidPipeline(t *testing.T) {
	graph := validGraph()
	diagnostics := Validate(graph)

	// Count errors
	var errors int
	for _, d := range diagnostics {
		if d.Severity == SeverityError {
			errors++
		}
	}

	if errors != 0 {
		t.Errorf("expected no errors for valid pipeline, got %d errors: %v", errors, diagnostics)
	}
}

func TestValidate_InvalidPipeline(t *testing.T) {
	graph := &dotparser.Graph{
		Name: "Invalid",
		Nodes: []*dotparser.Node{
			{ID: "work"},
		},
	}

	diagnostics := Validate(graph)

	// Should have errors for missing start and exit
	var errors int
	for _, d := range diagnostics {
		if d.Severity == SeverityError {
			errors++
		}
	}

	if errors < 2 {
		t.Errorf("expected at least 2 errors (start_node, terminal_node), got %d", errors)
	}
}

func TestValidateOrError_ValidPipeline(t *testing.T) {
	graph := validGraph()
	diagnostics, err := ValidateOrError(graph)

	if err != nil {
		t.Errorf("expected no error for valid pipeline, got: %v", err)
	}
	// diagnostics may be nil or contain warnings, just verify no errors
	for _, d := range diagnostics {
		if d.Severity == SeverityError {
			t.Errorf("unexpected error diagnostic: %v", d)
		}
	}
}

func TestValidateOrError_InvalidPipeline(t *testing.T) {
	graph := &dotparser.Graph{
		Name: "Invalid",
		Nodes: []*dotparser.Node{
			{ID: "work"},
		},
	}

	diagnostics, err := ValidateOrError(graph)

	if err == nil {
		t.Error("expected error for invalid pipeline")
	}
	if diagnostics == nil {
		t.Error("expected diagnostics to be non-nil even on error")
	}
}

// ---------- Custom lint rule test ----------

type customRule struct{}

func (r *customRule) Name() string { return "custom_rule" }

func (r *customRule) Apply(graph *dotparser.Graph) []Diagnostic {
	// Custom rule: warn if graph name is "Test"
	if graph.Name == "Test" {
		return []Diagnostic{{
			Rule:     r.Name(),
			Severity: SeverityWarning,
			Message:  "graph name 'Test' is not recommended for production",
		}}
	}
	return nil
}

func TestValidate_CustomRule(t *testing.T) {
	graph := &dotparser.Graph{
		Name: "Test",
		Nodes: []*dotparser.Node{
			{ID: "start", Attrs: []dotparser.Attr{{Key: "shape", Value: dotparser.Value{Kind: dotparser.ValueString, Str: "Mdiamond"}}}},
			{ID: "exit", Attrs: []dotparser.Attr{{Key: "shape", Value: dotparser.Value{Kind: dotparser.ValueString, Str: "Msquare"}}}},
		},
		Edges: []*dotparser.Edge{
			{From: "start", To: "exit"},
		},
	}

	diagnostics := Validate(graph, &customRule{})

	// Find our custom rule's diagnostic
	var found bool
	for _, d := range diagnostics {
		if d.Rule == "custom_rule" {
			found = true
			if d.Severity != SeverityWarning {
				t.Errorf("expected custom_rule severity WARNING, got %v", d.Severity)
			}
			break
		}
	}

	if !found {
		t.Error("expected to find custom_rule diagnostic")
	}
}

// ---------- isCodergenNode tests ----------

func TestIsCodergenNode(t *testing.T) {
	tests := []struct {
		name     string
		node     *dotparser.Node
		expected bool
	}{
		{
			name:     "no shape (default)",
			node:     &dotparser.Node{ID: "test"},
			expected: true,
		},
		{
			name: "box shape",
			node: &dotparser.Node{
				ID:    "test",
				Attrs: []dotparser.Attr{{Key: "shape", Value: dotparser.Value{Kind: dotparser.ValueString, Str: "box"}}},
			},
			expected: true,
		},
		{
			name: "explicit codergen type",
			node: &dotparser.Node{
				ID:    "test",
				Attrs: []dotparser.Attr{{Key: "type", Value: dotparser.Value{Kind: dotparser.ValueString, Str: "codergen"}}},
			},
			expected: true,
		},
		{
			name: "Mdiamond shape (start)",
			node: &dotparser.Node{
				ID:    "test",
				Attrs: []dotparser.Attr{{Key: "shape", Value: dotparser.Value{Kind: dotparser.ValueString, Str: "Mdiamond"}}},
			},
			expected: false,
		},
		{
			name: "Msquare shape (exit)",
			node: &dotparser.Node{
				ID:    "test",
				Attrs: []dotparser.Attr{{Key: "shape", Value: dotparser.Value{Kind: dotparser.ValueString, Str: "Msquare"}}},
			},
			expected: false,
		},
		{
			name: "hexagon shape (wait.human)",
			node: &dotparser.Node{
				ID:    "test",
				Attrs: []dotparser.Attr{{Key: "shape", Value: dotparser.Value{Kind: dotparser.ValueString, Str: "hexagon"}}},
			},
			expected: false,
		},
		{
			name: "diamond shape (conditional)",
			node: &dotparser.Node{
				ID:    "test",
				Attrs: []dotparser.Attr{{Key: "shape", Value: dotparser.Value{Kind: dotparser.ValueString, Str: "diamond"}}},
			},
			expected: false,
		},
		{
			name: "explicit non-codergen type",
			node: &dotparser.Node{
				ID:    "test",
				Attrs: []dotparser.Attr{{Key: "type", Value: dotparser.Value{Kind: dotparser.ValueString, Str: "wait.human"}}},
			},
			expected: false,
		},
		{
			name: "unknown shape (falls back to codergen)",
			node: &dotparser.Node{
				ID:    "test",
				Attrs: []dotparser.Attr{{Key: "shape", Value: dotparser.Value{Kind: dotparser.ValueString, Str: "ellipse"}}},
			},
			expected: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := isCodergenNode(tt.node)
			if result != tt.expected {
				t.Errorf("isCodergenNode() = %v, expected %v", result, tt.expected)
			}
		})
	}
}

// ---------- stylesheet_syntax rule tests ----------

func TestStylesheetSyntaxRule_ValidStylesheet(t *testing.T) {
	graph := &dotparser.Graph{
		Name: "ValidStylesheet",
		GraphAttrs: []dotparser.Attr{
			{Key: "model_stylesheet", Value: dotparser.Value{Kind: dotparser.ValueString, Str: `* { llm_model: "claude-sonnet-4-20250514" }`}},
		},
		Nodes: []*dotparser.Node{
			{ID: "start", Attrs: []dotparser.Attr{{Key: "shape", Value: dotparser.Value{Kind: dotparser.ValueString, Str: "Mdiamond"}}}},
			{ID: "exit", Attrs: []dotparser.Attr{{Key: "shape", Value: dotparser.Value{Kind: dotparser.ValueString, Str: "Msquare"}}}},
		},
		Edges: []*dotparser.Edge{
			{From: "start", To: "exit"},
		},
	}

	rule := &StylesheetSyntaxRule{}
	diagnostics := rule.Apply(graph)

	if len(diagnostics) != 0 {
		t.Errorf("expected no diagnostics for valid stylesheet, got %d: %v", len(diagnostics), diagnostics)
	}
}

func TestStylesheetSyntaxRule_InvalidStylesheet(t *testing.T) {
	graph := &dotparser.Graph{
		Name: "InvalidStylesheet",
		GraphAttrs: []dotparser.Attr{
			{Key: "model_stylesheet", Value: dotparser.Value{Kind: dotparser.ValueString, Str: `this is not valid { {`}},
		},
		Nodes: []*dotparser.Node{
			{ID: "start", Attrs: []dotparser.Attr{{Key: "shape", Value: dotparser.Value{Kind: dotparser.ValueString, Str: "Mdiamond"}}}},
			{ID: "exit", Attrs: []dotparser.Attr{{Key: "shape", Value: dotparser.Value{Kind: dotparser.ValueString, Str: "Msquare"}}}},
		},
		Edges: []*dotparser.Edge{
			{From: "start", To: "exit"},
		},
	}

	rule := &StylesheetSyntaxRule{}
	diagnostics := rule.Apply(graph)

	if len(diagnostics) != 1 {
		t.Fatalf("expected 1 diagnostic, got %d", len(diagnostics))
	}
	if diagnostics[0].Rule != "stylesheet_syntax" {
		t.Errorf("expected rule 'stylesheet_syntax', got %q", diagnostics[0].Rule)
	}
	if diagnostics[0].Severity != SeverityError {
		t.Errorf("expected severity ERROR, got %v", diagnostics[0].Severity)
	}
}

func TestStylesheetSyntaxRule_NoStylesheet(t *testing.T) {
	graph := validGraph()
	rule := &StylesheetSyntaxRule{}
	diagnostics := rule.Apply(graph)

	if len(diagnostics) != 0 {
		t.Errorf("expected no diagnostics when no stylesheet, got %d: %v", len(diagnostics), diagnostics)
	}
}

// ---------- Severity String tests ----------

func TestSeverityString(t *testing.T) {
	tests := []struct {
		severity Severity
		expected string
	}{
		{SeverityError, "ERROR"},
		{SeverityWarning, "WARNING"},
		{SeverityInfo, "INFO"},
		{Severity(99), "UNKNOWN"},
	}

	for _, tt := range tests {
		t.Run(tt.expected, func(t *testing.T) {
			if got := tt.severity.String(); got != tt.expected {
				t.Errorf("Severity.String() = %q, expected %q", got, tt.expected)
			}
		})
	}
}

// ---------- validateConditionSyntax tests ----------

func TestValidateConditionSyntax(t *testing.T) {
	tests := []struct {
		name      string
		condition string
		wantErr   bool
	}{
		{"empty", "", false},
		{"whitespace only", "   ", false},
		{"simple equals", "outcome=success", false},
		{"simple not equals", "outcome!=fail", false},
		{"with quotes", `outcome="success"`, false},
		{"bare key", "ready", false},
		{"compound", "outcome=success && ready", false},
		{"missing key for equals", "=value", true},
		{"missing key for not equals", "!=value", true},
		{"complex valid", "outcome=success && context.ready && status!=error", false},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := validateConditionSyntax(tt.condition)
			if (err != nil) != tt.wantErr {
				t.Errorf("validateConditionSyntax(%q) error = %v, wantErr %v", tt.condition, err, tt.wantErr)
			}
		})
	}
}

// ---------- BuiltInRules completeness test ----------

func TestBuiltInRules_AllPresent(t *testing.T) {
	expectedRules := map[string]bool{
		"start_node":          false,
		"terminal_node":       false,
		"reachability":        false,
		"edge_target_exists":  false,
		"start_no_incoming":   false,
		"exit_no_outgoing":    false,
		"condition_syntax":    false,
		"stylesheet_syntax":   false,
		"type_known":          false,
		"fidelity_valid":      false,
		"retry_target_exists": false,
		"goal_gate_has_retry": false,
		"prompt_on_llm_nodes": false,
	}

	for _, rule := range BuiltInRules {
		name := rule.Name()
		if _, exists := expectedRules[name]; !exists {
			t.Errorf("unexpected rule in BuiltInRules: %q", name)
		} else {
			expectedRules[name] = true
		}
	}

	for name, found := range expectedRules {
		if !found {
			t.Errorf("missing rule in BuiltInRules: %q", name)
		}
	}
}
