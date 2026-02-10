package pipeline

import (
	"testing"

	"github.com/martinemde/attractor/dotparser"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestStartHandler(t *testing.T) {
	h := &StartHandler{}
	outcome, err := h.Execute(makeNode("start"), NewContext(), nil, "")
	require.NoError(t, err)
	assert.Equal(t, StatusSuccess, outcome.Status)
}

func TestExitHandler(t *testing.T) {
	h := &ExitHandler{}
	outcome, err := h.Execute(makeNode("exit"), NewContext(), nil, "")
	require.NoError(t, err)
	assert.Equal(t, StatusSuccess, outcome.Status)
}

func TestHandlerFunc(t *testing.T) {
	called := false
	h := HandlerFunc(func(node *dotparser.Node, ctx *Context, graph *dotparser.Graph, logsRoot string) (*Outcome, error) {
		called = true
		return &Outcome{Status: StatusSuccess, Notes: "via func"}, nil
	})

	outcome, err := h.Execute(makeNode("n"), NewContext(), nil, "")
	require.NoError(t, err)
	assert.True(t, called)
	assert.Equal(t, "via func", outcome.Notes)
}
