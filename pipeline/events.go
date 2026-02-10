package pipeline

import (
	"sync"
	"time"
)

// EventType represents the type of pipeline event.
type EventType string

const (
	// Pipeline lifecycle events
	EventPipelineStarted   EventType = "pipeline_started"
	EventPipelineCompleted EventType = "pipeline_completed"
	EventPipelineFailed    EventType = "pipeline_failed"

	// Stage lifecycle events
	EventStageStarted   EventType = "stage_started"
	EventStageCompleted EventType = "stage_completed"
	EventStageFailed    EventType = "stage_failed"
	EventStageRetrying  EventType = "stage_retrying"

	// Parallel execution events
	EventParallelStarted         EventType = "parallel_started"
	EventParallelBranchStarted   EventType = "parallel_branch_started"
	EventParallelBranchCompleted EventType = "parallel_branch_completed"
	EventParallelCompleted       EventType = "parallel_completed"

	// Human interaction events
	EventInterviewStarted   EventType = "interview_started"
	EventInterviewCompleted EventType = "interview_completed"
	EventInterviewTimeout   EventType = "interview_timeout"

	// Checkpoint events
	EventCheckpointSaved EventType = "checkpoint_saved"
)

// Event represents an observable pipeline event with typed data.
type Event struct {
	Type      EventType      `json:"type"`
	Timestamp time.Time      `json:"timestamp"`
	Data      map[string]any `json:"data"`
}

// EventEmitter manages event listeners and dispatches events.
type EventEmitter struct {
	mu        sync.RWMutex
	listeners []func(Event)
}

// NewEventEmitter creates a new EventEmitter.
func NewEventEmitter() *EventEmitter {
	return &EventEmitter{
		listeners: make([]func(Event), 0),
	}
}

// On registers a listener function to receive events.
// Listeners are called synchronously in registration order.
func (e *EventEmitter) On(listener func(Event)) {
	e.mu.Lock()
	defer e.mu.Unlock()
	e.listeners = append(e.listeners, listener)
}

// Emit dispatches an event to all registered listeners.
// Listeners are called synchronously in registration order.
func (e *EventEmitter) Emit(event Event) {
	e.mu.RLock()
	listeners := make([]func(Event), len(e.listeners))
	copy(listeners, e.listeners)
	e.mu.RUnlock()

	for _, listener := range listeners {
		listener(event)
	}
}

// ListenerCount returns the number of registered listeners.
func (e *EventEmitter) ListenerCount() int {
	e.mu.RLock()
	defer e.mu.RUnlock()
	return len(e.listeners)
}

// Helper constructors for creating typed events

// PipelineStartedEvent creates a pipeline_started event.
func PipelineStartedEvent(name, id string) Event {
	return Event{
		Type:      EventPipelineStarted,
		Timestamp: time.Now(),
		Data: map[string]any{
			"name": name,
			"id":   id,
		},
	}
}

// PipelineCompletedEvent creates a pipeline_completed event.
func PipelineCompletedEvent(duration time.Duration, artifactCount int) Event {
	return Event{
		Type:      EventPipelineCompleted,
		Timestamp: time.Now(),
		Data: map[string]any{
			"duration_ms":    duration.Milliseconds(),
			"artifact_count": artifactCount,
		},
	}
}

// PipelineFailedEvent creates a pipeline_failed event.
func PipelineFailedEvent(err string, duration time.Duration) Event {
	return Event{
		Type:      EventPipelineFailed,
		Timestamp: time.Now(),
		Data: map[string]any{
			"error":       err,
			"duration_ms": duration.Milliseconds(),
		},
	}
}

// StageStartedEvent creates a stage_started event.
func StageStartedEvent(name string, index int) Event {
	return Event{
		Type:      EventStageStarted,
		Timestamp: time.Now(),
		Data: map[string]any{
			"name":  name,
			"index": index,
		},
	}
}

// StageCompletedEvent creates a stage_completed event.
func StageCompletedEvent(name string, index int, duration time.Duration) Event {
	return Event{
		Type:      EventStageCompleted,
		Timestamp: time.Now(),
		Data: map[string]any{
			"name":        name,
			"index":       index,
			"duration_ms": duration.Milliseconds(),
		},
	}
}

// StageFailedEvent creates a stage_failed event.
func StageFailedEvent(name string, index int, err string, willRetry bool) Event {
	return Event{
		Type:      EventStageFailed,
		Timestamp: time.Now(),
		Data: map[string]any{
			"name":       name,
			"index":      index,
			"error":      err,
			"will_retry": willRetry,
		},
	}
}

// StageRetryingEvent creates a stage_retrying event.
func StageRetryingEvent(name string, index, attempt int, delay time.Duration) Event {
	return Event{
		Type:      EventStageRetrying,
		Timestamp: time.Now(),
		Data: map[string]any{
			"name":     name,
			"index":    index,
			"attempt":  attempt,
			"delay_ms": delay.Milliseconds(),
		},
	}
}

// ParallelStartedEvent creates a parallel_started event.
func ParallelStartedEvent(branchCount int) Event {
	return Event{
		Type:      EventParallelStarted,
		Timestamp: time.Now(),
		Data: map[string]any{
			"branch_count": branchCount,
		},
	}
}

// ParallelBranchStartedEvent creates a parallel_branch_started event.
func ParallelBranchStartedEvent(branch string, index int) Event {
	return Event{
		Type:      EventParallelBranchStarted,
		Timestamp: time.Now(),
		Data: map[string]any{
			"branch": branch,
			"index":  index,
		},
	}
}

// ParallelBranchCompletedEvent creates a parallel_branch_completed event.
func ParallelBranchCompletedEvent(branch string, index int, duration time.Duration, success bool) Event {
	return Event{
		Type:      EventParallelBranchCompleted,
		Timestamp: time.Now(),
		Data: map[string]any{
			"branch":      branch,
			"index":       index,
			"duration_ms": duration.Milliseconds(),
			"success":     success,
		},
	}
}

// ParallelCompletedEvent creates a parallel_completed event.
func ParallelCompletedEvent(duration time.Duration, successCount, failureCount int) Event {
	return Event{
		Type:      EventParallelCompleted,
		Timestamp: time.Now(),
		Data: map[string]any{
			"duration_ms":   duration.Milliseconds(),
			"success_count": successCount,
			"failure_count": failureCount,
		},
	}
}

// InterviewStartedEvent creates an interview_started event.
func InterviewStartedEvent(question, stage string) Event {
	return Event{
		Type:      EventInterviewStarted,
		Timestamp: time.Now(),
		Data: map[string]any{
			"question": question,
			"stage":    stage,
		},
	}
}

// InterviewCompletedEvent creates an interview_completed event.
func InterviewCompletedEvent(question, answer string, duration time.Duration) Event {
	return Event{
		Type:      EventInterviewCompleted,
		Timestamp: time.Now(),
		Data: map[string]any{
			"question":    question,
			"answer":      answer,
			"duration_ms": duration.Milliseconds(),
		},
	}
}

// InterviewTimeoutEvent creates an interview_timeout event.
func InterviewTimeoutEvent(question, stage string, duration time.Duration) Event {
	return Event{
		Type:      EventInterviewTimeout,
		Timestamp: time.Now(),
		Data: map[string]any{
			"question":    question,
			"stage":       stage,
			"duration_ms": duration.Milliseconds(),
		},
	}
}

// CheckpointSavedEvent creates a checkpoint_saved event.
func CheckpointSavedEvent(nodeID string) Event {
	return Event{
		Type:      EventCheckpointSaved,
		Timestamp: time.Now(),
		Data: map[string]any{
			"node_id": nodeID,
		},
	}
}
