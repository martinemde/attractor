package pipeline

import (
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"sync"
	"time"

	"github.com/martinemde/attractor/dotparser"
)

// PipelineServer provides HTTP endpoints for managing pipeline execution.
type PipelineServer struct {
	mu        sync.RWMutex
	pipelines map[string]*runningPipeline
	registry  *HandlerRegistry
	parser    func(string) (*dotparser.Graph, error)
	nextID    int
}

// runningPipeline tracks the state of a running pipeline.
type runningPipeline struct {
	ID        string            `json:"id"`
	Status    string            `json:"status"` // "running", "completed", "failed", "cancelled"
	Graph     *dotparser.Graph  `json:"-"`
	Config    *RunConfig        `json:"-"`
	Result    *RunResult        `json:"result,omitempty"`
	Events    []Event           `json:"events,omitempty"`
	Questions []*pendingQuestion `json:"questions,omitempty"`
	Cancel    bool              `json:"-"`
	Context   *Context          `json:"-"`
	StartTime time.Time         `json:"start_time"`
	EndTime   *time.Time        `json:"end_time,omitempty"`

	eventMu     sync.RWMutex
	eventCh     chan Event
	subscribers []chan Event
}

// pendingQuestion represents a question waiting for a human answer.
type pendingQuestion struct {
	ID       string    `json:"id"`
	Question *Question `json:"question"`
	AnswerCh chan *Answer
}

// NewPipelineServer creates a new PipelineServer.
func NewPipelineServer(registry *HandlerRegistry, parser func(string) (*dotparser.Graph, error)) *PipelineServer {
	if registry == nil {
		registry = DefaultRegistry()
	}
	return &PipelineServer{
		pipelines: make(map[string]*runningPipeline),
		registry:  registry,
		parser:    parser,
	}
}

// generateID creates a unique pipeline ID.
func (s *PipelineServer) generateID() string {
	s.nextID++
	return fmt.Sprintf("pipeline-%d", s.nextID)
}

// Handler returns an http.Handler for the pipeline server.
func (s *PipelineServer) Handler() http.Handler {
	mux := http.NewServeMux()

	// POST /pipelines - Submit DOT source and start execution
	mux.HandleFunc("POST /pipelines", s.handleCreatePipeline)

	// GET /pipelines/{id} - Get pipeline status
	mux.HandleFunc("GET /pipelines/{id}", s.handleGetPipeline)

	// GET /pipelines/{id}/events - SSE stream of events
	mux.HandleFunc("GET /pipelines/{id}/events", s.handleStreamEvents)

	// POST /pipelines/{id}/cancel - Cancel pipeline
	mux.HandleFunc("POST /pipelines/{id}/cancel", s.handleCancelPipeline)

	// GET /pipelines/{id}/questions - Get pending questions
	mux.HandleFunc("GET /pipelines/{id}/questions", s.handleGetQuestions)

	// POST /pipelines/{id}/questions/{qid}/answer - Submit answer
	mux.HandleFunc("POST /pipelines/{id}/questions/{qid}/answer", s.handleAnswerQuestion)

	// GET /pipelines/{id}/checkpoint - Get current checkpoint
	mux.HandleFunc("GET /pipelines/{id}/checkpoint", s.handleGetCheckpoint)

	// GET /pipelines/{id}/context - Get current context
	mux.HandleFunc("GET /pipelines/{id}/context", s.handleGetContext)

	return mux
}

// createPipelineRequest is the request body for creating a pipeline.
type createPipelineRequest struct {
	DOTSource string `json:"dot_source"`
	LogsRoot  string `json:"logs_root,omitempty"`
}

// createPipelineResponse is the response body for creating a pipeline.
type createPipelineResponse struct {
	ID string `json:"id"`
}

// handleCreatePipeline handles POST /pipelines.
func (s *PipelineServer) handleCreatePipeline(w http.ResponseWriter, r *http.Request) {
	var req createPipelineRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		http.Error(w, "invalid request body: "+err.Error(), http.StatusBadRequest)
		return
	}

	if req.DOTSource == "" {
		http.Error(w, "dot_source is required", http.StatusBadRequest)
		return
	}

	// Parse the DOT source
	graph, err := s.parser(req.DOTSource)
	if err != nil {
		http.Error(w, "failed to parse DOT source: "+err.Error(), http.StatusBadRequest)
		return
	}

	// Create the pipeline
	s.mu.Lock()
	id := s.generateID()
	pipeline := &runningPipeline{
		ID:        id,
		Status:    "running",
		Graph:     graph,
		Events:    make([]Event, 0),
		Questions: make([]*pendingQuestion, 0),
		Context:   NewContext(),
		StartTime: time.Now(),
		eventCh:   make(chan Event, 100),
	}
	s.pipelines[id] = pipeline
	s.mu.Unlock()

	// Create event emitter for this pipeline
	emitter := NewEventEmitter()
	emitter.On(func(e Event) {
		pipeline.eventMu.Lock()
		pipeline.Events = append(pipeline.Events, e)
		// Send to all subscribers
		for _, sub := range pipeline.subscribers {
			select {
			case sub <- e:
			default:
				// Subscriber channel full, skip
			}
		}
		pipeline.eventMu.Unlock()
	})

	// Create interviewer that posts questions to the pipeline
	interviewer := &serverInterviewer{
		pipeline: pipeline,
		nextQID:  0,
	}

	// Configure and run the pipeline
	config := &RunConfig{
		Registry:     s.registry,
		LogsRoot:     req.LogsRoot,
		Interviewer:  interviewer,
		EventEmitter: emitter,
	}
	pipeline.Config = config

	// Run pipeline in background
	go s.runPipeline(pipeline, graph, config)

	// Return the pipeline ID
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(http.StatusCreated)
	json.NewEncoder(w).Encode(createPipelineResponse{ID: id})
}

// runPipeline executes the pipeline and updates its status.
func (s *PipelineServer) runPipeline(pipeline *runningPipeline, graph *dotparser.Graph, config *RunConfig) {
	result, err := Run(graph, config)

	s.mu.Lock()
	defer s.mu.Unlock()

	now := time.Now()
	pipeline.EndTime = &now
	pipeline.Result = result

	if pipeline.Cancel {
		pipeline.Status = "cancelled"
	} else if err != nil {
		pipeline.Status = "failed"
	} else if result != nil && result.FinalOutcome != nil && result.FinalOutcome.Status == StatusFail {
		pipeline.Status = "failed"
	} else {
		pipeline.Status = "completed"
	}

	if result != nil {
		pipeline.Context = result.Context
	}
}

// handleGetPipeline handles GET /pipelines/{id}.
func (s *PipelineServer) handleGetPipeline(w http.ResponseWriter, r *http.Request) {
	id := r.PathValue("id")

	s.mu.RLock()
	pipeline, ok := s.pipelines[id]
	if !ok {
		s.mu.RUnlock()
		http.Error(w, "pipeline not found", http.StatusNotFound)
		return
	}

	// Build response while holding the lock to avoid races with runPipeline
	response := struct {
		ID        string     `json:"id"`
		Status    string     `json:"status"`
		StartTime time.Time  `json:"start_time"`
		EndTime   *time.Time `json:"end_time,omitempty"`
	}{
		ID:        pipeline.ID,
		Status:    pipeline.Status,
		StartTime: pipeline.StartTime,
		EndTime:   pipeline.EndTime,
	}
	s.mu.RUnlock()

	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(response)
}

// handleStreamEvents handles GET /pipelines/{id}/events as SSE.
func (s *PipelineServer) handleStreamEvents(w http.ResponseWriter, r *http.Request) {
	id := r.PathValue("id")

	s.mu.RLock()
	pipeline, ok := s.pipelines[id]
	s.mu.RUnlock()

	if !ok {
		http.Error(w, "pipeline not found", http.StatusNotFound)
		return
	}

	// Set up SSE headers
	w.Header().Set("Content-Type", "text/event-stream")
	w.Header().Set("Cache-Control", "no-cache")
	w.Header().Set("Connection", "keep-alive")

	// Create a channel for this subscriber
	eventCh := make(chan Event, 100)

	// Register subscriber
	pipeline.eventMu.Lock()
	pipeline.subscribers = append(pipeline.subscribers, eventCh)
	// Send existing events
	existingEvents := make([]Event, len(pipeline.Events))
	copy(existingEvents, pipeline.Events)
	pipeline.eventMu.Unlock()

	// Get the flusher
	flusher, ok := w.(http.Flusher)
	if !ok {
		http.Error(w, "streaming not supported", http.StatusInternalServerError)
		return
	}

	// Send existing events first
	for _, event := range existingEvents {
		data, _ := json.Marshal(event)
		fmt.Fprintf(w, "data: %s\n\n", data)
		flusher.Flush()
	}

	// Stream new events
	for {
		select {
		case event := <-eventCh:
			data, _ := json.Marshal(event)
			fmt.Fprintf(w, "data: %s\n\n", data)
			flusher.Flush()
		case <-r.Context().Done():
			// Client disconnected
			return
		}
	}
}

// handleCancelPipeline handles POST /pipelines/{id}/cancel.
func (s *PipelineServer) handleCancelPipeline(w http.ResponseWriter, r *http.Request) {
	id := r.PathValue("id")

	s.mu.Lock()
	pipeline, ok := s.pipelines[id]
	if ok {
		pipeline.Cancel = true
	}
	s.mu.Unlock()

	if !ok {
		http.Error(w, "pipeline not found", http.StatusNotFound)
		return
	}

	w.WriteHeader(http.StatusOK)
	io.WriteString(w, `{"status":"cancelled"}`)
}

// handleGetQuestions handles GET /pipelines/{id}/questions.
func (s *PipelineServer) handleGetQuestions(w http.ResponseWriter, r *http.Request) {
	id := r.PathValue("id")

	s.mu.RLock()
	pipeline, ok := s.pipelines[id]
	s.mu.RUnlock()

	if !ok {
		http.Error(w, "pipeline not found", http.StatusNotFound)
		return
	}

	// Get pending questions
	pipeline.eventMu.RLock()
	questions := make([]*pendingQuestion, len(pipeline.Questions))
	copy(questions, pipeline.Questions)
	pipeline.eventMu.RUnlock()

	// Build response
	type questionResponse struct {
		ID       string    `json:"id"`
		Question *Question `json:"question"`
	}

	response := make([]questionResponse, len(questions))
	for i, q := range questions {
		response[i] = questionResponse{
			ID:       q.ID,
			Question: q.Question,
		}
	}

	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(response)
}

// answerRequest is the request body for answering a question.
type answerRequest struct {
	Value string `json:"value"`
	Text  string `json:"text,omitempty"`
}

// handleAnswerQuestion handles POST /pipelines/{id}/questions/{qid}/answer.
func (s *PipelineServer) handleAnswerQuestion(w http.ResponseWriter, r *http.Request) {
	id := r.PathValue("id")
	qid := r.PathValue("qid")

	s.mu.RLock()
	pipeline, ok := s.pipelines[id]
	s.mu.RUnlock()

	if !ok {
		http.Error(w, "pipeline not found", http.StatusNotFound)
		return
	}

	var req answerRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		http.Error(w, "invalid request body: "+err.Error(), http.StatusBadRequest)
		return
	}

	// Find the question
	pipeline.eventMu.Lock()
	var question *pendingQuestion
	for i, q := range pipeline.Questions {
		if q.ID == qid {
			question = q
			// Remove from pending list
			pipeline.Questions = append(pipeline.Questions[:i], pipeline.Questions[i+1:]...)
			break
		}
	}
	pipeline.eventMu.Unlock()

	if question == nil {
		http.Error(w, "question not found", http.StatusNotFound)
		return
	}

	// Send the answer
	answer := &Answer{
		Value: req.Value,
		Text:  req.Text,
	}

	// Non-blocking send
	select {
	case question.AnswerCh <- answer:
	default:
	}

	w.WriteHeader(http.StatusOK)
	io.WriteString(w, `{"status":"answered"}`)
}

// handleGetCheckpoint handles GET /pipelines/{id}/checkpoint.
func (s *PipelineServer) handleGetCheckpoint(w http.ResponseWriter, r *http.Request) {
	id := r.PathValue("id")

	s.mu.RLock()
	pipeline, ok := s.pipelines[id]
	s.mu.RUnlock()

	if !ok {
		http.Error(w, "pipeline not found", http.StatusNotFound)
		return
	}

	if pipeline.Config == nil || pipeline.Config.LogsRoot == "" {
		http.Error(w, "no checkpoint available (no logs root configured)", http.StatusNotFound)
		return
	}

	cp, err := LoadCheckpoint(pipeline.Config.LogsRoot)
	if err != nil {
		http.Error(w, "checkpoint not found: "+err.Error(), http.StatusNotFound)
		return
	}

	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(cp)
}

// handleGetContext handles GET /pipelines/{id}/context.
func (s *PipelineServer) handleGetContext(w http.ResponseWriter, r *http.Request) {
	id := r.PathValue("id")

	s.mu.RLock()
	pipeline, ok := s.pipelines[id]
	s.mu.RUnlock()

	if !ok {
		http.Error(w, "pipeline not found", http.StatusNotFound)
		return
	}

	if pipeline.Context == nil {
		http.Error(w, "no context available", http.StatusNotFound)
		return
	}

	snapshot := pipeline.Context.Snapshot()

	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(snapshot)
}

// serverInterviewer implements Interviewer for the HTTP server.
// It posts questions to the pipeline's pending questions list and waits for answers.
type serverInterviewer struct {
	pipeline *runningPipeline
	nextQID  int
	mu       sync.Mutex
}

// Ask posts a question and waits for an answer.
func (i *serverInterviewer) Ask(question *Question) (*Answer, error) {
	i.mu.Lock()
	i.nextQID++
	qid := fmt.Sprintf("q-%d", i.nextQID)
	i.mu.Unlock()

	pending := &pendingQuestion{
		ID:       qid,
		Question: question,
		AnswerCh: make(chan *Answer, 1),
	}

	// Add to pending questions
	i.pipeline.eventMu.Lock()
	i.pipeline.Questions = append(i.pipeline.Questions, pending)
	i.pipeline.eventMu.Unlock()

	// Wait for answer with optional timeout
	if question.TimeoutSeconds > 0 {
		timeout := time.Duration(question.TimeoutSeconds * float64(time.Second))
		select {
		case answer := <-pending.AnswerCh:
			return answer, nil
		case <-time.After(timeout):
			// Remove from pending
			i.pipeline.eventMu.Lock()
			for idx, q := range i.pipeline.Questions {
				if q.ID == qid {
					i.pipeline.Questions = append(i.pipeline.Questions[:idx], i.pipeline.Questions[idx+1:]...)
					break
				}
			}
			i.pipeline.eventMu.Unlock()

			return &Answer{Timeout: true}, nil
		}
	}

	// No timeout, wait indefinitely
	answer := <-pending.AnswerCh
	return answer, nil
}
