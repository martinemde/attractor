package pipeline

import (
	"os"
	"strings"
	"testing"

	"github.com/martinemde/attractor/dotparser"
	"github.com/stretchr/testify/assert"
)

func TestPreambleGenerator_FullFidelity_ReturnsEmpty(t *testing.T) {
	gen := &PreambleGenerator{}
	ctx := NewContext()
	graph := &dotparser.Graph{Name: "TestPipeline"}

	preamble := gen.GeneratePreamble(FidelityFull, ctx, graph, nil, nil, "node1")

	assert.Empty(t, preamble, "full fidelity should return empty preamble")
}

func TestPreambleGenerator_TruncateFidelity(t *testing.T) {
	gen := &PreambleGenerator{}
	ctx := NewContext()
	graph := &dotparser.Graph{
		Name: "TestPipeline",
		GraphAttrs: []dotparser.Attr{
			{Key: "goal", Value: dotparser.Value{Str: "Build a REST API"}},
		},
	}

	preamble := gen.GeneratePreamble(FidelityTruncate, ctx, graph, nil, nil, "node1")

	assert.Contains(t, preamble, "## Context")
	assert.Contains(t, preamble, "**Goal:** Build a REST API")
	assert.Contains(t, preamble, "**Pipeline:** TestPipeline")
	// Truncate mode should NOT include completed stages
	assert.NotContains(t, preamble, "### Completed Stages")
}

func TestPreambleGenerator_CompactFidelity_WithCompletedStages(t *testing.T) {
	gen := &PreambleGenerator{}
	ctx := NewContext()
	ctx.Set("last_stage", "implement")
	ctx.Set("result_code", 42)
	graph := &dotparser.Graph{
		Name: "TestPipeline",
		GraphAttrs: []dotparser.Attr{
			{Key: "goal", Value: dotparser.Value{Str: "Build a REST API"}},
		},
	}

	completedNodes := []string{"start", "plan", "implement"}
	nodeOutcomes := map[string]*Outcome{
		"start":     Success(),
		"plan":      Success().WithNotes("Plan created"),
		"implement": PartialSuccess("Tests failing"),
	}

	preamble := gen.GeneratePreamble(FidelityCompact, ctx, graph, completedNodes, nodeOutcomes, "review")

	// Check structure
	assert.Contains(t, preamble, "## Context")
	assert.Contains(t, preamble, "**Goal:** Build a REST API")
	assert.Contains(t, preamble, "**Pipeline:** TestPipeline")

	// Check completed stages
	assert.Contains(t, preamble, "### Completed Stages")
	assert.Contains(t, preamble, "- start: success")
	assert.Contains(t, preamble, "- plan: success (Plan created)")
	assert.Contains(t, preamble, "- implement: partial_success (Tests failing)")

	// Check key context values
	assert.Contains(t, preamble, "### Key Context Values")
	assert.Contains(t, preamble, "**last_stage:** implement")
	assert.Contains(t, preamble, "**result_code:** 42")
}

func TestPreambleGenerator_SummaryLow(t *testing.T) {
	gen := &PreambleGenerator{}
	ctx := NewContext()
	graph := &dotparser.Graph{
		Name: "TestPipeline",
		GraphAttrs: []dotparser.Attr{
			{Key: "goal", Value: dotparser.Value{Str: "Test goal"}},
		},
	}

	completedNodes := []string{"start", "stage1", "stage2", "stage3", "stage4", "stage5"}
	nodeOutcomes := map[string]*Outcome{}
	for _, id := range completedNodes {
		nodeOutcomes[id] = Success()
	}

	preamble := gen.GeneratePreamble(FidelitySummaryLow, ctx, graph, completedNodes, nodeOutcomes, "next")

	assert.Contains(t, preamble, "## Pipeline Context Summary")
	assert.Contains(t, preamble, "**Progress:**")

	// Summary:low shows only 2 recent stages
	// Count how many stages are shown in Recent Stage Outcomes
	lines := strings.Split(preamble, "\n")
	stageLineCount := 0
	for _, line := range lines {
		if strings.HasPrefix(line, "- **") && strings.Contains(line, ": success") {
			stageLineCount++
		}
	}
	assert.LessOrEqual(t, stageLineCount, 2, "summary:low should show at most 2 recent stages")
}

func TestPreambleGenerator_SummaryMedium(t *testing.T) {
	gen := &PreambleGenerator{}
	ctx := NewContext()
	ctx.Set("key1", "value1")
	ctx.Set("key2", "value2")
	graph := &dotparser.Graph{Name: "TestPipeline"}

	completedNodes := []string{"start", "stage1", "stage2", "stage3", "stage4", "stage5", "stage6", "stage7"}
	nodeOutcomes := map[string]*Outcome{}
	for _, id := range completedNodes {
		nodeOutcomes[id] = Success().WithNotes("Stage " + id + " complete")
	}

	preamble := gen.GeneratePreamble(FidelitySummaryMedium, ctx, graph, completedNodes, nodeOutcomes, "next")

	assert.Contains(t, preamble, "## Pipeline Context Summary")
	assert.Contains(t, preamble, "**Progress:**")
	assert.Contains(t, preamble, "8 stages completed") // all 8 stages
	// Medium should include notes in stage outcomes
	assert.Contains(t, preamble, "Stage ")
}

func TestPreambleGenerator_SummaryHigh(t *testing.T) {
	gen := &PreambleGenerator{}
	ctx := NewContext()
	ctx.AppendLog("Log entry 1")
	ctx.AppendLog("Log entry 2")
	graph := &dotparser.Graph{Name: "TestPipeline"}

	completedNodes := []string{"start", "implement"}
	nodeOutcomes := map[string]*Outcome{
		"start":     Success(),
		"implement": Success().WithNotes("Code written").WithContextUpdate("code_path", "/tmp/code.go"),
	}

	preamble := gen.GeneratePreamble(FidelitySummaryHigh, ctx, graph, completedNodes, nodeOutcomes, "review")

	assert.Contains(t, preamble, "## Pipeline Context Summary")
	// High level should include context updates from outcomes
	assert.Contains(t, preamble, "Context updates")
	assert.Contains(t, preamble, "code_path")
	// High level should include logs
	assert.Contains(t, preamble, "### Recent Logs")
	assert.Contains(t, preamble, "Log entry 1")
	assert.Contains(t, preamble, "Log entry 2")
}

func TestPreambleGenerator_ExcludesInternalKeys(t *testing.T) {
	gen := &PreambleGenerator{}
	ctx := NewContext()
	ctx.Set("internal.secret", "should not appear")
	ctx.Set("graph.goal", "should not appear as graph value")
	ctx.Set("outcome", "success")       // transient
	ctx.Set("preferred_label", "label") // transient
	ctx.Set("user_key", "should appear")

	graph := &dotparser.Graph{Name: "Test"}

	preamble := gen.GeneratePreamble(FidelityCompact, ctx, graph, nil, nil, "node1")

	assert.NotContains(t, preamble, "internal.secret")
	assert.NotContains(t, preamble, "graph.goal")
	assert.NotContains(t, preamble, "**outcome:**")     // outcome as key
	assert.NotContains(t, preamble, "**preferred_label:**")
	assert.Contains(t, preamble, "**user_key:** should appear")
}

func TestInjectPreambleIntoContext(t *testing.T) {
	ctx := NewContext()

	InjectPreambleIntoContext(ctx, "Test preamble content")

	result := GetPreambleFromContext(ctx)
	assert.Equal(t, "Test preamble content", result)
}

func TestGetPreambleFromContext_ReturnsEmptyWhenNotSet(t *testing.T) {
	ctx := NewContext()

	result := GetPreambleFromContext(ctx)

	assert.Empty(t, result)
}

func TestPreambleIntegration_InExecutionLoop(t *testing.T) {
	tmpDir := t.TempDir()

	// Create a simple pipeline with compact fidelity
	graph := &dotparser.Graph{
		Name: "IntegrationTest",
		GraphAttrs: []dotparser.Attr{
			{Key: "goal", Value: dotparser.Value{Str: "Test goal"}},
			{Key: "default_fidelity", Value: dotparser.Value{Str: "compact"}},
		},
		Nodes: []*dotparser.Node{
			{ID: "start", Attrs: []dotparser.Attr{{Key: "shape", Value: dotparser.Value{Str: "Mdiamond"}}}},
			{ID: "task1", Attrs: []dotparser.Attr{{Key: "prompt", Value: dotparser.Value{Str: "Do task 1"}}}},
			{ID: "task2", Attrs: []dotparser.Attr{{Key: "prompt", Value: dotparser.Value{Str: "Do task 2"}}}},
			{ID: "exit", Attrs: []dotparser.Attr{{Key: "shape", Value: dotparser.Value{Str: "Msquare"}}}},
		},
		Edges: []*dotparser.Edge{
			{From: "start", To: "task1"},
			{From: "task1", To: "task2"},
			{From: "task2", To: "exit"},
		},
	}

	result, err := Run(graph, &RunConfig{LogsRoot: tmpDir})

	assert.NoError(t, err)
	assert.Equal(t, []string{"start", "task1", "task2"}, result.CompletedNodes)

	// Read task2's prompt to verify preamble was injected
	// (since default_fidelity is compact, non-full fidelity will have preamble)
	promptData, err := readFile(tmpDir, "task2", "prompt.md")
	assert.NoError(t, err)

	// Should contain the preamble
	assert.Contains(t, string(promptData), "## Context")
	assert.Contains(t, string(promptData), "### Completed Stages")
	assert.Contains(t, string(promptData), "task1: success")
	// Should also contain the actual prompt after the preamble
	assert.Contains(t, string(promptData), "Do task 2")
}

func TestPreambleIntegration_FullFidelityNoPreamble(t *testing.T) {
	tmpDir := t.TempDir()

	// Create a pipeline with full fidelity
	graph := &dotparser.Graph{
		Name: "FullFidelityTest",
		GraphAttrs: []dotparser.Attr{
			{Key: "default_fidelity", Value: dotparser.Value{Str: "full"}},
		},
		Nodes: []*dotparser.Node{
			{ID: "start", Attrs: []dotparser.Attr{{Key: "shape", Value: dotparser.Value{Str: "Mdiamond"}}}},
			{ID: "task", Attrs: []dotparser.Attr{{Key: "prompt", Value: dotparser.Value{Str: "Simple prompt"}}}},
			{ID: "exit", Attrs: []dotparser.Attr{{Key: "shape", Value: dotparser.Value{Str: "Msquare"}}}},
		},
		Edges: []*dotparser.Edge{
			{From: "start", To: "task"},
			{From: "task", To: "exit"},
		},
	}

	result, err := Run(graph, &RunConfig{LogsRoot: tmpDir})

	assert.NoError(t, err)
	assert.Equal(t, []string{"start", "task"}, result.CompletedNodes)

	// Read task's prompt - should have NO preamble
	promptData, err := readFile(tmpDir, "task", "prompt.md")
	assert.NoError(t, err)

	// Should NOT contain preamble structure
	assert.NotContains(t, string(promptData), "## Context")
	assert.NotContains(t, string(promptData), "### Completed Stages")
	// Should contain only the actual prompt
	assert.Equal(t, "Simple prompt", string(promptData))
}

func TestPreambleIntegration_EdgeFidelityOverridesNode(t *testing.T) {
	tmpDir := t.TempDir()

	// Create a pipeline where node has compact but edge has full
	graph := &dotparser.Graph{
		Name: "EdgeOverrideTest",
		Nodes: []*dotparser.Node{
			{ID: "start", Attrs: []dotparser.Attr{{Key: "shape", Value: dotparser.Value{Str: "Mdiamond"}}}},
			{ID: "task", Attrs: []dotparser.Attr{
				{Key: "prompt", Value: dotparser.Value{Str: "Task prompt"}},
				{Key: "fidelity", Value: dotparser.Value{Str: "compact"}}, // node says compact
			}},
			{ID: "exit", Attrs: []dotparser.Attr{{Key: "shape", Value: dotparser.Value{Str: "Msquare"}}}},
		},
		Edges: []*dotparser.Edge{
			{From: "start", To: "task", Attrs: []dotparser.Attr{
				{Key: "fidelity", Value: dotparser.Value{Str: "full"}}, // edge says full
			}},
			{From: "task", To: "exit"},
		},
	}

	result, err := Run(graph, &RunConfig{LogsRoot: tmpDir})

	assert.NoError(t, err)
	assert.Equal(t, []string{"start", "task"}, result.CompletedNodes)

	// Read task's prompt - edge's full fidelity should win
	promptData, err := readFile(tmpDir, "task", "prompt.md")
	assert.NoError(t, err)

	// Should NOT contain preamble because edge fidelity=full overrides node
	assert.NotContains(t, string(promptData), "## Context")
	assert.Equal(t, "Task prompt", string(promptData))
}

// readFile is a helper to read a file from the logs directory
func readFile(logsRoot, nodeID, filename string) ([]byte, error) {
	path := logsRoot + "/" + nodeID + "/" + filename
	return os.ReadFile(path)
}
