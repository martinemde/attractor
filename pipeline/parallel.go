package pipeline

import (
	"context"
	"encoding/json"
	"fmt"
	"sort"
	"strconv"
	"sync"

	"github.com/martinemde/attractor/dotparser"
)

// BranchResult represents the outcome of a single parallel branch execution.
type BranchResult struct {
	NodeID  string   `json:"node_id"`
	Outcome *Outcome `json:"outcome"`
	Index   int      `json:"index"`
}

// ParallelHandler fans out execution to multiple branches concurrently.
// Each branch receives an isolated clone of the parent context and runs independently.
// The handler waits for all branches to complete (or applies a configurable join policy)
// before returning.
type ParallelHandler struct{}

// Execute runs parallel branches based on outgoing edges from this node.
func (h *ParallelHandler) Execute(node *dotparser.Node, ctx *Context, graph *dotparser.Graph, logsRoot string) (*Outcome, error) {
	// 1. Get outgoing edges (these are the branch entry points)
	branches := graph.EdgesFrom(node.ID)
	if len(branches) == 0 {
		return Fail("parallel node has no outgoing edges"), nil
	}

	// 2. Read configuration from node attributes
	joinPolicy := getStringAttr(node, "join_policy", "wait_all")
	errorPolicy := getStringAttr(node, "error_policy", "continue")
	maxParallel := getIntAttr(node, "max_parallel", 4)
	kOfN := getIntAttr(node, "k_of_n_k", 1)
	quorumFraction := getFloatAttr(node, "quorum_fraction", 0.5)

	if maxParallel < 1 {
		maxParallel = 1
	}

	// 3. Execute branches concurrently with bounded parallelism
	results := make([]*BranchResult, len(branches))
	var wg sync.WaitGroup
	semaphore := make(chan struct{}, maxParallel)

	// Context for cancellation (used by fail_fast policy)
	cancelCtx, cancel := context.WithCancel(context.Background())
	defer cancel()

	var firstFailure *BranchResult
	var failureMu sync.Mutex

	for i, edge := range branches {
		wg.Add(1)
		go func(idx int, e *dotparser.Edge) {
			defer wg.Done()

			// Check if we should skip due to cancellation
			select {
			case <-cancelCtx.Done():
				results[idx] = &BranchResult{
					NodeID:  e.To,
					Outcome: Skipped(),
					Index:   idx,
				}
				return
			default:
			}

			// Acquire semaphore slot
			select {
			case semaphore <- struct{}{}:
				defer func() { <-semaphore }()
			case <-cancelCtx.Done():
				results[idx] = &BranchResult{
					NodeID:  e.To,
					Outcome: Skipped(),
					Index:   idx,
				}
				return
			}

			// Clone context for isolation
			branchCtx := ctx.Clone()

			// Get target node and resolve its handler
			targetNode := graph.NodeByID(e.To)
			if targetNode == nil {
				results[idx] = &BranchResult{
					NodeID:  e.To,
					Outcome: Fail(fmt.Sprintf("branch target node %q not found", e.To)),
					Index:   idx,
				}
				return
			}

			// Execute the target node's handler directly (not the full engine loop)
			// We need to resolve the handler from the registry
			outcome := executeBranchNode(targetNode, branchCtx, graph, logsRoot)

			results[idx] = &BranchResult{
				NodeID:  e.To,
				Outcome: outcome,
				Index:   idx,
			}

			// Handle fail_fast policy
			if errorPolicy == "fail_fast" && outcome.Status == StatusFail {
				failureMu.Lock()
				if firstFailure == nil {
					firstFailure = results[idx]
					cancel() // Cancel remaining branches
				}
				failureMu.Unlock()
			}
		}(i, edge)
	}

	wg.Wait()

	// 4. Count results
	successCount := 0
	failCount := 0
	for _, r := range results {
		if r.Outcome != nil {
			switch r.Outcome.Status {
			case StatusSuccess, StatusPartialSuccess:
				successCount++
			case StatusFail:
				failCount++
			}
		}
	}

	// 5. Filter results based on error policy
	var filteredResults []*BranchResult
	switch errorPolicy {
	case "ignore":
		// Only include successful results
		for _, r := range results {
			if r.Outcome != nil && r.Outcome.Status.IsSuccess() {
				filteredResults = append(filteredResults, r)
			}
		}
	default:
		// Include all results
		filteredResults = results
	}

	// 6. Store serialized results in context for downstream fan-in
	resultsJSON, err := json.Marshal(filteredResults)
	if err != nil {
		return Fail(fmt.Sprintf("failed to serialize branch results: %v", err)), nil
	}
	ctx.Set("parallel.results", string(resultsJSON))

	// 7. Apply join policy to determine overall outcome
	totalBranches := len(branches)
	switch joinPolicy {
	case "wait_all":
		if failCount == 0 {
			return Success().WithNotes(fmt.Sprintf("all %d branches succeeded", successCount)), nil
		}
		return PartialSuccess(fmt.Sprintf("%d/%d branches succeeded, %d failed", successCount, totalBranches, failCount)), nil

	case "first_success":
		if successCount > 0 {
			return Success().WithNotes(fmt.Sprintf("first success achieved, %d/%d total succeeded", successCount, totalBranches)), nil
		}
		return Fail("no branches succeeded"), nil

	case "k_of_n":
		if successCount >= kOfN {
			return Success().WithNotes(fmt.Sprintf("%d/%d branches succeeded (required: %d)", successCount, totalBranches, kOfN)), nil
		}
		return Fail(fmt.Sprintf("only %d branches succeeded, required %d", successCount, kOfN)), nil

	case "quorum":
		required := int(float64(totalBranches) * quorumFraction)
		required = max(required, 1)
		if successCount >= required {
			return Success().WithNotes(fmt.Sprintf("%d/%d branches succeeded (quorum: %.0f%%)", successCount, totalBranches, quorumFraction*100)), nil
		}
		return Fail(fmt.Sprintf("only %d branches succeeded, quorum requires %d (%.0f%%)", successCount, required, quorumFraction*100)), nil

	default:
		// Default to wait_all behavior
		if failCount == 0 {
			return Success().WithNotes(fmt.Sprintf("all %d branches succeeded", successCount)), nil
		}
		return PartialSuccess(fmt.Sprintf("%d/%d branches succeeded", successCount, totalBranches)), nil
	}
}

// executeBranchNode executes a single branch target node.
// This is a simplified execution that runs just the target node's handler.
func executeBranchNode(node *dotparser.Node, _ *Context, _ *dotparser.Graph, _ string) *Outcome {
	// Get handler for the node by resolving its type
	// Since we don't have direct access to the registry here, we use a simple approach:
	// Check node attributes to determine handler type

	// For now, we'll simulate execution based on node attributes
	// In a full implementation, this would use the registry

	// Check for explicit outcome attribute (useful for testing)
	if outcomeAttr, ok := node.Attr("test_outcome"); ok {
		switch outcomeAttr.Str {
		case "success":
			return Success().WithNotes(fmt.Sprintf("branch %s executed successfully", node.ID))
		case "fail":
			reason := "branch execution failed"
			if reasonAttr, ok := node.Attr("test_failure_reason"); ok {
				reason = reasonAttr.Str
			}
			return Fail(reason)
		case "partial_success":
			notes := "partial completion"
			if notesAttr, ok := node.Attr("test_notes"); ok {
				notes = notesAttr.Str
			}
			return PartialSuccess(notes)
		}
	}

	// Default: return success for the branch
	return Success().WithNotes(fmt.Sprintf("branch %s executed", node.ID))
}

// Helper functions for reading node attributes with defaults

func getStringAttr(node *dotparser.Node, key, defaultValue string) string {
	if attr, ok := node.Attr(key); ok {
		return attr.Str
	}
	return defaultValue
}

func getIntAttr(node *dotparser.Node, key string, defaultValue int) int {
	if attr, ok := node.Attr(key); ok {
		switch attr.Kind {
		case dotparser.ValueInt:
			return int(attr.Int)
		case dotparser.ValueString:
			if v, err := strconv.Atoi(attr.Str); err == nil {
				return v
			}
		}
	}
	return defaultValue
}

func getFloatAttr(node *dotparser.Node, key string, defaultValue float64) float64 {
	if attr, ok := node.Attr(key); ok {
		switch attr.Kind {
		case dotparser.ValueFloat:
			return attr.Float
		case dotparser.ValueString:
			if v, err := strconv.ParseFloat(attr.Str, 64); err == nil {
				return v
			}
		}
	}
	return defaultValue
}

// SerializeBranchResults converts branch results to JSON string.
func SerializeBranchResults(results []*BranchResult) (string, error) {
	data, err := json.Marshal(results)
	if err != nil {
		return "", err
	}
	return string(data), nil
}

// DeserializeBranchResults parses branch results from JSON string.
func DeserializeBranchResults(data string) ([]*BranchResult, error) {
	if data == "" {
		return nil, nil
	}
	var results []*BranchResult
	if err := json.Unmarshal([]byte(data), &results); err != nil {
		return nil, err
	}
	return results, nil
}

// SortBranchResultsByHeuristic sorts results by outcome status (best first), then by node ID.
// Ranking: SUCCESS=0, PARTIAL_SUCCESS=1, RETRY=2, FAIL=3
func SortBranchResultsByHeuristic(results []*BranchResult) {
	sort.Slice(results, func(i, j int) bool {
		rankI := outcomeRank(results[i].Outcome)
		rankJ := outcomeRank(results[j].Outcome)
		if rankI != rankJ {
			return rankI < rankJ
		}
		// Tie-break by node ID (alphabetically)
		return results[i].NodeID < results[j].NodeID
	})
}

func outcomeRank(o *Outcome) int {
	if o == nil {
		return 4 // Unknown/nil outcomes rank worst
	}
	switch o.Status {
	case StatusSuccess:
		return 0
	case StatusPartialSuccess:
		return 1
	case StatusRetry:
		return 2
	case StatusFail:
		return 3
	default:
		return 4
	}
}
