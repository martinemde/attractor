package pipeline

import (
	"fmt"

	"github.com/martinemde/attractor/dotparser"
)

// FanInHandler consolidates results from a preceding parallel node and selects the best candidate.
// It reads branch results from the context and applies a heuristic to select the winner.
type FanInHandler struct{}

// Execute reads parallel results and selects the best candidate.
func (h *FanInHandler) Execute(node *dotparser.Node, ctx *Context, graph *dotparser.Graph, logsRoot string) (*Outcome, error) {
	// 1. Read parallel results from context
	resultsRaw, ok := ctx.Get("parallel.results")
	if !ok {
		return Fail("no parallel results found in context (key: parallel.results)"), nil
	}

	resultsStr, ok := resultsRaw.(string)
	if !ok {
		return Fail("parallel.results is not a string"), nil
	}

	if resultsStr == "" {
		return Fail("parallel.results is empty"), nil
	}

	// 2. Deserialize results
	results, err := DeserializeBranchResults(resultsStr)
	if err != nil {
		return Fail(fmt.Sprintf("failed to deserialize parallel.results: %v", err)), nil
	}

	if len(results) == 0 {
		return Fail("no branch results to evaluate"), nil
	}

	// 3. Check if ALL candidates failed
	allFailed := true
	for _, r := range results {
		if r.Outcome != nil && r.Outcome.Status != StatusFail {
			allFailed = false
			break
		}
	}

	if allFailed {
		return Fail("all parallel branches failed"), nil
	}

	// 4. Apply heuristic selection
	// Sort by outcome status (SUCCESS=0, PARTIAL_SUCCESS=1, RETRY=2, FAIL=3), then by node ID
	SortBranchResultsByHeuristic(results)

	// Select the best candidate (first after sorting)
	best := results[0]

	// 5. Record winner in context
	ctx.Set("parallel.fan_in.best_id", best.NodeID)

	// Store best outcome status as string
	if best.Outcome != nil {
		ctx.Set("parallel.fan_in.best_outcome", best.Outcome.Status.String())
	}

	// 6. Return SUCCESS with notes about selected candidate
	notes := fmt.Sprintf("selected best candidate: %s", best.NodeID)
	if best.Outcome != nil {
		notes = fmt.Sprintf("selected best candidate: %s (status: %s)", best.NodeID, best.Outcome.Status)
	}

	return Success().
		WithNotes(notes).
		WithContextUpdate("parallel.fan_in.best_id", best.NodeID).
		WithContextUpdate("parallel.fan_in.best_outcome", best.Outcome.Status.String()), nil
}
