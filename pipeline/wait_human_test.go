package pipeline

import (
	"strings"
	"testing"

	"github.com/martinemde/attractor/dotparser"
)

func TestWaitForHumanHandler_PresentsChoicesFromEdges(t *testing.T) {
	// Create a graph with a human gate node and outgoing edges
	graph := &dotparser.Graph{
		Name: "test_human_gate",
		Nodes: []*dotparser.Node{
			{
				ID: "gate",
				Attrs: []dotparser.Attr{
					{Key: "shape", Value: dotparser.Value{Kind: dotparser.ValueString, Str: "hexagon"}},
					{Key: "label", Value: dotparser.Value{Kind: dotparser.ValueString, Str: "Approve deployment?"}},
				},
			},
			{ID: "approve"},
			{ID: "reject"},
		},
		Edges: []*dotparser.Edge{
			{
				From: "gate",
				To:   "approve",
				Attrs: []dotparser.Attr{
					{Key: "label", Value: dotparser.Value{Kind: dotparser.ValueString, Str: "[Y] Yes, approve"}},
				},
			},
			{
				From: "gate",
				To:   "reject",
				Attrs: []dotparser.Attr{
					{Key: "label", Value: dotparser.Value{Kind: dotparser.ValueString, Str: "[N] No, reject"}},
				},
			},
		},
	}

	// Track the question that was asked
	var askedQuestion *Question
	interviewer := NewCallbackInterviewer(func(q *Question) (*Answer, error) {
		askedQuestion = q
		return &Answer{Value: "Y"}, nil
	})

	handler := NewWaitForHumanHandler(interviewer)
	ctx := NewContext()
	node := graph.NodeByID("gate")

	outcome, err := handler.Execute(node, ctx, graph, "")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	// Verify question was asked with correct options
	if askedQuestion == nil {
		t.Fatal("expected question to be asked")
	}
	if askedQuestion.Text != "Approve deployment?" {
		t.Errorf("expected question text 'Approve deployment?', got %q", askedQuestion.Text)
	}
	if askedQuestion.Type != QuestionMultipleChoice {
		t.Errorf("expected MULTIPLE_CHOICE type, got %v", askedQuestion.Type)
	}
	if len(askedQuestion.Options) != 2 {
		t.Fatalf("expected 2 options, got %d", len(askedQuestion.Options))
	}
	if askedQuestion.Options[0].Key != "Y" {
		t.Errorf("expected first option key 'Y', got %q", askedQuestion.Options[0].Key)
	}
	if askedQuestion.Stage != "gate" {
		t.Errorf("expected stage 'gate', got %q", askedQuestion.Stage)
	}

	// Verify outcome
	if outcome.Status != StatusSuccess {
		t.Errorf("expected SUCCESS, got %v", outcome.Status)
	}
	if len(outcome.SuggestedNextIDs) != 1 || outcome.SuggestedNextIDs[0] != "approve" {
		t.Errorf("expected suggested_next_ids=[approve], got %v", outcome.SuggestedNextIDs)
	}
}

func TestWaitForHumanHandler_RoutesOnSelection(t *testing.T) {
	graph := &dotparser.Graph{
		Name: "test_routing",
		Nodes: []*dotparser.Node{
			{ID: "gate", Attrs: []dotparser.Attr{
				{Key: "shape", Value: dotparser.Value{Kind: dotparser.ValueString, Str: "hexagon"}},
			}},
			{ID: "option_a"},
			{ID: "option_b"},
			{ID: "option_c"},
		},
		Edges: []*dotparser.Edge{
			{From: "gate", To: "option_a", Attrs: []dotparser.Attr{
				{Key: "label", Value: dotparser.Value{Kind: dotparser.ValueString, Str: "A) First choice"}},
			}},
			{From: "gate", To: "option_b", Attrs: []dotparser.Attr{
				{Key: "label", Value: dotparser.Value{Kind: dotparser.ValueString, Str: "B) Second choice"}},
			}},
			{From: "gate", To: "option_c", Attrs: []dotparser.Attr{
				{Key: "label", Value: dotparser.Value{Kind: dotparser.ValueString, Str: "C) Third choice"}},
			}},
		},
	}

	tests := []struct {
		name           string
		answerValue    string
		expectedTarget string
	}{
		{"select first by key", "A", "option_a"},
		{"select second by key", "B", "option_b"},
		{"select third by key", "C", "option_c"},
		{"select by lowercase key", "b", "option_b"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			interviewer := NewCallbackInterviewer(func(q *Question) (*Answer, error) {
				return &Answer{Value: tt.answerValue}, nil
			})

			handler := NewWaitForHumanHandler(interviewer)
			ctx := NewContext()
			node := graph.NodeByID("gate")

			outcome, err := handler.Execute(node, ctx, graph, "")
			if err != nil {
				t.Fatalf("unexpected error: %v", err)
			}

			if outcome.Status != StatusSuccess {
				t.Errorf("expected SUCCESS, got %v", outcome.Status)
			}
			if len(outcome.SuggestedNextIDs) != 1 {
				t.Fatalf("expected 1 suggested next ID, got %d", len(outcome.SuggestedNextIDs))
			}
			if outcome.SuggestedNextIDs[0] != tt.expectedTarget {
				t.Errorf("expected target %q, got %q", tt.expectedTarget, outcome.SuggestedNextIDs[0])
			}
		})
	}
}

func TestWaitForHumanHandler_SetsContextUpdates(t *testing.T) {
	graph := &dotparser.Graph{
		Name: "test_context",
		Nodes: []*dotparser.Node{
			{ID: "gate"},
			{ID: "next"},
		},
		Edges: []*dotparser.Edge{
			{From: "gate", To: "next", Attrs: []dotparser.Attr{
				{Key: "label", Value: dotparser.Value{Kind: dotparser.ValueString, Str: "[Y] Approve"}},
			}},
		},
	}

	interviewer := NewCallbackInterviewer(func(q *Question) (*Answer, error) {
		return &Answer{Value: "Y"}, nil
	})

	handler := NewWaitForHumanHandler(interviewer)
	ctx := NewContext()
	node := graph.NodeByID("gate")

	outcome, _ := handler.Execute(node, ctx, graph, "")

	if outcome.ContextUpdates["human.gate.selected"] != "Y" {
		t.Errorf("expected human.gate.selected='Y', got %v", outcome.ContextUpdates["human.gate.selected"])
	}
	if outcome.ContextUpdates["human.gate.label"] != "[Y] Approve" {
		t.Errorf("expected human.gate.label='[Y] Approve', got %v", outcome.ContextUpdates["human.gate.label"])
	}
}

func TestWaitForHumanHandler_TimeoutWithDefaultChoice(t *testing.T) {
	graph := &dotparser.Graph{
		Name: "test_timeout",
		Nodes: []*dotparser.Node{
			{ID: "gate", Attrs: []dotparser.Attr{
				{Key: "human.default_choice", Value: dotparser.Value{Kind: dotparser.ValueString, Str: "safe_exit"}},
			}},
			{ID: "proceed"},
			{ID: "safe_exit"},
		},
		Edges: []*dotparser.Edge{
			{From: "gate", To: "proceed", Attrs: []dotparser.Attr{
				{Key: "label", Value: dotparser.Value{Kind: dotparser.ValueString, Str: "Proceed"}},
			}},
			{From: "gate", To: "safe_exit", Attrs: []dotparser.Attr{
				{Key: "label", Value: dotparser.Value{Kind: dotparser.ValueString, Str: "Safe exit"}},
			}},
		},
	}

	// Simulate timeout
	interviewer := NewCallbackInterviewer(func(q *Question) (*Answer, error) {
		return &Answer{Timeout: true}, nil
	})

	handler := NewWaitForHumanHandler(interviewer)
	ctx := NewContext()
	node := graph.NodeByID("gate")

	outcome, err := handler.Execute(node, ctx, graph, "")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if outcome.Status != StatusSuccess {
		t.Errorf("expected SUCCESS (using default), got %v", outcome.Status)
	}
	if len(outcome.SuggestedNextIDs) != 1 || outcome.SuggestedNextIDs[0] != "safe_exit" {
		t.Errorf("expected default choice 'safe_exit', got %v", outcome.SuggestedNextIDs)
	}
}

func TestWaitForHumanHandler_TimeoutWithoutDefault(t *testing.T) {
	graph := &dotparser.Graph{
		Name: "test_timeout_no_default",
		Nodes: []*dotparser.Node{
			{ID: "gate"},
			{ID: "next"},
		},
		Edges: []*dotparser.Edge{
			{From: "gate", To: "next"},
		},
	}

	interviewer := NewCallbackInterviewer(func(q *Question) (*Answer, error) {
		return &Answer{Timeout: true}, nil
	})

	handler := NewWaitForHumanHandler(interviewer)
	ctx := NewContext()
	node := graph.NodeByID("gate")

	outcome, err := handler.Execute(node, ctx, graph, "")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if outcome.Status != StatusRetry {
		t.Errorf("expected RETRY on timeout without default, got %v", outcome.Status)
	}
	if !strings.Contains(outcome.FailureReason, "timeout") {
		t.Errorf("expected failure reason to mention timeout, got %q", outcome.FailureReason)
	}
}

func TestWaitForHumanHandler_FailsOnSkipped(t *testing.T) {
	graph := &dotparser.Graph{
		Name: "test_skipped",
		Nodes: []*dotparser.Node{
			{ID: "gate"},
			{ID: "next"},
		},
		Edges: []*dotparser.Edge{
			{From: "gate", To: "next"},
		},
	}

	interviewer := NewCallbackInterviewer(func(q *Question) (*Answer, error) {
		return &Answer{Skipped: true}, nil
	})

	handler := NewWaitForHumanHandler(interviewer)
	ctx := NewContext()
	node := graph.NodeByID("gate")

	outcome, err := handler.Execute(node, ctx, graph, "")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if outcome.Status != StatusFail {
		t.Errorf("expected FAIL on skipped, got %v", outcome.Status)
	}
	if !strings.Contains(outcome.FailureReason, "skipped") {
		t.Errorf("expected failure reason to mention skipped, got %q", outcome.FailureReason)
	}
}

func TestWaitForHumanHandler_FailsOnNoOutgoingEdges(t *testing.T) {
	graph := &dotparser.Graph{
		Name: "test_no_edges",
		Nodes: []*dotparser.Node{
			{ID: "gate"},
		},
		Edges: []*dotparser.Edge{},
	}

	interviewer := &AutoApproveInterviewer{}
	handler := NewWaitForHumanHandler(interviewer)
	ctx := NewContext()
	node := graph.NodeByID("gate")

	outcome, err := handler.Execute(node, ctx, graph, "")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if outcome.Status != StatusFail {
		t.Errorf("expected FAIL when no outgoing edges, got %v", outcome.Status)
	}
	if !strings.Contains(outcome.FailureReason, "No outgoing edges") {
		t.Errorf("expected failure reason about no edges, got %q", outcome.FailureReason)
	}
}

func TestWaitForHumanHandler_FailsWithoutInterviewer(t *testing.T) {
	graph := &dotparser.Graph{
		Name: "test_no_interviewer",
		Nodes: []*dotparser.Node{
			{ID: "gate"},
			{ID: "next"},
		},
		Edges: []*dotparser.Edge{
			{From: "gate", To: "next"},
		},
	}

	handler := NewWaitForHumanHandler(nil) // No interviewer
	ctx := NewContext()
	node := graph.NodeByID("gate")

	outcome, err := handler.Execute(node, ctx, graph, "")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if outcome.Status != StatusFail {
		t.Errorf("expected FAIL without interviewer, got %v", outcome.Status)
	}
}

func TestWaitForHumanHandler_UsesNodeIDAsLabelFallback(t *testing.T) {
	graph := &dotparser.Graph{
		Name: "test_fallback_label",
		Nodes: []*dotparser.Node{
			{ID: "gate"},
			{ID: "approve_node"},
			{ID: "reject_node"},
		},
		Edges: []*dotparser.Edge{
			{From: "gate", To: "approve_node"}, // No label
			{From: "gate", To: "reject_node"},  // No label
		},
	}

	var askedQuestion *Question
	interviewer := NewCallbackInterviewer(func(q *Question) (*Answer, error) {
		askedQuestion = q
		return &Answer{Value: "A"}, nil
	})

	handler := NewWaitForHumanHandler(interviewer)
	ctx := NewContext()
	node := graph.NodeByID("gate")

	_, err := handler.Execute(node, ctx, graph, "")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	// When no edge label, node ID should be used
	if askedQuestion.Options[0].Label != "approve_node" {
		t.Errorf("expected label 'approve_node', got %q", askedQuestion.Options[0].Label)
	}
	if askedQuestion.Options[1].Label != "reject_node" {
		t.Errorf("expected label 'reject_node', got %q", askedQuestion.Options[1].Label)
	}
}

func TestWaitForHumanHandler_FallbackToFirstChoice(t *testing.T) {
	graph := &dotparser.Graph{
		Name: "test_fallback_first",
		Nodes: []*dotparser.Node{
			{ID: "gate"},
			{ID: "first"},
			{ID: "second"},
		},
		Edges: []*dotparser.Edge{
			{From: "gate", To: "first", Attrs: []dotparser.Attr{
				{Key: "label", Value: dotparser.Value{Kind: dotparser.ValueString, Str: "First"}},
			}},
			{From: "gate", To: "second", Attrs: []dotparser.Attr{
				{Key: "label", Value: dotparser.Value{Kind: dotparser.ValueString, Str: "Second"}},
			}},
		},
	}

	// Return an answer that doesn't match any option
	interviewer := NewCallbackInterviewer(func(q *Question) (*Answer, error) {
		return &Answer{Value: "UNKNOWN"}, nil
	})

	handler := NewWaitForHumanHandler(interviewer)
	ctx := NewContext()
	node := graph.NodeByID("gate")

	outcome, err := handler.Execute(node, ctx, graph, "")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	// Should fall back to first choice
	if outcome.Status != StatusSuccess {
		t.Errorf("expected SUCCESS, got %v", outcome.Status)
	}
	if len(outcome.SuggestedNextIDs) != 1 || outcome.SuggestedNextIDs[0] != "first" {
		t.Errorf("expected fallback to 'first', got %v", outcome.SuggestedNextIDs)
	}
}

func TestWaitForHumanHandler_SelectedOptionMatching(t *testing.T) {
	graph := &dotparser.Graph{
		Name: "test_selected_option",
		Nodes: []*dotparser.Node{
			{ID: "gate"},
			{ID: "target_a"},
			{ID: "target_b"},
		},
		Edges: []*dotparser.Edge{
			{From: "gate", To: "target_a", Attrs: []dotparser.Attr{
				{Key: "label", Value: dotparser.Value{Kind: dotparser.ValueString, Str: "[A] Option A"}},
			}},
			{From: "gate", To: "target_b", Attrs: []dotparser.Attr{
				{Key: "label", Value: dotparser.Value{Kind: dotparser.ValueString, Str: "[B] Option B"}},
			}},
		},
	}

	// Return answer with SelectedOption set
	interviewer := NewCallbackInterviewer(func(q *Question) (*Answer, error) {
		return &Answer{
			Value:          "B",
			SelectedOption: &Option{Key: "B", Label: "[B] Option B"},
		}, nil
	})

	handler := NewWaitForHumanHandler(interviewer)
	ctx := NewContext()
	node := graph.NodeByID("gate")

	outcome, err := handler.Execute(node, ctx, graph, "")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if outcome.SuggestedNextIDs[0] != "target_b" {
		t.Errorf("expected target_b, got %v", outcome.SuggestedNextIDs)
	}
}

func TestWaitForHumanHandler_IntegrationWithPipeline(t *testing.T) {
	// Test a complete pipeline with a human gate
	dotSrc := `
digraph test_pipeline {
    start [shape=Mdiamond]
    gate [shape=hexagon, label="Proceed with deployment?"]
    deploy [label="Deploy"]
    cancel [label="Cancel"]
    done [shape=Msquare]

    start -> gate
    gate -> deploy [label="[Y] Yes, deploy"]
    gate -> cancel [label="[N] No, cancel"]
    deploy -> done
    cancel -> done
}
`
	graph, err := dotparser.Parse([]byte(dotSrc))
	if err != nil {
		t.Fatalf("failed to parse DOT: %v", err)
	}

	// Auto-approve will select "Y" (first option)
	interviewer := &AutoApproveInterviewer{}

	result, err := Run(graph, &RunConfig{
		Interviewer: interviewer,
	})
	if err != nil {
		t.Fatalf("pipeline failed: %v", err)
	}

	// Verify the pipeline took the "deploy" path
	foundDeploy := false
	for _, nodeID := range result.CompletedNodes {
		if nodeID == "deploy" {
			foundDeploy = true
		}
		if nodeID == "cancel" {
			t.Error("pipeline should not have visited 'cancel' node")
		}
	}

	if !foundDeploy {
		t.Error("pipeline should have visited 'deploy' node")
	}

	// Check context was updated
	if result.Context.GetString("human.gate.selected", "") != "Y" {
		t.Errorf("expected human.gate.selected='Y', got %q", result.Context.GetString("human.gate.selected", ""))
	}
}

func TestWaitForHumanHandler_IntegrationWithQueueInterviewer(t *testing.T) {
	dotSrc := `
digraph test_queue {
    start [shape=Mdiamond]
    gate [shape=hexagon, label="Choose path"]
    path_a [label="Path A"]
    path_b [label="Path B"]
    done [shape=Msquare]

    start -> gate
    gate -> path_a [label="[A] Path A"]
    gate -> path_b [label="[B] Path B"]
    path_a -> done
    path_b -> done
}
`
	graph, err := dotparser.Parse([]byte(dotSrc))
	if err != nil {
		t.Fatalf("failed to parse DOT: %v", err)
	}

	// Pre-fill the queue to select path B
	interviewer := NewQueueInterviewer(&Answer{Value: "B"})

	result, err := Run(graph, &RunConfig{
		Interviewer: interviewer,
	})
	if err != nil {
		t.Fatalf("pipeline failed: %v", err)
	}

	// Verify the pipeline took path B
	foundPathB := false
	for _, nodeID := range result.CompletedNodes {
		if nodeID == "path_b" {
			foundPathB = true
		}
		if nodeID == "path_a" {
			t.Error("pipeline should not have visited 'path_a' node")
		}
	}

	if !foundPathB {
		t.Error("pipeline should have visited 'path_b' node")
	}
}

func TestWaitForHumanHandler_DefaultQuestionText(t *testing.T) {
	graph := &dotparser.Graph{
		Name: "test_default_text",
		Nodes: []*dotparser.Node{
			{ID: "gate"}, // No label attribute
			{ID: "next"},
		},
		Edges: []*dotparser.Edge{
			{From: "gate", To: "next"},
		},
	}

	var askedQuestion *Question
	interviewer := NewCallbackInterviewer(func(q *Question) (*Answer, error) {
		askedQuestion = q
		return &Answer{Value: "next"}, nil
	})

	handler := NewWaitForHumanHandler(interviewer)
	ctx := NewContext()
	node := graph.NodeByID("gate")

	_, err := handler.Execute(node, ctx, graph, "")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if askedQuestion.Text != "Select an option:" {
		t.Errorf("expected default text 'Select an option:', got %q", askedQuestion.Text)
	}
}
