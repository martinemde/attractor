package agentloop

import (
	"context"
	"encoding/json"
	"fmt"
	"sync"
	"testing"
	"time"

	"github.com/martinemde/attractor/unifiedllm"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// --- Mock Provider Adapter ---

// mockAdapter implements unifiedllm.ProviderAdapter for testing.
// It returns responses from a pre-configured sequence.
type mockAdapter struct {
	name      string
	responses []*unifiedllm.Response
	callCount int
	mu        sync.Mutex
}

func (m *mockAdapter) Name() string { return m.name }

func (m *mockAdapter) Complete(ctx context.Context, req unifiedllm.Request) (*unifiedllm.Response, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	if m.callCount >= len(m.responses) {
		return nil, fmt.Errorf("no more mock responses (call #%d)", m.callCount)
	}
	resp := m.responses[m.callCount]
	m.callCount++
	return resp, nil
}

func (m *mockAdapter) Stream(ctx context.Context, req unifiedllm.Request) (<-chan unifiedllm.StreamEvent, error) {
	ch := make(chan unifiedllm.StreamEvent)
	close(ch)
	return ch, nil
}

func (m *mockAdapter) CallCount() int {
	m.mu.Lock()
	defer m.mu.Unlock()
	return m.callCount
}

// makeTextResponse creates a Response that contains only text (no tool calls).
func makeTextResponse(text string) *unifiedllm.Response {
	return &unifiedllm.Response{
		ID:       "resp_1",
		Model:    "test-model",
		Provider: "test",
		Message: unifiedllm.Message{
			Role:    unifiedllm.RoleAssistant,
			Content: []unifiedllm.ContentPart{unifiedllm.TextPart(text)},
		},
		FinishReason: unifiedllm.FinishReason{Reason: "stop"},
		Usage:        unifiedllm.Usage{InputTokens: 10, OutputTokens: 5, TotalTokens: 15},
	}
}

// makeToolCallResponse creates a Response that contains one or more tool calls.
func makeToolCallResponse(calls ...struct{ id, name, args string }) *unifiedllm.Response {
	parts := []unifiedllm.ContentPart{}
	for _, c := range calls {
		parts = append(parts, unifiedllm.ToolCallPart(c.id, c.name, json.RawMessage(c.args)))
	}
	return &unifiedllm.Response{
		ID:       "resp_tc",
		Model:    "test-model",
		Provider: "test",
		Message: unifiedllm.Message{
			Role:    unifiedllm.RoleAssistant,
			Content: parts,
		},
		FinishReason: unifiedllm.FinishReason{Reason: "tool_calls"},
		Usage:        unifiedllm.Usage{InputTokens: 10, OutputTokens: 5, TotalTokens: 15},
	}
}

// --- Mock Execution Environment ---

type mockExecEnv struct {
	workingDir string
	files      map[string]string
	mu         sync.Mutex
}

func newMockExecEnv(workingDir string) *mockExecEnv {
	return &mockExecEnv{
		workingDir: workingDir,
		files:      make(map[string]string),
	}
}

func (m *mockExecEnv) ReadFile(path string, offset, limit int) (string, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	content, ok := m.files[path]
	if !ok {
		return "", fmt.Errorf("file not found: %s", path)
	}
	return content, nil
}

func (m *mockExecEnv) WriteFile(path string, content string) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.files[path] = content
	return nil
}

func (m *mockExecEnv) FileExists(path string) bool {
	m.mu.Lock()
	defer m.mu.Unlock()
	_, ok := m.files[path]
	return ok
}

func (m *mockExecEnv) ListDirectory(path string, depth int) ([]DirEntry, error) {
	return nil, nil
}

func (m *mockExecEnv) ExecCommand(ctx context.Context, command string, timeoutMs int, workingDir string, envVars map[string]string) (*ExecResult, error) {
	return &ExecResult{Stdout: "mock output", ExitCode: 0, DurationMs: 10}, nil
}

func (m *mockExecEnv) Grep(ctx context.Context, pattern string, path string, options GrepOptions) (string, error) {
	return "mock grep results", nil
}

func (m *mockExecEnv) Glob(pattern string, path string) ([]string, error) {
	return []string{"file1.go", "file2.go"}, nil
}

func (m *mockExecEnv) Initialize() error      { return nil }
func (m *mockExecEnv) Cleanup() error          { return nil }
func (m *mockExecEnv) WorkingDirectory() string { return m.workingDir }
func (m *mockExecEnv) Platform() string         { return "linux" }
func (m *mockExecEnv) OSVersion() string        { return "linux/amd64" }

// --- Helper to create a test session ---

func newTestSession(adapter *mockAdapter, env *mockExecEnv, config *SessionConfig) *Session {
	profile := NewAnthropicProfile("test-model")
	client := unifiedllm.NewClient(
		unifiedllm.WithProvider("anthropic", adapter),
		unifiedllm.WithDefaultProvider("anthropic"),
	)

	s := NewSession(profile, env, config)
	s.SetClient(client)
	return s
}

// --- Tests ---

// TestSessionCreation verifies that a session is created with correct
// initial state (spec Section 2.1).
func TestSessionCreation(t *testing.T) {
	adapter := &mockAdapter{name: "anthropic", responses: []*unifiedllm.Response{}}
	env := newMockExecEnv("/tmp/test")
	s := newTestSession(adapter, env, nil)

	assert.NotEmpty(t, s.ID())
	assert.Equal(t, StateIdle, s.State())
	assert.Empty(t, s.History())
}

// TestSessionDefaultConfig verifies default configuration values
// per spec Section 2.2.
func TestSessionDefaultConfig(t *testing.T) {
	cfg := DefaultSessionConfig()

	assert.Equal(t, 0, cfg.MaxTurns)          // 0 = unlimited
	assert.Equal(t, 200, cfg.MaxToolRoundsPerInput)
	assert.Equal(t, 10000, cfg.DefaultCommandTimeoutMs)  // 10 seconds
	assert.Equal(t, 600000, cfg.MaxCommandTimeoutMs)     // 10 minutes
	assert.True(t, cfg.EnableLoopDetection)
	assert.Equal(t, 10, cfg.LoopDetectionWindow)
	assert.Equal(t, 1, cfg.MaxSubagentDepth)
}

// TestSessionSubmitNaturalCompletion verifies the core loop exits on
// natural completion (text-only response, no tool calls) per spec Section 2.5.
func TestSessionSubmitNaturalCompletion(t *testing.T) {
	adapter := &mockAdapter{
		name:      "anthropic",
		responses: []*unifiedllm.Response{makeTextResponse("The answer is 42.")},
	}
	env := newMockExecEnv("/tmp/test")
	s := newTestSession(adapter, env, nil)

	err := s.Submit(context.Background(), "What is the answer?")
	require.NoError(t, err)

	assert.Equal(t, StateIdle, s.State())
	history := s.History()
	require.Len(t, history, 2) // user + assistant
	assert.Equal(t, TurnUser, history[0].Kind)
	assert.Equal(t, "What is the answer?", history[0].User.Content)
	assert.Equal(t, TurnAssistant, history[1].Kind)
	assert.Equal(t, "The answer is 42.", history[1].Assistant.Content)
}

// TestSessionSubmitWithToolCalls verifies the agentic loop: LLM call -> tool
// execution -> loop -> natural completion (spec Section 2.5).
func TestSessionSubmitWithToolCalls(t *testing.T) {
	env := newMockExecEnv("/tmp/test")
	env.files["/tmp/test.txt"] = "hello world"

	adapter := &mockAdapter{
		name: "anthropic",
		responses: []*unifiedllm.Response{
			// First response: model wants to read a file.
			makeToolCallResponse(struct{ id, name, args string }{
				"call_1", "read_file", `{"file_path":"/tmp/test.txt"}`,
			}),
			// Second response: model provides the answer (natural completion).
			makeTextResponse("The file contains: hello world"),
		},
	}
	s := newTestSession(adapter, env, nil)

	err := s.Submit(context.Background(), "Read the file /tmp/test.txt")
	require.NoError(t, err)

	assert.Equal(t, StateIdle, s.State())
	assert.Equal(t, 2, adapter.CallCount())

	history := s.History()
	// user + assistant(tool_call) + tool_results + assistant(text)
	require.Len(t, history, 4)
	assert.Equal(t, TurnUser, history[0].Kind)
	assert.Equal(t, TurnAssistant, history[1].Kind)
	assert.Len(t, history[1].Assistant.ToolCalls, 1)
	assert.Equal(t, TurnToolResults, history[2].Kind)
	assert.Len(t, history[2].ToolResults.Results, 1)
	assert.False(t, history[2].ToolResults.Results[0].IsError)
	assert.Equal(t, TurnAssistant, history[3].Kind)
	assert.Equal(t, "The file contains: hello world", history[3].Assistant.Content)
}

// TestSessionSubmitUnknownToolCall verifies that calling an unknown tool
// returns an error result to the LLM rather than crashing (spec Section 3.8).
func TestSessionSubmitUnknownToolCall(t *testing.T) {
	adapter := &mockAdapter{
		name: "anthropic",
		responses: []*unifiedllm.Response{
			makeToolCallResponse(struct{ id, name, args string }{
				"call_1", "nonexistent_tool", `{}`,
			}),
			makeTextResponse("Sorry, I tried an unknown tool."),
		},
	}
	env := newMockExecEnv("/tmp/test")
	s := newTestSession(adapter, env, nil)

	err := s.Submit(context.Background(), "Do something")
	require.NoError(t, err)

	history := s.History()
	// Find the tool results turn.
	var toolResultsTurn *Turn
	for i := range history {
		if history[i].Kind == TurnToolResults {
			toolResultsTurn = &history[i]
			break
		}
	}
	require.NotNil(t, toolResultsTurn)
	require.Len(t, toolResultsTurn.ToolResults.Results, 1)
	assert.True(t, toolResultsTurn.ToolResults.Results[0].IsError)
	assert.Contains(t, toolResultsTurn.ToolResults.Results[0].Content, "Unknown tool")
}

// TestSessionSubmitRoundLimit verifies that max_tool_rounds_per_input stops
// the loop when reached (spec Section 2.8).
func TestSessionSubmitRoundLimit(t *testing.T) {
	// Create adapter that always returns tool calls.
	responses := make([]*unifiedllm.Response, 10)
	for i := range responses {
		responses[i] = makeToolCallResponse(struct{ id, name, args string }{
			fmt.Sprintf("call_%d", i), "shell", `{"command":"echo hi"}`,
		})
	}

	adapter := &mockAdapter{name: "anthropic", responses: responses}
	env := newMockExecEnv("/tmp/test")
	config := DefaultSessionConfig()
	config.MaxToolRoundsPerInput = 3

	s := newTestSession(adapter, env, &config)

	err := s.Submit(context.Background(), "run forever")
	require.NoError(t, err)

	// With MaxToolRoundsPerInput=3, the loop checks the count BEFORE each
	// LLM call and increments AFTER tool execution. So we get 3 LLM calls
	// that each produce tool calls, then the 4th check breaks.
	assert.Equal(t, 3, adapter.CallCount())
}

// TestSessionSubmitTurnLimit verifies that max_turns stops the loop
// across the session (spec Section 2.8).
func TestSessionSubmitTurnLimit(t *testing.T) {
	adapter := &mockAdapter{
		name: "anthropic",
		responses: []*unifiedllm.Response{
			makeTextResponse("Response 1"),
			makeTextResponse("Response 2"),
		},
	}
	env := newMockExecEnv("/tmp/test")
	config := DefaultSessionConfig()
	config.MaxTurns = 2 // 2 total turns (user + assistant = 2)

	s := newTestSession(adapter, env, &config)

	err := s.Submit(context.Background(), "first input")
	require.NoError(t, err)

	// Second submit should be limited.
	err = s.Submit(context.Background(), "second input")
	require.NoError(t, err)
}

// TestSessionAbort verifies that the abort signal stops the loop
// (spec Section 2.8, state transition: any -> CLOSED).
func TestSessionAbort(t *testing.T) {
	// Create adapter that returns tool calls with a slight delay.
	responses := make([]*unifiedllm.Response, 100)
	for i := range responses {
		responses[i] = makeToolCallResponse(struct{ id, name, args string }{
			fmt.Sprintf("call_%d", i), "shell", `{"command":"echo hi"}`,
		})
	}

	adapter := &mockAdapter{name: "anthropic", responses: responses}
	env := newMockExecEnv("/tmp/test")
	s := newTestSession(adapter, env, nil)

	// Abort after a short delay.
	go func() {
		time.Sleep(50 * time.Millisecond)
		s.Abort()
	}()

	err := s.Submit(context.Background(), "run a long task")
	require.NoError(t, err)

	// Should have stopped early due to abort.
	assert.Less(t, adapter.CallCount(), 100)
}

// TestSessionContextCancellation verifies that context cancellation
// stops the session (spec Section 2.3, any -> CLOSED).
func TestSessionContextCancellation(t *testing.T) {
	responses := make([]*unifiedllm.Response, 100)
	for i := range responses {
		responses[i] = makeToolCallResponse(struct{ id, name, args string }{
			fmt.Sprintf("call_%d", i), "shell", `{"command":"echo hi"}`,
		})
	}

	adapter := &mockAdapter{name: "anthropic", responses: responses}
	env := newMockExecEnv("/tmp/test")
	s := newTestSession(adapter, env, nil)

	ctx, cancel := context.WithTimeout(context.Background(), 100*time.Millisecond)
	defer cancel()

	err := s.Submit(ctx, "run forever")
	assert.Error(t, err) // Should get context error.
	assert.Equal(t, StateClosed, s.State())
}

// TestSessionSteer verifies that steering messages are injected between
// tool rounds (spec Section 2.6).
func TestSessionSteer(t *testing.T) {
	adapter := &mockAdapter{
		name: "anthropic",
		responses: []*unifiedllm.Response{
			makeToolCallResponse(struct{ id, name, args string }{
				"call_1", "shell", `{"command":"echo step1"}`,
			}),
			makeTextResponse("Adjusted my approach."),
		},
	}
	env := newMockExecEnv("/tmp/test")
	s := newTestSession(adapter, env, nil)

	// Queue a steering message before submit.
	s.Steer("change to a different approach")

	err := s.Submit(context.Background(), "do something")
	require.NoError(t, err)

	// History should contain the steering turn.
	history := s.History()
	hasSteeringTurn := false
	for _, turn := range history {
		if turn.Kind == TurnSteering {
			hasSteeringTurn = true
			assert.Equal(t, "change to a different approach", turn.Steering.Content)
		}
	}
	assert.True(t, hasSteeringTurn, "steering turn should be in history")
}

// TestSessionFollowUp verifies that follow-up messages are processed
// after the current input completes (spec Section 2.6).
func TestSessionFollowUp(t *testing.T) {
	adapter := &mockAdapter{
		name: "anthropic",
		responses: []*unifiedllm.Response{
			makeTextResponse("First response."),
			makeTextResponse("Follow-up response."),
		},
	}
	env := newMockExecEnv("/tmp/test")
	s := newTestSession(adapter, env, nil)

	s.FollowUp("follow-up message")

	err := s.Submit(context.Background(), "initial message")
	require.NoError(t, err)

	// Should have processed both the initial and follow-up.
	assert.Equal(t, 2, adapter.CallCount())

	history := s.History()
	userTurns := 0
	for _, turn := range history {
		if turn.Kind == TurnUser {
			userTurns++
		}
	}
	assert.Equal(t, 2, userTurns)
}

// TestSessionSetReasoningEffort verifies that changing reasoning_effort
// mid-session is reflected in the config (spec Section 2.7).
func TestSessionSetReasoningEffort(t *testing.T) {
	adapter := &mockAdapter{name: "anthropic", responses: []*unifiedllm.Response{}}
	env := newMockExecEnv("/tmp/test")
	config := DefaultSessionConfig()
	config.ReasoningEffort = "low"
	s := newTestSession(adapter, env, &config)

	s.SetReasoningEffort("high")

	s.mu.Lock()
	assert.Equal(t, "high", s.config.ReasoningEffort)
	s.mu.Unlock()
}

// TestSessionClose verifies that closing a session transitions to CLOSED
// state and emits SESSION_END (spec Section 2.3).
func TestSessionClose(t *testing.T) {
	adapter := &mockAdapter{name: "anthropic", responses: []*unifiedllm.Response{}}
	env := newMockExecEnv("/tmp/test")
	s := newTestSession(adapter, env, nil)

	events := s.Events()
	s.Close()

	assert.Equal(t, StateClosed, s.State())

	// Should have emitted SESSION_END.
	var foundEnd bool
	for event := range events {
		if event.Kind == EventSessionEnd {
			foundEnd = true
		}
	}
	assert.True(t, foundEnd, "should emit SESSION_END on close")
}

// TestSessionSubmitWhenClosed verifies that Submit returns error on closed
// session (spec Section 2.3).
func TestSessionSubmitWhenClosed(t *testing.T) {
	adapter := &mockAdapter{name: "anthropic", responses: []*unifiedllm.Response{}}
	env := newMockExecEnv("/tmp/test")
	s := newTestSession(adapter, env, nil)

	s.Close()

	err := s.Submit(context.Background(), "hello")
	assert.Error(t, err)
	assert.Contains(t, err.Error(), "closed")
}

// TestSessionToolCallEmitsEvents verifies that tool execution emits the
// correct events: TOOL_CALL_START and TOOL_CALL_END (spec Section 2.9).
func TestSessionToolCallEmitsEvents(t *testing.T) {
	adapter := &mockAdapter{
		name: "anthropic",
		responses: []*unifiedllm.Response{
			makeToolCallResponse(struct{ id, name, args string }{
				"call_1", "shell", `{"command":"echo hi"}`,
			}),
			makeTextResponse("Done."),
		},
	}
	env := newMockExecEnv("/tmp/test")
	s := newTestSession(adapter, env, nil)

	events := s.Events()

	go func() {
		_ = s.Submit(context.Background(), "run a command")
	}()

	var foundToolStart, foundToolEnd bool
	timeout := time.After(5 * time.Second)
	for {
		select {
		case event, ok := <-events:
			if !ok {
				goto done
			}
			if event.Kind == EventToolCallStart {
				foundToolStart = true
				assert.Equal(t, "shell", event.Data["tool_name"])
				assert.Equal(t, "call_1", event.Data["call_id"])
			}
			if event.Kind == EventToolCallEnd {
				foundToolEnd = true
				assert.Equal(t, "call_1", event.Data["call_id"])
			}
			if event.Kind == EventSessionEnd {
				goto done
			}
		case <-timeout:
			goto done
		}
	}
done:
	assert.True(t, foundToolStart, "should emit TOOL_CALL_START")
	assert.True(t, foundToolEnd, "should emit TOOL_CALL_END")
}

// TestSessionToolCallEndHasFullOutput verifies that TOOL_CALL_END events
// carry the full untruncated output (spec Section 2.9 key design decision).
func TestSessionToolCallEndHasFullOutput(t *testing.T) {
	adapter := &mockAdapter{
		name: "anthropic",
		responses: []*unifiedllm.Response{
			makeToolCallResponse(struct{ id, name, args string }{
				"call_1", "shell", `{"command":"echo hi"}`,
			}),
			makeTextResponse("Done."),
		},
	}
	env := newMockExecEnv("/tmp/test")
	s := newTestSession(adapter, env, nil)
	events := s.Events()

	go func() {
		_ = s.Submit(context.Background(), "run a command")
	}()

	timeout := time.After(5 * time.Second)
	for {
		select {
		case event, ok := <-events:
			if !ok {
				return
			}
			if event.Kind == EventToolCallEnd && event.Data["output"] != nil {
				// The output key should contain the full raw output.
				output, ok := event.Data["output"].(string)
				assert.True(t, ok, "output should be a string")
				assert.NotEmpty(t, output)
				return
			}
			if event.Kind == EventSessionEnd {
				return
			}
		case <-timeout:
			t.Fatal("timed out waiting for TOOL_CALL_END event with output")
		}
	}
}

// TestSessionMultipleSequentialSubmits verifies that multiple sequential
// inputs work correctly (spec Section 9.1: "Multiple sequential inputs work").
func TestSessionMultipleSequentialSubmits(t *testing.T) {
	adapter := &mockAdapter{
		name: "anthropic",
		responses: []*unifiedllm.Response{
			makeTextResponse("Response 1"),
			makeTextResponse("Response 2"),
			makeTextResponse("Response 3"),
		},
	}
	env := newMockExecEnv("/tmp/test")
	s := newTestSession(adapter, env, nil)

	for i, input := range []string{"first", "second", "third"} {
		err := s.Submit(context.Background(), input)
		require.NoError(t, err, "submit %d should succeed", i)
		assert.Equal(t, StateIdle, s.State())
	}

	assert.Equal(t, 3, adapter.CallCount())
}

// TestSessionParallelToolExecution verifies that tool calls are executed
// in parallel when the profile supports it (spec Section 5.7).
func TestSessionParallelToolExecution(t *testing.T) {
	adapter := &mockAdapter{
		name: "anthropic",
		responses: []*unifiedllm.Response{
			makeToolCallResponse(
				struct{ id, name, args string }{"call_1", "shell", `{"command":"echo a"}`},
				struct{ id, name, args string }{"call_2", "shell", `{"command":"echo b"}`},
			),
			makeTextResponse("Both commands done."),
		},
	}
	env := newMockExecEnv("/tmp/test")
	s := newTestSession(adapter, env, nil)

	err := s.Submit(context.Background(), "run two commands")
	require.NoError(t, err)

	history := s.History()
	// Find tool results turn.
	var toolResultsTurn *Turn
	for i := range history {
		if history[i].Kind == TurnToolResults {
			toolResultsTurn = &history[i]
			break
		}
	}
	require.NotNil(t, toolResultsTurn)
	// Should have results for both tool calls.
	assert.Len(t, toolResultsTurn.ToolResults.Results, 2)
	// Order should be preserved.
	assert.Equal(t, "call_1", toolResultsTurn.ToolResults.Results[0].ToolCallID)
	assert.Equal(t, "call_2", toolResultsTurn.ToolResults.Results[1].ToolCallID)
}

// TestSessionLoopDetection verifies that repeated tool call patterns
// trigger a warning SteeringTurn (spec Section 2.10).
func TestSessionLoopDetection(t *testing.T) {
	// Create responses that will trigger loop detection:
	// 10 identical tool calls followed by text completion.
	responses := make([]*unifiedllm.Response, 12)
	for i := 0; i < 11; i++ {
		responses[i] = makeToolCallResponse(struct{ id, name, args string }{
			"call_1", "read_file", `{"file_path":"/tmp/same"}`,
		})
	}
	responses[11] = makeTextResponse("Giving up.")

	adapter := &mockAdapter{name: "anthropic", responses: responses}
	env := newMockExecEnv("/tmp/test")
	env.files["/tmp/same"] = "content"

	config := DefaultSessionConfig()
	config.EnableLoopDetection = true
	config.LoopDetectionWindow = 10
	s := newTestSession(adapter, env, &config)

	err := s.Submit(context.Background(), "read the same file over and over")
	require.NoError(t, err)

	// Check that a loop detection steering turn was injected.
	history := s.History()
	hasLoopWarning := false
	for _, turn := range history {
		if turn.Kind == TurnSteering && turn.Steering != nil {
			if contains(turn.Steering.Content, "Loop detected") {
				hasLoopWarning = true
			}
		}
	}
	assert.True(t, hasLoopWarning, "should have injected loop detection warning")
}

func contains(s, substr string) bool {
	return len(s) >= len(substr) && (s == substr || len(s) > 0 && containsHelper(s, substr))
}

func containsHelper(s, substr string) bool {
	for i := 0; i <= len(s)-len(substr); i++ {
		if s[i:i+len(substr)] == substr {
			return true
		}
	}
	return false
}
