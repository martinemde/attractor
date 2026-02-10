package pipeline

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestHandlerRegistry_ResolveByType(t *testing.T) {
	reg := NewHandlerRegistry(nil)
	handler := &StartHandler{}
	reg.Register("my_type", handler)

	node := makeNode("n", makeAttr("type", "my_type"))
	resolved, err := reg.Resolve(node)
	require.NoError(t, err)
	assert.Equal(t, handler, resolved)
}

func TestHandlerRegistry_ResolveByShape(t *testing.T) {
	reg := NewDefaultRegistry(nil)

	// Mdiamond -> start handler
	node := makeNode("s", makeAttr("shape", "Mdiamond"))
	resolved, err := reg.Resolve(node)
	require.NoError(t, err)
	_, ok := resolved.(*StartHandler)
	assert.True(t, ok)

	// Msquare -> exit handler
	node = makeNode("e", makeAttr("shape", "Msquare"))
	resolved, err = reg.Resolve(node)
	require.NoError(t, err)
	_, ok = resolved.(*ExitHandler)
	assert.True(t, ok)
}

func TestHandlerRegistry_ResolveDefault(t *testing.T) {
	defaultHandler := &StartHandler{} // using start as a stand-in
	reg := NewDefaultRegistry(defaultHandler)

	// Node with shape=box, no explicit type -> should use default
	node := makeNode("n", makeAttr("shape", "box"))
	resolved, err := reg.Resolve(node)
	require.NoError(t, err)
	// box maps to "codergen" in ShapeToHandlerType, but we didn't register
	// a codergen handler, so it falls through to default
	assert.Equal(t, defaultHandler, resolved)
}

func TestHandlerRegistry_ResolveNoHandler(t *testing.T) {
	reg := NewHandlerRegistry(nil) // no default

	node := makeNode("n", makeAttr("type", "nonexistent"))
	_, err := reg.Resolve(node)
	assert.Error(t, err)
	assert.Contains(t, err.Error(), "no handler registered for type")
}

func TestHandlerRegistry_ResolveNoShapeNoDefault(t *testing.T) {
	reg := NewHandlerRegistry(nil)
	node := makeNode("n") // no type, no shape -> default shape "box" -> "codergen" -> not registered -> no default
	_, err := reg.Resolve(node)
	assert.Error(t, err)
}

func TestHandlerRegistry_TypePrecedenceOverShape(t *testing.T) {
	reg := NewDefaultRegistry(nil)
	customHandler := &ExitHandler{} // using exit as a stand-in
	reg.Register("custom", customHandler)

	// Node has shape=Mdiamond (which would resolve to start), but explicit type overrides
	node := makeNode("n", makeAttr("shape", "Mdiamond"), makeAttr("type", "custom"))
	resolved, err := reg.Resolve(node)
	require.NoError(t, err)
	assert.Equal(t, customHandler, resolved)
}

func TestHandlerRegistry_RegisterOverwrites(t *testing.T) {
	reg := NewHandlerRegistry(nil)
	handler1 := &StartHandler{}
	handler2 := &ExitHandler{}

	reg.Register("test", handler1)
	reg.Register("test", handler2)

	node := makeNode("n", makeAttr("type", "test"))
	resolved, err := reg.Resolve(node)
	require.NoError(t, err)
	assert.Equal(t, handler2, resolved) // second registration wins
}
