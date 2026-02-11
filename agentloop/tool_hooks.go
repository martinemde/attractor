package agentloop

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"time"
)

// DefaultToolHookTimeout is the default timeout for tool hook commands.
const DefaultToolHookTimeout = 30 * time.Second

// ToolCallMetadata contains information about a tool call passed to hooks.
type ToolCallMetadata struct {
	ToolName  string          `json:"tool_name"`
	ToolID    string          `json:"tool_id"`
	Arguments json.RawMessage `json:"arguments"`
	NodeID    string          `json:"node_id,omitempty"`
	StageDir  string          `json:"stage_dir,omitempty"`
}

// ToolCallResult contains the result of a tool call passed to post-hooks.
type ToolCallResult struct {
	ToolCallMetadata
	Output  string `json:"output"`
	IsError bool   `json:"is_error"`
}

// ToolHookResult contains the result of executing a tool hook.
type ToolHookResult struct {
	Executed bool   // Whether the hook was executed
	Skipped  bool   // Whether the tool call should be skipped (pre-hook non-zero exit)
	ExitCode int    // Exit code of the hook command
	Stdout   string // Standard output from the hook
	Stderr   string // Standard error from the hook
	Error    error  // Any error during execution
}

// RunPreToolHook executes the pre-tool hook command.
// Returns ToolHookResult with Skipped=true if exit code is non-zero (meaning skip the tool call).
// Hook failures (non-zero exit) do not block but are recorded.
func RunPreToolHook(hookCmd string, metadata ToolCallMetadata, logsRoot string) ToolHookResult {
	if hookCmd == "" {
		return ToolHookResult{Executed: false}
	}

	result := executeToolHook(hookCmd, metadata, logsRoot)
	result.Executed = true

	// Per spec: exit code 0 = proceed, non-zero = skip the tool call
	if result.ExitCode != 0 && result.Error == nil {
		result.Skipped = true
	}

	return result
}

// RunPostToolHook executes the post-tool hook command.
// Post-hooks are primarily for logging/auditing; failures are recorded but don't affect the tool result.
func RunPostToolHook(hookCmd string, callResult ToolCallResult, logsRoot string) ToolHookResult {
	if hookCmd == "" {
		return ToolHookResult{Executed: false}
	}

	result := executeToolHook(hookCmd, callResult, logsRoot)
	result.Executed = true

	return result
}

// executeToolHook runs a hook command with the given metadata passed via stdin JSON and environment variables.
func executeToolHook(hookCmd string, metadata any, logsRoot string) ToolHookResult {
	result := ToolHookResult{}

	// Serialize metadata to JSON for stdin
	jsonData, err := json.Marshal(metadata)
	if err != nil {
		result.Error = fmt.Errorf("failed to marshal hook metadata: %w", err)
		result.ExitCode = -1
		return result
	}

	// Create context with timeout
	ctx, cancel := context.WithTimeout(context.Background(), DefaultToolHookTimeout)
	defer cancel()

	// Execute the hook command
	cmd := exec.CommandContext(ctx, "sh", "-c", hookCmd)

	// Set up stdin with JSON metadata
	cmd.Stdin = bytes.NewReader(jsonData)

	var stdout, stderr bytes.Buffer
	cmd.Stdout = &stdout
	cmd.Stderr = &stderr

	// Set environment variables based on metadata type
	cmd.Env = os.Environ()
	switch m := metadata.(type) {
	case ToolCallMetadata:
		cmd.Env = append(cmd.Env,
			"TOOL_NAME="+m.ToolName,
			"TOOL_ID="+m.ToolID,
			"NODE_ID="+m.NodeID,
		)
		if m.StageDir != "" {
			cmd.Env = append(cmd.Env, "STAGE_DIR="+m.StageDir)
		}
	case ToolCallResult:
		cmd.Env = append(cmd.Env,
			"TOOL_NAME="+m.ToolName,
			"TOOL_ID="+m.ToolID,
			"NODE_ID="+m.NodeID,
			fmt.Sprintf("TOOL_IS_ERROR=%t", m.IsError),
		)
		if m.StageDir != "" {
			cmd.Env = append(cmd.Env, "STAGE_DIR="+m.StageDir)
		}
	}

	err = cmd.Run()
	result.Stdout = stdout.String()
	result.Stderr = stderr.String()

	if err != nil {
		if ctx.Err() == context.DeadlineExceeded {
			result.Error = fmt.Errorf("hook timed out after %s", DefaultToolHookTimeout)
			result.ExitCode = -1
		} else if exitErr, ok := err.(*exec.ExitError); ok {
			result.ExitCode = exitErr.ExitCode()
			// Non-zero exit is not an error per spec, just recorded
		} else {
			result.Error = fmt.Errorf("hook execution failed: %w", err)
			result.ExitCode = -1
		}
	}

	return result
}

// LogToolHookResult writes the hook result to the session's log/event stream.
func LogToolHookResult(hookType string, result ToolHookResult, logsRoot, nodeID string, emitter *EventEmitter) {
	if !result.Executed {
		return
	}

	eventData := map[string]interface{}{
		"hook_type": hookType,
		"exit_code": result.ExitCode,
		"skipped":   result.Skipped,
	}
	if result.Error != nil {
		eventData["error"] = result.Error.Error()
	}

	if emitter != nil {
		emitter.Emit(EventToolHook, eventData)
	}

	// Write detailed hook result to file if logsRoot is provided
	if logsRoot != "" && nodeID != "" {
		hookLogFile := filepath.Join(logsRoot, nodeID, fmt.Sprintf("tool_hook_%s.log", hookType))
		hookLog := fmt.Sprintf("exit_code: %d\nskipped: %t\nstdout:\n%s\nstderr:\n%s\n",
			result.ExitCode, result.Skipped, result.Stdout, result.Stderr)
		if result.Error != nil {
			hookLog += fmt.Sprintf("error: %v\n", result.Error)
		}
		// Best effort write, don't fail on error
		_ = os.MkdirAll(filepath.Dir(hookLogFile), 0o755)
		_ = os.WriteFile(hookLogFile, []byte(hookLog), 0o644)
	}
}
