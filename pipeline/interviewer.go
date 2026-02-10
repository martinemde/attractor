package pipeline

import "sync"

// Interviewer implementations and related types for human-in-the-loop interactions.
// The core Interviewer interface and Question/Answer models are defined in engine.go.

// AskMultiple presents multiple questions and collects all answers.
// This is a convenience function that wraps individual Ask calls.
// If the interviewer implements AskMultipler, that method is used instead.
func AskMultiple(i Interviewer, questions []*Question) ([]*Answer, error) {
	if m, ok := i.(AskMultipler); ok {
		return m.AskMultiple(questions)
	}

	answers := make([]*Answer, len(questions))
	for idx, q := range questions {
		a, err := i.Ask(q)
		if err != nil {
			return nil, err
		}
		answers[idx] = a
	}
	return answers, nil
}

// AskMultipler is an optional interface for interviewers that can handle
// multiple questions more efficiently than individual Ask calls.
type AskMultipler interface {
	AskMultiple(questions []*Question) ([]*Answer, error)
}

// Informer is an optional interface for interviewers that support
// one-way informational messages (no response expected).
type Informer interface {
	Inform(message string, stage string)
}

// Inform sends an informational message through the interviewer if it supports it.
// If the interviewer does not implement Informer, this is a no-op.
func Inform(i Interviewer, message string, stage string) {
	if inf, ok := i.(Informer); ok {
		inf.Inform(message, stage)
	}
}

// AutoApproveInterviewer always selects YES for yes/no questions and
// the first option for multiple choice. Used for automated testing and
// CI/CD pipelines where no human is available.
type AutoApproveInterviewer struct{}

// Ask returns an auto-approved answer based on question type.
func (a *AutoApproveInterviewer) Ask(question *Question) (*Answer, error) {
	switch question.Type {
	case QuestionYesNo, QuestionConfirmation:
		return &Answer{Value: AnswerYes}, nil
	case QuestionMultipleChoice:
		if len(question.Options) > 0 {
			return &Answer{
				Value:          question.Options[0].Key,
				SelectedOption: &question.Options[0],
			}, nil
		}
		return &Answer{Value: "auto-approved", Text: "auto-approved"}, nil
	default:
		return &Answer{Value: "auto-approved", Text: "auto-approved"}, nil
	}
}

// QueueInterviewer reads answers from a pre-filled answer queue.
// Used for deterministic testing and replay. When the queue is empty,
// it returns an answer with Skipped=true.
type QueueInterviewer struct {
	mu      sync.Mutex
	answers []*Answer
}

// NewQueueInterviewer creates a QueueInterviewer with the given answers.
func NewQueueInterviewer(answers ...*Answer) *QueueInterviewer {
	return &QueueInterviewer{
		answers: answers,
	}
}

// Enqueue adds an answer to the queue.
func (q *QueueInterviewer) Enqueue(answer *Answer) {
	q.mu.Lock()
	defer q.mu.Unlock()
	q.answers = append(q.answers, answer)
}

// Ask dequeues and returns the next answer, or a skipped answer if empty.
func (q *QueueInterviewer) Ask(question *Question) (*Answer, error) {
	q.mu.Lock()
	defer q.mu.Unlock()

	if len(q.answers) == 0 {
		return &Answer{Skipped: true}, nil
	}

	answer := q.answers[0]
	q.answers = q.answers[1:]
	return answer, nil
}

// Remaining returns the number of answers remaining in the queue.
func (q *QueueInterviewer) Remaining() int {
	q.mu.Lock()
	defer q.mu.Unlock()
	return len(q.answers)
}

// CallbackInterviewer delegates question answering to a provided callback function.
// Useful for integrating with external systems (Slack, web UI, API).
type CallbackInterviewer struct {
	Callback func(*Question) (*Answer, error)
}

// NewCallbackInterviewer creates a CallbackInterviewer with the given callback.
func NewCallbackInterviewer(callback func(*Question) (*Answer, error)) *CallbackInterviewer {
	return &CallbackInterviewer{Callback: callback}
}

// Ask delegates to the callback function.
func (c *CallbackInterviewer) Ask(question *Question) (*Answer, error) {
	return c.Callback(question)
}

// Recording represents a recorded question-answer pair.
type Recording struct {
	Question *Question
	Answer   *Answer
}

// RecordingInterviewer wraps another interviewer and records all
// question-answer pairs. Used for replay, debugging, and audit trails.
type RecordingInterviewer struct {
	Inner      Interviewer
	mu         sync.Mutex
	recordings []Recording
}

// NewRecordingInterviewer creates a RecordingInterviewer wrapping the given interviewer.
func NewRecordingInterviewer(inner Interviewer) *RecordingInterviewer {
	return &RecordingInterviewer{
		Inner:      inner,
		recordings: make([]Recording, 0),
	}
}

// Ask delegates to the inner interviewer and records the pair.
func (r *RecordingInterviewer) Ask(question *Question) (*Answer, error) {
	answer, err := r.Inner.Ask(question)
	if err != nil {
		return nil, err
	}

	r.mu.Lock()
	r.recordings = append(r.recordings, Recording{
		Question: question,
		Answer:   answer,
	})
	r.mu.Unlock()

	return answer, nil
}

// Recordings returns a copy of all recorded question-answer pairs.
func (r *RecordingInterviewer) Recordings() []Recording {
	r.mu.Lock()
	defer r.mu.Unlock()

	result := make([]Recording, len(r.recordings))
	copy(result, r.recordings)
	return result
}

// Clear removes all recordings.
func (r *RecordingInterviewer) Clear() {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.recordings = r.recordings[:0]
}
