package pipeline

import (
	"context"
	"errors"
	"testing"

	"github.com/martinemde/attractor/dotparser"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestLLMBackend_Run_ReturnsResponse(t *testing.T) {
	backend := &LLMBackend{
		RunFunc: func(ctx context.Context, prompt string, pctx *Context) (string, error) {
			return "mock response for: " + prompt, nil
		},
	}

	node := &dotparser.Node{ID: "test_node"}
	pctx := NewContext()

	result, err := backend.Run(node, "hello world", pctx)
	require.NoError(t, err)
	assert.Equal(t, "mock response for: hello world", result)
}

func TestLLMBackend_Run_ReturnsError(t *testing.T) {
	backend := &LLMBackend{
		RunFunc: func(ctx context.Context, prompt string, pctx *Context) (string, error) {
			return "", errors.New("llm unavailable")
		},
	}

	node := &dotparser.Node{ID: "test_node"}
	pctx := NewContext()

	result, err := backend.Run(node, "hello", pctx)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "llm unavailable")
	assert.Nil(t, result)
}

func TestLLMBackend_Run_NilRunFunc(t *testing.T) {
	backend := &LLMBackend{}

	node := &dotparser.Node{ID: "test_node"}
	pctx := NewContext()

	result, err := backend.Run(node, "hello", pctx)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "RunFunc is nil")
	assert.Nil(t, result)
}

func TestLLMBackend_IntegrationWithCodergenHandler(t *testing.T) {
	backend := &LLMBackend{
		RunFunc: func(ctx context.Context, prompt string, pctx *Context) (string, error) {
			return "implementation complete", nil
		},
	}

	handler := NewCodergenHandler(backend)
	node := &dotparser.Node{
		ID: "implement",
		Attrs: []dotparser.Attr{
			{Key: "prompt", Value: dotparser.Value{Kind: dotparser.ValueString, Str: "Write the code"}},
		},
	}
	graph := &dotparser.Graph{Name: "Test"}
	pctx := NewContext()

	outcome, err := handler.Execute(node, pctx, graph, "")
	require.NoError(t, err)
	assert.Equal(t, StatusSuccess, outcome.Status)
}

func TestLLMBackend_ErrorBecomesFailOutcome(t *testing.T) {
	backend := &LLMBackend{
		RunFunc: func(ctx context.Context, prompt string, pctx *Context) (string, error) {
			return "", errors.New("rate limited")
		},
	}

	handler := NewCodergenHandler(backend)
	node := &dotparser.Node{
		ID: "implement",
		Attrs: []dotparser.Attr{
			{Key: "prompt", Value: dotparser.Value{Kind: dotparser.ValueString, Str: "Write code"}},
		},
	}
	graph := &dotparser.Graph{Name: "Test"}
	pctx := NewContext()

	outcome, err := handler.Execute(node, pctx, graph, "")
	require.NoError(t, err) // CodergenHandler catches backend errors as Fail outcomes
	assert.Equal(t, StatusFail, outcome.Status)
	assert.Contains(t, outcome.FailureReason, "rate limited")
}
