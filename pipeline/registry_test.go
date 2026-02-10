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
