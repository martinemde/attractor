// Package pipeline provides core data structures for the Attractor pipeline engine.
package pipeline

// StageStatus represents the result status of executing a node handler.
type StageStatus string

const (
	// StatusSuccess indicates the stage completed its work. Proceed to next edge.
	StatusSuccess StageStatus = "success"

	// StatusFail indicates the stage failed permanently.
	StatusFail StageStatus = "fail"

	// StatusPartialSuccess indicates the stage completed with caveats.
	// Treated as success for routing but notes describe what was incomplete.
	StatusPartialSuccess StageStatus = "partial_success"

	// StatusRetry indicates the stage requests re-execution.
	StatusRetry StageStatus = "retry"

	// StatusSkipped indicates the stage was skipped (e.g., condition not met).
	StatusSkipped StageStatus = "skipped"
)

// String returns the string representation of the status.
func (s StageStatus) String() string {
	return string(s)
}

// IsSuccess returns true if the status represents a successful completion.
func (s StageStatus) IsSuccess() bool {
	return s == StatusSuccess || s == StatusPartialSuccess
}

// Outcome is the result of executing a node handler.
// It drives routing decisions and state updates.
type Outcome struct {
	// Status is the execution result status.
	Status StageStatus

	// PreferredLabel indicates which edge label to follow (optional).
	// Used in edge selection when routing.
	PreferredLabel string

	// SuggestedNextIDs provides explicit next node IDs (optional).
	// Used when preferred label matching fails.
	SuggestedNextIDs []string

	// ContextUpdates contains key-value pairs to merge into the context.
	ContextUpdates map[string]any

	// Notes is a human-readable execution summary.
	Notes string

	// FailureReason explains why the stage failed (when Status is FAIL or RETRY).
	FailureReason string
}

// NewOutcome creates a new Outcome with the given status.
func NewOutcome(status StageStatus) *Outcome {
	return &Outcome{
		Status:         status,
		ContextUpdates: make(map[string]any),
	}
}

// Success creates a successful outcome.
func Success() *Outcome {
	return NewOutcome(StatusSuccess)
}

// Fail creates a failed outcome with the given reason.
func Fail(reason string) *Outcome {
	o := NewOutcome(StatusFail)
	o.FailureReason = reason
	return o
}

// PartialSuccess creates a partial success outcome with notes.
func PartialSuccess(notes string) *Outcome {
	o := NewOutcome(StatusPartialSuccess)
	o.Notes = notes
	return o
}

// Retry creates a retry outcome with the given reason.
func Retry(reason string) *Outcome {
	o := NewOutcome(StatusRetry)
	o.FailureReason = reason
	return o
}

// Skipped creates a skipped outcome.
func Skipped() *Outcome {
	return NewOutcome(StatusSkipped)
}

// WithPreferredLabel sets the preferred label for edge selection.
func (o *Outcome) WithPreferredLabel(label string) *Outcome {
	o.PreferredLabel = label
	return o
}

// WithSuggestedNextIDs sets the suggested next node IDs.
func (o *Outcome) WithSuggestedNextIDs(ids ...string) *Outcome {
	o.SuggestedNextIDs = ids
	return o
}

// WithContextUpdate adds a key-value pair to the context updates.
func (o *Outcome) WithContextUpdate(key string, value any) *Outcome {
	if o.ContextUpdates == nil {
		o.ContextUpdates = make(map[string]any)
	}
	o.ContextUpdates[key] = value
	return o
}

// WithNotes sets the notes field.
func (o *Outcome) WithNotes(notes string) *Outcome {
	o.Notes = notes
	return o
}
