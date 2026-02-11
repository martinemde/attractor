package pipeline

import (
	"fmt"
	"strings"

	"github.com/martinemde/attractor/dotparser"
)

// FanInHandler consolidates results from a preceding parallel node and selects the best candidate.
// It reads branch results from the context and applies either:
// - LLM-based evaluation: when node.prompt is not empty, calls the LLM to rank candidates
// - Heuristic selection: when node.prompt is empty, ranks by outcome status then node ID
type FanInHandler struct {
	// Backend is the LLM execution backend for LLM-based evaluation.
	// If nil, only heuristic selection is available.
	Backend CodergenBackend
}

// NewFanInHandler creates a new FanInHandler with the given backend.
// Pass nil to use only heuristic selection.
func NewFanInHandler(backend CodergenBackend) *FanInHandler {
	return &FanInHandler{Backend: backend}
}

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

	// 4. Select best candidate using LLM or heuristic
	var best *BranchResult

	promptAttr, hasPrompt := node.Attr("prompt")
	if hasPrompt && promptAttr.Str != "" && h.Backend != nil {
		// LLM-based evaluation
		best, err = h.llmEvaluate(node, promptAttr.Str, results, ctx, graph)
		if err != nil {
			// Fall back to heuristic on LLM error
			SortBranchResultsByHeuristic(results)
			best = results[0]
		}
	} else {
		// Heuristic selection: sort by outcome status (SUCCESS=0, PARTIAL_SUCCESS=1, RETRY=2, FAIL=3), then by node ID
		SortBranchResultsByHeuristic(results)
		best = results[0]
	}

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

// llmEvaluate calls the LLM backend to evaluate and rank candidates.
// It formats the candidates as context for the LLM and parses the response
// to determine which candidate the LLM selected.
func (h *FanInHandler) llmEvaluate(node *dotparser.Node, prompt string, results []*BranchResult, ctx *Context, graph *dotparser.Graph) (*BranchResult, error) {
	// Build the full prompt with candidate context
	fullPrompt := h.buildEvaluationPrompt(prompt, results, ctx, graph)

	// Call LLM backend
	response, err := h.Backend.Run(node, fullPrompt, ctx)
	if err != nil {
		return nil, fmt.Errorf("LLM evaluation failed: %w", err)
	}

	// Parse response to find selected candidate
	responseStr := fmt.Sprintf("%v", response)

	// If backend returned an Outcome directly, we need to handle that
	if outcome, ok := response.(*Outcome); ok {
		if outcome.Status == StatusFail {
			return nil, fmt.Errorf("LLM evaluation returned failure: %s", outcome.FailureReason)
		}
		// Try to extract selection from outcome notes or context updates
		responseStr = outcome.Notes
		if selectedID, ok := outcome.ContextUpdates["selected_id"]; ok {
			responseStr = fmt.Sprintf("%v", selectedID)
		}
	}

	// Find the selected candidate from the LLM response
	selected := h.parseSelection(responseStr, results)
	if selected == nil {
		return nil, fmt.Errorf("could not parse LLM selection from response")
	}

	return selected, nil
}

// buildEvaluationPrompt constructs the full prompt for LLM evaluation.
// It includes the user's prompt and formats all candidates for evaluation.
func (h *FanInHandler) buildEvaluationPrompt(prompt string, results []*BranchResult, ctx *Context, graph *dotparser.Graph) string {
	var sb strings.Builder

	// Add the base prompt
	sb.WriteString(prompt)
	sb.WriteString("\n\n")

	// Add candidate context
	sb.WriteString("## Candidates to Evaluate\n\n")

	for i, r := range results {
		sb.WriteString(fmt.Sprintf("### Candidate %d: %s\n", i+1, r.NodeID))
		if r.Outcome != nil {
			sb.WriteString(fmt.Sprintf("- Status: %s\n", r.Outcome.Status.String()))
			if r.Outcome.Notes != "" {
				sb.WriteString(fmt.Sprintf("- Notes: %s\n", r.Outcome.Notes))
			}
			if r.Outcome.FailureReason != "" {
				sb.WriteString(fmt.Sprintf("- Failure Reason: %s\n", r.Outcome.FailureReason))
			}
		}
		sb.WriteString("\n")
	}

	sb.WriteString("## Instructions\n\n")
	sb.WriteString("Evaluate the candidates above and select the best one. ")
	sb.WriteString("Respond with the ID of the best candidate (e.g., 'branchA' or 'candidate_1').\n")

	return sb.String()
}

// parseSelection extracts the selected candidate ID from the LLM response.
// It looks for candidate IDs mentioned in the response text.
func (h *FanInHandler) parseSelection(response string, results []*BranchResult) *BranchResult {
	responseLower := strings.ToLower(response)

	// Try to find an exact match for any candidate ID in the response
	for _, r := range results {
		if strings.Contains(responseLower, strings.ToLower(r.NodeID)) {
			return r
		}
	}

	// If no exact match, try to find candidate references like "candidate 1" or "Candidate #1"
	for i, r := range results {
		patterns := []string{
			fmt.Sprintf("candidate %d", i+1),
			fmt.Sprintf("candidate #%d", i+1),
			fmt.Sprintf("candidate_%d", i+1),
			fmt.Sprintf("option %d", i+1),
			fmt.Sprintf("choice %d", i+1),
		}
		for _, pattern := range patterns {
			if strings.Contains(responseLower, pattern) {
				return r
			}
		}
	}

	return nil
}
