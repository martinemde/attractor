package agentloop

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// TestAnthropicProfileCreation verifies the Anthropic profile configuration
// per spec Section 3.5.
func TestAnthropicProfileCreation(t *testing.T) {
	p := NewAnthropicProfile("claude-opus-4-6")

	assert.Equal(t, "anthropic", p.ID())
	assert.Equal(t, "claude-opus-4-6", p.ModelID())
	assert.Equal(t, 200000, p.ContextWindowSize())
	assert.True(t, p.SupportsReasoning())
	assert.True(t, p.SupportsStreaming())
	assert.True(t, p.SupportsParallelToolCalls())
}

// TestAnthropicProfileCoreTools verifies that the Anthropic profile includes
// all shared core tools (spec Section 3.3).
func TestAnthropicProfileCoreTools(t *testing.T) {
	p := NewAnthropicProfile("claude-opus-4-6")
	reg := p.ToolRegistry()

	requiredTools := []string{"read_file", "write_file", "edit_file", "shell", "grep", "glob"}
	for _, name := range requiredTools {
		assert.NotNil(t, reg.Get(name), "Anthropic profile should have tool: %s", name)
	}

	// Anthropic should NOT have apply_patch (that's OpenAI-specific).
	assert.Nil(t, reg.Get("apply_patch"), "Anthropic profile should NOT have apply_patch")
}

// TestAnthropicProfileProviderOptions verifies beta headers are set.
func TestAnthropicProfileProviderOptions(t *testing.T) {
	p := NewAnthropicProfile("claude-opus-4-6")
	opts := p.ProviderOptions()

	require.NotNil(t, opts)
	anthropicOpts, ok := opts["anthropic"].(map[string]interface{})
	require.True(t, ok)

	headers, ok := anthropicOpts["beta_headers"].([]string)
	require.True(t, ok)
	assert.Contains(t, headers, "extended-thinking-2025-04-11")
}

// TestAnthropicProfileSystemPrompt verifies the system prompt includes
// required sections (spec Section 6.1).
func TestAnthropicProfileSystemPrompt(t *testing.T) {
	p := NewAnthropicProfile("claude-opus-4-6")
	env := newMockExecEnv("/tmp/test")

	prompt := p.BuildSystemPrompt(env, "project docs here")

	// Should contain base instructions.
	assert.Contains(t, prompt, "autonomous coding agent")
	// Should contain tool guidelines.
	assert.Contains(t, prompt, "edit_file")
	assert.Contains(t, prompt, "old_string")
	// Should contain environment context.
	assert.Contains(t, prompt, "/tmp/test")
	// Should contain project docs.
	assert.Contains(t, prompt, "project docs here")
	// Should contain tool descriptions.
	assert.Contains(t, prompt, "Available Tools")
}

// TestOpenAIProfileCreation verifies the OpenAI profile configuration
// per spec Section 3.4.
func TestOpenAIProfileCreation(t *testing.T) {
	p := NewOpenAIProfile("gpt-5.2-codex")

	assert.Equal(t, "openai", p.ID())
	assert.Equal(t, "gpt-5.2-codex", p.ModelID())
	assert.Equal(t, 1047576, p.ContextWindowSize())
	assert.True(t, p.SupportsReasoning())
	assert.True(t, p.SupportsStreaming())
	assert.True(t, p.SupportsParallelToolCalls())
}

// TestOpenAIProfileCoreToolsPlusApplyPatch verifies OpenAI profile has
// both core tools and the apply_patch tool (spec Section 3.4).
func TestOpenAIProfileCoreToolsPlusApplyPatch(t *testing.T) {
	p := NewOpenAIProfile("gpt-5.2-codex")
	reg := p.ToolRegistry()

	// Should have all core tools.
	requiredTools := []string{"read_file", "write_file", "edit_file", "shell", "grep", "glob"}
	for _, name := range requiredTools {
		assert.NotNil(t, reg.Get(name), "OpenAI profile should have tool: %s", name)
	}

	// OpenAI profile should also have apply_patch.
	assert.NotNil(t, reg.Get("apply_patch"), "OpenAI profile should have apply_patch")
}

// TestOpenAIProfileSystemPrompt verifies the system prompt references
// apply_patch and v4a format (spec Section 3.4).
func TestOpenAIProfileSystemPrompt(t *testing.T) {
	p := NewOpenAIProfile("gpt-5.2-codex")
	env := newMockExecEnv("/tmp/test")

	prompt := p.BuildSystemPrompt(env, "")

	assert.Contains(t, prompt, "apply_patch")
	assert.Contains(t, prompt, "v4a")
	assert.Contains(t, prompt, "Available Tools")
}

// TestGeminiProfileCreation verifies the Gemini profile configuration
// per spec Section 3.6.
func TestGeminiProfileCreation(t *testing.T) {
	p := NewGeminiProfile("gemini-3-flash-preview")

	assert.Equal(t, "gemini", p.ID())
	assert.Equal(t, "gemini-3-flash-preview", p.ModelID())
	assert.Equal(t, 1048576, p.ContextWindowSize())
	assert.True(t, p.SupportsReasoning())
	assert.True(t, p.SupportsStreaming())
	assert.True(t, p.SupportsParallelToolCalls())
}

// TestGeminiProfileCoreTools verifies the Gemini profile includes
// all shared core tools.
func TestGeminiProfileCoreTools(t *testing.T) {
	p := NewGeminiProfile("gemini-3-flash-preview")
	reg := p.ToolRegistry()

	requiredTools := []string{"read_file", "write_file", "edit_file", "shell", "grep", "glob"}
	for _, name := range requiredTools {
		assert.NotNil(t, reg.Get(name), "Gemini profile should have tool: %s", name)
	}
}

// TestGeminiProfileProviderOptions verifies Gemini-specific options.
func TestGeminiProfileProviderOptions(t *testing.T) {
	p := NewGeminiProfile("gemini-3-flash-preview")
	opts := p.ProviderOptions()
	require.NotNil(t, opts)
	geminiOpts, ok := opts["gemini"].(map[string]interface{})
	require.True(t, ok)
	assert.NotNil(t, geminiOpts["safety_settings"])
}

// TestCustomToolRegistration verifies that custom tools can be registered
// on top of any profile and that name collisions are resolved by latest-wins
// (spec Section 3.7).
func TestCustomToolRegistration(t *testing.T) {
	p := NewAnthropicProfile("claude-opus-4-6")
	reg := p.ToolRegistry()

	// Get original description.
	origTool := reg.Get("read_file")
	require.NotNil(t, origTool)
	origDesc := origTool.Definition.Description

	// Override with custom tool.
	reg.Register(RegisteredTool{
		Definition: ToolDefinition{
			Name:        "read_file",
			Description: "Custom read_file override",
			Parameters:  map[string]interface{}{"type": "object"},
		},
	})

	// Should have the new description.
	got := reg.Get("read_file")
	require.NotNil(t, got)
	assert.Equal(t, "Custom read_file override", got.Definition.Description)
	assert.NotEqual(t, origDesc, got.Definition.Description)
}

// TestBaseProfileDefaults verifies BaseProfile method implementations.
func TestBaseProfileDefaults(t *testing.T) {
	bp := &BaseProfile{
		providerID:                "test",
		model:                     "test-model",
		registry:                  NewToolRegistry(),
		supportsReasoning:         true,
		supportsStreaming:          false,
		supportsParallelToolCalls: true,
		contextWindowSize:         100000,
	}

	assert.Equal(t, "test", bp.ID())
	assert.Equal(t, "test-model", bp.ModelID())
	assert.NotNil(t, bp.ToolRegistry())
	assert.True(t, bp.SupportsReasoning())
	assert.False(t, bp.SupportsStreaming())
	assert.True(t, bp.SupportsParallelToolCalls())
	assert.Equal(t, 100000, bp.ContextWindowSize())
	assert.Nil(t, bp.ProviderOptions())
}
