package pipeline

import (
	"encoding/json"
	"os"
	"path/filepath"
	"testing"

	"github.com/martinemde/attractor/dotparser"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// MockCodergenBackend is a test backend for CodergenHandler.
type MockCodergenBackend struct {
	Calls    []MockBackendCall
	Response any // string or *Outcome
	Error    error
}

type MockBackendCall struct {
	NodeID string
	Prompt string
}

func (m *MockCodergenBackend) Run(node *dotparser.Node, prompt string, ctx *Context) (any, error) {
	m.Calls = append(m.Calls, MockBackendCall{NodeID: node.ID, Prompt: prompt})
	if m.Error != nil {
		return nil, m.Error
	}
	return m.Response, nil
}

func TestCodergenHandler_SimulationMode(t *testing.T) {
	tmpDir := t.TempDir()

	handler := NewCodergenHandler(nil) // simulation mode
	node := newNode("test_stage", strAttr("prompt", "Generate code for testing"))
	graph := newTestGraph([]*dotparser.Node{node}, nil, nil)
	ctx := NewContext()

	outcome, err := handler.Execute(node, ctx, graph, tmpDir)

	require.NoError(t, err)
	assert.Equal(t, StatusSuccess, outcome.Status)
	assert.Contains(t, outcome.Notes, "Stage completed: test_stage")

	// Verify context updates
	assert.Equal(t, "test_stage", outcome.ContextUpdates["last_stage"])
	assert.Contains(t, outcome.ContextUpdates["last_response"].(string), "[Simulated]")

	// Verify files were written
	promptFile := filepath.Join(tmpDir, "test_stage", "prompt.md")
	responseFile := filepath.Join(tmpDir, "test_stage", "response.md")
	statusFile := filepath.Join(tmpDir, "test_stage", "status.json")

	promptData, err := os.ReadFile(promptFile)
	require.NoError(t, err)
	assert.Equal(t, "Generate code for testing", string(promptData))

	responseData, err := os.ReadFile(responseFile)
	require.NoError(t, err)
	assert.Equal(t, "[Simulated] Response for stage: test_stage", string(responseData))

	_, err = os.Stat(statusFile)
	assert.NoError(t, err)
}

func TestCodergenHandler_WithMockBackend(t *testing.T) {
	tmpDir := t.TempDir()

	mockBackend := &MockCodergenBackend{Response: "Generated code output"}
	handler := NewCodergenHandler(mockBackend)
	node := newNode("code_stage", strAttr("prompt", "Write tests"))
	graph := newTestGraph([]*dotparser.Node{node}, nil, nil)
	ctx := NewContext()

	outcome, err := handler.Execute(node, ctx, graph, tmpDir)

	require.NoError(t, err)
	assert.Equal(t, StatusSuccess, outcome.Status)

	// Verify backend was called
	require.Len(t, mockBackend.Calls, 1)
	assert.Equal(t, "code_stage", mockBackend.Calls[0].NodeID)
	assert.Equal(t, "Write tests", mockBackend.Calls[0].Prompt)

	// Verify response file
	responseFile := filepath.Join(tmpDir, "code_stage", "response.md")
	responseData, err := os.ReadFile(responseFile)
	require.NoError(t, err)
	assert.Equal(t, "Generated code output", string(responseData))
}

func TestCodergenHandler_BackendReturnsOutcome(t *testing.T) {
	tmpDir := t.TempDir()

	customOutcome := Fail("Backend decided to fail").WithContextUpdate("error_code", 42)
	mockBackend := &MockCodergenBackend{Response: customOutcome}
	handler := NewCodergenHandler(mockBackend)
	node := newNode("failing_stage")
	graph := newTestGraph([]*dotparser.Node{node}, nil, nil)
	ctx := NewContext()

	outcome, err := handler.Execute(node, ctx, graph, tmpDir)

	require.NoError(t, err)
	assert.Equal(t, StatusFail, outcome.Status)
	assert.Equal(t, "Backend decided to fail", outcome.FailureReason)
	assert.Equal(t, 42, outcome.ContextUpdates["error_code"])

	// Verify status was written (but not response.md since we returned outcome directly)
	statusFile := filepath.Join(tmpDir, "failing_stage", "status.json")
	_, err = os.Stat(statusFile)
	assert.NoError(t, err)
}

func TestCodergenHandler_PromptFallbackToLabel(t *testing.T) {
	tmpDir := t.TempDir()

	handler := NewCodergenHandler(nil)
	node := newNode("my_stage", strAttr("label", "Process the data"))
	graph := newTestGraph([]*dotparser.Node{node}, nil, nil)
	ctx := NewContext()

	_, err := handler.Execute(node, ctx, graph, tmpDir)
	require.NoError(t, err)

	promptFile := filepath.Join(tmpDir, "my_stage", "prompt.md")
	promptData, err := os.ReadFile(promptFile)
	require.NoError(t, err)
	assert.Equal(t, "Process the data", string(promptData))
}

func TestCodergenHandler_PromptFallbackToNodeID(t *testing.T) {
	tmpDir := t.TempDir()

	handler := NewCodergenHandler(nil)
	node := newNode("generate_report") // no prompt or label
	graph := newTestGraph([]*dotparser.Node{node}, nil, nil)
	ctx := NewContext()

	_, err := handler.Execute(node, ctx, graph, tmpDir)
	require.NoError(t, err)

	promptFile := filepath.Join(tmpDir, "generate_report", "prompt.md")
	promptData, err := os.ReadFile(promptFile)
	require.NoError(t, err)
	assert.Equal(t, "generate_report", string(promptData))
}

func TestCodergenHandler_GoalVariableExpansion(t *testing.T) {
	tmpDir := t.TempDir()

	handler := NewCodergenHandler(nil)
	node := newNode("plan", strAttr("prompt", "Create a plan to achieve: $goal"))
	graph := newTestGraph(
		[]*dotparser.Node{node},
		nil,
		[]dotparser.Attr{strAttr("goal", "Build a REST API")},
	)
	ctx := NewContext()

	_, err := handler.Execute(node, ctx, graph, tmpDir)
	require.NoError(t, err)

	promptFile := filepath.Join(tmpDir, "plan", "prompt.md")
	promptData, err := os.ReadFile(promptFile)
	require.NoError(t, err)
	assert.Equal(t, "Create a plan to achieve: Build a REST API", string(promptData))
}

func TestCodergenHandler_NoLogsRoot(t *testing.T) {
	handler := NewCodergenHandler(nil)
	node := newNode("test")
	graph := newTestGraph([]*dotparser.Node{node}, nil, nil)
	ctx := NewContext()

	outcome, err := handler.Execute(node, ctx, graph, "") // empty logsRoot

	require.NoError(t, err)
	assert.Equal(t, StatusSuccess, outcome.Status)
}

func TestCodergenHandler_ResponseTruncation(t *testing.T) {
	longResponse := make([]byte, 300)
	for i := range longResponse {
		longResponse[i] = 'a'
	}

	mockBackend := &MockCodergenBackend{Response: string(longResponse)}
	handler := NewCodergenHandler(mockBackend)
	node := newNode("test")
	graph := newTestGraph([]*dotparser.Node{node}, nil, nil)
	ctx := NewContext()

	outcome, err := handler.Execute(node, ctx, graph, "")
	require.NoError(t, err)

	lastResponse := outcome.ContextUpdates["last_response"].(string)
	assert.Len(t, lastResponse, 200)
	assert.True(t, lastResponse[len(lastResponse)-3:] == "...")
}

func TestVariableExpansionTransform(t *testing.T) {
	graph := &dotparser.Graph{
		Name: "TestGraph",
		GraphAttrs: []dotparser.Attr{
			strAttr("goal", "Build a CLI tool"),
		},
		Nodes: []*dotparser.Node{
			newNode("start", strAttr("shape", "Mdiamond")),
			newNode("plan", strAttr("prompt", "Plan how to $goal")),
			newNode("implement", strAttr("prompt", "$goal should be implemented step by step")),
			newNode("no_variable", strAttr("prompt", "Just do something")),
			newNode("exit", strAttr("shape", "Msquare")),
		},
	}

	transform := &VariableExpansionTransform{}
	result := transform.Apply(graph)

	// Check plan node
	planNode := result.NodeByID("plan")
	promptAttr, ok := planNode.Attr("prompt")
	require.True(t, ok)
	assert.Equal(t, "Plan how to Build a CLI tool", promptAttr.Str)

	// Check implement node
	implNode := result.NodeByID("implement")
	implPrompt, ok := implNode.Attr("prompt")
	require.True(t, ok)
	assert.Equal(t, "Build a CLI tool should be implemented step by step", implPrompt.Str)

	// Check node without variable is unchanged
	noVarNode := result.NodeByID("no_variable")
	noVarPrompt, ok := noVarNode.Attr("prompt")
	require.True(t, ok)
	assert.Equal(t, "Just do something", noVarPrompt.Str)
}

func TestVariableExpansionTransform_NoGoal(t *testing.T) {
	graph := &dotparser.Graph{
		Name: "TestGraph",
		Nodes: []*dotparser.Node{
			newNode("plan", strAttr("prompt", "Plan for $goal")),
		},
	}

	transform := &VariableExpansionTransform{}
	result := transform.Apply(graph)

	// Should remain unchanged when no goal attribute
	planNode := result.NodeByID("plan")
	promptAttr, ok := planNode.Attr("prompt")
	require.True(t, ok)
	assert.Equal(t, "Plan for $goal", promptAttr.Str)
}

func TestApplyTransforms_Order(t *testing.T) {
	var order []string

	t1 := TransformFunc(func(g *dotparser.Graph) *dotparser.Graph {
		order = append(order, "first")
		return g
	})
	t2 := TransformFunc(func(g *dotparser.Graph) *dotparser.Graph {
		order = append(order, "second")
		return g
	})
	t3 := TransformFunc(func(g *dotparser.Graph) *dotparser.Graph {
		order = append(order, "third")
		return g
	})

	graph := &dotparser.Graph{Name: "Test"}
	ApplyTransforms(graph, []Transform{t1, t2, t3})

	assert.Equal(t, []string{"first", "second", "third"}, order)
}

func TestRun_WritesManifest(t *testing.T) {
	tmpDir := t.TempDir()

	graph := newTestGraph(
		[]*dotparser.Node{
			newNode("start", strAttr("shape", "Mdiamond")),
			newNode("exit", strAttr("shape", "Msquare")),
		},
		[]*dotparser.Edge{
			newEdge("start", "exit"),
		},
		[]dotparser.Attr{
			strAttr("goal", "Test manifest writing"),
		},
	)
	graph.Name = "TestPipeline"

	_, err := Run(graph, &RunConfig{LogsRoot: tmpDir})
	require.NoError(t, err)

	manifestFile := filepath.Join(tmpDir, "manifest.json")
	data, err := os.ReadFile(manifestFile)
	require.NoError(t, err)

	var manifest map[string]any
	err = json.Unmarshal(data, &manifest)
	require.NoError(t, err)

	assert.Equal(t, "TestPipeline", manifest["name"])
	assert.Equal(t, "Test manifest writing", manifest["goal"])
	assert.NotEmpty(t, manifest["start_time"])
}

func TestRun_AppliesTransforms(t *testing.T) {
	tmpDir := t.TempDir()

	// Create a transform that modifies node prompts
	customTransform := TransformFunc(func(g *dotparser.Graph) *dotparser.Graph {
		for _, node := range g.Nodes {
			for i, attr := range node.Attrs {
				if attr.Key == "prompt" {
					node.Attrs[i].Value.Str = "[TRANSFORMED] " + attr.Value.Str
					node.Attrs[i].Value.Raw = node.Attrs[i].Value.Str
				}
			}
		}
		return g
	})

	graph := newTestGraph(
		[]*dotparser.Node{
			newNode("start", strAttr("shape", "Mdiamond")),
			newNode("task", strAttr("prompt", "Original prompt")),
			newNode("exit", strAttr("shape", "Msquare")),
		},
		[]*dotparser.Edge{
			newEdge("start", "task"),
			newEdge("task", "exit"),
		},
		nil,
	)

	_, err := Run(graph, &RunConfig{
		LogsRoot:   tmpDir,
		Transforms: []Transform{customTransform},
	})
	require.NoError(t, err)

	// Verify the transformed prompt was used
	promptFile := filepath.Join(tmpDir, "task", "prompt.md")
	promptData, err := os.ReadFile(promptFile)
	require.NoError(t, err)
	assert.Equal(t, "[TRANSFORMED] Original prompt", string(promptData))
}

func TestRun_FullLinearPipeline_Simulation(t *testing.T) {
	tmpDir := t.TempDir()

	graph := newTestGraph(
		[]*dotparser.Node{
			newNode("start", strAttr("shape", "Mdiamond")),
			newNode("plan", strAttr("shape", "box"), strAttr("prompt", "Create a plan for: $goal")),
			newNode("implement", strAttr("shape", "box"), strAttr("prompt", "Implement the plan")),
			newNode("review", strAttr("shape", "box"), strAttr("label", "Review the implementation")),
			newNode("exit", strAttr("shape", "Msquare")),
		},
		[]*dotparser.Edge{
			newEdge("start", "plan"),
			newEdge("plan", "implement"),
			newEdge("implement", "review"),
			newEdge("review", "exit"),
		},
		[]dotparser.Attr{
			strAttr("goal", "Build a REST API"),
		},
	)
	graph.Name = "LinearPipeline"

	result, err := Run(graph, &RunConfig{LogsRoot: tmpDir})

	require.NoError(t, err)
	assert.Equal(t, []string{"start", "plan", "implement", "review"}, result.CompletedNodes)
	assert.Equal(t, StatusSuccess, result.FinalOutcome.Status)

	// Verify manifest
	manifestData, err := os.ReadFile(filepath.Join(tmpDir, "manifest.json"))
	require.NoError(t, err)
	assert.Contains(t, string(manifestData), "LinearPipeline")
	assert.Contains(t, string(manifestData), "Build a REST API")

	// Verify plan stage files
	planPrompt, err := os.ReadFile(filepath.Join(tmpDir, "plan", "prompt.md"))
	require.NoError(t, err)
	assert.Equal(t, "Create a plan for: Build a REST API", string(planPrompt))

	planResponse, err := os.ReadFile(filepath.Join(tmpDir, "plan", "response.md"))
	require.NoError(t, err)
	assert.Contains(t, string(planResponse), "[Simulated]")

	// Verify review stage uses label as prompt
	reviewPrompt, err := os.ReadFile(filepath.Join(tmpDir, "review", "prompt.md"))
	require.NoError(t, err)
	assert.Equal(t, "Review the implementation", string(reviewPrompt))

	// Verify context updates propagate
	lastStage, ok := result.Context.Get("last_stage")
	assert.True(t, ok)
	assert.Equal(t, "review", lastStage)
}

func TestDefaultRegistry_IncludesCodergen(t *testing.T) {
	registry := DefaultRegistry()

	// Test that codergen handler is registered
	node := newNode("test", strAttr("type", "codergen"))
	handler := registry.Resolve(node)
	assert.NotNil(t, handler)

	// Test that shape=box resolves to codergen
	boxNode := newNode("box_test", strAttr("shape", "box"))
	boxHandler := registry.Resolve(boxNode)
	assert.NotNil(t, boxHandler)

	// Test that default handler works for unknown nodes
	unknownNode := newNode("unknown")
	defaultHandler := registry.Resolve(unknownNode)
	assert.NotNil(t, defaultHandler)
}

func TestCodergenHandler_StatusJSON_Format(t *testing.T) {
	tmpDir := t.TempDir()

	handler := NewCodergenHandler(nil)
	node := newNode("test_status")
	graph := newTestGraph([]*dotparser.Node{node}, nil, nil)
	ctx := NewContext()

	_, err := handler.Execute(node, ctx, graph, tmpDir)
	require.NoError(t, err)

	statusFile := filepath.Join(tmpDir, "test_status", "status.json")
	data, err := os.ReadFile(statusFile)
	require.NoError(t, err)

	var status map[string]any
	err = json.Unmarshal(data, &status)
	require.NoError(t, err)

	assert.Equal(t, "success", status["status"])
	assert.Contains(t, status["notes"], "Stage completed")

	contextUpdates, ok := status["context_updates"].(map[string]any)
	require.True(t, ok)
	assert.Equal(t, "test_status", contextUpdates["last_stage"])
	assert.NotEmpty(t, contextUpdates["last_response"])
}
