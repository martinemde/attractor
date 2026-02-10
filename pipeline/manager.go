package pipeline

import (
	"strings"
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
		if err := h.startChild(config, ctx); err != nil {
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

	// Read poll interval from node attributes
	if attr, ok := node.Attr("manager.poll_interval"); ok {
		if attr.Kind == dotparser.ValueDuration {
			config.PollInterval = attr.Duration
		} else if d, err := time.ParseDuration(attr.Str); err == nil {
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
// For now, this is a stub that sets initial context values.
// In a full implementation, this would parse the child DOT file and run it
// in a goroutine, monitoring progress via shared context or file-based communication.
func (h *ManagerLoopHandler) startChild(config *managerConfig, ctx *Context) error {
	// Only set initial child status if not already set (allows tests to pre-configure)
	if _, ok := ctx.Get("stack.child.status"); !ok {
		ctx.Set("stack.child.status", "running")
	}
	ctx.Set("stack.child.dotfile", config.ChildDotfile)
	ctx.Set("stack.child.started_at", time.Now().Format(time.RFC3339))

	// In a full implementation:
	// 1. Parse the child DOT file
	// 2. Start execution in a goroutine
	// 3. Set up a communication channel or file-based status updates

	return nil
}

// ingestChildTelemetry reads child status from various sources.
// For now, this reads from context keys that would be set by a running child.
func (h *ManagerLoopHandler) ingestChildTelemetry(ctx *Context) {
	// In a full implementation, this would:
	// 1. Read checkpoint files from the child's logs directory
	// 2. Parse status.json files from completed stages
	// 3. Update context with child progress metrics

	// For now, we just ensure the context keys are accessible
	// The actual telemetry would come from:
	// - stack.child.status: "running", "completed", "failed"
	// - stack.child.outcome: "success", "fail", "partial_success"
	// - stack.child.current_node: the active stage ID
	// - stack.child.completed_nodes: list of completed node IDs
}
