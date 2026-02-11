package pipeline

import (
	"github.com/martinemde/attractor/dotparser"
)

// shapeToHandlerType maps Graphviz shapes to handler type strings.
// This is the canonical shape-to-handler-type mapping from Section 2.8 of the spec.
var shapeToHandlerType = map[string]string{
	"Mdiamond":       "start",
	"Msquare":        "exit",
	"box":            "codergen",
	"hexagon":        "wait.human",
	"diamond":        "conditional",
	"component":      "parallel",
	"tripleoctagon":  "parallel.fan_in",
	"parallelogram":  "tool",
	"house":          "stack.manager_loop",
}

// HandlerRegistry maps type strings to handler instances.
// It resolves which handler should execute a given node.
type HandlerRegistry struct {
	handlers       map[string]Handler
	defaultHandler Handler
}

// NewHandlerRegistry creates a new empty HandlerRegistry.
func NewHandlerRegistry() *HandlerRegistry {
	return &HandlerRegistry{
		handlers: make(map[string]Handler),
	}
}

// Register adds a handler for the given type string.
// Registering for an already-registered type replaces the previous handler.
func (r *HandlerRegistry) Register(typeString string, handler Handler) {
	r.handlers[typeString] = handler
}

// SetDefaultHandler sets the fallback handler for nodes that don't match
// any explicit type or shape-based resolution.
func (r *HandlerRegistry) SetDefaultHandler(handler Handler) {
	r.defaultHandler = handler
}

// Resolve returns the handler for the given node.
// Resolution order:
//  1. Explicit type attribute on the node
//  2. Shape-based resolution using the shape-to-handler-type mapping
//  3. Default handler
func (r *HandlerRegistry) Resolve(node *dotparser.Node) Handler {
	// Step 1: Check for explicit type attribute
	if typeAttr, ok := node.Attr("type"); ok {
		typeStr := typeAttr.Str
		if handler, exists := r.handlers[typeStr]; exists {
			return handler
		}
	}

	// Step 2: Shape-based resolution
	if shapeAttr, ok := node.Attr("shape"); ok {
		shapeStr := shapeAttr.Str
		if handlerType, exists := shapeToHandlerType[shapeStr]; exists {
			if handler, exists := r.handlers[handlerType]; exists {
				return handler
			}
		}
	}

	// Step 3: Default handler
	return r.defaultHandler
}

// DefaultRegistry creates a HandlerRegistry with the standard handlers registered.
// This includes start, exit, and codergen handlers.
// For human-in-the-loop support, use DefaultRegistryWithInterviewer instead.
func DefaultRegistry() *HandlerRegistry {
	return DefaultRegistryWithInterviewer(nil)
}

// DefaultRegistryWithInterviewer creates a HandlerRegistry with standard handlers
// and configures the wait.human handler with the provided interviewer.
func DefaultRegistryWithInterviewer(interviewer Interviewer) *HandlerRegistry {
	r := NewHandlerRegistry()
	r.Register("start", &StartHandler{})
	r.Register("exit", &ExitHandler{})

	// Register codergen handler for shape=box and as the default fallback
	codergenHandler := NewCodergenHandler(nil) // simulation mode
	r.Register("codergen", codergenHandler)
	r.SetDefaultHandler(codergenHandler)

	// Register wait.human handler for shape=hexagon
	r.Register("wait.human", NewWaitForHumanHandler(interviewer))

	// Register conditional handler for shape=diamond
	r.Register("conditional", &ConditionalHandler{})

	// Register parallel handler for shape=component
	// Note: The registry reference is set after creation to avoid circular dependency
	parallelHandler := NewParallelHandler(nil)
	r.Register("parallel", parallelHandler)
	// Set registry after registration to allow parallel branches to resolve handlers
	parallelHandler.Registry = r

	// Register fan-in handler for shape=tripleoctagon
	r.Register("parallel.fan_in", &FanInHandler{})

	// Register tool handler for shape=parallelogram
	r.Register("tool", &ToolHandler{})

	// Register manager loop handler for shape=house
	r.Register("stack.manager_loop", NewManagerLoopHandler())

	return r
}
