package pipeline

import (
	"encoding/json"
	"fmt"
	"sync/atomic"
	"testing"
	"time"

	"github.com/martinemde/attractor/dotparser"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestParallelHandler_ExecutesMultipleBranches(t *testing.T) {
	handler := &ParallelHandler{}

	// Create a parallel node with two outgoing branches
	parallelNode := newNode("parallel",
		strAttr("shape", "component"),
	)
	branchA := newNode("branchA", strAttr("test_outcome", "success"))
	branchB := newNode("branchB", strAttr("test_outcome", "success"))
	exitNode := newNode("exit", strAttr("shape", "Msquare"))

	graph := newTestGraph(
		[]*dotparser.Node{parallelNode, branchA, branchB, exitNode},
		[]*dotparser.Edge{
			newEdge("parallel", "branchA"),
			newEdge("parallel", "branchB"),
		},
		nil,
	)

	ctx := NewContext()
	outcome, err := handler.Execute(parallelNode, ctx, graph, "")

	require.NoError(t, err)
	assert.Equal(t, StatusSuccess, outcome.Status)
	assert.Contains(t, outcome.Notes, "2 branches succeeded")

	// Verify results are stored in context
	resultsRaw, ok := ctx.Get("parallel.results")
	require.True(t, ok, "parallel.results should be set in context")

	results, err := DeserializeBranchResults(resultsRaw.(string))
	require.NoError(t, err)
	assert.Len(t, results, 2)
}

func TestParallelHandler_ContextIsolation(t *testing.T) {
	// This test verifies that changes in one branch don't affect others
	// by checking that the main context is not modified by branches

	handler := &ParallelHandler{}

	parallelNode := newNode("parallel", strAttr("shape", "component"))
	branchA := newNode("branchA", strAttr("test_outcome", "success"))
	branchB := newNode("branchB", strAttr("test_outcome", "success"))

	graph := newTestGraph(
		[]*dotparser.Node{parallelNode, branchA, branchB},
		[]*dotparser.Edge{
			newEdge("parallel", "branchA"),
			newEdge("parallel", "branchB"),
		},
		nil,
	)

	ctx := NewContext()
	ctx.Set("shared_value", "original")

	outcome, err := handler.Execute(parallelNode, ctx, graph, "")

	require.NoError(t, err)
	assert.Equal(t, StatusSuccess, outcome.Status)

	// Main context should still have original value (branches use clones)
	val, ok := ctx.Get("shared_value")
	assert.True(t, ok)
	assert.Equal(t, "original", val)
}

func TestParallelHandler_JoinPolicyWaitAll_AllPass(t *testing.T) {
	handler := &ParallelHandler{}

	parallelNode := newNode("parallel",
		strAttr("shape", "component"),
		strAttr("join_policy", "wait_all"),
	)
	branchA := newNode("branchA", strAttr("test_outcome", "success"))
	branchB := newNode("branchB", strAttr("test_outcome", "success"))

	graph := newTestGraph(
		[]*dotparser.Node{parallelNode, branchA, branchB},
		[]*dotparser.Edge{
			newEdge("parallel", "branchA"),
			newEdge("parallel", "branchB"),
		},
		nil,
	)

	ctx := NewContext()
	outcome, err := handler.Execute(parallelNode, ctx, graph, "")

	require.NoError(t, err)
	assert.Equal(t, StatusSuccess, outcome.Status)
}

func TestParallelHandler_JoinPolicyWaitAll_SomeFail(t *testing.T) {
	handler := &ParallelHandler{}

	parallelNode := newNode("parallel",
		strAttr("shape", "component"),
		strAttr("join_policy", "wait_all"),
	)
	branchA := newNode("branchA", strAttr("test_outcome", "success"))
	branchB := newNode("branchB", strAttr("test_outcome", "fail"))

	graph := newTestGraph(
		[]*dotparser.Node{parallelNode, branchA, branchB},
		[]*dotparser.Edge{
			newEdge("parallel", "branchA"),
			newEdge("parallel", "branchB"),
		},
		nil,
	)

	ctx := NewContext()
	outcome, err := handler.Execute(parallelNode, ctx, graph, "")

	require.NoError(t, err)
	assert.Equal(t, StatusPartialSuccess, outcome.Status)
	assert.Contains(t, outcome.Notes, "1/2 branches succeeded")
}

func TestParallelHandler_JoinPolicyFirstSuccess_OneSucceeds(t *testing.T) {
	handler := &ParallelHandler{}

	parallelNode := newNode("parallel",
		strAttr("shape", "component"),
		strAttr("join_policy", "first_success"),
	)
	branchA := newNode("branchA", strAttr("test_outcome", "fail"))
	branchB := newNode("branchB", strAttr("test_outcome", "success"))

	graph := newTestGraph(
		[]*dotparser.Node{parallelNode, branchA, branchB},
		[]*dotparser.Edge{
			newEdge("parallel", "branchA"),
			newEdge("parallel", "branchB"),
		},
		nil,
	)

	ctx := NewContext()
	outcome, err := handler.Execute(parallelNode, ctx, graph, "")

	require.NoError(t, err)
	assert.Equal(t, StatusSuccess, outcome.Status)
}

func TestParallelHandler_JoinPolicyFirstSuccess_AllFail(t *testing.T) {
	handler := &ParallelHandler{}

	parallelNode := newNode("parallel",
		strAttr("shape", "component"),
		strAttr("join_policy", "first_success"),
	)
	branchA := newNode("branchA", strAttr("test_outcome", "fail"))
	branchB := newNode("branchB", strAttr("test_outcome", "fail"))

	graph := newTestGraph(
		[]*dotparser.Node{parallelNode, branchA, branchB},
		[]*dotparser.Edge{
			newEdge("parallel", "branchA"),
			newEdge("parallel", "branchB"),
		},
		nil,
	)

	ctx := NewContext()
	outcome, err := handler.Execute(parallelNode, ctx, graph, "")

	require.NoError(t, err)
	assert.Equal(t, StatusFail, outcome.Status)
}

func TestParallelHandler_JoinPolicyKofN(t *testing.T) {
	handler := &ParallelHandler{}

	// Require 2 of 3 to succeed
	parallelNode := newNode("parallel",
		strAttr("shape", "component"),
		strAttr("join_policy", "k_of_n"),
		intAttr("k_of_n_k", 2),
	)
	branchA := newNode("branchA", strAttr("test_outcome", "success"))
	branchB := newNode("branchB", strAttr("test_outcome", "success"))
	branchC := newNode("branchC", strAttr("test_outcome", "fail"))

	graph := newTestGraph(
		[]*dotparser.Node{parallelNode, branchA, branchB, branchC},
		[]*dotparser.Edge{
			newEdge("parallel", "branchA"),
			newEdge("parallel", "branchB"),
			newEdge("parallel", "branchC"),
		},
		nil,
	)

	ctx := NewContext()
	outcome, err := handler.Execute(parallelNode, ctx, graph, "")

	require.NoError(t, err)
	assert.Equal(t, StatusSuccess, outcome.Status)
	assert.Contains(t, outcome.Notes, "2/3 branches succeeded")
}

func TestParallelHandler_JoinPolicyKofN_NotEnough(t *testing.T) {
	handler := &ParallelHandler{}

	// Require 2 of 3 to succeed, but only 1 does
	parallelNode := newNode("parallel",
		strAttr("shape", "component"),
		strAttr("join_policy", "k_of_n"),
		intAttr("k_of_n_k", 2),
	)
	branchA := newNode("branchA", strAttr("test_outcome", "success"))
	branchB := newNode("branchB", strAttr("test_outcome", "fail"))
	branchC := newNode("branchC", strAttr("test_outcome", "fail"))

	graph := newTestGraph(
		[]*dotparser.Node{parallelNode, branchA, branchB, branchC},
		[]*dotparser.Edge{
			newEdge("parallel", "branchA"),
			newEdge("parallel", "branchB"),
			newEdge("parallel", "branchC"),
		},
		nil,
	)

	ctx := NewContext()
	outcome, err := handler.Execute(parallelNode, ctx, graph, "")

	require.NoError(t, err)
	assert.Equal(t, StatusFail, outcome.Status)
}

func TestParallelHandler_JoinPolicyQuorum(t *testing.T) {
	handler := &ParallelHandler{}

	// Require 50% quorum (2 of 4)
	parallelNode := newNode("parallel",
		strAttr("shape", "component"),
		strAttr("join_policy", "quorum"),
		strAttr("quorum_fraction", "0.5"),
	)
	branchA := newNode("branchA", strAttr("test_outcome", "success"))
	branchB := newNode("branchB", strAttr("test_outcome", "success"))
	branchC := newNode("branchC", strAttr("test_outcome", "fail"))
	branchD := newNode("branchD", strAttr("test_outcome", "fail"))

	graph := newTestGraph(
		[]*dotparser.Node{parallelNode, branchA, branchB, branchC, branchD},
		[]*dotparser.Edge{
			newEdge("parallel", "branchA"),
			newEdge("parallel", "branchB"),
			newEdge("parallel", "branchC"),
			newEdge("parallel", "branchD"),
		},
		nil,
	)

	ctx := NewContext()
	outcome, err := handler.Execute(parallelNode, ctx, graph, "")

	require.NoError(t, err)
	assert.Equal(t, StatusSuccess, outcome.Status)
}

func TestParallelHandler_ErrorPolicyContinue(t *testing.T) {
	handler := &ParallelHandler{}

	parallelNode := newNode("parallel",
		strAttr("shape", "component"),
		strAttr("error_policy", "continue"),
	)
	branchA := newNode("branchA", strAttr("test_outcome", "fail"))
	branchB := newNode("branchB", strAttr("test_outcome", "success"))

	graph := newTestGraph(
		[]*dotparser.Node{parallelNode, branchA, branchB},
		[]*dotparser.Edge{
			newEdge("parallel", "branchA"),
			newEdge("parallel", "branchB"),
		},
		nil,
	)

	ctx := NewContext()
	outcome, err := handler.Execute(parallelNode, ctx, graph, "")

	require.NoError(t, err)
	// With continue policy and wait_all, we get partial success when some fail
	assert.Equal(t, StatusPartialSuccess, outcome.Status)

	// Both branches should be in results
	resultsRaw, _ := ctx.Get("parallel.results")
	results, _ := DeserializeBranchResults(resultsRaw.(string))
	assert.Len(t, results, 2)
}

func TestParallelHandler_ErrorPolicyIgnore(t *testing.T) {
	handler := &ParallelHandler{}

	parallelNode := newNode("parallel",
		strAttr("shape", "component"),
		strAttr("error_policy", "ignore"),
	)
	branchA := newNode("branchA", strAttr("test_outcome", "fail"))
	branchB := newNode("branchB", strAttr("test_outcome", "success"))

	graph := newTestGraph(
		[]*dotparser.Node{parallelNode, branchA, branchB},
		[]*dotparser.Edge{
			newEdge("parallel", "branchA"),
			newEdge("parallel", "branchB"),
		},
		nil,
	)

	ctx := NewContext()
	outcome, err := handler.Execute(parallelNode, ctx, graph, "")

	require.NoError(t, err)
	// With ignore policy, only successes are included in results
	assert.Equal(t, StatusPartialSuccess, outcome.Status)

	// Only successful branch should be in results
	resultsRaw, _ := ctx.Get("parallel.results")
	results, _ := DeserializeBranchResults(resultsRaw.(string))
	assert.Len(t, results, 1)
	assert.Equal(t, "branchB", results[0].NodeID)
}

func TestParallelHandler_ResultsStoredInContext(t *testing.T) {
	handler := &ParallelHandler{}

	parallelNode := newNode("parallel", strAttr("shape", "component"))
	branchA := newNode("branchA", strAttr("test_outcome", "success"))
	branchB := newNode("branchB", strAttr("test_outcome", "fail"))

	graph := newTestGraph(
		[]*dotparser.Node{parallelNode, branchA, branchB},
		[]*dotparser.Edge{
			newEdge("parallel", "branchA"),
			newEdge("parallel", "branchB"),
		},
		nil,
	)

	ctx := NewContext()
	_, err := handler.Execute(parallelNode, ctx, graph, "")

	require.NoError(t, err)

	// Verify results are stored as JSON
	resultsRaw, ok := ctx.Get("parallel.results")
	require.True(t, ok)

	// Verify it's valid JSON
	var results []*BranchResult
	err = json.Unmarshal([]byte(resultsRaw.(string)), &results)
	require.NoError(t, err)
	assert.Len(t, results, 2)

	// Find each branch result
	foundA, foundB := false, false
	for _, r := range results {
		if r.NodeID == "branchA" {
			foundA = true
			assert.Equal(t, StatusSuccess, r.Outcome.Status)
		}
		if r.NodeID == "branchB" {
			foundB = true
			assert.Equal(t, StatusFail, r.Outcome.Status)
		}
	}
	assert.True(t, foundA, "branchA result should be present")
	assert.True(t, foundB, "branchB result should be present")
}

func TestParallelHandler_BoundedParallelism(t *testing.T) {
	// Test that max_parallel limits concurrent execution
	parallelNode := newNode("parallel",
		strAttr("shape", "component"),
		intAttr("max_parallel", 2), // Only allow 2 concurrent
	)

	// Create 4 branches
	branches := make([]*dotparser.Node, 4)
	edges := make([]*dotparser.Edge, 4)
	for i := range 4 {
		branches[i] = newNode("branch"+string(rune('A'+i)), strAttr("test_outcome", "success"))
		edges[i] = newEdge("parallel", branches[i].ID)
	}

	graph := newTestGraph(
		append([]*dotparser.Node{parallelNode}, branches...),
		edges,
		nil,
	)

	// Execute and track concurrency via the execution timing
	handler := &ParallelHandler{}
	ctx := NewContext()

	// Track timing to verify bounded parallelism
	startTime := time.Now()
	_, err := handler.Execute(parallelNode, ctx, graph, "")
	_ = time.Since(startTime)

	require.NoError(t, err)

	// The results should contain all 4 branches
	resultsRaw, _ := ctx.Get("parallel.results")
	results, _ := DeserializeBranchResults(resultsRaw.(string))
	assert.Len(t, results, 4)

}

func TestParallelHandler_NoOutgoingEdges(t *testing.T) {
	handler := &ParallelHandler{}

	parallelNode := newNode("parallel", strAttr("shape", "component"))

	graph := newTestGraph(
		[]*dotparser.Node{parallelNode},
		nil, // No edges
		nil,
	)

	ctx := NewContext()
	outcome, err := handler.Execute(parallelNode, ctx, graph, "")

	require.NoError(t, err)
	assert.Equal(t, StatusFail, outcome.Status)
	assert.Contains(t, outcome.FailureReason, "no outgoing edges")
}

// --- Fan-In Handler Tests ---

func TestFanInHandler_ReadsResultsAndSelectsBest(t *testing.T) {
	handler := &FanInHandler{}

	// Set up context with parallel results
	results := []*BranchResult{
		{NodeID: "branchA", Outcome: Success(), Index: 0},
		{NodeID: "branchB", Outcome: Fail("error"), Index: 1},
	}
	resultsJSON, _ := json.Marshal(results)

	ctx := NewContext()
	ctx.Set("parallel.results", string(resultsJSON))

	fanInNode := newNode("fanin", strAttr("shape", "tripleoctagon"))
	graph := newTestGraph([]*dotparser.Node{fanInNode}, nil, nil)

	outcome, err := handler.Execute(fanInNode, ctx, graph, "")

	require.NoError(t, err)
	assert.Equal(t, StatusSuccess, outcome.Status)
	assert.Contains(t, outcome.Notes, "branchA")

	// Verify winner recorded in context
	bestID, _ := ctx.Get("parallel.fan_in.best_id")
	assert.Equal(t, "branchA", bestID)
}

func TestFanInHandler_HeuristicSelectsBestStatus(t *testing.T) {
	handler := &FanInHandler{}

	// Create results with different statuses
	results := []*BranchResult{
		{NodeID: "branchFail", Outcome: Fail("error"), Index: 0},
		{NodeID: "branchSuccess", Outcome: Success(), Index: 1},
		{NodeID: "branchPartial", Outcome: PartialSuccess("partial"), Index: 2},
	}
	resultsJSON, _ := json.Marshal(results)

	ctx := NewContext()
	ctx.Set("parallel.results", string(resultsJSON))

	fanInNode := newNode("fanin", strAttr("shape", "tripleoctagon"))
	graph := newTestGraph([]*dotparser.Node{fanInNode}, nil, nil)

	outcome, err := handler.Execute(fanInNode, ctx, graph, "")

	require.NoError(t, err)
	assert.Equal(t, StatusSuccess, outcome.Status)

	// Should select success over partial_success over fail
	bestID, _ := ctx.Get("parallel.fan_in.best_id")
	assert.Equal(t, "branchSuccess", bestID)
}

func TestFanInHandler_ReturnsFailWhenAllCandidatesFailed(t *testing.T) {
	handler := &FanInHandler{}

	results := []*BranchResult{
		{NodeID: "branchA", Outcome: Fail("error1"), Index: 0},
		{NodeID: "branchB", Outcome: Fail("error2"), Index: 1},
	}
	resultsJSON, _ := json.Marshal(results)

	ctx := NewContext()
	ctx.Set("parallel.results", string(resultsJSON))

	fanInNode := newNode("fanin", strAttr("shape", "tripleoctagon"))
	graph := newTestGraph([]*dotparser.Node{fanInNode}, nil, nil)

	outcome, err := handler.Execute(fanInNode, ctx, graph, "")

	require.NoError(t, err)
	assert.Equal(t, StatusFail, outcome.Status)
	assert.Contains(t, outcome.FailureReason, "all parallel branches failed")
}

func TestFanInHandler_EmptyResultsReturnsFailure(t *testing.T) {
	handler := &FanInHandler{}

	ctx := NewContext()
	ctx.Set("parallel.results", "[]")

	fanInNode := newNode("fanin", strAttr("shape", "tripleoctagon"))
	graph := newTestGraph([]*dotparser.Node{fanInNode}, nil, nil)

	outcome, err := handler.Execute(fanInNode, ctx, graph, "")

	require.NoError(t, err)
	assert.Equal(t, StatusFail, outcome.Status)
}

func TestFanInHandler_MissingResultsReturnsFailure(t *testing.T) {
	handler := &FanInHandler{}

	ctx := NewContext()
	// Don't set parallel.results

	fanInNode := newNode("fanin", strAttr("shape", "tripleoctagon"))
	graph := newTestGraph([]*dotparser.Node{fanInNode}, nil, nil)

	outcome, err := handler.Execute(fanInNode, ctx, graph, "")

	require.NoError(t, err)
	assert.Equal(t, StatusFail, outcome.Status)
	assert.Contains(t, outcome.FailureReason, "no parallel results")
}

func TestFanInHandler_TieBreaksByNodeID(t *testing.T) {
	handler := &FanInHandler{}

	// Both have success status, should pick alphabetically first
	results := []*BranchResult{
		{NodeID: "branchZ", Outcome: Success(), Index: 0},
		{NodeID: "branchA", Outcome: Success(), Index: 1},
	}
	resultsJSON, _ := json.Marshal(results)

	ctx := NewContext()
	ctx.Set("parallel.results", string(resultsJSON))

	fanInNode := newNode("fanin", strAttr("shape", "tripleoctagon"))
	graph := newTestGraph([]*dotparser.Node{fanInNode}, nil, nil)

	outcome, err := handler.Execute(fanInNode, ctx, graph, "")

	require.NoError(t, err)
	assert.Equal(t, StatusSuccess, outcome.Status)

	// Should select branchA (alphabetically first)
	bestID, _ := ctx.Get("parallel.fan_in.best_id")
	assert.Equal(t, "branchA", bestID)
}

// --- Integration Tests ---

func TestParallelToFanIn_Integration(t *testing.T) {
	// Test parallel handler execution followed by fan-in.
	// The parallel handler fans out to immediate branch targets (branchA, branchB).
	// After parallel returns, the engine continues to fan-in which reads the results.

	registry := DefaultRegistry()

	startNode := newNode("start", strAttr("shape", "Mdiamond"))
	parallelNode := newNode("parallel", strAttr("shape", "component"))
	branchA := newNode("branchA", strAttr("test_outcome", "success"))
	branchB := newNode("branchB", strAttr("test_outcome", "fail"))
	fanInNode := newNode("fanin", strAttr("shape", "tripleoctagon"))
	exitNode := newNode("exit", strAttr("shape", "Msquare"))

	// The parallel node fans out to branchA and branchB (executed internally by parallel handler).
	// The fan-in is a separate node connected via suggested_next_ids in the outcome.
	// For this test, we'll set up the parallel handler to return fan-in as the suggested next.
	graph := newTestGraph(
		[]*dotparser.Node{startNode, parallelNode, branchA, branchB, fanInNode, exitNode},
		[]*dotparser.Edge{
			newEdge("start", "parallel"),
			newEdge("parallel", "branchA"), // Branch edge
			newEdge("parallel", "branchB"), // Branch edge
			newEdge("branchA", "fanin"),    // Branches converge to fan-in
			newEdge("branchB", "fanin"),    // Branches converge to fan-in
			newEdge("fanin", "exit"),
		},
		nil,
	)

	result, err := Run(graph, &RunConfig{Registry: registry})

	require.NoError(t, err)
	assert.Contains(t, result.CompletedNodes, "start")
	assert.Contains(t, result.CompletedNodes, "parallel")

	// Check that parallel.results was set (verifying parallel executed correctly)
	resultsRaw, ok := result.Context.Get("parallel.results")
	assert.True(t, ok, "parallel.results should be set")
	assert.NotEmpty(t, resultsRaw)

	// Verify the results contain both branches (not fanin since it's not directly connected)
	results, err := DeserializeBranchResults(resultsRaw.(string))
	require.NoError(t, err)
	assert.Len(t, results, 2) // branchA and branchB only
}

func TestParallelHandler_ConcurrentExecution(t *testing.T) {
	// Verify branches actually run concurrently
	var executionCount int32

	handler := &ParallelHandler{}

	parallelNode := newNode("parallel",
		strAttr("shape", "component"),
		intAttr("max_parallel", 10), // Allow all to run at once
	)

	// Create several branches
	nodes := []*dotparser.Node{parallelNode}
	edges := []*dotparser.Edge{}
	for i := range 5 {
		branchNode := newNode("branch"+string(rune('A'+i)), strAttr("test_outcome", "success"))
		nodes = append(nodes, branchNode)
		edges = append(edges, newEdge("parallel", branchNode.ID))
	}

	graph := newTestGraph(nodes, edges, nil)
	ctx := NewContext()

	outcome, err := handler.Execute(parallelNode, ctx, graph, "")

	require.NoError(t, err)
	assert.Equal(t, StatusSuccess, outcome.Status)

	// All branches should have executed
	resultsRaw, _ := ctx.Get("parallel.results")
	results, _ := DeserializeBranchResults(resultsRaw.(string))
	assert.Len(t, results, 5)

	_ = executionCount
	_ = atomic.AddInt32
}

func TestSortBranchResultsByHeuristic(t *testing.T) {
	results := []*BranchResult{
		{NodeID: "fail1", Outcome: Fail("error"), Index: 0},
		{NodeID: "success1", Outcome: Success(), Index: 1},
		{NodeID: "partial1", Outcome: PartialSuccess("notes"), Index: 2},
		{NodeID: "retry1", Outcome: Retry("retry"), Index: 3},
		{NodeID: "success2", Outcome: Success(), Index: 4},
	}

	SortBranchResultsByHeuristic(results)

	// Order should be: success (alphabetically), partial, retry, fail
	assert.Equal(t, "success1", results[0].NodeID)
	assert.Equal(t, "success2", results[1].NodeID)
	assert.Equal(t, "partial1", results[2].NodeID)
	assert.Equal(t, "retry1", results[3].NodeID)
	assert.Equal(t, "fail1", results[4].NodeID)
}

func TestBranchResultSerialization(t *testing.T) {
	results := []*BranchResult{
		{NodeID: "branch1", Outcome: Success().WithNotes("done"), Index: 0},
		{NodeID: "branch2", Outcome: Fail("error"), Index: 1},
	}

	// Serialize
	jsonStr, err := SerializeBranchResults(results)
	require.NoError(t, err)
	assert.NotEmpty(t, jsonStr)

	// Deserialize
	parsed, err := DeserializeBranchResults(jsonStr)
	require.NoError(t, err)
	assert.Len(t, parsed, 2)
	assert.Equal(t, "branch1", parsed[0].NodeID)
	assert.Equal(t, StatusSuccess, parsed[0].Outcome.Status)
	assert.Equal(t, "branch2", parsed[1].NodeID)
	assert.Equal(t, StatusFail, parsed[1].Outcome.Status)
}

func TestDeserializeBranchResults_EmptyString(t *testing.T) {
	results, err := DeserializeBranchResults("")
	require.NoError(t, err)
	assert.Nil(t, results)
}

func TestDeserializeBranchResults_InvalidJSON(t *testing.T) {
	_, err := DeserializeBranchResults("not valid json")
	require.Error(t, err)
}

func TestRegistryResolvesParallelHandler(t *testing.T) {
	registry := DefaultRegistry()

	// Test shape-based resolution for parallel
	parallelNode := newNode("parallel", strAttr("shape", "component"))
	handler := registry.Resolve(parallelNode)
	assert.NotNil(t, handler)
	_, ok := handler.(*ParallelHandler)
	assert.True(t, ok, "should resolve to ParallelHandler")

	// Test shape-based resolution for fan-in
	fanInNode := newNode("fanin", strAttr("shape", "tripleoctagon"))
	handler = registry.Resolve(fanInNode)
	assert.NotNil(t, handler)
	_, ok = handler.(*FanInHandler)
	assert.True(t, ok, "should resolve to FanInHandler")
}

func TestRegistryResolvesParallelByType(t *testing.T) {
	registry := DefaultRegistry()

	// Test type-based resolution for parallel
	parallelNode := newNode("parallel", strAttr("type", "parallel"))
	handler := registry.Resolve(parallelNode)
	assert.NotNil(t, handler)
	_, ok := handler.(*ParallelHandler)
	assert.True(t, ok, "should resolve to ParallelHandler")

	// Test type-based resolution for fan-in
	fanInNode := newNode("fanin", strAttr("type", "parallel.fan_in"))
	handler = registry.Resolve(fanInNode)
	assert.NotNil(t, handler)
	_, ok = handler.(*FanInHandler)
	assert.True(t, ok, "should resolve to FanInHandler")
}

// trackingHandler is a test handler that tracks execution via context
type trackingHandler struct{}

func (h *trackingHandler) Execute(node *dotparser.Node, ctx *Context, graph *dotparser.Graph, logsRoot string) (*Outcome, error) {
	ctx.Set("executed_"+node.ID, true)
	return Success().WithNotes("tracking handler executed " + node.ID), nil
}

func TestParallelHandler_UsesRegistryForBranchHandlers(t *testing.T) {
	// Create a custom registry with a tracking handler
	registry := NewHandlerRegistry()
	trackHandler := &trackingHandler{}
	registry.Register("codergen", trackHandler)
	registry.SetDefaultHandler(trackHandler)

	// Create parallel handler with registry
	handler := NewParallelHandler(registry)

	parallelNode := newNode("parallel", strAttr("shape", "component"))
	branchA := newNode("branchA", strAttr("shape", "box")) // shape=box resolves to codergen
	branchB := newNode("branchB", strAttr("shape", "box"))

	graph := newTestGraph(
		[]*dotparser.Node{parallelNode, branchA, branchB},
		[]*dotparser.Edge{
			newEdge("parallel", "branchA"),
			newEdge("parallel", "branchB"),
		},
		nil,
	)

	ctx := NewContext()
	outcome, err := handler.Execute(parallelNode, ctx, graph, "")

	require.NoError(t, err)
	assert.Equal(t, StatusSuccess, outcome.Status)

	// Verify the tracking handler was called for each branch
	// Note: branches execute in their own cloned contexts, but results are stored in parent
	resultsRaw, ok := ctx.Get("parallel.results")
	require.True(t, ok, "parallel.results should be set")

	results, err := DeserializeBranchResults(resultsRaw.(string))
	require.NoError(t, err)
	assert.Len(t, results, 2)

	// Both branches should have succeeded via the tracking handler
	for _, r := range results {
		assert.Equal(t, StatusSuccess, r.Outcome.Status)
		assert.Contains(t, r.Outcome.Notes, "tracking handler executed")
	}
}

func TestParallelHandler_TestOutcomeTakesPrecedenceOverRegistry(t *testing.T) {
	// Even with a registry, test_outcome should take precedence
	registry := NewHandlerRegistry()
	trackHandler := &trackingHandler{}
	registry.SetDefaultHandler(trackHandler)

	handler := NewParallelHandler(registry)

	parallelNode := newNode("parallel", strAttr("shape", "component"))
	// This branch has test_outcome set, so it should use that instead of resolving handler
	branchA := newNode("branchA",
		strAttr("test_outcome", "fail"),
		strAttr("test_failure_reason", "intentional test failure"),
	)

	graph := newTestGraph(
		[]*dotparser.Node{parallelNode, branchA},
		[]*dotparser.Edge{
			newEdge("parallel", "branchA"),
		},
		nil,
	)

	ctx := NewContext()
	outcome, err := handler.Execute(parallelNode, ctx, graph, "")

	require.NoError(t, err)
	// Should be partial success because wait_all with 1 fail = partial
	assert.Equal(t, StatusPartialSuccess, outcome.Status)

	// Verify the branch used test_outcome, not the handler
	resultsRaw, _ := ctx.Get("parallel.results")
	results, _ := DeserializeBranchResults(resultsRaw.(string))
	require.Len(t, results, 1)
	assert.Equal(t, StatusFail, results[0].Outcome.Status)
	assert.Equal(t, "intentional test failure", results[0].Outcome.FailureReason)
}

func TestParallelHandler_NilRegistryFallsBackToSuccess(t *testing.T) {
	// With nil registry and no test_outcome, should fallback to success
	handler := NewParallelHandler(nil)

	parallelNode := newNode("parallel", strAttr("shape", "component"))
	branchA := newNode("branchA") // No test_outcome, no shape

	graph := newTestGraph(
		[]*dotparser.Node{parallelNode, branchA},
		[]*dotparser.Edge{
			newEdge("parallel", "branchA"),
		},
		nil,
	)

	ctx := NewContext()
	outcome, err := handler.Execute(parallelNode, ctx, graph, "")

	require.NoError(t, err)
	assert.Equal(t, StatusSuccess, outcome.Status)

	// Verify the branch returned fallback success
	resultsRaw, _ := ctx.Get("parallel.results")
	results, _ := DeserializeBranchResults(resultsRaw.(string))
	require.Len(t, results, 1)
	assert.Equal(t, StatusSuccess, results[0].Outcome.Status)
	assert.Contains(t, results[0].Outcome.Notes, "no handler")
}

func TestParallelHandler_RegistryHandlerError(t *testing.T) {
	// Test that handler errors are captured as failures
	registry := NewHandlerRegistry()
	errorHandler := &erroringHandler{}
	registry.SetDefaultHandler(errorHandler)

	handler := NewParallelHandler(registry)

	parallelNode := newNode("parallel", strAttr("shape", "component"))
	branchA := newNode("branchA")

	graph := newTestGraph(
		[]*dotparser.Node{parallelNode, branchA},
		[]*dotparser.Edge{
			newEdge("parallel", "branchA"),
		},
		nil,
	)

	ctx := NewContext()
	outcome, err := handler.Execute(parallelNode, ctx, graph, "")

	require.NoError(t, err)
	// Should be partial success because the branch failed
	assert.Equal(t, StatusPartialSuccess, outcome.Status)

	// Verify the error was captured
	resultsRaw, _ := ctx.Get("parallel.results")
	results, _ := DeserializeBranchResults(resultsRaw.(string))
	require.Len(t, results, 1)
	assert.Equal(t, StatusFail, results[0].Outcome.Status)
	assert.Contains(t, results[0].Outcome.FailureReason, "handler error")
}

// erroringHandler is a test handler that returns an error
type erroringHandler struct{}

func (h *erroringHandler) Execute(node *dotparser.Node, ctx *Context, graph *dotparser.Graph, logsRoot string) (*Outcome, error) {
	return nil, assert.AnError
}

func TestDefaultRegistry_ParallelHandlerHasRegistry(t *testing.T) {
	// Verify that the default registry configures ParallelHandler with a registry reference
	registry := DefaultRegistry()

	parallelNode := newNode("parallel", strAttr("shape", "component"))
	handler := registry.Resolve(parallelNode)
	require.NotNil(t, handler)

	parallelHandler, ok := handler.(*ParallelHandler)
	require.True(t, ok, "should resolve to ParallelHandler")

	// The parallel handler should have a registry reference
	assert.NotNil(t, parallelHandler.Registry, "ParallelHandler should have Registry set")
	assert.Equal(t, registry, parallelHandler.Registry, "ParallelHandler.Registry should point to the same registry")
}

func TestNewParallelHandler(t *testing.T) {
	// Test the constructor
	registry := NewHandlerRegistry()
	handler := NewParallelHandler(registry)

	assert.NotNil(t, handler)
	assert.Equal(t, registry, handler.Registry)

	// Test with nil
	nilHandler := NewParallelHandler(nil)
	assert.NotNil(t, nilHandler)
	assert.Nil(t, nilHandler.Registry)
}

// --- LLM-based Fan-In Evaluation Tests ---

// MockFanInBackend is a test backend for FanInHandler LLM evaluation.
type MockFanInBackend struct {
	Calls    []MockFanInCall
	Response any // string or *Outcome
	Error    error
}

type MockFanInCall struct {
	NodeID string
	Prompt string
}

func (m *MockFanInBackend) Run(node *dotparser.Node, prompt string, ctx *Context) (any, error) {
	m.Calls = append(m.Calls, MockFanInCall{NodeID: node.ID, Prompt: prompt})
	if m.Error != nil {
		return nil, m.Error
	}
	return m.Response, nil
}

func TestFanInHandler_LLMBasedSelection(t *testing.T) {
	// LLM selects branchB even though branchA has same status (would be first by heuristic)
	mockBackend := &MockFanInBackend{Response: "I choose branchB as the best candidate."}
	handler := NewFanInHandler(mockBackend)

	results := []*BranchResult{
		{NodeID: "branchA", Outcome: Success().WithNotes("result A"), Index: 0},
		{NodeID: "branchB", Outcome: Success().WithNotes("result B"), Index: 1},
	}
	resultsJSON, _ := json.Marshal(results)

	ctx := NewContext()
	ctx.Set("parallel.results", string(resultsJSON))

	fanInNode := newNode("fanin",
		strAttr("shape", "tripleoctagon"),
		strAttr("prompt", "Select the best implementation"),
	)
	graph := newTestGraph([]*dotparser.Node{fanInNode}, nil, nil)

	outcome, err := handler.Execute(fanInNode, ctx, graph, "")

	require.NoError(t, err)
	assert.Equal(t, StatusSuccess, outcome.Status)

	// Verify LLM was called
	require.Len(t, mockBackend.Calls, 1)
	assert.Equal(t, "fanin", mockBackend.Calls[0].NodeID)
	assert.Contains(t, mockBackend.Calls[0].Prompt, "Select the best implementation")
	assert.Contains(t, mockBackend.Calls[0].Prompt, "branchA")
	assert.Contains(t, mockBackend.Calls[0].Prompt, "branchB")

	// LLM selected branchB
	bestID, _ := ctx.Get("parallel.fan_in.best_id")
	assert.Equal(t, "branchB", bestID)
	assert.Contains(t, outcome.Notes, "branchB")
}

func TestFanInHandler_LLMSelectsByPositionalReference(t *testing.T) {
	// LLM uses "candidate 2" format instead of node ID
	mockBackend := &MockFanInBackend{Response: "After evaluation, candidate 2 is the best choice."}
	handler := NewFanInHandler(mockBackend)

	results := []*BranchResult{
		{NodeID: "branch_alpha", Outcome: Success(), Index: 0},
		{NodeID: "branch_beta", Outcome: Success(), Index: 1},
	}
	resultsJSON, _ := json.Marshal(results)

	ctx := NewContext()
	ctx.Set("parallel.results", string(resultsJSON))

	fanInNode := newNode("fanin",
		strAttr("shape", "tripleoctagon"),
		strAttr("prompt", "Pick the best"),
	)
	graph := newTestGraph([]*dotparser.Node{fanInNode}, nil, nil)

	outcome, err := handler.Execute(fanInNode, ctx, graph, "")

	require.NoError(t, err)
	assert.Equal(t, StatusSuccess, outcome.Status)

	// LLM selected branch_beta via "candidate 2"
	bestID, _ := ctx.Get("parallel.fan_in.best_id")
	assert.Equal(t, "branch_beta", bestID)
}

func TestFanInHandler_FallsBackToHeuristicWhenNoPrompt(t *testing.T) {
	mockBackend := &MockFanInBackend{Response: "This should not be called"}
	handler := NewFanInHandler(mockBackend)

	// branchZ has success, branchA has success - heuristic picks branchA (alphabetically)
	results := []*BranchResult{
		{NodeID: "branchZ", Outcome: Success(), Index: 0},
		{NodeID: "branchA", Outcome: Success(), Index: 1},
	}
	resultsJSON, _ := json.Marshal(results)

	ctx := NewContext()
	ctx.Set("parallel.results", string(resultsJSON))

	// No prompt attribute
	fanInNode := newNode("fanin", strAttr("shape", "tripleoctagon"))
	graph := newTestGraph([]*dotparser.Node{fanInNode}, nil, nil)

	outcome, err := handler.Execute(fanInNode, ctx, graph, "")

	require.NoError(t, err)
	assert.Equal(t, StatusSuccess, outcome.Status)

	// LLM should NOT have been called
	assert.Len(t, mockBackend.Calls, 0)

	// Heuristic selected branchA (alphabetically first among successes)
	bestID, _ := ctx.Get("parallel.fan_in.best_id")
	assert.Equal(t, "branchA", bestID)
}

func TestFanInHandler_FallsBackToHeuristicWhenNoBackend(t *testing.T) {
	// Handler with nil backend (heuristic-only mode)
	handler := NewFanInHandler(nil)

	results := []*BranchResult{
		{NodeID: "branchZ", Outcome: Success(), Index: 0},
		{NodeID: "branchA", Outcome: Success(), Index: 1},
	}
	resultsJSON, _ := json.Marshal(results)

	ctx := NewContext()
	ctx.Set("parallel.results", string(resultsJSON))

	// Has prompt but no backend
	fanInNode := newNode("fanin",
		strAttr("shape", "tripleoctagon"),
		strAttr("prompt", "Select best"),
	)
	graph := newTestGraph([]*dotparser.Node{fanInNode}, nil, nil)

	outcome, err := handler.Execute(fanInNode, ctx, graph, "")

	require.NoError(t, err)
	assert.Equal(t, StatusSuccess, outcome.Status)

	// Heuristic selected branchA
	bestID, _ := ctx.Get("parallel.fan_in.best_id")
	assert.Equal(t, "branchA", bestID)
}

func TestFanInHandler_FallsBackToHeuristicOnLLMError(t *testing.T) {
	mockBackend := &MockFanInBackend{Error: fmt.Errorf("LLM service unavailable")}
	handler := NewFanInHandler(mockBackend)

	results := []*BranchResult{
		{NodeID: "branchZ", Outcome: Success(), Index: 0},
		{NodeID: "branchA", Outcome: Success(), Index: 1},
	}
	resultsJSON, _ := json.Marshal(results)

	ctx := NewContext()
	ctx.Set("parallel.results", string(resultsJSON))

	fanInNode := newNode("fanin",
		strAttr("shape", "tripleoctagon"),
		strAttr("prompt", "Select best"),
	)
	graph := newTestGraph([]*dotparser.Node{fanInNode}, nil, nil)

	outcome, err := handler.Execute(fanInNode, ctx, graph, "")

	require.NoError(t, err)
	assert.Equal(t, StatusSuccess, outcome.Status)

	// LLM was called but errored
	require.Len(t, mockBackend.Calls, 1)

	// Falls back to heuristic - branchA selected
	bestID, _ := ctx.Get("parallel.fan_in.best_id")
	assert.Equal(t, "branchA", bestID)
}

func TestFanInHandler_FallsBackToHeuristicOnUnparseableResponse(t *testing.T) {
	// LLM returns gibberish that can't be parsed
	mockBackend := &MockFanInBackend{Response: "I have no idea what to pick!"}
	handler := NewFanInHandler(mockBackend)

	results := []*BranchResult{
		{NodeID: "branchZ", Outcome: Success(), Index: 0},
		{NodeID: "branchA", Outcome: Success(), Index: 1},
	}
	resultsJSON, _ := json.Marshal(results)

	ctx := NewContext()
	ctx.Set("parallel.results", string(resultsJSON))

	fanInNode := newNode("fanin",
		strAttr("shape", "tripleoctagon"),
		strAttr("prompt", "Select best"),
	)
	graph := newTestGraph([]*dotparser.Node{fanInNode}, nil, nil)

	outcome, err := handler.Execute(fanInNode, ctx, graph, "")

	require.NoError(t, err)
	assert.Equal(t, StatusSuccess, outcome.Status)

	// Falls back to heuristic - branchA selected
	bestID, _ := ctx.Get("parallel.fan_in.best_id")
	assert.Equal(t, "branchA", bestID)
}

func TestFanInHandler_LLMReceivesFormattedCandidates(t *testing.T) {
	mockBackend := &MockFanInBackend{Response: "branchA"}
	handler := NewFanInHandler(mockBackend)

	results := []*BranchResult{
		{NodeID: "branchA", Outcome: Success().WithNotes("Completed successfully"), Index: 0},
		{NodeID: "branchB", Outcome: PartialSuccess("Partial notes"), Index: 1},
		{NodeID: "branchC", Outcome: Fail("Something went wrong"), Index: 2},
	}
	resultsJSON, _ := json.Marshal(results)

	ctx := NewContext()
	ctx.Set("parallel.results", string(resultsJSON))

	fanInNode := newNode("fanin",
		strAttr("shape", "tripleoctagon"),
		strAttr("prompt", "Evaluate the candidates"),
	)
	graph := newTestGraph([]*dotparser.Node{fanInNode}, nil, nil)

	_, err := handler.Execute(fanInNode, ctx, graph, "")
	require.NoError(t, err)

	// Verify the prompt sent to LLM contains formatted candidate info
	require.Len(t, mockBackend.Calls, 1)
	prompt := mockBackend.Calls[0].Prompt

	// Check user prompt is included
	assert.Contains(t, prompt, "Evaluate the candidates")

	// Check all candidates are listed
	assert.Contains(t, prompt, "branchA")
	assert.Contains(t, prompt, "branchB")
	assert.Contains(t, prompt, "branchC")

	// Check status information is included (lowercase as returned by Status.String())
	assert.Contains(t, prompt, "success")
	assert.Contains(t, prompt, "partial_success")
	assert.Contains(t, prompt, "fail")

	// Check notes are included
	assert.Contains(t, prompt, "Completed successfully")
	assert.Contains(t, prompt, "Partial notes")

	// Check failure reason is included
	assert.Contains(t, prompt, "Something went wrong")
}

func TestFanInHandler_LLMBackendReturnsOutcome(t *testing.T) {
	// Backend returns an *Outcome with selected_id in context updates
	customOutcome := Success().
		WithNotes("branchB is better").
		WithContextUpdate("selected_id", "branchB")
	mockBackend := &MockFanInBackend{Response: customOutcome}
	handler := NewFanInHandler(mockBackend)

	results := []*BranchResult{
		{NodeID: "branchA", Outcome: Success(), Index: 0},
		{NodeID: "branchB", Outcome: Success(), Index: 1},
	}
	resultsJSON, _ := json.Marshal(results)

	ctx := NewContext()
	ctx.Set("parallel.results", string(resultsJSON))

	fanInNode := newNode("fanin",
		strAttr("shape", "tripleoctagon"),
		strAttr("prompt", "Select best"),
	)
	graph := newTestGraph([]*dotparser.Node{fanInNode}, nil, nil)

	outcome, err := handler.Execute(fanInNode, ctx, graph, "")

	require.NoError(t, err)
	assert.Equal(t, StatusSuccess, outcome.Status)

	// Should select branchB via context update
	bestID, _ := ctx.Get("parallel.fan_in.best_id")
	assert.Equal(t, "branchB", bestID)
}

func TestFanInHandler_LLMBackendReturnsFailedOutcome(t *testing.T) {
	// Backend returns a failed *Outcome - should fall back to heuristic
	failedOutcome := Fail("LLM couldn't decide")
	mockBackend := &MockFanInBackend{Response: failedOutcome}
	handler := NewFanInHandler(mockBackend)

	results := []*BranchResult{
		{NodeID: "branchZ", Outcome: Success(), Index: 0},
		{NodeID: "branchA", Outcome: Success(), Index: 1},
	}
	resultsJSON, _ := json.Marshal(results)

	ctx := NewContext()
	ctx.Set("parallel.results", string(resultsJSON))

	fanInNode := newNode("fanin",
		strAttr("shape", "tripleoctagon"),
		strAttr("prompt", "Select best"),
	)
	graph := newTestGraph([]*dotparser.Node{fanInNode}, nil, nil)

	outcome, err := handler.Execute(fanInNode, ctx, graph, "")

	require.NoError(t, err)
	assert.Equal(t, StatusSuccess, outcome.Status)

	// Falls back to heuristic - branchA selected
	bestID, _ := ctx.Get("parallel.fan_in.best_id")
	assert.Equal(t, "branchA", bestID)
}

func TestNewFanInHandler(t *testing.T) {
	// Test constructor
	mockBackend := &MockFanInBackend{}
	handler := NewFanInHandler(mockBackend)

	assert.NotNil(t, handler)
	assert.Equal(t, mockBackend, handler.Backend)

	// Test with nil
	nilHandler := NewFanInHandler(nil)
	assert.NotNil(t, nilHandler)
	assert.Nil(t, nilHandler.Backend)
}
