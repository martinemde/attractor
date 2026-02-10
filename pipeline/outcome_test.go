package pipeline

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestStageStatusString(t *testing.T) {
	tests := []struct {
		status   StageStatus
		expected string
	}{
		{StatusSuccess, "success"},
		{StatusFail, "fail"},
		{StatusPartialSuccess, "partial_success"},
		{StatusRetry, "retry"},
		{StatusSkipped, "skipped"},
	}

	for _, tt := range tests {
		t.Run(tt.expected, func(t *testing.T) {
			assert.Equal(t, tt.expected, tt.status.String())
		})
	}
}

func TestStageStatusIsSuccess(t *testing.T) {
	tests := []struct {
		status   StageStatus
		expected bool
	}{
		{StatusSuccess, true},
		{StatusPartialSuccess, true},
		{StatusFail, false},
		{StatusRetry, false},
		{StatusSkipped, false},
	}

	for _, tt := range tests {
		t.Run(tt.status.String(), func(t *testing.T) {
			assert.Equal(t, tt.expected, tt.status.IsSuccess())
		})
	}
}

func TestNewOutcome(t *testing.T) {
	o := NewOutcome(StatusSuccess)
	require.NotNil(t, o)
	assert.Equal(t, StatusSuccess, o.Status)
	assert.NotNil(t, o.ContextUpdates)
	assert.Empty(t, o.PreferredLabel)
	assert.Empty(t, o.SuggestedNextIDs)
	assert.Empty(t, o.Notes)
	assert.Empty(t, o.FailureReason)
}

func TestSuccess(t *testing.T) {
	o := Success()
	require.NotNil(t, o)
	assert.Equal(t, StatusSuccess, o.Status)
}

func TestFail(t *testing.T) {
	reason := "something went wrong"
	o := Fail(reason)
	require.NotNil(t, o)
	assert.Equal(t, StatusFail, o.Status)
	assert.Equal(t, reason, o.FailureReason)
}

func TestPartialSuccess(t *testing.T) {
	notes := "completed with warnings"
	o := PartialSuccess(notes)
	require.NotNil(t, o)
	assert.Equal(t, StatusPartialSuccess, o.Status)
	assert.Equal(t, notes, o.Notes)
}

func TestRetry(t *testing.T) {
	reason := "temporary failure"
	o := Retry(reason)
	require.NotNil(t, o)
	assert.Equal(t, StatusRetry, o.Status)
	assert.Equal(t, reason, o.FailureReason)
}

func TestSkipped(t *testing.T) {
	o := Skipped()
	require.NotNil(t, o)
	assert.Equal(t, StatusSkipped, o.Status)
}

func TestOutcomeWithPreferredLabel(t *testing.T) {
	o := Success().WithPreferredLabel("next")
	assert.Equal(t, "next", o.PreferredLabel)
}

func TestOutcomeWithSuggestedNextIDs(t *testing.T) {
	o := Success().WithSuggestedNextIDs("node_a", "node_b")
	assert.Equal(t, []string{"node_a", "node_b"}, o.SuggestedNextIDs)
}

func TestOutcomeWithContextUpdate(t *testing.T) {
	o := Success().
		WithContextUpdate("key1", "value1").
		WithContextUpdate("key2", 42)

	assert.Equal(t, "value1", o.ContextUpdates["key1"])
	assert.Equal(t, 42, o.ContextUpdates["key2"])
}

func TestOutcomeWithNotes(t *testing.T) {
	o := Success().WithNotes("execution summary")
	assert.Equal(t, "execution summary", o.Notes)
}

func TestOutcomeChainedBuilders(t *testing.T) {
	o := Success().
		WithPreferredLabel("approve").
		WithSuggestedNextIDs("deploy", "notify").
		WithContextUpdate("deployed", true).
		WithNotes("deployment complete")

	assert.Equal(t, StatusSuccess, o.Status)
	assert.Equal(t, "approve", o.PreferredLabel)
	assert.Equal(t, []string{"deploy", "notify"}, o.SuggestedNextIDs)
	assert.Equal(t, true, o.ContextUpdates["deployed"])
	assert.Equal(t, "deployment complete", o.Notes)
}
