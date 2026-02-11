package agentloop

import (
	"encoding/json"
	"os"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestRunPreToolHook_EmptyCommandReturnsNotExecuted(t *testing.T) {
	metadata := ToolCallMetadata{
		ToolName: "test_tool",
		ToolID:   "call_1",
	}

	result := RunPreToolHook("", metadata, "")

	assert.False(t, result.Executed)
	assert.False(t, result.Skipped)
}

func TestRunPreToolHook_SuccessfulExecution(t *testing.T) {
	metadata := ToolCallMetadata{
		ToolName: "test_tool",
		ToolID:   "call_1",
		NodeID:   "stage_1",
	}

	result := RunPreToolHook("exit 0", metadata, "")

	assert.True(t, result.Executed)
	assert.False(t, result.Skipped)
	assert.Equal(t, 0, result.ExitCode)
	assert.NoError(t, result.Error)
}

func TestRunPreToolHook_NonZeroExitSkipsToolCall(t *testing.T) {
	metadata := ToolCallMetadata{
		ToolName: "test_tool",
		ToolID:   "call_1",
	}

	result := RunPreToolHook("exit 1", metadata, "")

	assert.True(t, result.Executed)
	assert.True(t, result.Skipped, "non-zero exit should skip the tool call")
	assert.Equal(t, 1, result.ExitCode)
	assert.NoError(t, result.Error, "non-zero exit is not an error, just recorded")
}

func TestRunPreToolHook_ReceivesMetadataViaStdin(t *testing.T) {
	tmpDir := t.TempDir()
	outputFile := filepath.Join(tmpDir, "stdin.json")

	metadata := ToolCallMetadata{
		ToolName:  "read_file",
		ToolID:    "call_123",
		Arguments: json.RawMessage(`{"path": "/tmp/test.txt"}`),
		NodeID:    "code_stage",
	}

	// Hook reads stdin and writes to file
	hookCmd := "cat > " + outputFile

	result := RunPreToolHook(hookCmd, metadata, "")

	require.True(t, result.Executed)
	require.NoError(t, result.Error)

	// Verify the metadata was passed via stdin
	data, err := os.ReadFile(outputFile)
	require.NoError(t, err)

	var received ToolCallMetadata
	err = json.Unmarshal(data, &received)
	require.NoError(t, err)

	assert.Equal(t, "read_file", received.ToolName)
	assert.Equal(t, "call_123", received.ToolID)
	assert.Equal(t, "code_stage", received.NodeID)
}

func TestRunPreToolHook_SetsEnvironmentVariables(t *testing.T) {
	tmpDir := t.TempDir()
	outputFile := filepath.Join(tmpDir, "env.txt")

	metadata := ToolCallMetadata{
		ToolName: "write_file",
		ToolID:   "call_456",
		NodeID:   "edit_stage",
	}

	// Hook writes environment variables to file
	hookCmd := `printf "TOOL_NAME=%s\nTOOL_ID=%s\nNODE_ID=%s\n" "$TOOL_NAME" "$TOOL_ID" "$NODE_ID" > ` + outputFile

	result := RunPreToolHook(hookCmd, metadata, "")

	require.True(t, result.Executed)
	require.NoError(t, result.Error)

	data, err := os.ReadFile(outputFile)
	require.NoError(t, err)

	content := string(data)
	assert.Contains(t, content, "TOOL_NAME=write_file")
	assert.Contains(t, content, "TOOL_ID=call_456")
	assert.Contains(t, content, "NODE_ID=edit_stage")
}

func TestRunPostToolHook_EmptyCommandReturnsNotExecuted(t *testing.T) {
	callResult := ToolCallResult{
		ToolCallMetadata: ToolCallMetadata{
			ToolName: "test_tool",
			ToolID:   "call_1",
		},
		Output:  "tool output",
		IsError: false,
	}

	result := RunPostToolHook("", callResult, "")

	assert.False(t, result.Executed)
}

func TestRunPostToolHook_ReceivesResultMetadata(t *testing.T) {
	tmpDir := t.TempDir()
	outputFile := filepath.Join(tmpDir, "result.json")

	callResult := ToolCallResult{
		ToolCallMetadata: ToolCallMetadata{
			ToolName:  "exec_command",
			ToolID:    "call_789",
			Arguments: json.RawMessage(`{"command": "ls -la"}`),
			NodeID:    "tool_stage",
		},
		Output:  "file1.txt\nfile2.txt",
		IsError: false,
	}

	hookCmd := "cat > " + outputFile

	result := RunPostToolHook(hookCmd, callResult, "")

	require.True(t, result.Executed)
	require.NoError(t, result.Error)

	data, err := os.ReadFile(outputFile)
	require.NoError(t, err)

	var received ToolCallResult
	err = json.Unmarshal(data, &received)
	require.NoError(t, err)

	assert.Equal(t, "exec_command", received.ToolName)
	assert.Equal(t, "call_789", received.ToolID)
	assert.Equal(t, "file1.txt\nfile2.txt", received.Output)
	assert.False(t, received.IsError)
}

func TestRunPostToolHook_NonZeroExitDoesNotBlock(t *testing.T) {
	callResult := ToolCallResult{
		ToolCallMetadata: ToolCallMetadata{
			ToolName: "test_tool",
			ToolID:   "call_1",
		},
		Output: "output",
	}

	// Post-hook fails with non-zero exit
	result := RunPostToolHook("exit 2", callResult, "")

	assert.True(t, result.Executed)
	assert.False(t, result.Skipped, "post-hook failures don't skip anything")
	assert.Equal(t, 2, result.ExitCode)
	assert.NoError(t, result.Error, "non-zero exit is recorded but not an error")
}

func TestRunPreToolHook_CapturesStdoutAndStderr(t *testing.T) {
	metadata := ToolCallMetadata{
		ToolName: "test_tool",
		ToolID:   "call_1",
	}

	hookCmd := `echo "stdout message"; echo "stderr message" >&2`

	result := RunPreToolHook(hookCmd, metadata, "")

	assert.True(t, result.Executed)
	assert.Contains(t, result.Stdout, "stdout message")
	assert.Contains(t, result.Stderr, "stderr message")
}

func TestLogToolHookResult_EmitsEvent(t *testing.T) {
	emitter := NewEventEmitter("test-session", 10)

	result := ToolHookResult{
		Executed: true,
		ExitCode: 0,
		Stdout:   "hook output",
	}

	LogToolHookResult("pre", result, "", "test_node", emitter)

	// Read the event from the channel
	select {
	case event := <-emitter.Events():
		assert.Equal(t, EventToolHook, event.Kind)
		assert.Equal(t, "pre", event.Data["hook_type"])
		assert.Equal(t, 0, event.Data["exit_code"])
	default:
		t.Fatal("expected an event to be emitted")
	}
}

func TestLogToolHookResult_WritesToLogFile(t *testing.T) {
	tmpDir := t.TempDir()
	emitter := NewEventEmitter("test-session", 10)

	result := ToolHookResult{
		Executed: true,
		ExitCode: 0,
		Stdout:   "hook stdout",
		Stderr:   "hook stderr",
	}

	LogToolHookResult("post", result, tmpDir, "stage_1", emitter)

	logFile := filepath.Join(tmpDir, "stage_1", "tool_hook_post.log")
	data, err := os.ReadFile(logFile)
	require.NoError(t, err)

	content := string(data)
	assert.Contains(t, content, "exit_code: 0")
	assert.Contains(t, content, "stdout:\nhook stdout")
	assert.Contains(t, content, "stderr:\nhook stderr")
}

func TestLogToolHookResult_DoesNothingWhenNotExecuted(t *testing.T) {
	emitter := NewEventEmitter("test-session", 10)

	result := ToolHookResult{
		Executed: false,
	}

	LogToolHookResult("pre", result, "", "test_node", emitter)

	// No event should be emitted
	select {
	case <-emitter.Events():
		t.Fatal("no event should be emitted when hook was not executed")
	default:
		// Expected - no event
	}
}
