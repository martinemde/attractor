package pipeline

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"time"

	"github.com/martinemde/attractor/dotparser"
)

// DefaultPollInterval is the default polling interval for the manager loop.
const DefaultPollInterval = 45 * time.Second

// DefaultMaxCycles is the default maximum number of observation cycles.
const DefaultMaxCycles = 1000

// ManagerLoopHandler orchestrates a supervisor loop over a child pipeline.
// It observes the child's telemetry, evaluates progress, and optionally steers
// the child through intervention.
type ManagerLoopHandler struct {
	// Sleeper is used for polling delays. If nil, DefaultSleeper is used.
	Sleeper Sleeper

	// childState tracks active child pipelines for this handler instance.
	// This is used to monitor child completion and ingest telemetry.
	childState *childPipelineState
}

// childPipelineState tracks the state of a running child pipeline.
type childPipelineState struct {
	mu          sync.Mutex
	logsRoot    string
	graph       *dotparser.Graph
	result      *RunResult
	err         error
	done        bool
	completedAt time.Time
}

// NewManagerLoopHandler creates a new ManagerLoopHandler.
func NewManagerLoopHandler() *ManagerLoopHandler {
	return &ManagerLoopHandler{}
}

// Execute runs the manager supervision loop.
// It reads configuration from node and graph attributes, starts the child pipeline
// if configured for autostart, and then enters an observation loop.
func (h *ManagerLoopHandler) Execute(node *dotparser.Node, ctx *Context, graph *dotparser.Graph, logsRoot string) (*Outcome, error) {
	// 1. Read configuration from node/graph attributes
	config := h.readConfig(node, graph)

	sleeper := h.Sleeper
	if sleeper == nil {
		sleeper = DefaultSleeper
	}

	// 2. Auto-start child if configured
	if config.AutoStart {
		if err := h.startChild(config, ctx, logsRoot); err != nil {
			return Fail("failed to start child pipeline: " + err.Error()), nil
		}
	}

	// 3. Observation loop
	for cycle := 1; cycle <= config.MaxCycles; cycle++ {
		// Observe: check child status from context
		if config.Observe {
			h.ingestChildTelemetry(ctx)
		}

		// Steer: optionally write steering instructions (placeholder)
		if config.Steer {
			// Future: implement steering logic
			// For now, this is a no-op placeholder
		}

		// Check stop conditions
		childStatus := ctx.GetString("stack.child.status", "")

		// Check if child is done
		if childStatus == "completed" || childStatus == "failed" {
			childOutcome := ctx.GetString("stack.child.outcome", "")
			if childStatus == "completed" && childOutcome == "success" {
				return Success().WithNotes("Child pipeline completed successfully"), nil
			}
			if childStatus == "failed" {
				reason := ctx.GetString("stack.child.failure_reason", "child pipeline failed")
				return Fail(reason), nil
			}
			// Child completed but outcome is not success - treat as partial success
			return PartialSuccess("Child completed with outcome: " + childOutcome), nil
		}

		// Check custom stop condition
		if config.StopCondition != "" {
			// Use the condition evaluation from the pipeline
			// Create a minimal outcome for condition evaluation
			if EvaluateCondition(config.StopCondition, Success(), ctx) {
				return Success().WithNotes("Stop condition satisfied"), nil
			}
		}

		// Wait before next cycle
		if config.Wait {
			sleeper.Sleep(config.PollInterval)
		}
	}

	// Max cycles exceeded
	return Fail("max cycles exceeded without child completion"), nil
}

// managerConfig holds the parsed configuration for the manager loop.
type managerConfig struct {
	ChildDotfile  string
	PollInterval  time.Duration
	MaxCycles     int
	StopCondition string
	AutoStart     bool
	Observe       bool
	Steer         bool
	Wait          bool
}

// readConfig reads and parses manager configuration from node and graph attributes.
func (h *ManagerLoopHandler) readConfig(node *dotparser.Node, graph *dotparser.Graph) *managerConfig {
	config := &managerConfig{
		PollInterval: DefaultPollInterval,
		MaxCycles:    DefaultMaxCycles,
		AutoStart:    true,
		Observe:      true,
		Steer:        false,
		Wait:         true,
	}

	// Read child dotfile from graph attributes
	if attr, ok := graph.GraphAttr("stack.child_dotfile"); ok {
		config.ChildDotfile = attr.Str
	}

	// Read poll interval from node attributes (e.g. manager.poll_interval="100ms")
	if attr, ok := node.Attr("manager.poll_interval"); ok {
		if d, err := time.ParseDuration(attr.Str); err == nil {
			config.PollInterval = d
		}
	}

	// Read max cycles from node attributes
	if attr, ok := node.Attr("manager.max_cycles"); ok {
		if attr.Kind == dotparser.ValueInt {
			config.MaxCycles = int(attr.Int)
		}
	}

	// Read stop condition from node attributes
	if attr, ok := node.Attr("manager.stop_condition"); ok {
		config.StopCondition = attr.Str
	}

	// Read autostart from graph attributes (default true)
	if attr, ok := graph.GraphAttr("stack.child_autostart"); ok {
		config.AutoStart = attr.Str != "false" && attr.Bool
	}

	// Read actions from node attributes (default "observe,wait")
	actionsStr := "observe,wait"
	if attr, ok := node.Attr("manager.actions"); ok {
		actionsStr = attr.Str
	}

	// Parse actions
	actions := strings.Split(actionsStr, ",")
	config.Observe = false
	config.Steer = false
	config.Wait = false
	for _, action := range actions {
		action = strings.TrimSpace(action)
		switch action {
		case "observe":
			config.Observe = true
		case "steer":
			config.Steer = true
		case "wait":
			config.Wait = true
		}
	}

	return config
}

// startChild starts the child pipeline.
// It reads the child DOT file, parses it, and starts execution in a goroutine.
// Progress is tracked via file-based telemetry (checkpoint.json and status.json files).
func (h *ManagerLoopHandler) startChild(config *managerConfig, ctx *Context, logsRoot string) error {
	// If child status is already set (e.g., by tests), skip actual startup
	if status, ok := ctx.Get("stack.child.status"); ok && status != "" {
		ctx.Set("stack.child.dotfile", config.ChildDotfile)
		return nil
	}

	// Validate dotfile path
	if config.ChildDotfile == "" {
		return fmt.Errorf("stack.child_dotfile not specified in graph attributes")
	}

	// Read and parse the child DOT file
	dotContent, err := os.ReadFile(config.ChildDotfile)
	if err != nil {
		return fmt.Errorf("failed to read child dotfile %q: %w", config.ChildDotfile, err)
	}

	childGraph, err := dotparser.Parse(dotContent)
	if err != nil {
		return fmt.Errorf("failed to parse child dotfile: %w", err)
	}

	// Create a logs directory for the child pipeline
	childLogsRoot := ""
	if logsRoot != "" {
		childLogsRoot = filepath.Join(logsRoot, "child-"+childGraph.Name)
		if err := os.MkdirAll(childLogsRoot, 0o755); err != nil {
			return fmt.Errorf("failed to create child logs directory: %w", err)
		}
	}

	// Initialize child state tracking
	h.childState = &childPipelineState{
		logsRoot: childLogsRoot,
		graph:    childGraph,
	}

	// Set initial context values
	ctx.Set("stack.child.status", "running")
	ctx.Set("stack.child.dotfile", config.ChildDotfile)
	ctx.Set("stack.child.logs_root", childLogsRoot)
	ctx.Set("stack.child.started_at", time.Now().Format(time.RFC3339))
	ctx.Set("stack.child.graph_name", childGraph.Name)

	// Start child pipeline execution in a goroutine
	go func() {
		childConfig := &RunConfig{
			LogsRoot: childLogsRoot,
			Sleeper:  h.Sleeper,
			// Child inherits the default registry
		}

		result, err := RunLoop(childGraph, childConfig)

		// Update child state with result
		h.childState.mu.Lock()
		h.childState.result = result
		h.childState.err = err
		h.childState.done = true
		h.childState.completedAt = time.Now()
		h.childState.mu.Unlock()
	}()

	return nil
}

// ingestChildTelemetry reads child status from various sources and updates context.
// It checks both the in-memory child state (for goroutine-based execution) and
// file-based telemetry (checkpoint.json and status.json files).
func (h *ManagerLoopHandler) ingestChildTelemetry(ctx *Context) {
	// First, check in-memory child state from goroutine execution
	if h.childState != nil {
		h.childState.mu.Lock()
		done := h.childState.done
		result := h.childState.result
		childErr := h.childState.err
		logsRoot := h.childState.logsRoot
		h.childState.mu.Unlock()

		if done {
			// Child goroutine has completed
			if childErr != nil {
				ctx.Set("stack.child.status", "failed")
				ctx.Set("stack.child.failure_reason", childErr.Error())
				return
			}
			if result != nil {
				ctx.Set("stack.child.status", "completed")
				if result.FinalOutcome != nil {
					ctx.Set("stack.child.outcome", result.FinalOutcome.Status.String())
					if result.FinalOutcome.FailureReason != "" {
						ctx.Set("stack.child.failure_reason", result.FinalOutcome.FailureReason)
					}
				} else {
					ctx.Set("stack.child.outcome", "success")
				}
				ctx.Set("stack.child.completed_nodes", result.CompletedNodes)
				return
			}
		}

		// Child still running - try to read telemetry from files
		if logsRoot != "" {
			h.ingestTelemetryFromFiles(ctx, logsRoot)
		}
		return
	}

	// Fallback: try to read from context-specified logs root
	logsRoot := ctx.GetString("stack.child.logs_root", "")
	if logsRoot != "" {
		h.ingestTelemetryFromFiles(ctx, logsRoot)
	}
}

// ingestTelemetryFromFiles reads telemetry from checkpoint and status files.
func (h *ManagerLoopHandler) ingestTelemetryFromFiles(ctx *Context, logsRoot string) {
	// Read checkpoint.json for current progress
	checkpoint, err := LoadCheckpoint(logsRoot)
	if err == nil && checkpoint != nil {
		// Update context with checkpoint data
		ctx.Set("stack.child.current_node", checkpoint.CurrentNode)
		ctx.Set("stack.child.completed_nodes", checkpoint.CompletedNodes)

		// Copy retry counts with stack.child prefix
		for nodeID, count := range checkpoint.NodeRetries {
			ctx.Set("stack.child.retry_count."+nodeID, count)
		}
	}

	// Read status.json files from completed node directories
	nodeOutcomes := make(map[string]string)
	nodeArtifacts := make(map[string][]string)

	entries, err := os.ReadDir(logsRoot)
	if err != nil {
		return
	}

	for _, entry := range entries {
		if !entry.IsDir() {
			continue
		}
		nodeID := entry.Name()
		nodeDir := filepath.Join(logsRoot, nodeID)

		// Read status.json
		statusPath := filepath.Join(nodeDir, "status.json")
		statusData, err := os.ReadFile(statusPath)
		if err != nil {
			continue
		}

		var status map[string]any
		if err := json.Unmarshal(statusData, &status); err != nil {
			continue
		}

		if statusStr, ok := status["status"].(string); ok {
			nodeOutcomes[nodeID] = statusStr
		}

		// Collect artifacts from node directory
		artifactEntries, err := os.ReadDir(nodeDir)
		if err != nil {
			continue
		}
		var artifacts []string
		for _, ae := range artifactEntries {
			if ae.Name() != "status.json" {
				artifacts = append(artifacts, filepath.Join(nodeDir, ae.Name()))
			}
		}
		if len(artifacts) > 0 {
			nodeArtifacts[nodeID] = artifacts
		}
	}

	// Update context with aggregated data
	if len(nodeOutcomes) > 0 {
		ctx.Set("stack.child.node_outcomes", nodeOutcomes)
	}
	if len(nodeArtifacts) > 0 {
		ctx.Set("stack.child.artifacts", nodeArtifacts)
	}
}
