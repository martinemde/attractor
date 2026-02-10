package pipeline

import (
	"bytes"
	"context"
	"os/exec"
	"time"

	"github.com/martinemde/attractor/dotparser"
)

// DefaultToolTimeout is the default timeout for tool commands.
const DefaultToolTimeout = 30 * time.Second

// ToolHandler executes external tools (shell commands) configured via node attributes.
// It reads the tool_command attribute, executes it with timeout, and returns the result.
type ToolHandler struct{}

// Execute runs the tool command specified in the node's tool_command attribute.
func (h *ToolHandler) Execute(node *dotparser.Node, ctx *Context, graph *dotparser.Graph, logsRoot string) (*Outcome, error) {
	// Read tool_command from node attributes
	toolCommandAttr, ok := node.Attr("tool_command")
	if !ok || toolCommandAttr.Str == "" {
		return Fail("No tool_command specified"), nil
	}
	command := toolCommandAttr.Str

	// Determine timeout
	timeout := DefaultToolTimeout
	if timeoutAttr, ok := node.Attr("timeout"); ok && timeoutAttr.Kind == dotparser.ValueDuration {
		timeout = timeoutAttr.Duration
	}

	// Create context with timeout
	execCtx, cancel := context.WithTimeout(context.Background(), timeout)
	defer cancel()

	// Execute the command
	cmd := exec.CommandContext(execCtx, "sh", "-c", command)

	var stdout, stderr bytes.Buffer
	cmd.Stdout = &stdout
	cmd.Stderr = &stderr

	err := cmd.Run()
	if err != nil {
		// Check if it was a timeout
		if execCtx.Err() == context.DeadlineExceeded {
			return Fail("command timed out after " + timeout.String()), nil
		}
		// Include stderr in the error message
		errMsg := err.Error()
		if stderr.Len() > 0 {
			errMsg += ": " + stderr.String()
		}
		return Fail(errMsg), nil
	}

	return Success().
		WithContextUpdate("tool.output", stdout.String()).
		WithNotes("Tool completed: " + command), nil
}
