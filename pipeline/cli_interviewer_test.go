package pipeline

import (
	"bytes"
	"io"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestCLIInterviewer_YesNo_Timeout(t *testing.T) {
	// Create a pipe where we control when data arrives
	r, w := io.Pipe()
	defer r.Close()
	defer w.Close()

	var out bytes.Buffer
	interviewer := NewCLIInterviewer(r, &out)

	// Ask with a short timeout - don't write anything to the pipe
	start := time.Now()
	answer, err := interviewer.Ask(&Question{
		Text:           "Do you approve?",
		Type:           QuestionYesNo,
		TimeoutSeconds: 0.1, // 100ms timeout
		Stage:          "test",
	})
	elapsed := time.Since(start)

	require.NoError(t, err)
	assert.True(t, answer.Timeout, "expected timeout to be true")
	assert.Less(t, elapsed, 500*time.Millisecond, "should timeout quickly")
}

func TestCLIInterviewer_YesNo_AnswerBeforeTimeout(t *testing.T) {
	// Create a pipe and write immediately
	r, w := io.Pipe()
	defer r.Close()

	var out bytes.Buffer
	interviewer := NewCLIInterviewer(r, &out)

	// Write answer in goroutine
	go func() {
		defer w.Close()
		w.Write([]byte("y\n"))
	}()

	answer, err := interviewer.Ask(&Question{
		Text:           "Do you approve?",
		Type:           QuestionYesNo,
		TimeoutSeconds: 5, // Long timeout - should get answer before
		Stage:          "test",
	})

	require.NoError(t, err)
	assert.False(t, answer.Timeout)
	assert.Equal(t, AnswerYes, answer.Value)
}

func TestCLIInterviewer_YesNo_ZeroTimeoutBlocks(t *testing.T) {
	// With TimeoutSeconds=0, should use blocking behavior
	r, w := io.Pipe()
	defer r.Close()

	var out bytes.Buffer
	interviewer := NewCLIInterviewer(r, &out)

	// Write answer in goroutine after short delay
	go func() {
		defer w.Close()
		time.Sleep(50 * time.Millisecond)
		w.Write([]byte("yes\n"))
	}()

	answer, err := interviewer.Ask(&Question{
		Text:           "Do you approve?",
		Type:           QuestionYesNo,
		TimeoutSeconds: 0, // No timeout - blocking behavior
		Stage:          "test",
	})

	require.NoError(t, err)
	assert.False(t, answer.Timeout)
	assert.Equal(t, AnswerYes, answer.Value)
}

func TestCLIInterviewer_MultipleChoice_Timeout(t *testing.T) {
	r, w := io.Pipe()
	defer r.Close()
	defer w.Close()

	var out bytes.Buffer
	interviewer := NewCLIInterviewer(r, &out)

	start := time.Now()
	answer, err := interviewer.Ask(&Question{
		Text: "Pick one",
		Type: QuestionMultipleChoice,
		Options: []Option{
			{Key: "a", Label: "Option A"},
			{Key: "b", Label: "Option B"},
		},
		TimeoutSeconds: 0.1,
		Stage:          "test",
	})
	elapsed := time.Since(start)

	require.NoError(t, err)
	assert.True(t, answer.Timeout)
	assert.Less(t, elapsed, 500*time.Millisecond)
}

func TestCLIInterviewer_MultipleChoice_AnswerBeforeTimeout(t *testing.T) {
	r, w := io.Pipe()
	defer r.Close()

	var out bytes.Buffer
	interviewer := NewCLIInterviewer(r, &out)

	go func() {
		defer w.Close()
		w.Write([]byte("b\n"))
	}()

	answer, err := interviewer.Ask(&Question{
		Text: "Pick one",
		Type: QuestionMultipleChoice,
		Options: []Option{
			{Key: "a", Label: "Option A"},
			{Key: "b", Label: "Option B"},
		},
		TimeoutSeconds: 5,
		Stage:          "test",
	})

	require.NoError(t, err)
	assert.False(t, answer.Timeout)
	assert.Equal(t, "b", answer.Value)
	require.NotNil(t, answer.SelectedOption)
	assert.Equal(t, "Option B", answer.SelectedOption.Label)
}

func TestCLIInterviewer_Freeform_Timeout(t *testing.T) {
	r, w := io.Pipe()
	defer r.Close()
	defer w.Close()

	var out bytes.Buffer
	interviewer := NewCLIInterviewer(r, &out)

	start := time.Now()
	answer, err := interviewer.Ask(&Question{
		Text:           "Enter your name",
		Type:           QuestionFreeform,
		TimeoutSeconds: 0.1,
		Stage:          "test",
	})
	elapsed := time.Since(start)

	require.NoError(t, err)
	assert.True(t, answer.Timeout)
	assert.Less(t, elapsed, 500*time.Millisecond)
}

func TestCLIInterviewer_Freeform_AnswerBeforeTimeout(t *testing.T) {
	r, w := io.Pipe()
	defer r.Close()

	var out bytes.Buffer
	interviewer := NewCLIInterviewer(r, &out)

	go func() {
		defer w.Close()
		w.Write([]byte("Alice\n"))
	}()

	answer, err := interviewer.Ask(&Question{
		Text:           "Enter your name",
		Type:           QuestionFreeform,
		TimeoutSeconds: 5,
		Stage:          "test",
	})

	require.NoError(t, err)
	assert.False(t, answer.Timeout)
	assert.Equal(t, "Alice", answer.Value)
	assert.Equal(t, "Alice", answer.Text)
}

func TestCLIInterviewer_Confirmation_Timeout(t *testing.T) {
	r, w := io.Pipe()
	defer r.Close()
	defer w.Close()

	var out bytes.Buffer
	interviewer := NewCLIInterviewer(r, &out)

	start := time.Now()
	answer, err := interviewer.Ask(&Question{
		Text:           "Continue?",
		Type:           QuestionConfirmation,
		TimeoutSeconds: 0.1,
		Stage:          "test",
	})
	elapsed := time.Since(start)

	require.NoError(t, err)
	assert.True(t, answer.Timeout)
	assert.Less(t, elapsed, 500*time.Millisecond)
}
