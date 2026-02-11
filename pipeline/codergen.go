package pipeline

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"github.com/martinemde/attractor/dotparser"
)

// CodergenBackend is the interface for LLM execution backends.
// Implementations can return either a string response or an *Outcome directly.
type CodergenBackend interface {
	// Run executes the LLM with the given prompt and returns either
	// a string response text or an *Outcome directly.
	Run(node *dotparser.Node, prompt string, ctx *Context) (any, error)
}

// CodergenHandler is the default handler for LLM task nodes.
// It reads the node's prompt, expands template variables, calls the LLM backend,
// writes the prompt and response to the logs directory, and returns the outcome.
type CodergenHandler struct {
	// Backend is the LLM execution backend. If nil, simulation mode is used.
	Backend CodergenBackend
}

// NewCodergenHandler creates a new CodergenHandler with the given backend.
// Pass nil for simulation mode.
func NewCodergenHandler(backend CodergenBackend) *CodergenHandler {
	return &CodergenHandler{Backend: backend}
}

// Execute runs the codergen handler for the given node.
func (h *CodergenHandler) Execute(node *dotparser.Node, ctx *Context, graph *dotparser.Graph, logsRoot string) (*Outcome, error) {
	// 1. Build prompt: use node's prompt attr, fall back to label, fall back to node ID
	prompt := h.buildPrompt(node, graph, ctx)

	// 2. Create stage directory and write prompt
	if logsRoot != "" {
		stageDir := filepath.Join(logsRoot, node.ID)
		if err := os.MkdirAll(stageDir, 0o755); err != nil {
			return nil, fmt.Errorf("failed to create stage directory: %w", err)
		}

		promptFile := filepath.Join(stageDir, "prompt.md")
		if err := os.WriteFile(promptFile, []byte(prompt), 0o644); err != nil {
			return nil, fmt.Errorf("failed to write prompt file: %w", err)
		}
	}

	// 3. Call LLM backend or use simulation
	var responseText string

	if h.Backend != nil {
		result, err := h.Backend.Run(node, prompt, ctx)
		if err != nil {
			return Fail(err.Error()), nil
		}

		// If result is an Outcome, return it directly
		if outcome, ok := result.(*Outcome); ok {
			if logsRoot != "" {
				h.writeStatus(logsRoot, node.ID, outcome)
			}
			return outcome, nil
		}

		// Otherwise treat as string response
		responseText = fmt.Sprintf("%v", result)
	} else {
		// Simulation mode
		responseText = fmt.Sprintf("[Simulated] Response for stage: %s", node.ID)
	}

	// 4. Write response to logs
	if logsRoot != "" {
		stageDir := filepath.Join(logsRoot, node.ID)
		responseFile := filepath.Join(stageDir, "response.md")
		if err := os.WriteFile(responseFile, []byte(responseText), 0o644); err != nil {
			return nil, fmt.Errorf("failed to write response file: %w", err)
		}
	}

	// 5. Check for refusal patterns in response
	if isRefusalResponse(responseText) {
		outcome := Fail("LLM response indicates refusal or inability to complete task").
			WithContextUpdate("last_stage", node.ID).
			WithContextUpdate("last_response", responseText)

		if logsRoot != "" {
			h.writeStatus(logsRoot, node.ID, outcome)
		}
		return outcome, nil
	}

	// 6. Build outcome with context updates
	outcome := Success().
		WithNotes(fmt.Sprintf("Stage completed: %s", node.ID)).
		WithContextUpdate("last_stage", node.ID).
		WithContextUpdate("last_response", responseText)

	// 7. Write status to logs
	if logsRoot != "" {
		h.writeStatus(logsRoot, node.ID, outcome)
	}

	return outcome, nil
}

// isRefusalResponse detects refusal patterns in LLM responses that indicate
// the model couldn't or wouldn't complete the task.
func isRefusalResponse(response string) bool {
	lowerResponse := strings.ToLower(response)
	refusalPatterns := []string{
		"i can't",
		"i cannot",
		"i don't have access",
		"i'm unable to",
		"i am unable to",
		"please provide",
		"please paste",
		"i don't have the ability",
		"i'm not able to",
		"i am not able to",
	}
	for _, pattern := range refusalPatterns {
		if strings.Contains(lowerResponse, pattern) {
			return true
		}
	}
	return false
}

// buildPrompt constructs the prompt from node attributes with variable expansion.
// It expands both graph-level attributes (like $goal) and context variables
// (like $last_response, $last_stage) to allow stages to reference prior output.
// If a preamble is available in context (from the Preamble Transform), it is
// prepended to provide context carryover based on the resolved fidelity mode.
func (h *CodergenHandler) buildPrompt(node *dotparser.Node, graph *dotparser.Graph, ctx *Context) string {
	// Priority: prompt attr > label attr > node ID
	var prompt string

	if promptAttr, ok := node.Attr("prompt"); ok && promptAttr.Str != "" {
		prompt = promptAttr.Str
	} else if labelAttr, ok := node.Attr("label"); ok && labelAttr.Str != "" {
		prompt = labelAttr.Str
	} else {
		prompt = node.ID
	}

	// Expand $goal variable from graph attributes
	if goalAttr, ok := graph.GraphAttr("goal"); ok {
		prompt = strings.ReplaceAll(prompt, "$goal", goalAttr.Str)
	}

	// Expand all context variables (e.g., $last_response, $last_stage)
	// This allows stages to reference output from prior stages.
	if ctx != nil {
		snapshot := ctx.Snapshot()
		for key, value := range snapshot {
			varName := "$" + key
			prompt = strings.ReplaceAll(prompt, varName, toString(value))
		}
	}

	// Prepend preamble if available (from Preamble Transform based on fidelity mode)
	// The preamble provides context carryover for non-full fidelity modes.
	if ctx != nil {
		preamble := GetPreambleFromContext(ctx)
		if preamble != "" {
			prompt = preamble + "\n\n---\n\n" + prompt
		}
	}

	return prompt
}

// writeStatus writes the outcome to status.json in the stage directory.
func (h *CodergenHandler) writeStatus(logsRoot, nodeID string, outcome *Outcome) error {
	stageDir := filepath.Join(logsRoot, nodeID)
	statusFile := filepath.Join(stageDir, "status.json")

	status := map[string]any{
		"status": outcome.Status.String(),
	}
	if outcome.Notes != "" {
		status["notes"] = outcome.Notes
	}
	if outcome.FailureReason != "" {
		status["failure_reason"] = outcome.FailureReason
	}
	if outcome.PreferredLabel != "" {
		status["preferred_label"] = outcome.PreferredLabel
	}
	if len(outcome.SuggestedNextIDs) > 0 {
		status["suggested_next_ids"] = outcome.SuggestedNextIDs
	}
	if len(outcome.ContextUpdates) > 0 {
		status["context_updates"] = outcome.ContextUpdates
	}

	data, err := json.MarshalIndent(status, "", "  ")
	if err != nil {
		return fmt.Errorf("failed to marshal status: %w", err)
	}

	return os.WriteFile(statusFile, data, 0o644)
}
