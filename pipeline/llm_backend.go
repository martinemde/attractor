package pipeline

import (
	"context"
	"fmt"

	"github.com/martinemde/attractor/dotparser"
)

// LLMBackend is a CodergenBackend that delegates to a function for LLM calls.
// The RunFunc receives a context and prompt and returns the LLM response text.
// This design keeps the pipeline package free of agentloop imports while
// allowing the CLI to wire in any LLM execution strategy.
type LLMBackend struct {
	RunFunc func(ctx context.Context, prompt string) (string, error)
}

// Run executes the LLM backend for a pipeline node.
func (b *LLMBackend) Run(node *dotparser.Node, prompt string, pctx *Context) (any, error) {
	if b.RunFunc == nil {
		return nil, fmt.Errorf("LLMBackend.RunFunc is nil")
	}
	result, err := b.RunFunc(context.Background(), prompt)
	if err != nil {
		return nil, err
	}
	return result, nil
}
