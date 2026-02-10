package pipeline

import (
	"testing"

	"github.com/martinemde/attractor/dotparser"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestHandlerRegistry_ExplicitTypeResolution(t *testing.T) {
	registry := NewHandlerRegistry()

	customHandler := &MockHandler{}
	registry.Register("custom", customHandler)

	// Also register a shape-based handler
	boxHandler := &MockHandler{}
	registry.Register("codergen", boxHandler)

	// Node with explicit type should use that, not shape-based
	node := newNode("test",
		strAttr("type", "custom"),
		strAttr("shape", "box"),
	)

	resolved := registry.Resolve(node)

	assert.Equal(t, customHandler, resolved)
}

func TestHandlerRegistry_ShapeBasedResolution(t *testing.T) {
	registry := NewHandlerRegistry()

	startHandler := &MockHandler{}
	registry.Register("start", startHandler)

	exitHandler := &MockHandler{}
	registry.Register("exit", exitHandler)

	// Node with shape=Mdiamond should resolve to start handler
	startNode := newNode("entry", strAttr("shape", "Mdiamond"))
	assert.Equal(t, startHandler, registry.Resolve(startNode))

	// Node with shape=Msquare should resolve to exit handler
	exitNode := newNode("done", strAttr("shape", "Msquare"))
	assert.Equal(t, exitHandler, registry.Resolve(exitNode))
}

func TestHandlerRegistry_DefaultHandlerFallback(t *testing.T) {
	registry := NewHandlerRegistry()

	defaultHandler := &MockHandler{}
	registry.SetDefaultHandler(defaultHandler)

	// Node with no type and unrecognized shape
	node := newNode("unknown", strAttr("shape", "ellipse"))

	resolved := registry.Resolve(node)

	assert.Equal(t, defaultHandler, resolved)
}

func TestHandlerRegistry_ResolutionOrder(t *testing.T) {
	// Full resolution order test: explicit type > shape > default
	registry := NewHandlerRegistry()

	explicitHandler := &MockHandler{}
	shapeHandler := &MockHandler{}
	defaultHandler := &MockHandler{}

	registry.Register("explicit_type", explicitHandler)
	registry.Register("codergen", shapeHandler) // codergen is for shape=box
	registry.SetDefaultHandler(defaultHandler)

	tests := []struct {
		name     string
		node     *dotparser.Node
		expected Handler
	}{
		{
			name:     "explicit type wins over shape",
			node:     newNode("n1", strAttr("type", "explicit_type"), strAttr("shape", "box")),
			expected: explicitHandler,
		},
		{
			name:     "shape resolution when no explicit type",
			node:     newNode("n2", strAttr("shape", "box")),
			expected: shapeHandler,
		},
		{
			name:     "default when no type and unknown shape",
			node:     newNode("n3", strAttr("shape", "ellipse")),
			expected: defaultHandler,
		},
		{
			name:     "default when no type and no shape",
			node:     newNode("n4"),
			expected: defaultHandler,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			resolved := registry.Resolve(tt.node)
			assert.Equal(t, tt.expected, resolved)
		})
	}
}

func TestHandlerRegistry_ReregisterReplacesHandler(t *testing.T) {
	registry := NewHandlerRegistry()

	firstHandler := &MockHandler{}
	secondHandler := &MockHandler{}

	registry.Register("mytype", firstHandler)
	assert.Equal(t, firstHandler, registry.Resolve(newNode("n", strAttr("type", "mytype"))))

	// Re-register should replace
	registry.Register("mytype", secondHandler)
	assert.Equal(t, secondHandler, registry.Resolve(newNode("n", strAttr("type", "mytype"))))
}

func TestHandlerRegistry_ShapeToHandlerMapping(t *testing.T) {
	// Test all documented shape-to-handler mappings
	registry := NewHandlerRegistry()

	// Register handlers for all expected types
	handlers := map[string]*MockHandler{
		"start":              {},
		"exit":               {},
		"codergen":           {},
		"wait.human":         {},
		"conditional":        {},
		"parallel":           {},
		"parallel.fan_in":    {},
		"tool":               {},
		"stack.manager_loop": {},
	}

	for handlerType, handler := range handlers {
		registry.Register(handlerType, handler)
	}

	tests := []struct {
		shape        string
		expectedType string
	}{
		{"Mdiamond", "start"},
		{"Msquare", "exit"},
		{"box", "codergen"},
		{"hexagon", "wait.human"},
		{"diamond", "conditional"},
		{"component", "parallel"},
		{"tripleoctagon", "parallel.fan_in"},
		{"parallelogram", "tool"},
		{"house", "stack.manager_loop"},
	}

	for _, tt := range tests {
		t.Run(tt.shape, func(t *testing.T) {
			node := newNode("test", strAttr("shape", tt.shape))
			resolved := registry.Resolve(node)
			assert.Equal(t, handlers[tt.expectedType], resolved)
		})
	}
}

func TestHandlerRegistry_UnknownTypeAttribute(t *testing.T) {
	// When type attribute exists but handler not registered, fall through to shape
	registry := NewHandlerRegistry()

	boxHandler := &MockHandler{}
	registry.Register("codergen", boxHandler)

	defaultHandler := &MockHandler{}
	registry.SetDefaultHandler(defaultHandler)

	// Node with unknown type but known shape
	node := newNode("test", strAttr("type", "unknown"), strAttr("shape", "box"))

	// Should fall through to shape-based resolution
	resolved := registry.Resolve(node)
	assert.Equal(t, boxHandler, resolved)
}

func TestHandlerRegistry_UnknownShapeWithDefault(t *testing.T) {
	registry := NewHandlerRegistry()

	defaultHandler := &MockHandler{}
	registry.SetDefaultHandler(defaultHandler)

	// Node with unknown shape
	node := newNode("test", strAttr("shape", "star"))

	resolved := registry.Resolve(node)
	assert.Equal(t, defaultHandler, resolved)
}

func TestHandlerRegistry_NilWhenNoMatch(t *testing.T) {
	registry := NewHandlerRegistry()
	// No default handler set

	node := newNode("orphan")

	resolved := registry.Resolve(node)
	assert.Nil(t, resolved)
}

func TestDefaultRegistry(t *testing.T) {
	registry := DefaultRegistry()

	// Should have start and exit handlers registered
	startNode := newNode("s", strAttr("shape", "Mdiamond"))
	exitNode := newNode("e", strAttr("shape", "Msquare"))

	startHandler := registry.Resolve(startNode)
	exitHandler := registry.Resolve(exitNode)

	require.NotNil(t, startHandler)
	require.NotNil(t, exitHandler)

	// Verify they work correctly
	outcome, err := startHandler.Execute(startNode, NewContext(), nil, "")
	require.NoError(t, err)
	assert.Equal(t, StatusSuccess, outcome.Status)

	outcome, err = exitHandler.Execute(exitNode, NewContext(), nil, "")
	require.NoError(t, err)
	assert.Equal(t, StatusSuccess, outcome.Status)
}

// TestCustomHandlerEndToEnd demonstrates end-to-end custom handler registration
// and execution through a pipeline, as per Section 4.12 of the spec.
func TestCustomHandlerEndToEnd(t *testing.T) {
	tmpDir := t.TempDir()

	// Define a custom handler that implements business logic
	customExecuted := false
	customContextValue := ""

	customHandler := HandlerFunc(func(node *dotparser.Node, ctx *Context, graph *dotparser.Graph, logsRoot string) (*Outcome, error) {
		customExecuted = true

		// Read a custom attribute
		if customAttr, ok := node.Attr("custom_attr"); ok {
			customContextValue = customAttr.Str
		}

		return Success().
			WithContextUpdate("custom.executed", true).
			WithContextUpdate("custom.value", customContextValue).
			WithNotes("Custom handler executed for " + node.ID), nil
	})

	// Register the custom handler
	registry := DefaultRegistry()
	registry.Register("my_custom_type", customHandler)

	// Create a graph that uses the custom handler
	graph := newTestGraph(
		[]*dotparser.Node{
			newNode("start", strAttr("shape", "Mdiamond")),
			newNode("custom_node",
				strAttr("type", "my_custom_type"),
				strAttr("custom_attr", "test_value"),
				strAttr("label", "Custom Processing"),
			),
			newNode("exit", strAttr("shape", "Msquare")),
		},
		[]*dotparser.Edge{
			newEdge("start", "custom_node"),
			newEdge("custom_node", "exit"),
		},
		nil,
	)

	// Run the pipeline
	result, err := Run(graph, &RunConfig{
		Registry: registry,
		LogsRoot: tmpDir,
	})

	require.NoError(t, err)
	assert.Equal(t, StatusSuccess, result.FinalOutcome.Status)

	// Verify the custom handler was executed
	assert.True(t, customExecuted, "Custom handler should have been executed")
	assert.Equal(t, "test_value", customContextValue, "Custom handler should have read custom_attr")

	// Verify the completed nodes include our custom node
	assert.Contains(t, result.CompletedNodes, "custom_node")

	// Verify context updates from the custom handler were applied
	executed, ok := result.Context.Get("custom.executed")
	assert.True(t, ok)
	assert.Equal(t, true, executed)

	value, ok := result.Context.Get("custom.value")
	assert.True(t, ok)
	assert.Equal(t, "test_value", value)
}
