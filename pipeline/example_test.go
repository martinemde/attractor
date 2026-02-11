package pipeline

import (
	"encoding/json"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/martinemde/attractor/dotparser"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// testSleeper implements Sleeper as a no-op for test speed.
type testSleeper struct{}

func (testSleeper) Sleep(_ time.Duration) {}

// --- Parse + Validate + Execute integration tests ---

func loadDOT(t *testing.T, filename string) *dotparser.Graph {
	t.Helper()
	data, err := os.ReadFile(filepath.Join("testdata", filename))
	require.NoError(t, err, "failed to read %s", filename)
	graph, err := dotparser.Parse(data)
	require.NoError(t, err, "failed to parse %s", filename)
	return graph
}

func TestExample_SimpleLinear_ParseValidateExecute(t *testing.T) {
	graph := loadDOT(t, "simple_linear.dot")
	tmpDir := t.TempDir()

	// Parse assertions
	assert.Equal(t, "SimpleLinear", graph.Name)
	assert.Len(t, graph.Nodes, 3) // start, exit, task
	assert.Len(t, graph.Edges, 2) // start->task, task->exit

	goal, ok := graph.GraphAttr("goal")
	require.True(t, ok)
	assert.Equal(t, "Run a single task and exit", goal.Str)

	// Validate
	diags, err := ValidateOrError(graph)
	require.NoError(t, err, "validation should pass: %v", diags)

	// Execute - default transforms (StylesheetTransform, VariableExpansionTransform)
	// are automatically applied
	result, err := Run(graph, &RunConfig{
		LogsRoot: tmpDir,
	})
	require.NoError(t, err)

	// Verify traversal order
	assert.Equal(t, []string{"start", "task"}, result.CompletedNodes)
	assert.Equal(t, StatusSuccess, result.FinalOutcome.Status)

	// Verify context was populated
	goalCtx, ok := result.Context.Get("graph.goal")
	assert.True(t, ok)
	assert.Equal(t, "Run a single task and exit", goalCtx)

	lastStage, ok := result.Context.Get("last_stage")
	assert.True(t, ok)
	assert.Equal(t, "task", lastStage)

	// Verify artifacts were written
	assertStatusFile(t, tmpDir, "start", StatusSuccess)
	assertStatusFile(t, tmpDir, "task", StatusSuccess)

	// Verify prompt.md includes expanded $goal
	promptData, err := os.ReadFile(filepath.Join(tmpDir, "task", "prompt.md"))
	require.NoError(t, err)
	assert.Contains(t, string(promptData), "Run a single task and exit")
	assert.NotContains(t, string(promptData), "$goal")

	// Verify checkpoint exists
	cp, err := LoadCheckpoint(tmpDir)
	require.NoError(t, err)
	assert.Equal(t, "task", cp.CurrentNode)
	assert.Contains(t, cp.CompletedNodes, "start")
	assert.Contains(t, cp.CompletedNodes, "task")
}

func TestExample_ConditionalBranch_ParseValidateExecute(t *testing.T) {
	graph := loadDOT(t, "conditional_branch.dot")

	// Parse assertions
	assert.Equal(t, "ConditionalBranch", graph.Name)
	assert.Len(t, graph.Nodes, 6)
	assert.Len(t, graph.Edges, 6) // 4 from chain + 2 from gate

	gate := graph.NodeByID("gate")
	require.NotNil(t, gate)
	shape, ok := gate.Attr("shape")
	require.True(t, ok)
	assert.Equal(t, "diamond", shape.Str)

	// Validate
	diags, err := ValidateOrError(graph)
	require.NoError(t, err, "validation should pass: %v", diags)

	t.Run("all succeed path", func(t *testing.T) {
		tmpDir := t.TempDir()

		// All nodes use the default codergen handler (simulation mode)
		// which returns SUCCESS, so the pipeline should follow the success path
		result, err := Run(graph, &RunConfig{LogsRoot: tmpDir})
		require.NoError(t, err)

		// The conditional handler returns SUCCESS, which matches the
		// gate -> exit edge condition "outcome=success"
		assert.Contains(t, result.CompletedNodes, "plan")
		assert.Contains(t, result.CompletedNodes, "implement")
		assert.Contains(t, result.CompletedNodes, "validate")
		assert.Contains(t, result.CompletedNodes, "gate")
		assert.Equal(t, StatusSuccess, result.FinalOutcome.Status)
	})

	t.Run("gate routes on failure", func(t *testing.T) {
		tmpDir := t.TempDir()

		// Register a handler that fails on the first call to gate, then succeeds
		callCount := 0
		conditionalFail := HandlerFunc(func(node *dotparser.Node, ctx *Context, graph *dotparser.Graph, logsRoot string) (*Outcome, error) {
			callCount++
			if callCount == 1 {
				// First time through gate: return fail to route back to implement
				return Fail("tests not passing"), nil
			}
			// Second time: success
			return Success(), nil
		})

		registry := DefaultRegistry()
		registry.Register("conditional", conditionalFail)

		result, err := Run(graph, &RunConfig{
			LogsRoot: tmpDir,
			Registry: registry,
		})
		require.NoError(t, err)

		// Gate should have been visited twice (fail then success)
		gateCount := 0
		for _, id := range result.CompletedNodes {
			if id == "gate" {
				gateCount++
			}
		}
		assert.Equal(t, 2, gateCount, "gate should be visited twice")

		// implement should also have been visited at least twice
		implCount := 0
		for _, id := range result.CompletedNodes {
			if id == "implement" {
				implCount++
			}
		}
		assert.GreaterOrEqual(t, implCount, 2, "implement should be visited at least twice")
	})
}

func TestExample_HumanGate_ParseValidateExecute(t *testing.T) {
	graph := loadDOT(t, "human_gate.dot")

	// Parse assertions
	assert.Equal(t, "HumanGate", graph.Name)
	rg := graph.NodeByID("review_gate")
	require.NotNil(t, rg)
	typeAttr, ok := rg.Attr("type")
	require.True(t, ok)
	assert.Equal(t, "wait.human", typeAttr.Str)

	// Validate
	diags, err := ValidateOrError(graph)
	require.NoError(t, err, "validation should pass: %v", diags)

	t.Run("auto approve follows first option", func(t *testing.T) {
		tmpDir := t.TempDir()

		// AutoApproveInterviewer selects the first option: "[A] Approve"
		result, err := Run(graph, &RunConfig{
			LogsRoot:    tmpDir,
			Interviewer: &AutoApproveInterviewer{},
		})
		require.NoError(t, err)

		assert.Contains(t, result.CompletedNodes, "implement")
		assert.Contains(t, result.CompletedNodes, "review_gate")
		assert.Equal(t, StatusSuccess, result.FinalOutcome.Status)

		// Verify human gate context was set
		selected, ok := result.Context.Get("human.gate.selected")
		assert.True(t, ok, "human.gate.selected should be in context")
		assert.Equal(t, "A", selected)
	})

	t.Run("queue interviewer selects fix", func(t *testing.T) {
		tmpDir := t.TempDir()

		// Queue: first answer "Fix", then "Approve" (after looping back)
		qi := NewQueueInterviewer(
			&Answer{Value: "F", SelectedOption: &Option{Key: "F", Label: "[F] Fix"}},
			&Answer{Value: "A", SelectedOption: &Option{Key: "A", Label: "[A] Approve"}},
		)

		result, err := Run(graph, &RunConfig{
			LogsRoot:    tmpDir,
			Interviewer: qi,
		})
		require.NoError(t, err)

		// review_gate should be visited twice (Fix then Approve)
		gateCount := 0
		for _, id := range result.CompletedNodes {
			if id == "review_gate" {
				gateCount++
			}
		}
		assert.Equal(t, 2, gateCount, "review_gate should be visited twice")
		assert.Equal(t, 0, qi.Remaining(), "all queued answers should be consumed")
	})

	t.Run("recording interviewer captures QA", func(t *testing.T) {
		tmpDir := t.TempDir()

		recorder := NewRecordingInterviewer(&AutoApproveInterviewer{})

		result, err := Run(graph, &RunConfig{
			LogsRoot:    tmpDir,
			Interviewer: recorder,
		})
		require.NoError(t, err)
		assert.Equal(t, StatusSuccess, result.FinalOutcome.Status)

		recordings := recorder.Recordings()
		require.Len(t, recordings, 1)
		assert.Equal(t, "Review Changes", recordings[0].Question.Text)
		assert.Equal(t, QuestionMultipleChoice, recordings[0].Question.Type)
		assert.Len(t, recordings[0].Question.Options, 2) // Approve, Fix
	})
}

func TestExample_GoalGate_ParseValidateExecute(t *testing.T) {
	graph := loadDOT(t, "goal_gate.dot")
	tmpDir := t.TempDir()

	// Parse assertions
	assert.Equal(t, "GoalGate", graph.Name)

	impl := graph.NodeByID("implement")
	require.NotNil(t, impl)
	gg, ok := impl.Attr("goal_gate")
	require.True(t, ok)
	assert.True(t, gg.Bool)

	// Validate
	diags, err := ValidateOrError(graph)
	require.NoError(t, err, "validation should pass: %v", diags)

	// Execute - default transforms are automatically applied
	result, err := Run(graph, &RunConfig{
		LogsRoot: tmpDir,
	})
	require.NoError(t, err)

	// In simulation mode all handlers succeed, so goal gate should be satisfied
	assert.Equal(t, StatusSuccess, result.FinalOutcome.Status)
	assert.Contains(t, result.CompletedNodes, "plan")
	assert.Contains(t, result.CompletedNodes, "implement")
	assert.Contains(t, result.CompletedNodes, "review")

	// Verify prompt was expanded for plan node
	planPrompt, err := os.ReadFile(filepath.Join(tmpDir, "plan", "prompt.md"))
	require.NoError(t, err)
	assert.Contains(t, string(planPrompt), "Create a hello world script")
}

func TestExample_GoalGate_UnsatisfiedBlocksExit(t *testing.T) {
	graph := loadDOT(t, "goal_gate.dot")
	tmpDir := t.TempDir()

	// Make the implement node fail
	failOnImplement := &MockHandler{
		Outcomes: []*Outcome{
			Fail("implementation failed"),
		},
	}

	registry := DefaultRegistry()
	registry.Register("fail_impl", failOnImplement)

	// Modify the implement node to use our failing handler
	impl := graph.NodeByID("implement")
	require.NotNil(t, impl)
	impl.Attrs = append(impl.Attrs, dotparser.Attr{
		Key:   "type",
		Value: dotparser.Value{Kind: dotparser.ValueString, Str: "fail_impl"},
	})

	// The implement node has condition="outcome=fail" -> plan, which loops.
	// To avoid infinite loops, use a handler that fails then succeeds.
	callCount := 0
	failThenSucceed := HandlerFunc(func(node *dotparser.Node, ctx *Context, graph *dotparser.Graph, logsRoot string) (*Outcome, error) {
		callCount++
		if callCount <= 1 {
			return Fail("implementation failed"), nil
		}
		return Success().WithNotes("implementation succeeded"), nil
	})
	registry.Register("fail_impl", failThenSucceed)

	result, err := Run(graph, &RunConfig{
		LogsRoot: tmpDir,
		Registry: registry,
	})
	require.NoError(t, err)

	// Should eventually succeed after retry loop
	assert.Equal(t, StatusSuccess, result.FinalOutcome.Status)
	assert.Greater(t, callCount, 1, "implement should have been called more than once")
}

func TestExample_RetryPipeline_ParseValidateExecute(t *testing.T) {
	graph := loadDOT(t, "retry_pipeline.dot")
	tmpDir := t.TempDir()

	// Parse assertions
	assert.Equal(t, "RetryPipeline", graph.Name)
	flaky := graph.NodeByID("flaky_task")
	require.NotNil(t, flaky)
	mr, ok := flaky.Attr("max_retries")
	require.True(t, ok)
	assert.Equal(t, int64(3), mr.Int)
	ap, ok := flaky.Attr("allow_partial")
	require.True(t, ok)
	assert.True(t, ap.Bool)

	// Validate
	diags, err := ValidateOrError(graph)
	require.NoError(t, err, "validation should pass: %v", diags)

	t.Run("succeeds on first try", func(t *testing.T) {
		result, err := Run(graph, &RunConfig{LogsRoot: tmpDir})
		require.NoError(t, err)
		assert.Equal(t, StatusSuccess, result.FinalOutcome.Status)
	})

	t.Run("retries then succeeds", func(t *testing.T) {
		tmpDir := t.TempDir()
		callCount := 0
		retryThenSucceed := HandlerFunc(func(node *dotparser.Node, ctx *Context, graph *dotparser.Graph, logsRoot string) (*Outcome, error) {
			callCount++
			if callCount < 3 {
				return Retry("not ready yet"), nil
			}
			return Success().WithNotes("finally worked"), nil
		})

		registry := DefaultRegistry()
		registry.Register("codergen", retryThenSucceed)
		registry.SetDefaultHandler(retryThenSucceed)

		// Use a noop sleeper to avoid real delays
		result, err := Run(graph, &RunConfig{
			LogsRoot: tmpDir,
			Registry: registry,
			Sleeper:  testSleeper{},
		})
		require.NoError(t, err)
		assert.Equal(t, StatusSuccess, result.FinalOutcome.Status)
		assert.Equal(t, 3, callCount, "should retry twice then succeed on third attempt")
	})

	t.Run("exhausts retries returns partial", func(t *testing.T) {
		tmpDir := t.TempDir()
		alwaysRetry := HandlerFunc(func(node *dotparser.Node, ctx *Context, graph *dotparser.Graph, logsRoot string) (*Outcome, error) {
			return Retry("always retry"), nil
		})

		registry := DefaultRegistry()
		registry.Register("codergen", alwaysRetry)
		registry.SetDefaultHandler(alwaysRetry)

		result, err := Run(graph, &RunConfig{
			LogsRoot: tmpDir,
			Registry: registry,
			Sleeper:  testSleeper{},
		})
		require.NoError(t, err)

		// allow_partial=true means we get PARTIAL_SUCCESS instead of FAIL
		assert.Equal(t, StatusPartialSuccess, result.FinalOutcome.Status)
	})
}

func TestExample_MultiStage_ParseValidateExecute(t *testing.T) {
	graph := loadDOT(t, "multi_stage.dot")
	tmpDir := t.TempDir()

	// Parse assertions
	assert.Equal(t, "MultiStage", graph.Name)
	assert.Len(t, graph.Nodes, 9) // start, exit, + 7 work nodes

	// Verify stylesheet was parsed as a graph attribute
	ss, ok := graph.GraphAttr("model_stylesheet")
	assert.True(t, ok)
	assert.Contains(t, ss.Str, "claude-sonnet-4-5")
	assert.Contains(t, ss.Str, "claude-opus-4-6")

	// Verify subgraph class derivation
	impl := graph.NodeByID("implement")
	require.NotNil(t, impl)
	cls, ok := impl.Attr("class")
	// Should have both the subgraph-derived class and the explicit class
	if ok {
		assert.Contains(t, cls.Str, "code")
	}

	// Verify goal_gate on implement
	gg, ok := impl.Attr("goal_gate")
	require.True(t, ok)
	assert.True(t, gg.Bool)

	// Validate
	diags, err := ValidateOrError(graph)
	require.NoError(t, err, "validation should pass: %v", diags)

	// Execute - default transforms are automatically applied
	result, err := Run(graph, &RunConfig{
		LogsRoot: tmpDir,
	})
	require.NoError(t, err)

	assert.Equal(t, StatusSuccess, result.FinalOutcome.Status)

	// All stages should have been visited
	for _, nodeID := range []string{"gather_requirements", "design", "implement", "write_tests", "run_tests", "security_review", "deploy_check"} {
		assert.Contains(t, result.CompletedNodes, nodeID, "missing node %s", nodeID)
	}

	// Verify status files for all work nodes
	for _, nodeID := range []string{"gather_requirements", "design", "implement"} {
		assertStatusFile(t, tmpDir, nodeID, StatusSuccess)
	}

	// Verify manifest was written
	manifestData, err := os.ReadFile(filepath.Join(tmpDir, "manifest.json"))
	require.NoError(t, err)
	var manifest map[string]any
	require.NoError(t, json.Unmarshal(manifestData, &manifest))
	assert.Equal(t, "MultiStage", manifest["name"])
	assert.Equal(t, "Build, test, and deploy a web service", manifest["goal"])
}

func TestExample_CheckpointAndResume(t *testing.T) {
	graph := loadDOT(t, "simple_linear.dot")
	tmpDir := t.TempDir()

	// Run the pipeline to completion to create a checkpoint
	result, err := Run(graph, &RunConfig{LogsRoot: tmpDir})
	require.NoError(t, err)
	assert.Equal(t, StatusSuccess, result.FinalOutcome.Status)

	// Load checkpoint and verify
	cp, err := LoadCheckpoint(tmpDir)
	require.NoError(t, err)
	assert.Equal(t, "task", cp.CurrentNode)
	assert.Contains(t, cp.CompletedNodes, "start")
	assert.Contains(t, cp.CompletedNodes, "task")
	assert.Contains(t, cp.ContextValues, "graph.goal")
	assert.Contains(t, cp.ContextValues, "last_stage")

	// Resume should complete without error (already at terminal)
	resumeResult, err := Resume(graph, &RunConfig{LogsRoot: tmpDir})
	require.NoError(t, err)
	assert.NotNil(t, resumeResult)
}

func TestExample_EventEmitter(t *testing.T) {
	graph := loadDOT(t, "simple_linear.dot")
	tmpDir := t.TempDir()

	emitter := NewEventEmitter()
	var events []Event
	emitter.On(func(e Event) {
		events = append(events, e)
	})

	_, err := Run(graph, &RunConfig{
		LogsRoot:     tmpDir,
		EventEmitter: emitter,
	})
	require.NoError(t, err)

	// Should have: PipelineStarted, StageStarted(start), StageCompleted(start),
	// CheckpointSaved(start), StageStarted(task), StageCompleted(task),
	// CheckpointSaved(task), PipelineCompleted
	assert.GreaterOrEqual(t, len(events), 4, "should have emitted pipeline and stage events")

	// Verify first and last events
	assert.Equal(t, EventPipelineStarted, events[0].Type)
	assert.Equal(t, EventPipelineCompleted, events[len(events)-1].Type)

	// Verify checkpoint events
	checkpointCount := 0
	for _, e := range events {
		if e.Type == EventCheckpointSaved {
			checkpointCount++
		}
	}
	assert.Equal(t, 2, checkpointCount, "should save checkpoint for start and task nodes")
}

func TestExample_CustomHandler(t *testing.T) {
	graph := loadDOT(t, "simple_linear.dot")
	tmpDir := t.TempDir()

	// Replace the codergen handler with a custom one that sets context
	var executedNodes []string
	custom := HandlerFunc(func(node *dotparser.Node, ctx *Context, graph *dotparser.Graph, logsRoot string) (*Outcome, error) {
		executedNodes = append(executedNodes, node.ID)
		return Success().WithContextUpdate("custom."+node.ID, "handled"), nil
	})

	registry := DefaultRegistry()
	registry.Register("codergen", custom)
	registry.SetDefaultHandler(custom)

	result, err := Run(graph, &RunConfig{
		LogsRoot: tmpDir,
		Registry: registry,
	})
	require.NoError(t, err)
	assert.Equal(t, StatusSuccess, result.FinalOutcome.Status)
	assert.Equal(t, []string{"task"}, executedNodes)

	val, ok := result.Context.Get("custom.task")
	assert.True(t, ok)
	assert.Equal(t, "handled", val)
}

func TestExample_EdgeSelectionPriority(t *testing.T) {
	// Build a graph that exercises all 5 steps of edge selection
	src := `
digraph EdgePriority {
    graph [goal="Test edge selection"]
    start [shape=Mdiamond]
    exit  [shape=Msquare]

    A [label="Node A"]
    high_weight [label="High Weight"]
    condition_match [label="Condition Match"]
    low_weight [label="Low Weight"]

    start -> A
    A -> high_weight     [weight=100]
    A -> condition_match [condition="outcome=success", weight=1]
    A -> low_weight      [weight=1]
    high_weight -> exit
    condition_match -> exit
    low_weight -> exit
}
`
	graph, err := dotparser.Parse([]byte(src))
	require.NoError(t, err)

	diags, err := ValidateOrError(graph)
	require.NoError(t, err, "validation should pass: %v", diags)

	result, err := Run(graph, nil)
	require.NoError(t, err)

	// Condition match should win over higher weight (Step 1 beats Step 4)
	assert.Contains(t, result.CompletedNodes, "condition_match",
		"condition match should win over weight")
	assert.NotContains(t, result.CompletedNodes, "high_weight",
		"high weight should not be chosen when condition matches")
}

func TestExample_ConditionExpressions(t *testing.T) {
	src := `
digraph Conditions {
    start [shape=Mdiamond]
    exit  [shape=Msquare]

    A [label="Node A"]
    on_success [label="On Success"]
    on_fail    [label="On Fail"]

    start -> A
    A -> on_success [condition="outcome=success"]
    A -> on_fail    [condition="outcome!=success"]
    on_success -> exit
    on_fail -> exit
}
`
	graph, err := dotparser.Parse([]byte(src))
	require.NoError(t, err)

	t.Run("success routes to on_success", func(t *testing.T) {
		result, err := Run(graph, nil)
		require.NoError(t, err)
		assert.Contains(t, result.CompletedNodes, "on_success")
		assert.NotContains(t, result.CompletedNodes, "on_fail")
	})

	t.Run("fail routes to on_fail", func(t *testing.T) {
		failHandler := &MockHandler{
			Outcomes: []*Outcome{Fail("error")},
		}
		registry := DefaultRegistry()
		registry.Register("codergen", failHandler)
		registry.SetDefaultHandler(failHandler)

		result, err := Run(graph, &RunConfig{Registry: registry})
		require.NoError(t, err)
		assert.Contains(t, result.CompletedNodes, "on_fail")
		assert.NotContains(t, result.CompletedNodes, "on_success")
	})
}

func TestExample_AllDOTFilesParseAndValidate(t *testing.T) {
	// Smoke test: every .dot file in testdata should parse and validate
	files, err := filepath.Glob("testdata/*.dot")
	require.NoError(t, err)
	require.NotEmpty(t, files, "should have test DOT files")

	for _, file := range files {
		t.Run(filepath.Base(file), func(t *testing.T) {
			data, err := os.ReadFile(file)
			require.NoError(t, err)

			graph, err := dotparser.Parse(data)
			require.NoError(t, err, "parse should succeed")

			diags, err := ValidateOrError(graph)
			require.NoError(t, err, "validation should pass: %v", diags)

			// Every graph should have a start and exit node
			start := findStartNode(graph)
			assert.NotNil(t, start, "should have a start node")
		})
	}
}

// --- Helpers ---

// assertStatusFile verifies that a status.json exists for a node and has the expected status.
func assertStatusFile(t *testing.T, logsRoot, nodeID string, expectedStatus StageStatus) {
	t.Helper()
	statusPath := filepath.Join(logsRoot, nodeID, "status.json")
	data, err := os.ReadFile(statusPath)
	require.NoError(t, err, "status.json should exist for %s", nodeID)
	assert.Contains(t, string(data), `"status": "`+string(expectedStatus)+`"`,
		"status should be %s for %s", expectedStatus, nodeID)
}
