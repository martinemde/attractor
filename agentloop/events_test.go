package agentloop

import (
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// TestEventEmitterEmit verifies that events are delivered through the channel.
func TestEventEmitterEmit(t *testing.T) {
	emitter := NewEventEmitter("session-1", 10)
	defer emitter.Close()

	emitter.Emit(EventUserInput, map[string]interface{}{
		"content": "hello",
	})

	select {
	case event := <-emitter.Events():
		assert.Equal(t, EventUserInput, event.Kind)
		assert.Equal(t, "session-1", event.SessionID)
		assert.Equal(t, "hello", event.Data["content"])
		assert.False(t, event.Timestamp.IsZero())
	case <-time.After(time.Second):
		t.Fatal("timed out waiting for event")
	}
}

// TestEventEmitterAllEventKinds verifies all spec-defined event kinds exist
// (spec Section 2.9).
func TestEventEmitterAllEventKinds(t *testing.T) {
	expectedKinds := []EventKind{
		EventSessionStart,
		EventSessionEnd,
		EventUserInput,
		EventAssistantTextStart,
		EventAssistantTextDelta,
		EventAssistantTextEnd,
		EventToolCallStart,
		EventToolCallOutputDelta,
		EventToolCallEnd,
		EventSteeringInjected,
		EventTurnLimit,
		EventLoopDetection,
		EventWarning,
		EventError,
	}

	for _, kind := range expectedKinds {
		assert.NotEmpty(t, string(kind), "event kind should have a non-empty string value")
	}
}

// TestEventEmitterClose verifies that the emitter can be closed and that
// subsequent emits are silently dropped.
func TestEventEmitterClose(t *testing.T) {
	emitter := NewEventEmitter("session-1", 10)
	emitter.Close()

	// Should not panic.
	emitter.Emit(EventError, map[string]interface{}{
		"error": "after close",
	})

	// Double close should not panic.
	emitter.Close()
}

// TestEventEmitterDropsWhenFull verifies that the emitter doesn't block
// when the channel is full (non-blocking emit).
func TestEventEmitterDropsWhenFull(t *testing.T) {
	emitter := NewEventEmitter("session-1", 2)
	defer emitter.Close()

	// Fill the buffer.
	emitter.Emit(EventUserInput, nil)
	emitter.Emit(EventUserInput, nil)

	// This should not block (drops event silently).
	done := make(chan struct{})
	go func() {
		emitter.Emit(EventUserInput, nil)
		close(done)
	}()

	select {
	case <-done:
		// Good, didn't block.
	case <-time.After(time.Second):
		t.Fatal("emitter blocked when channel was full")
	}
}

// TestEventEmitterDefaultBufferSize verifies that zero/negative buffer size
// gets a default of 256.
func TestEventEmitterDefaultBufferSize(t *testing.T) {
	emitter := NewEventEmitter("test", 0)
	defer emitter.Close()

	// Verify we can emit at least a few events.
	for i := 0; i < 10; i++ {
		emitter.Emit(EventUserInput, nil)
	}

	// Drain them all.
	for i := 0; i < 10; i++ {
		select {
		case <-emitter.Events():
		case <-time.After(time.Second):
			t.Fatal("failed to drain events")
		}
	}
}

// TestEventEmitterEventDataNil verifies events with nil data work correctly.
func TestEventEmitterEventDataNil(t *testing.T) {
	emitter := NewEventEmitter("session-1", 10)
	defer emitter.Close()

	emitter.Emit(EventSessionStart, nil)

	event := <-emitter.Events()
	assert.Equal(t, EventSessionStart, event.Kind)
	assert.Nil(t, event.Data)
}

// drainEvents is a test helper that reads all available events from the
// emitter channel within a timeout.
func drainEvents(ch <-chan SessionEvent, timeout time.Duration) []SessionEvent {
	var events []SessionEvent
	deadline := time.After(timeout)
	for {
		select {
		case event, ok := <-ch:
			if !ok {
				return events
			}
			events = append(events, event)
		case <-deadline:
			return events
		}
	}
}

// TestDrainEventsHelper verifies the test helper works.
func TestDrainEventsHelper(t *testing.T) {
	emitter := NewEventEmitter("test", 10)
	emitter.Emit(EventUserInput, nil)
	emitter.Emit(EventSessionEnd, nil)

	events := drainEvents(emitter.Events(), 100*time.Millisecond)
	require.Len(t, events, 2)
	assert.Equal(t, EventUserInput, events[0].Kind)
	assert.Equal(t, EventSessionEnd, events[1].Kind)
}
