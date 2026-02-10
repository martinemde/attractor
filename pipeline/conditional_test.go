package pipeline

import (
	"testing"

	"github.com/martinemde/attractor/dotparser"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestConditionalHandler_ReturnsSuccessWithNotes(t *testing.T) {
	handler := &ConditionalHandler{}
	node := newNode("check_result", strAttr("shape", "diamond"))
	ctx := NewContext()

	outcome, err := handler.Execute(node, ctx, nil, "")

	require.NoError(t, err)
	assert.Equal(t, StatusSuccess, outcome.Status)
	assert.Contains(t, outcome.Notes, "Conditional node evaluated: check_result")
}

func TestConditionalHandler_IntegrationWithPipeline(t *testing.T) {
	// Build a pipeline with: start -> conditional -> [pass/fail branches] -> exit
	graph := newTestGraph(
		[]*dotparser.Node{
			newNode("start", strAttr("shape", "Mdiamond")),
			newNode("decide", strAttr("shape", "diamond")),
			newNode("pass_path", strAttr("type", "mock")),
			newNode("exit", strAttr("shape", "Msquare")),
		},
		[]*dotparser.Edge{
			newEdge("start", "decide"),
			newEdge("decide", "pass_path", strAttr("label", "pass")),
			newEdge("decide", "exit", strAttr("label", "fail")),
			newEdge("pass_path", "exit"),
		},
		nil,
	)

	mockHandler := &MockHandler{
		Outcomes: []*Outcome{Success().WithNotes("mock executed")},
	}

	registry := DefaultRegistry()
	registry.Register("mock", mockHandler)

	result, err := Run(graph, &RunConfig{Registry: registry})

	require.NoError(t, err)
	// The conditional node should have been executed
	assert.Contains(t, result.CompletedNodes, "decide")
	// Since no condition expression matches, it should take the first edge (pass_path)
	// or the unlabeled one; either way the pipeline completes
	assert.Equal(t, StatusSuccess, result.FinalOutcome.Status)
}

func TestConditionalHandler_ShapeBasedResolution(t *testing.T) {
	// Verify that diamond shape resolves to conditional handler
	registry := DefaultRegistry()

	node := newNode("branch", strAttr("shape", "diamond"))
	handler := registry.Resolve(node)

	require.NotNil(t, handler)

	// Verify it's a ConditionalHandler by executing and checking the notes
	outcome, err := handler.Execute(node, NewContext(), nil, "")
	require.NoError(t, err)
	assert.Equal(t, StatusSuccess, outcome.Status)
	assert.Contains(t, outcome.Notes, "Conditional node evaluated: branch")
}
