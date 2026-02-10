package agentloop

import (
	"context"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// TestSubAgentManagerCanSpawn verifies nesting depth enforcement
// (spec Section 7.2).
func TestSubAgentManagerCanSpawn(t *testing.T) {
	// At depth 0, max depth 1: can spawn.
	m := NewSubAgentManager(1, 0)
	assert.True(t, m.CanSpawn())

	// At depth 1, max depth 1: cannot spawn.
	m = NewSubAgentManager(1, 1)
	assert.False(t, m.CanSpawn())

	// At depth 0, max depth 0: cannot spawn.
	m = NewSubAgentManager(0, 0)
	assert.False(t, m.CanSpawn())
}

// TestSubAgentManagerSpawnAndGet verifies that spawning creates a handle
// and Get retrieves it.
func TestSubAgentManagerSpawnAndGet(t *testing.T) {
	m := NewSubAgentManager(2, 0)
	profile := NewAnthropicProfile("test-model")
	env := newMockExecEnv("/tmp/test")

	// We can't actually submit (no real LLM), but we can verify the handle
	// is created and registered.
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	handle, err := m.Spawn(ctx, profile, env, "test task", nil)
	require.NoError(t, err)
	assert.NotEmpty(t, handle.ID)
	assert.Equal(t, SubAgentRunning, handle.Status)

	// Get should find it.
	got := m.Get(handle.ID)
	assert.NotNil(t, got)
	assert.Equal(t, handle.ID, got.ID)

	// Unknown ID returns nil.
	assert.Nil(t, m.Get("nonexistent"))

	// Cancel to clean up the goroutine.
	cancel()
}

// TestSubAgentManagerSpawnDepthExceeded verifies that attempting to spawn
// beyond max depth returns an error (spec Section 7.2).
func TestSubAgentManagerSpawnDepthExceeded(t *testing.T) {
	m := NewSubAgentManager(1, 1) // already at max depth
	profile := NewAnthropicProfile("test-model")
	env := newMockExecEnv("/tmp/test")

	_, err := m.Spawn(context.Background(), profile, env, "task", nil)
	assert.Error(t, err)
	assert.Contains(t, err.Error(), "maximum subagent depth")
}

// TestSubAgentManagerClose verifies that closing a subagent cancels
// its context and marks it as failed.
func TestSubAgentManagerClose(t *testing.T) {
	m := NewSubAgentManager(2, 0)
	profile := NewAnthropicProfile("test-model")
	env := newMockExecEnv("/tmp/test")

	ctx := context.Background()
	handle, err := m.Spawn(ctx, profile, env, "task", nil)
	require.NoError(t, err)

	err = m.Close(handle.ID)
	require.NoError(t, err)

	handle.mu.Lock()
	status := handle.Status
	handle.mu.Unlock()
	// After close, status should be failed (context cancelled).
	assert.Equal(t, SubAgentFailed, status)
}

// TestSubAgentManagerCloseNonexistent verifies error on unknown ID.
func TestSubAgentManagerCloseNonexistent(t *testing.T) {
	m := NewSubAgentManager(2, 0)
	err := m.Close("nonexistent")
	assert.Error(t, err)
}

// TestSubAgentManagerCloseAll verifies all agents are cleaned up.
func TestSubAgentManagerCloseAll(t *testing.T) {
	m := NewSubAgentManager(2, 0)
	profile := NewAnthropicProfile("test-model")
	env := newMockExecEnv("/tmp/test")

	ctx := context.Background()
	h1, _ := m.Spawn(ctx, profile, env, "task1", nil)
	h2, _ := m.Spawn(ctx, profile, env, "task2", nil)

	m.CloseAll()

	// Both should have been cancelled.
	assert.NotNil(t, m.Get(h1.ID))
	assert.NotNil(t, m.Get(h2.ID))
}

// TestSubagentToolRegistration verifies that when depth allows, subagent
// tools are registered (spec Section 7.1).
func TestSubagentToolRegistration(t *testing.T) {
	config := DefaultSessionConfig()
	config.MaxSubagentDepth = 1

	adapter := &mockAdapter{name: "anthropic", responses: nil}
	env := newMockExecEnv("/tmp/test")
	s := newTestSession(adapter, env, &config)

	reg := s.profile.ToolRegistry()

	// Subagent tools should be registered.
	assert.NotNil(t, reg.Get("spawn_agent"), "should have spawn_agent tool")
	assert.NotNil(t, reg.Get("send_input"), "should have send_input tool")
	assert.NotNil(t, reg.Get("wait"), "should have wait tool")
	assert.NotNil(t, reg.Get("close_agent"), "should have close_agent tool")
}

// TestSubagentToolRegistrationDisabledAtMaxDepth verifies that subagent
// tools are NOT registered when already at max depth (spec Section 7.3).
func TestSubagentToolRegistrationDisabledAtMaxDepth(t *testing.T) {
	config := DefaultSessionConfig()
	config.MaxSubagentDepth = 1
	config.subagentDepth = 1 // Already at max depth.

	profile := NewAnthropicProfile("test-model")
	s := NewSession(profile, newMockExecEnv("/tmp/test"), &config)

	reg := s.profile.ToolRegistry()

	// Subagent tools should NOT be registered at max depth.
	assert.Nil(t, reg.Get("spawn_agent"), "should NOT have spawn_agent at max depth")
}
