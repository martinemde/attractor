package pipeline

import (
	"sync"
	"testing"
	"time"

	"github.com/martinemde/attractor/dotparser"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestEventEmitter_RegistersAndCallsListeners(t *testing.T) {
	emitter := NewEventEmitter()

	var received []Event
	emitter.On(func(e Event) {
		received = append(received, e)
	})

	event := Event{
		Type:      EventStageStarted,
		Timestamp: time.Now(),
		Data:      map[string]any{"name": "test-stage", "index": 0},
	}
	emitter.Emit(event)

	require.Len(t, received, 1)
	assert.Equal(t, EventStageStarted, received[0].Type)
	assert.Equal(t, "test-stage", received[0].Data["name"])
	assert.Equal(t, 0, received[0].Data["index"])
}

func TestEventEmitter_MultipleListeners(t *testing.T) {
	emitter := NewEventEmitter()

	var count1, count2 int
	emitter.On(func(e Event) { count1++ })
	emitter.On(func(e Event) { count2++ })

	emitter.Emit(Event{Type: EventStageStarted, Timestamp: time.Now()})
	emitter.Emit(Event{Type: EventStageCompleted, Timestamp: time.Now()})

	assert.Equal(t, 2, count1)
	assert.Equal(t, 2, count2)
}

func TestEventEmitter_ConcurrentAccess(t *testing.T) {
	emitter := NewEventEmitter()

	var mu sync.Mutex
	var received []Event

	// Register multiple listeners concurrently
	var wg sync.WaitGroup
	for i := range 10 {
		wg.Add(1)
		go func(idx int) {
			defer wg.Done()
			emitter.On(func(e Event) {
				mu.Lock()
				received = append(received, e)
				mu.Unlock()
			})
		}(i)
	}
	wg.Wait()

	// Emit events concurrently
	for range 5 {
		wg.Add(1)
		go func() {
			defer wg.Done()
			emitter.Emit(Event{Type: EventStageStarted, Timestamp: time.Now()})
		}()
	}
	wg.Wait()

	// Should have 10 listeners * 5 events = 50 total received
	assert.Equal(t, 50, len(received))
}

func TestEventEmitter_ListenerCount(t *testing.T) {
	emitter := NewEventEmitter()

	assert.Equal(t, 0, emitter.ListenerCount())

	emitter.On(func(e Event) {})
	assert.Equal(t, 1, emitter.ListenerCount())

	emitter.On(func(e Event) {})
	emitter.On(func(e Event) {})
	assert.Equal(t, 3, emitter.ListenerCount())
}

func TestPipelineStartedEvent(t *testing.T) {
	event := PipelineStartedEvent("test-pipeline", "run-123")

	assert.Equal(t, EventPipelineStarted, event.Type)
	assert.Equal(t, "test-pipeline", event.Data["name"])
	assert.Equal(t, "run-123", event.Data["id"])
	assert.False(t, event.Timestamp.IsZero())
}

func TestPipelineCompletedEvent(t *testing.T) {
	event := PipelineCompletedEvent(5*time.Second, 10)

	assert.Equal(t, EventPipelineCompleted, event.Type)
	assert.Equal(t, int64(5000), event.Data["duration_ms"])
	assert.Equal(t, 10, event.Data["artifact_count"])
}

func TestPipelineFailedEvent(t *testing.T) {
	event := PipelineFailedEvent("something went wrong", 3*time.Second)

	assert.Equal(t, EventPipelineFailed, event.Type)
	assert.Equal(t, "something went wrong", event.Data["error"])
	assert.Equal(t, int64(3000), event.Data["duration_ms"])
}

func TestStageStartedEvent(t *testing.T) {
	event := StageStartedEvent("my-stage", 5)

	assert.Equal(t, EventStageStarted, event.Type)
	assert.Equal(t, "my-stage", event.Data["name"])
	assert.Equal(t, 5, event.Data["index"])
}

func TestStageCompletedEvent(t *testing.T) {
	event := StageCompletedEvent("my-stage", 5, 2*time.Second)

	assert.Equal(t, EventStageCompleted, event.Type)
	assert.Equal(t, "my-stage", event.Data["name"])
	assert.Equal(t, 5, event.Data["index"])
	assert.Equal(t, int64(2000), event.Data["duration_ms"])
}

func TestStageFailedEvent(t *testing.T) {
	event := StageFailedEvent("my-stage", 5, "handler error", true)

	assert.Equal(t, EventStageFailed, event.Type)
	assert.Equal(t, "my-stage", event.Data["name"])
	assert.Equal(t, 5, event.Data["index"])
	assert.Equal(t, "handler error", event.Data["error"])
	assert.Equal(t, true, event.Data["will_retry"])
}

func TestStageRetryingEvent(t *testing.T) {
	event := StageRetryingEvent("my-stage", 5, 2, 500*time.Millisecond)

	assert.Equal(t, EventStageRetrying, event.Type)
	assert.Equal(t, "my-stage", event.Data["name"])
	assert.Equal(t, 5, event.Data["index"])
	assert.Equal(t, 2, event.Data["attempt"])
	assert.Equal(t, int64(500), event.Data["delay_ms"])
}

func TestParallelStartedEvent(t *testing.T) {
	event := ParallelStartedEvent(4)

	assert.Equal(t, EventParallelStarted, event.Type)
	assert.Equal(t, 4, event.Data["branch_count"])
}

func TestParallelBranchStartedEvent(t *testing.T) {
	event := ParallelBranchStartedEvent("branch-a", 0)

	assert.Equal(t, EventParallelBranchStarted, event.Type)
	assert.Equal(t, "branch-a", event.Data["branch"])
	assert.Equal(t, 0, event.Data["index"])
}

func TestParallelBranchCompletedEvent(t *testing.T) {
	event := ParallelBranchCompletedEvent("branch-a", 0, 1*time.Second, true)

	assert.Equal(t, EventParallelBranchCompleted, event.Type)
	assert.Equal(t, "branch-a", event.Data["branch"])
	assert.Equal(t, 0, event.Data["index"])
	assert.Equal(t, int64(1000), event.Data["duration_ms"])
	assert.Equal(t, true, event.Data["success"])
}

func TestParallelCompletedEvent(t *testing.T) {
	event := ParallelCompletedEvent(5*time.Second, 3, 1)

	assert.Equal(t, EventParallelCompleted, event.Type)
	assert.Equal(t, int64(5000), event.Data["duration_ms"])
	assert.Equal(t, 3, event.Data["success_count"])
	assert.Equal(t, 1, event.Data["failure_count"])
}

func TestInterviewStartedEvent(t *testing.T) {
	event := InterviewStartedEvent("Do you approve?", "review-gate")

	assert.Equal(t, EventInterviewStarted, event.Type)
	assert.Equal(t, "Do you approve?", event.Data["question"])
	assert.Equal(t, "review-gate", event.Data["stage"])
}

func TestInterviewCompletedEvent(t *testing.T) {
	event := InterviewCompletedEvent("Do you approve?", "yes", 10*time.Second)

	assert.Equal(t, EventInterviewCompleted, event.Type)
	assert.Equal(t, "Do you approve?", event.Data["question"])
	assert.Equal(t, "yes", event.Data["answer"])
	assert.Equal(t, int64(10000), event.Data["duration_ms"])
}

func TestInterviewTimeoutEvent(t *testing.T) {
	event := InterviewTimeoutEvent("Do you approve?", "review-gate", 30*time.Second)

	assert.Equal(t, EventInterviewTimeout, event.Type)
	assert.Equal(t, "Do you approve?", event.Data["question"])
	assert.Equal(t, "review-gate", event.Data["stage"])
	assert.Equal(t, int64(30000), event.Data["duration_ms"])
}

func TestCheckpointSavedEvent(t *testing.T) {
	event := CheckpointSavedEvent("node-123")

	assert.Equal(t, EventCheckpointSaved, event.Type)
	assert.Equal(t, "node-123", event.Data["node_id"])
}

func TestEventsEmittedDuringPipelineExecution(t *testing.T) {
	// Create a simple pipeline
	graph := newTestGraph(
		[]*dotparser.Node{
			newNode("start", strAttr("shape", "Mdiamond")),
			newNode("task", strAttr("type", "mock")),
			newNode("exit", strAttr("shape", "Msquare")),
		},
		[]*dotparser.Edge{
			newEdge("start", "task"),
			newEdge("task", "exit"),
		},
		nil,
	)

	// Set up event collector
	emitter := NewEventEmitter()
	var events []Event
	var mu sync.Mutex
	emitter.On(func(e Event) {
		mu.Lock()
		events = append(events, e)
		mu.Unlock()
	})

	// Set up mock handler
	mockHandler := &MockHandler{
		Outcomes: []*Outcome{Success()},
	}
	registry := DefaultRegistry()
	registry.Register("mock", mockHandler)

	// Run pipeline with event emitter
	_, err := Run(graph, &RunConfig{
		Registry:     registry,
		EventEmitter: emitter,
	})

	require.NoError(t, err)

	// Verify events were emitted
	mu.Lock()
	defer mu.Unlock()

	// Should have: pipeline_started, stage_started(start), stage_completed(start),
	// stage_started(task), stage_completed(task), pipeline_completed
	require.GreaterOrEqual(t, len(events), 5, "expected at least 5 events")

	// First event should be pipeline_started
	assert.Equal(t, EventPipelineStarted, events[0].Type)

	// Last event should be pipeline_completed
	assert.Equal(t, EventPipelineCompleted, events[len(events)-1].Type)

	// Check for stage events
	stageStartedCount := 0
	stageCompletedCount := 0
	for _, e := range events {
		if e.Type == EventStageStarted {
			stageStartedCount++
		}
		if e.Type == EventStageCompleted {
			stageCompletedCount++
		}
	}
	assert.GreaterOrEqual(t, stageStartedCount, 2)   // start and task
	assert.GreaterOrEqual(t, stageCompletedCount, 2) // start and task
}

func TestEventsContainExpectedFields(t *testing.T) {
	graph := newTestGraph(
		[]*dotparser.Node{
			newNode("start", strAttr("shape", "Mdiamond")),
			newNode("exit", strAttr("shape", "Msquare")),
		},
		[]*dotparser.Edge{
			newEdge("start", "exit"),
		},
		nil,
	)
	graph.Name = "test-pipeline"

	emitter := NewEventEmitter()
	var startEvent *Event
	emitter.On(func(e Event) {
		if e.Type == EventPipelineStarted {
			startEvent = &e
		}
	})

	_, err := Run(graph, &RunConfig{EventEmitter: emitter})
	require.NoError(t, err)

	require.NotNil(t, startEvent)
	assert.Equal(t, "test-pipeline", startEvent.Data["name"])
	assert.Contains(t, startEvent.Data["id"].(string), "run-")
}
