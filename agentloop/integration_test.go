package agentloop

import (
	"context"
	"fmt"
	"sync"
	"testing"

	"github.com/martinemde/attractor/unifiedllm"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// Integration tests verify that the agentloop and unifiedllm packages
// work together correctly per both specs.

// --- Stateful Mock Adapter for Integration Tests ---

// statefulAdapter simulates an LLM that reasons about tool results.
// It uses a callback to decide the next response based on the request.
type statefulAdapter struct {
	name    string
	handler func(req unifiedllm.Request) (*unifiedllm.Response, error)
	calls   []unifiedllm.Request
	mu      sync.Mutex
}

func (a *statefulAdapter) Name() string { return a.name }

func (a *statefulAdapter) Complete(ctx context.Context, req unifiedllm.Request) (*unifiedllm.Response, error) {
	a.mu.Lock()
	a.calls = append(a.calls, req)
	a.mu.Unlock()
	return a.handler(req)
}

func (a *statefulAdapter) Stream(ctx context.Context, req unifiedllm.Request) (<-chan unifiedllm.StreamEvent, error) {
	ch := make(chan unifiedllm.StreamEvent)
	close(ch)
	return ch, nil
}

func (a *statefulAdapter) CallCount() int {
	a.mu.Lock()
	defer a.mu.Unlock()
	return len(a.calls)
}

func (a *statefulAdapter) LastRequest() unifiedllm.Request {
	a.mu.Lock()
	defer a.mu.Unlock()
	return a.calls[len(a.calls)-1]
}

// TestIntegrationEndToEndAgenticLoop tests a full workflow:
// 1. User asks to read a file, edit it, and verify the change
// 2. LLM reads the file
// 3. LLM edits the file
// 4. LLM reads the file again to verify
// 5. LLM produces the final answer
//
// This exercises the full spec integration:
// - unifiedllm.Client routes requests to the mock adapter
// - agentloop.Session manages the conversation history
// - Tool execution through the ExecutionEnvironment
// - History conversion to unifiedllm.Message format
// - Multi-round tool loops
func TestIntegrationEndToEndAgenticLoop(t *testing.T) {
	env := newMockExecEnv("/tmp/project")
	env.files["/tmp/project/main.go"] = "package main\n\nfunc main() {\n\tfmt.Println(\"hello\")\n}\n"

	callNum := 0
	adapter := &statefulAdapter{
		name: "anthropic",
		handler: func(req unifiedllm.Request) (*unifiedllm.Response, error) {
			callNum++
			switch callNum {
			case 1:
				// Step 1: Read the file.
				return makeToolCallResponse(struct{ id, name, args string }{
					"call_1", "read_file", `{"file_path":"/tmp/project/main.go"}`,
				}), nil
			case 2:
				// Step 2: Edit the file.
				return makeToolCallResponse(struct{ id, name, args string }{
					"call_2", "write_file",
					`{"file_path":"/tmp/project/main.go","content":"package main\n\nfunc main() {\n\tfmt.Println(\"hello world\")\n}\n"}`,
				}), nil
			case 3:
				// Step 3: Verify by reading again.
				return makeToolCallResponse(struct{ id, name, args string }{
					"call_3", "read_file", `{"file_path":"/tmp/project/main.go"}`,
				}), nil
			case 4:
				// Step 4: Final answer.
				return makeTextResponse("I've updated main.go to print 'hello world'."), nil
			default:
				return makeTextResponse("Unexpected call."), nil
			}
		},
	}

	profile := NewAnthropicProfile("claude-opus-4-6")
	client := unifiedllm.NewClient(
		unifiedllm.WithProvider("anthropic", adapter),
		unifiedllm.WithDefaultProvider("anthropic"),
	)
	s := NewSession(profile, env, nil)
	s.SetClient(client)

	err := s.Submit(context.Background(), "Edit main.go to print 'hello world' instead of 'hello'")
	require.NoError(t, err)

	assert.Equal(t, 4, adapter.CallCount())

	// Verify the file was actually modified.
	content, exists := env.files["/tmp/project/main.go"]
	require.True(t, exists)
	assert.Contains(t, content, "hello world")

	// Verify conversation history is correct.
	history := s.History()
	assert.Equal(t, TurnUser, history[0].Kind)

	// Count each turn type.
	turnCounts := map[TurnKind]int{}
	for _, turn := range history {
		turnCounts[turn.Kind]++
	}
	assert.Equal(t, 1, turnCounts[TurnUser])
	assert.Equal(t, 4, turnCounts[TurnAssistant])  // 3 tool call responses + 1 text
	assert.Equal(t, 3, turnCounts[TurnToolResults]) // 3 tool executions
}

// TestIntegrationHistoryTruncation verifies that tool output sent to the
// LLM is properly truncated while the full output appears in events
// (spec: Section 5.1-5.3 integration with events).
func TestIntegrationHistoryTruncation(t *testing.T) {
	env := newMockExecEnv("/tmp/project")
	// Create a file with content exceeding the 50000 char read_file limit.
	bigContent := ""
	for i := 0; i < 60000; i++ {
		bigContent += "x"
	}
	env.files["/tmp/project/huge.txt"] = bigContent

	callNum := 0
	adapter := &statefulAdapter{
		name: "anthropic",
		handler: func(req unifiedllm.Request) (*unifiedllm.Response, error) {
			callNum++
			if callNum == 1 {
				return makeToolCallResponse(struct{ id, name, args string }{
					"call_1", "read_file", `{"file_path":"/tmp/project/huge.txt"}`,
				}), nil
			}
			return makeTextResponse("File is very large."), nil
		},
	}

	profile := NewAnthropicProfile("claude-opus-4-6")
	client := unifiedllm.NewClient(
		unifiedllm.WithProvider("anthropic", adapter),
		unifiedllm.WithDefaultProvider("anthropic"),
	)
	s := NewSession(profile, env, nil)
	s.SetClient(client)

	events := s.Events()
	var fullOutput string
	var mu sync.Mutex
	done := make(chan struct{})

	go func() {
		defer close(done)
		for event := range events {
			if event.Kind == EventToolCallEnd && event.Data["output"] != nil {
				mu.Lock()
				fullOutput = event.Data["output"].(string)
				mu.Unlock()
			}
		}
	}()

	err := s.Submit(context.Background(), "Read the huge file")
	require.NoError(t, err)

	// Close the session to close the event channel and wait for drain.
	s.Close()
	<-done

	mu.Lock()
	outputLen := len(fullOutput)
	mu.Unlock()

	// Event output should be the full content (not truncated).
	assert.Greater(t, outputLen, 50000, "event output should have full content")

	// Tool result in history should be truncated.
	var truncatedContent string
	history := s.History()
	for _, turn := range history {
		if turn.Kind == TurnToolResults && turn.ToolResults != nil {
			for _, r := range turn.ToolResults.Results {
				if s, ok := r.Content.(string); ok {
					truncatedContent = s
				}
			}
		}
	}
	assert.Contains(t, truncatedContent, "[WARNING: Tool output was truncated.")
}

// TestIntegrationClientProviderResolution verifies that the agentloop
// Session correctly passes provider info through to the unifiedllm.Client
// for proper routing (integration point between the two specs).
func TestIntegrationClientProviderResolution(t *testing.T) {
	adapter := &statefulAdapter{
		name: "anthropic",
		handler: func(req unifiedllm.Request) (*unifiedllm.Response, error) {
			return makeTextResponse("response"), nil
		},
	}

	profile := NewAnthropicProfile("claude-opus-4-6")
	client := unifiedllm.NewClient(
		unifiedllm.WithProvider("anthropic", adapter),
	)
	s := NewSession(profile, newMockExecEnv("/tmp/test"), nil)
	s.SetClient(client)

	err := s.Submit(context.Background(), "hello")
	require.NoError(t, err)

	// Verify the request included the correct provider and model.
	lastReq := adapter.LastRequest()
	assert.Equal(t, "anthropic", lastReq.Provider)
	assert.Equal(t, "claude-opus-4-6", lastReq.Model)
}

// TestIntegrationToolDefsSentToLLM verifies that tool definitions from
// the profile are correctly translated and included in LLM requests
// (integration: agentloop.ToolDefinition -> unifiedllm.ToolDefinition).
func TestIntegrationToolDefsSentToLLM(t *testing.T) {
	adapter := &statefulAdapter{
		name: "anthropic",
		handler: func(req unifiedllm.Request) (*unifiedllm.Response, error) {
			return makeTextResponse("response"), nil
		},
	}

	profile := NewAnthropicProfile("claude-opus-4-6")
	client := unifiedllm.NewClient(
		unifiedllm.WithProvider("anthropic", adapter),
	)
	s := NewSession(profile, newMockExecEnv("/tmp/test"), nil)
	s.SetClient(client)

	err := s.Submit(context.Background(), "hello")
	require.NoError(t, err)

	lastReq := adapter.LastRequest()

	// Should have tool definitions from the profile.
	require.NotEmpty(t, lastReq.ToolDefs)

	// Verify some key tool names are present.
	toolNames := make(map[string]bool)
	for _, td := range lastReq.ToolDefs {
		toolNames[td.Name] = true
	}

	assert.True(t, toolNames["read_file"], "should include read_file tool")
	assert.True(t, toolNames["edit_file"], "should include edit_file tool")
	assert.True(t, toolNames["shell"], "should include shell tool")
}

// TestIntegrationToolChoiceSetToAuto verifies that tool_choice is set to
// "auto" in all LLM requests (spec Section 2.5 step 2).
func TestIntegrationToolChoiceSetToAuto(t *testing.T) {
	adapter := &statefulAdapter{
		name: "anthropic",
		handler: func(req unifiedllm.Request) (*unifiedllm.Response, error) {
			return makeTextResponse("response"), nil
		},
	}

	profile := NewAnthropicProfile("claude-opus-4-6")
	client := unifiedllm.NewClient(
		unifiedllm.WithProvider("anthropic", adapter),
	)
	s := NewSession(profile, newMockExecEnv("/tmp/test"), nil)
	s.SetClient(client)

	err := s.Submit(context.Background(), "hello")
	require.NoError(t, err)

	lastReq := adapter.LastRequest()
	require.NotNil(t, lastReq.ToolChoice)
	assert.Equal(t, "auto", lastReq.ToolChoice.Mode)
}

// TestIntegrationProviderOptionsPassThrough verifies that provider-specific
// options from the profile are included in LLM requests (integration point).
func TestIntegrationProviderOptionsPassThrough(t *testing.T) {
	adapter := &statefulAdapter{
		name: "anthropic",
		handler: func(req unifiedllm.Request) (*unifiedllm.Response, error) {
			return makeTextResponse("response"), nil
		},
	}

	profile := NewAnthropicProfile("claude-opus-4-6")
	client := unifiedllm.NewClient(
		unifiedllm.WithProvider("anthropic", adapter),
	)
	s := NewSession(profile, newMockExecEnv("/tmp/test"), nil)
	s.SetClient(client)

	err := s.Submit(context.Background(), "hello")
	require.NoError(t, err)

	lastReq := adapter.LastRequest()
	require.NotNil(t, lastReq.ProviderOptions)

	// Anthropic profile should pass through beta headers.
	anthropicOpts, ok := lastReq.ProviderOptions["anthropic"].(map[string]interface{})
	require.True(t, ok, "should have anthropic provider options")
	assert.NotNil(t, anthropicOpts["beta_headers"])
}

// TestIntegrationReasoningEffortPassThrough verifies that reasoning_effort
// is passed through to the LLM request (unifiedllm spec Section 3.6).
func TestIntegrationReasoningEffortPassThrough(t *testing.T) {
	adapter := &statefulAdapter{
		name: "anthropic",
		handler: func(req unifiedllm.Request) (*unifiedllm.Response, error) {
			return makeTextResponse("response"), nil
		},
	}

	profile := NewAnthropicProfile("claude-opus-4-6")
	client := unifiedllm.NewClient(
		unifiedllm.WithProvider("anthropic", adapter),
	)
	config := DefaultSessionConfig()
	config.ReasoningEffort = "high"
	s := NewSession(profile, newMockExecEnv("/tmp/test"), &config)
	s.SetClient(client)

	err := s.Submit(context.Background(), "hello")
	require.NoError(t, err)

	lastReq := adapter.LastRequest()
	assert.Equal(t, "high", lastReq.ReasoningEffort)
}

// TestIntegrationMultiToolCallHistoryBuildup verifies that multi-round
// tool conversations correctly accumulate in the message history sent to
// the LLM (integration: turns.go -> unifiedllm messages).
func TestIntegrationMultiToolCallHistoryBuildup(t *testing.T) {
	env := newMockExecEnv("/tmp/project")
	env.files["/tmp/project/a.txt"] = "aaa"
	env.files["/tmp/project/b.txt"] = "bbb"

	callNum := 0
	var requestsReceived []unifiedllm.Request

	adapter := &statefulAdapter{
		name: "anthropic",
		handler: func(req unifiedllm.Request) (*unifiedllm.Response, error) {
			requestsReceived = append(requestsReceived, req)
			callNum++
			switch callNum {
			case 1:
				return makeToolCallResponse(struct{ id, name, args string }{
					"call_1", "read_file", `{"file_path":"/tmp/project/a.txt"}`,
				}), nil
			case 2:
				return makeToolCallResponse(struct{ id, name, args string }{
					"call_2", "read_file", `{"file_path":"/tmp/project/b.txt"}`,
				}), nil
			default:
				return makeTextResponse("Done."), nil
			}
		},
	}

	profile := NewAnthropicProfile("claude-opus-4-6")
	client := unifiedllm.NewClient(
		unifiedllm.WithProvider("anthropic", adapter),
	)
	s := NewSession(profile, env, nil)
	s.SetClient(client)

	err := s.Submit(context.Background(), "read both files")
	require.NoError(t, err)

	// Verify second request includes full history: system + user + assistant(tc1) + tool_result(1).
	require.Len(t, requestsReceived, 3)

	// Third request should have: system + user + asst(tc1) + tool(1) + asst(tc2) + tool(2).
	thirdReq := requestsReceived[2]
	// Count messages by role (excluding system which is first).
	msgRoles := make(map[unifiedllm.Role]int)
	for _, msg := range thirdReq.Messages {
		msgRoles[msg.Role]++
	}
	assert.Equal(t, 1, msgRoles[unifiedllm.RoleUser])       // original user input
	assert.Equal(t, 2, msgRoles[unifiedllm.RoleAssistant])   // two assistant responses
	assert.Equal(t, 2, msgRoles[unifiedllm.RoleTool])        // two tool results
}

// TestIntegrationConversationEventSequence verifies the correct ordering
// of events emitted during a tool-use conversation (spec Section 2.9).
func TestIntegrationConversationEventSequence(t *testing.T) {
	adapter := &statefulAdapter{
		name: "anthropic",
		handler: func(req unifiedllm.Request) (*unifiedllm.Response, error) {
			return makeTextResponse("response"), nil
		},
	}

	profile := NewAnthropicProfile("claude-opus-4-6")
	client := unifiedllm.NewClient(
		unifiedllm.WithProvider("anthropic", adapter),
	)
	s := NewSession(profile, newMockExecEnv("/tmp/test"), nil)
	s.SetClient(client)

	events := s.Events()
	var eventSequence []EventKind
	var emu sync.Mutex
	done := make(chan struct{})

	go func() {
		defer close(done)
		for event := range events {
			emu.Lock()
			eventSequence = append(eventSequence, event.Kind)
			emu.Unlock()
		}
	}()

	err := s.Submit(context.Background(), "hello")
	require.NoError(t, err)

	// Close the session to close the event channel.
	s.Close()
	<-done

	emu.Lock()
	defer emu.Unlock()

	// Verify event sequence.
	require.NotEmpty(t, eventSequence)
	assert.Equal(t, EventUserInput, eventSequence[0])
	// Should have assistant text start.
	assert.Contains(t, eventSequence, EventAssistantTextStart)
	assert.Contains(t, eventSequence, EventAssistantTextEnd)
	// Should end with session_end.
	lastNonClose := eventSequence[len(eventSequence)-1]
	assert.Equal(t, EventSessionEnd, lastNonClose)
}

// TestIntegrationSystemPromptIncludesEnvironment verifies that the system
// prompt sent to the LLM includes working directory and model info
// (spec Section 6.1, integration: system_prompt.go -> unifiedllm.Request).
func TestIntegrationSystemPromptIncludesEnvironment(t *testing.T) {
	adapter := &statefulAdapter{
		name: "anthropic",
		handler: func(req unifiedllm.Request) (*unifiedllm.Response, error) {
			return makeTextResponse("response"), nil
		},
	}

	profile := NewAnthropicProfile("claude-opus-4-6")
	client := unifiedllm.NewClient(
		unifiedllm.WithProvider("anthropic", adapter),
	)
	s := NewSession(profile, newMockExecEnv("/workspace/project"), nil)
	s.SetClient(client)

	err := s.Submit(context.Background(), "hello")
	require.NoError(t, err)

	lastReq := adapter.LastRequest()
	require.NotEmpty(t, lastReq.Messages)

	// First message should be system prompt.
	systemMsg := lastReq.Messages[0]
	assert.Equal(t, unifiedllm.RoleSystem, systemMsg.Role)
	systemText := systemMsg.TextContent()
	assert.Contains(t, systemText, "/workspace/project")
}

// TestIntegrationUserInstructionsAppended verifies that user-configured
// instructions are appended to the system prompt (spec Section 6.6).
func TestIntegrationUserInstructionsAppended(t *testing.T) {
	adapter := &statefulAdapter{
		name: "anthropic",
		handler: func(req unifiedllm.Request) (*unifiedllm.Response, error) {
			return makeTextResponse("response"), nil
		},
	}

	profile := NewAnthropicProfile("claude-opus-4-6")
	client := unifiedllm.NewClient(
		unifiedllm.WithProvider("anthropic", adapter),
	)
	config := DefaultSessionConfig()
	config.UserInstructions = "Always use TypeScript. Never use var."
	s := NewSession(profile, newMockExecEnv("/tmp/test"), &config)
	s.SetClient(client)

	err := s.Submit(context.Background(), "write code")
	require.NoError(t, err)

	lastReq := adapter.LastRequest()
	systemText := lastReq.Messages[0].TextContent()
	assert.Contains(t, systemText, "Always use TypeScript")
	assert.Contains(t, systemText, "Never use var")
}

// TestIntegrationMiddlewareWithSession verifies that unifiedllm middleware
// is applied to requests from the agentloop session.
func TestIntegrationMiddlewareWithSession(t *testing.T) {
	var middlewareCalled bool

	adapter := &statefulAdapter{
		name: "anthropic",
		handler: func(req unifiedllm.Request) (*unifiedllm.Response, error) {
			return makeTextResponse("response"), nil
		},
	}

	mw := func(ctx context.Context, req unifiedllm.Request, next func(context.Context, unifiedllm.Request) (*unifiedllm.Response, error)) (*unifiedllm.Response, error) {
		middlewareCalled = true
		return next(ctx, req)
	}

	client := unifiedllm.NewClient(
		unifiedllm.WithProvider("anthropic", adapter),
		unifiedllm.WithDefaultProvider("anthropic"),
		unifiedllm.WithMiddleware(mw),
	)

	profile := NewAnthropicProfile("claude-opus-4-6")
	s := NewSession(profile, newMockExecEnv("/tmp/test"), nil)
	s.SetClient(client)

	err := s.Submit(context.Background(), "hello")
	require.NoError(t, err)

	assert.True(t, middlewareCalled, "middleware should be called for session requests")
}

// TestIntegrationToolErrorReportedAsToolResult verifies that when a tool
// execution fails, the error is reported as an error ToolResult back to
// the LLM (not a crash) per both specs.
func TestIntegrationToolErrorReportedAsToolResult(t *testing.T) {
	callNum := 0
	var secondRequest unifiedllm.Request

	adapter := &statefulAdapter{
		name: "anthropic",
		handler: func(req unifiedllm.Request) (*unifiedllm.Response, error) {
			callNum++
			if callNum == 1 {
				return makeToolCallResponse(struct{ id, name, args string }{
					"call_1", "read_file", `{"file_path":"/nonexistent/path"}`,
				}), nil
			}
			secondRequest = req
			return makeTextResponse("The file doesn't exist."), nil
		},
	}

	profile := NewAnthropicProfile("claude-opus-4-6")
	client := unifiedllm.NewClient(
		unifiedllm.WithProvider("anthropic", adapter),
	)
	s := NewSession(profile, newMockExecEnv("/tmp/test"), nil)
	s.SetClient(client)

	err := s.Submit(context.Background(), "read missing file")
	require.NoError(t, err)

	// The second request should contain the error tool result.
	var foundToolResult bool
	for _, msg := range secondRequest.Messages {
		if msg.Role == unifiedllm.RoleTool {
			foundToolResult = true
			for _, part := range msg.Content {
				if part.Kind == unifiedllm.ContentToolResult && part.ToolResult != nil {
					assert.True(t, part.ToolResult.IsError)
				}
			}
		}
	}
	assert.True(t, foundToolResult, "should have error tool result in second request")
}

// TestIntegrationAllThreeProfiles verifies that all three provider profiles
// can create sessions and build valid system prompts (spec Section 3).
func TestIntegrationAllThreeProfiles(t *testing.T) {
	profiles := []struct {
		name    string
		factory func() ProviderProfile
	}{
		{"anthropic", func() ProviderProfile { return NewAnthropicProfile("claude-opus-4-6") }},
		{"openai", func() ProviderProfile { return NewOpenAIProfile("gpt-5.2-codex") }},
		{"gemini", func() ProviderProfile { return NewGeminiProfile("gemini-3-flash-preview") }},
	}

	for _, pp := range profiles {
		t.Run(pp.name, func(t *testing.T) {
			profile := pp.factory()
			env := newMockExecEnv("/tmp/test")

			// Verify profile basics.
			assert.Equal(t, pp.name, profile.ID())
			assert.NotEmpty(t, profile.ModelID())
			assert.Greater(t, profile.ContextWindowSize(), 0)

			// Verify system prompt builds without error.
			prompt := profile.BuildSystemPrompt(env, "")
			assert.NotEmpty(t, prompt)

			// Verify tools are registered.
			tools := profile.Tools()
			assert.NotEmpty(t, tools)

			// Verify core tools exist.
			reg := profile.ToolRegistry()
			for _, toolName := range []string{"read_file", "write_file", "edit_file", "shell", "grep", "glob"} {
				assert.NotNil(t, reg.Get(toolName),
					fmt.Sprintf("%s profile should have %s tool", pp.name, toolName))
			}
		})
	}
}
