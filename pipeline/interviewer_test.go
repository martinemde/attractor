package pipeline

import (
	"errors"
	"testing"
)

func TestAutoApproveInterviewer_YesNo(t *testing.T) {
	interviewer := &AutoApproveInterviewer{}

	question := &Question{
		Text: "Proceed with deployment?",
		Type: QuestionYesNo,
	}

	answer, err := interviewer.Ask(question)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if answer.Value != AnswerYes {
		t.Errorf("expected YES, got %q", answer.Value)
	}
}

func TestAutoApproveInterviewer_Confirmation(t *testing.T) {
	interviewer := &AutoApproveInterviewer{}

	question := &Question{
		Text: "Are you sure?",
		Type: QuestionConfirmation,
	}

	answer, err := interviewer.Ask(question)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if answer.Value != AnswerYes {
		t.Errorf("expected YES, got %q", answer.Value)
	}
}

func TestAutoApproveInterviewer_MultipleChoice_SelectsFirst(t *testing.T) {
	interviewer := &AutoApproveInterviewer{}

	question := &Question{
		Text: "Select environment",
		Type: QuestionMultipleChoice,
		Options: []Option{
			{Key: "D", Label: "Development"},
			{Key: "S", Label: "Staging"},
			{Key: "P", Label: "Production"},
		},
	}

	answer, err := interviewer.Ask(question)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if answer.Value != "D" {
		t.Errorf("expected first option key 'D', got %q", answer.Value)
	}
	if answer.SelectedOption == nil {
		t.Fatal("expected SelectedOption to be set")
	}
	if answer.SelectedOption.Key != "D" {
		t.Errorf("expected SelectedOption.Key 'D', got %q", answer.SelectedOption.Key)
	}
	if answer.SelectedOption.Label != "Development" {
		t.Errorf("expected SelectedOption.Label 'Development', got %q", answer.SelectedOption.Label)
	}
}

func TestAutoApproveInterviewer_MultipleChoice_EmptyOptions(t *testing.T) {
	interviewer := &AutoApproveInterviewer{}

	question := &Question{
		Text:    "Select something",
		Type:    QuestionMultipleChoice,
		Options: []Option{},
	}

	answer, err := interviewer.Ask(question)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if answer.Value != "auto-approved" {
		t.Errorf("expected 'auto-approved', got %q", answer.Value)
	}
	if answer.Text != "auto-approved" {
		t.Errorf("expected Text 'auto-approved', got %q", answer.Text)
	}
}

func TestAutoApproveInterviewer_Freeform(t *testing.T) {
	interviewer := &AutoApproveInterviewer{}

	question := &Question{
		Text: "Enter your name",
		Type: QuestionFreeform,
	}

	answer, err := interviewer.Ask(question)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if answer.Value != "auto-approved" {
		t.Errorf("expected 'auto-approved', got %q", answer.Value)
	}
}

func TestQueueInterviewer_Dequeue(t *testing.T) {
	answers := []*Answer{
		{Value: "A"},
		{Value: "B"},
		{Value: "C"},
	}
	interviewer := NewQueueInterviewer(answers...)

	for _, expected := range answers {
		answer, err := interviewer.Ask(&Question{Text: "test"})
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		if answer.Value != expected.Value {
			t.Errorf("expected %q, got %q", expected.Value, answer.Value)
		}
	}
}

func TestQueueInterviewer_EmptyReturnsSkipped(t *testing.T) {
	interviewer := NewQueueInterviewer()

	answer, err := interviewer.Ask(&Question{Text: "test"})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if !answer.Skipped {
		t.Error("expected Skipped=true when queue is empty")
	}
}

func TestQueueInterviewer_Enqueue(t *testing.T) {
	interviewer := NewQueueInterviewer()

	if interviewer.Remaining() != 0 {
		t.Errorf("expected 0 remaining, got %d", interviewer.Remaining())
	}

	interviewer.Enqueue(&Answer{Value: "X"})
	interviewer.Enqueue(&Answer{Value: "Y"})

	if interviewer.Remaining() != 2 {
		t.Errorf("expected 2 remaining, got %d", interviewer.Remaining())
	}

	answer, _ := interviewer.Ask(&Question{Text: "test"})
	if answer.Value != "X" {
		t.Errorf("expected 'X', got %q", answer.Value)
	}

	if interviewer.Remaining() != 1 {
		t.Errorf("expected 1 remaining after dequeue, got %d", interviewer.Remaining())
	}
}

func TestCallbackInterviewer_DelegatesCorrectly(t *testing.T) {
	var receivedQuestion *Question
	callback := func(q *Question) (*Answer, error) {
		receivedQuestion = q
		return &Answer{Value: "callback-answer", Text: "from callback"}, nil
	}

	interviewer := NewCallbackInterviewer(callback)

	question := &Question{
		Text:  "Test question",
		Type:  QuestionFreeform,
		Stage: "test-stage",
	}

	answer, err := interviewer.Ask(question)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if receivedQuestion != question {
		t.Error("callback did not receive the correct question")
	}
	if answer.Value != "callback-answer" {
		t.Errorf("expected 'callback-answer', got %q", answer.Value)
	}
	if answer.Text != "from callback" {
		t.Errorf("expected 'from callback', got %q", answer.Text)
	}
}

func TestCallbackInterviewer_PropagatesError(t *testing.T) {
	expectedErr := errors.New("callback error")
	callback := func(q *Question) (*Answer, error) {
		return nil, expectedErr
	}

	interviewer := NewCallbackInterviewer(callback)

	_, err := interviewer.Ask(&Question{Text: "test"})
	if err != expectedErr {
		t.Errorf("expected error %v, got %v", expectedErr, err)
	}
}

func TestRecordingInterviewer_RecordsAllPairs(t *testing.T) {
	inner := &AutoApproveInterviewer{}
	recorder := NewRecordingInterviewer(inner)

	questions := []*Question{
		{Text: "Q1", Type: QuestionYesNo},
		{Text: "Q2", Type: QuestionConfirmation},
		{Text: "Q3", Type: QuestionMultipleChoice, Options: []Option{{Key: "A", Label: "Option A"}}},
	}

	for _, q := range questions {
		_, err := recorder.Ask(q)
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
	}

	recordings := recorder.Recordings()
	if len(recordings) != 3 {
		t.Fatalf("expected 3 recordings, got %d", len(recordings))
	}

	for i, rec := range recordings {
		if rec.Question.Text != questions[i].Text {
			t.Errorf("recording %d: expected question %q, got %q", i, questions[i].Text, rec.Question.Text)
		}
		if rec.Answer == nil {
			t.Errorf("recording %d: answer is nil", i)
		}
	}
}

func TestRecordingInterviewer_PropagatesError(t *testing.T) {
	expectedErr := errors.New("inner error")
	inner := NewCallbackInterviewer(func(q *Question) (*Answer, error) {
		return nil, expectedErr
	})
	recorder := NewRecordingInterviewer(inner)

	_, err := recorder.Ask(&Question{Text: "test"})
	if err != expectedErr {
		t.Errorf("expected error %v, got %v", expectedErr, err)
	}

	// Should not record on error
	recordings := recorder.Recordings()
	if len(recordings) != 0 {
		t.Errorf("expected 0 recordings on error, got %d", len(recordings))
	}
}

func TestRecordingInterviewer_Clear(t *testing.T) {
	inner := &AutoApproveInterviewer{}
	recorder := NewRecordingInterviewer(inner)

	recorder.Ask(&Question{Text: "Q1", Type: QuestionYesNo})
	recorder.Ask(&Question{Text: "Q2", Type: QuestionYesNo})

	if len(recorder.Recordings()) != 2 {
		t.Fatalf("expected 2 recordings before clear")
	}

	recorder.Clear()

	if len(recorder.Recordings()) != 0 {
		t.Errorf("expected 0 recordings after clear, got %d", len(recorder.Recordings()))
	}
}

func TestAskMultiple_UsesIndividualAsks(t *testing.T) {
	answers := []*Answer{
		{Value: "A"},
		{Value: "B"},
		{Value: "C"},
	}
	interviewer := NewQueueInterviewer(answers...)

	questions := []*Question{
		{Text: "Q1"},
		{Text: "Q2"},
		{Text: "Q3"},
	}

	results, err := AskMultiple(interviewer, questions)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if len(results) != 3 {
		t.Fatalf("expected 3 answers, got %d", len(results))
	}

	for i, result := range results {
		if result.Value != answers[i].Value {
			t.Errorf("answer %d: expected %q, got %q", i, answers[i].Value, result.Value)
		}
	}
}

func TestInform_NoOpWhenNotSupported(t *testing.T) {
	interviewer := &AutoApproveInterviewer{}

	// Should not panic
	Inform(interviewer, "test message", "test-stage")
}

type informerMock struct {
	messages []string
}

func (m *informerMock) Ask(q *Question) (*Answer, error) {
	return &Answer{Value: "mock"}, nil
}

func (m *informerMock) Inform(message string, stage string) {
	m.messages = append(m.messages, message+" ["+stage+"]")
}

func TestInform_CallsInformer(t *testing.T) {
	mock := &informerMock{}

	Inform(mock, "Hello", "my-stage")

	if len(mock.messages) != 1 {
		t.Fatalf("expected 1 message, got %d", len(mock.messages))
	}
	if mock.messages[0] != "Hello [my-stage]" {
		t.Errorf("unexpected message: %q", mock.messages[0])
	}
}
