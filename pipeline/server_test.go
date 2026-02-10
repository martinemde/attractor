package pipeline

import (
	"bytes"
	"context"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/martinemde/attractor/dotparser"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// mockParser simulates DOT parsing for tests.
func mockParser(source string) (*dotparser.Graph, error) {
	// Return a simple graph for any input
	return &dotparser.Graph{
		Name: "test-pipeline",
		Nodes: []*dotparser.Node{
			{ID: "start", Attrs: []dotparser.Attr{{Key: "shape", Value: dotparser.Value{Kind: dotparser.ValueString, Str: "Mdiamond"}}}},
			{ID: "exit", Attrs: []dotparser.Attr{{Key: "shape", Value: dotparser.Value{Kind: dotparser.ValueString, Str: "Msquare"}}}},
		},
		Edges: []*dotparser.Edge{
			{From: "start", To: "exit"},
		},
	}, nil
}

func TestServer_PostPipelinesStartsPipeline(t *testing.T) {
	server := NewPipelineServer(DefaultRegistry(), mockParser)
	handler := server.Handler()

	reqBody := `{"dot_source": "digraph { start -> exit }"}`
	req := httptest.NewRequest("POST", "/pipelines", strings.NewReader(reqBody))
	req.Header.Set("Content-Type", "application/json")
	w := httptest.NewRecorder()

	handler.ServeHTTP(w, req)

	assert.Equal(t, http.StatusCreated, w.Code)

	var resp createPipelineResponse
	err := json.NewDecoder(w.Body).Decode(&resp)
	require.NoError(t, err)
	assert.NotEmpty(t, resp.ID)
	assert.Contains(t, resp.ID, "pipeline-")
}

func TestServer_PostPipelinesInvalidBody(t *testing.T) {
	server := NewPipelineServer(DefaultRegistry(), mockParser)
	handler := server.Handler()

	req := httptest.NewRequest("POST", "/pipelines", strings.NewReader("invalid json"))
	w := httptest.NewRecorder()

	handler.ServeHTTP(w, req)

	assert.Equal(t, http.StatusBadRequest, w.Code)
}

func TestServer_PostPipelinesMissingDOTSource(t *testing.T) {
	server := NewPipelineServer(DefaultRegistry(), mockParser)
	handler := server.Handler()

	reqBody := `{}`
	req := httptest.NewRequest("POST", "/pipelines", strings.NewReader(reqBody))
	req.Header.Set("Content-Type", "application/json")
	w := httptest.NewRecorder()

	handler.ServeHTTP(w, req)

	assert.Equal(t, http.StatusBadRequest, w.Code)
	assert.Contains(t, w.Body.String(), "dot_source is required")
}

func TestServer_GetPipelineReturnsStatus(t *testing.T) {
	server := NewPipelineServer(DefaultRegistry(), mockParser)
	handler := server.Handler()

	// First create a pipeline
	reqBody := `{"dot_source": "digraph { start -> exit }"}`
	req := httptest.NewRequest("POST", "/pipelines", strings.NewReader(reqBody))
	req.Header.Set("Content-Type", "application/json")
	w := httptest.NewRecorder()
	handler.ServeHTTP(w, req)

	var createResp createPipelineResponse
	json.NewDecoder(w.Body).Decode(&createResp)

	// Wait a moment for pipeline to start/complete
	time.Sleep(100 * time.Millisecond)

	// Get pipeline status
	req = httptest.NewRequest("GET", "/pipelines/"+createResp.ID, nil)
	w = httptest.NewRecorder()
	handler.ServeHTTP(w, req)

	assert.Equal(t, http.StatusOK, w.Code)

	var statusResp struct {
		ID        string `json:"id"`
		Status    string `json:"status"`
		StartTime string `json:"start_time"`
	}
	err := json.NewDecoder(w.Body).Decode(&statusResp)
	require.NoError(t, err)
	assert.Equal(t, createResp.ID, statusResp.ID)
	assert.NotEmpty(t, statusResp.Status)
	assert.NotEmpty(t, statusResp.StartTime)
}

func TestServer_GetPipelineNotFound(t *testing.T) {
	server := NewPipelineServer(DefaultRegistry(), mockParser)
	handler := server.Handler()

	req := httptest.NewRequest("GET", "/pipelines/nonexistent", nil)
	w := httptest.NewRecorder()

	handler.ServeHTTP(w, req)

	assert.Equal(t, http.StatusNotFound, w.Code)
}

func TestServer_GetEventsStreamsSSE(t *testing.T) {
	server := NewPipelineServer(DefaultRegistry(), mockParser)
	handler := server.Handler()

	// Create a pipeline
	reqBody := `{"dot_source": "digraph { start -> exit }"}`
	req := httptest.NewRequest("POST", "/pipelines", strings.NewReader(reqBody))
	req.Header.Set("Content-Type", "application/json")
	w := httptest.NewRecorder()
	handler.ServeHTTP(w, req)

	var createResp createPipelineResponse
	json.NewDecoder(w.Body).Decode(&createResp)

	// Wait for pipeline to complete
	time.Sleep(100 * time.Millisecond)

	// Create a context with timeout to prevent hanging
	ctx, cancel := context.WithTimeout(context.Background(), 100*time.Millisecond)
	defer cancel()

	req = httptest.NewRequest("GET", "/pipelines/"+createResp.ID+"/events", nil)
	req = req.WithContext(ctx)
	w = httptest.NewRecorder()

	// Run in goroutine since it streams
	done := make(chan struct{})
	go func() {
		handler.ServeHTTP(w, req)
		close(done)
	}()

	// Wait for handler to finish or timeout
	<-done

	// Check headers
	assert.Equal(t, "text/event-stream", w.Header().Get("Content-Type"))

	// Should have some event data
	body := w.Body.String()
	assert.Contains(t, body, "data:")
}

func TestServer_GetContextReturnsContext(t *testing.T) {
	server := NewPipelineServer(DefaultRegistry(), mockParser)
	handler := server.Handler()

	// Create a pipeline
	reqBody := `{"dot_source": "digraph { start -> exit }"}`
	req := httptest.NewRequest("POST", "/pipelines", strings.NewReader(reqBody))
	req.Header.Set("Content-Type", "application/json")
	w := httptest.NewRecorder()
	handler.ServeHTTP(w, req)

	var createResp createPipelineResponse
	json.NewDecoder(w.Body).Decode(&createResp)

	// Wait for pipeline to run
	time.Sleep(100 * time.Millisecond)

	// Get context
	req = httptest.NewRequest("GET", "/pipelines/"+createResp.ID+"/context", nil)
	w = httptest.NewRecorder()
	handler.ServeHTTP(w, req)

	assert.Equal(t, http.StatusOK, w.Code)

	var contextMap map[string]any
	err := json.NewDecoder(w.Body).Decode(&contextMap)
	require.NoError(t, err)
	// Context should contain at least the outcome from execution
	assert.NotEmpty(t, contextMap)
}

func TestServer_PostCancelSetsCancelStatus(t *testing.T) {
	server := NewPipelineServer(DefaultRegistry(), mockParser)
	handler := server.Handler()

	// Create a pipeline
	reqBody := `{"dot_source": "digraph { start -> exit }"}`
	req := httptest.NewRequest("POST", "/pipelines", strings.NewReader(reqBody))
	req.Header.Set("Content-Type", "application/json")
	w := httptest.NewRecorder()
	handler.ServeHTTP(w, req)

	var createResp createPipelineResponse
	json.NewDecoder(w.Body).Decode(&createResp)

	// Cancel the pipeline
	req = httptest.NewRequest("POST", "/pipelines/"+createResp.ID+"/cancel", nil)
	w = httptest.NewRecorder()
	handler.ServeHTTP(w, req)

	assert.Equal(t, http.StatusOK, w.Code)

	body := w.Body.String()
	assert.Contains(t, body, "cancelled")
}

func TestServer_PostCancelNotFound(t *testing.T) {
	server := NewPipelineServer(DefaultRegistry(), mockParser)
	handler := server.Handler()

	req := httptest.NewRequest("POST", "/pipelines/nonexistent/cancel", nil)
	w := httptest.NewRecorder()

	handler.ServeHTTP(w, req)

	assert.Equal(t, http.StatusNotFound, w.Code)
}

func TestServer_GetQuestionsReturnsEmptyList(t *testing.T) {
	server := NewPipelineServer(DefaultRegistry(), mockParser)
	handler := server.Handler()

	// Create a pipeline
	reqBody := `{"dot_source": "digraph { start -> exit }"}`
	req := httptest.NewRequest("POST", "/pipelines", strings.NewReader(reqBody))
	req.Header.Set("Content-Type", "application/json")
	w := httptest.NewRecorder()
	handler.ServeHTTP(w, req)

	var createResp createPipelineResponse
	json.NewDecoder(w.Body).Decode(&createResp)

	// Get questions
	req = httptest.NewRequest("GET", "/pipelines/"+createResp.ID+"/questions", nil)
	w = httptest.NewRecorder()
	handler.ServeHTTP(w, req)

	assert.Equal(t, http.StatusOK, w.Code)

	var questions []struct {
		ID       string    `json:"id"`
		Question *Question `json:"question"`
	}
	err := json.NewDecoder(w.Body).Decode(&questions)
	require.NoError(t, err)
	// Should be empty since our test pipeline has no human gates
	assert.Empty(t, questions)
}

func TestServer_AnswerQuestionNotFound(t *testing.T) {
	server := NewPipelineServer(DefaultRegistry(), mockParser)
	handler := server.Handler()

	// Create a pipeline
	reqBody := `{"dot_source": "digraph { start -> exit }"}`
	req := httptest.NewRequest("POST", "/pipelines", strings.NewReader(reqBody))
	req.Header.Set("Content-Type", "application/json")
	w := httptest.NewRecorder()
	handler.ServeHTTP(w, req)

	var createResp createPipelineResponse
	json.NewDecoder(w.Body).Decode(&createResp)

	// Try to answer a non-existent question
	answerBody := `{"value": "yes"}`
	req = httptest.NewRequest("POST", "/pipelines/"+createResp.ID+"/questions/nonexistent/answer", strings.NewReader(answerBody))
	req.Header.Set("Content-Type", "application/json")
	w = httptest.NewRecorder()
	handler.ServeHTTP(w, req)

	assert.Equal(t, http.StatusNotFound, w.Code)
}

func TestServer_GetCheckpointNotConfigured(t *testing.T) {
	server := NewPipelineServer(DefaultRegistry(), mockParser)
	handler := server.Handler()

	// Create a pipeline without logs root
	reqBody := `{"dot_source": "digraph { start -> exit }"}`
	req := httptest.NewRequest("POST", "/pipelines", strings.NewReader(reqBody))
	req.Header.Set("Content-Type", "application/json")
	w := httptest.NewRecorder()
	handler.ServeHTTP(w, req)

	var createResp createPipelineResponse
	json.NewDecoder(w.Body).Decode(&createResp)

	// Wait for pipeline
	time.Sleep(100 * time.Millisecond)

	// Get checkpoint - should fail because no logs root
	req = httptest.NewRequest("GET", "/pipelines/"+createResp.ID+"/checkpoint", nil)
	w = httptest.NewRecorder()
	handler.ServeHTTP(w, req)

	assert.Equal(t, http.StatusNotFound, w.Code)
}

func TestServer_GetCheckpointWithLogsRoot(t *testing.T) {
	server := NewPipelineServer(DefaultRegistry(), mockParser)
	handler := server.Handler()

	tmpDir := t.TempDir()

	// Create a pipeline with logs root
	reqBody, _ := json.Marshal(map[string]string{
		"dot_source": "digraph { start -> exit }",
		"logs_root":  tmpDir,
	})
	req := httptest.NewRequest("POST", "/pipelines", bytes.NewReader(reqBody))
	req.Header.Set("Content-Type", "application/json")
	w := httptest.NewRecorder()
	handler.ServeHTTP(w, req)

	var createResp createPipelineResponse
	json.NewDecoder(w.Body).Decode(&createResp)

	// Wait for pipeline to complete and write checkpoint
	time.Sleep(200 * time.Millisecond)

	// Get checkpoint
	req = httptest.NewRequest("GET", "/pipelines/"+createResp.ID+"/checkpoint", nil)
	w = httptest.NewRecorder()
	handler.ServeHTTP(w, req)

	// Should find the checkpoint
	assert.Equal(t, http.StatusOK, w.Code)

	var checkpoint Checkpoint
	err := json.NewDecoder(w.Body).Decode(&checkpoint)
	require.NoError(t, err)
	assert.NotEmpty(t, checkpoint.CurrentNode)
}

func TestServerInterviewer_AsksAndReceivesAnswer(t *testing.T) {
	server := NewPipelineServer(DefaultRegistry(), mockParser)

	// Create pipeline manually for testing the interviewer
	pipeline := &runningPipeline{
		ID:        "test-1",
		Status:    "running",
		Questions: make([]*pendingQuestion, 0),
	}
	server.pipelines["test-1"] = pipeline

	interviewer := &serverInterviewer{
		pipeline: pipeline,
	}

	// Start ask in goroutine
	done := make(chan *Answer)
	go func() {
		answer, _ := interviewer.Ask(&Question{
			Text:           "Do you approve?",
			Type:           QuestionYesNo,
			TimeoutSeconds: 5,
		})
		done <- answer
	}()

	// Wait for question to be posted
	time.Sleep(50 * time.Millisecond)

	// Verify question was posted (access through mutex)
	pipeline.eventMu.RLock()
	require.Len(t, pipeline.Questions, 1)
	questionText := pipeline.Questions[0].Question.Text
	answerCh := pipeline.Questions[0].AnswerCh
	pipeline.eventMu.RUnlock()

	assert.Equal(t, "Do you approve?", questionText)

	// Answer the question
	answerCh <- &Answer{Value: "yes"}

	// Wait for answer to be received
	answer := <-done
	assert.Equal(t, "yes", answer.Value)
}

func TestServerInterviewer_Timeout(t *testing.T) {
	server := NewPipelineServer(DefaultRegistry(), mockParser)

	pipeline := &runningPipeline{
		ID:        "test-1",
		Status:    "running",
		Questions: make([]*pendingQuestion, 0),
	}
	server.pipelines["test-1"] = pipeline

	interviewer := &serverInterviewer{
		pipeline: pipeline,
	}

	// Ask with short timeout
	answer, err := interviewer.Ask(&Question{
		Text:           "Quick question",
		Type:           QuestionYesNo,
		TimeoutSeconds: 0.1, // 100ms timeout
	})

	require.NoError(t, err)
	assert.True(t, answer.Timeout)
	// Question should be removed from pending list
	assert.Empty(t, pipeline.Questions)
}

func TestServer_MultiplePipelines(t *testing.T) {
	server := NewPipelineServer(DefaultRegistry(), mockParser)
	handler := server.Handler()

	// Create multiple pipelines
	var ids []string
	for range 3 {
		reqBody := `{"dot_source": "digraph { start -> exit }"}`
		req := httptest.NewRequest("POST", "/pipelines", strings.NewReader(reqBody))
		req.Header.Set("Content-Type", "application/json")
		w := httptest.NewRecorder()
		handler.ServeHTTP(w, req)

		var resp createPipelineResponse
		json.NewDecoder(w.Body).Decode(&resp)
		ids = append(ids, resp.ID)
	}

	// All should have unique IDs
	assert.Len(t, ids, 3)
	assert.NotEqual(t, ids[0], ids[1])
	assert.NotEqual(t, ids[1], ids[2])
	assert.NotEqual(t, ids[0], ids[2])

	// All should be queryable
	for _, id := range ids {
		req := httptest.NewRequest("GET", "/pipelines/"+id, nil)
		w := httptest.NewRecorder()
		handler.ServeHTTP(w, req)
		assert.Equal(t, http.StatusOK, w.Code)
	}
}

func TestServer_ListenAndServe(t *testing.T) {
	server := NewPipelineServer(DefaultRegistry(), mockParser)
	handler := server.Handler()

	// Start a test server
	ts := httptest.NewServer(handler)
	defer ts.Close()

	// Make a real HTTP request
	resp, err := http.Post(ts.URL+"/pipelines", "application/json", strings.NewReader(`{"dot_source": "test"}`))
	require.NoError(t, err)
	defer resp.Body.Close()

	assert.Equal(t, http.StatusCreated, resp.StatusCode)

	body, _ := io.ReadAll(resp.Body)
	assert.Contains(t, string(body), "pipeline-")
}
