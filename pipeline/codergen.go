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
	prompt := h.buildPrompt(node, graph)

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

	// 5. Build outcome with context updates
	outcome := Success().
		WithNotes(fmt.Sprintf("Stage completed: %s", node.ID)).
		WithContextUpdate("last_stage", node.ID).
		WithContextUpdate("last_response", truncateString(responseText, 200))

	// 6. Write status to logs
	if logsRoot != "" {
		h.writeStatus(logsRoot, node.ID, outcome)
	}

	return outcome, nil
}

// buildPrompt constructs the prompt from node attributes with variable expansion.
func (h *CodergenHandler) buildPrompt(node *dotparser.Node, graph *dotparser.Graph) string {
	// Priority: prompt attr > label attr > node ID
	var prompt string

	if promptAttr, ok := node.Attr("prompt"); ok && promptAttr.Str != "" {
		prompt = promptAttr.Str
	} else if labelAttr, ok := node.Attr("label"); ok && labelAttr.Str != "" {
		prompt = labelAttr.Str
	} else {
		prompt = node.ID
	}

	// Expand $goal variable
	if goalAttr, ok := graph.GraphAttr("goal"); ok {
		prompt = strings.ReplaceAll(prompt, "$goal", goalAttr.Str)
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

// truncateString truncates a string to maxLen characters with "..." suffix if needed.
func truncateString(s string, maxLen int) string {
	if len(s) <= maxLen {
		return s
	}
	if maxLen <= 3 {
		return s[:maxLen]
	}
	return s[:maxLen-3] + "..."
}
