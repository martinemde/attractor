package pipeline

import (
	"fmt"

	"github.com/martinemde/attractor/dotparser"
)

// HandlerRegistry maps type strings to handler instances.
// Resolution follows: explicit type -> shape-based -> default handler.
type HandlerRegistry struct {
	handlers       map[string]Handler
	defaultHandler Handler
}

// NewHandlerRegistry creates a registry with the given default handler.
// If defaultHandler is nil, resolution will return an error when no
// handler matches.
func NewHandlerRegistry(defaultHandler Handler) *HandlerRegistry {
	return &HandlerRegistry{
		handlers:       make(map[string]Handler),
		defaultHandler: defaultHandler,
	}
}

// Register adds or replaces a handler for the given type string.
func (r *HandlerRegistry) Register(typeString string, handler Handler) {
	r.handlers[typeString] = handler
}

// Resolve returns the handler for a node using the three-step resolution:
//  1. Explicit type attribute on the node
//  2. Shape-based resolution via ShapeToHandlerType
//  3. Default handler
func (r *HandlerRegistry) Resolve(node *dotparser.Node) (Handler, error) {
	// 1. Explicit type attribute
	if typeVal, ok := node.Attr("type"); ok && typeVal.Str != "" {
		if h, exists := r.handlers[typeVal.Str]; exists {
			return h, nil
		}
		return nil, fmt.Errorf("no handler registered for type %q", typeVal.Str)
	}

	// 2. Shape-based resolution
	shape := "box" // default shape per spec
	if shapeVal, ok := node.Attr("shape"); ok && shapeVal.Str != "" {
		shape = shapeVal.Str
	}
	if handlerType, ok := ShapeToHandlerType[shape]; ok {
		if h, exists := r.handlers[handlerType]; exists {
			return h, nil
		}
	}

	// 3. Default handler
	if r.defaultHandler != nil {
		return r.defaultHandler, nil
	}

	return nil, fmt.Errorf("no handler found for node %q (shape=%q) and no default handler", node.ID, shape)
}

// NewDefaultRegistry creates a HandlerRegistry pre-populated with the
// built-in start and exit handlers. The defaultHandler is used as the
// fallback for nodes that don't match any registered type.
func NewDefaultRegistry(defaultHandler Handler) *HandlerRegistry {
	r := NewHandlerRegistry(defaultHandler)
	r.Register("start", &StartHandler{})
	r.Register("exit", &ExitHandler{})
	return r
}
