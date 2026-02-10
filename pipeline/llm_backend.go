package pipeline

import (
	"context"
	"fmt"

	"github.com/martinemde/attractor/dotparser"
)

// LLMBackend is a CodergenBackend that delegates to a function for LLM calls.
// The RunFunc receives a Go context, the prompt, and the pipeline context for
// accessing prior stage outputs and context variables.
// This design keeps the pipeline package free of agentloop imports while
// allowing the CLI to wire in any LLM execution strategy.
type LLMBackend struct {
	RunFunc func(ctx context.Context, prompt string, pctx *Context) (string, error)
}

// Run executes the LLM backend for a pipeline node.
// Uses the Go context from the pipeline context for cancellation support.
func (b *LLMBackend) Run(node *dotparser.Node, prompt string, pctx *Context) (any, error) {
	if b.RunFunc == nil {
		return nil, fmt.Errorf("LLMBackend.RunFunc is nil")
	}

	// Use Go context from pipeline context for cancellation propagation
	ctx := context.Background()
	if pctx != nil {
		ctx = pctx.GoCtx()
	}

	result, err := b.RunFunc(ctx, prompt, pctx)
	if err != nil {
		return nil, err
	}
	return result, nil
}
