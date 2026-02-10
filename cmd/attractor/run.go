package main

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"time"

	"github.com/martinemde/attractor/agentloop"
	"github.com/martinemde/attractor/dotparser"
	"github.com/martinemde/attractor/pipeline"
	"github.com/martinemde/attractor/unifiedllm"
	"github.com/spf13/cobra"
	"github.com/spf13/viper"
)

var runCmd = &cobra.Command{
	Use:   "run <pipeline.dot>",
	Short: "Run a DOT-defined pipeline",
	Long:  "Parse and execute a pipeline defined in a DOT file, running each stage against an LLM.",
	Args:  cobra.ExactArgs(1),
	RunE:  runPipeline,
}

func init() {
	runCmd.Flags().String("logs-dir", "", "Log directory (default: ./logs/<timestamp>)")
	runCmd.Flags().Bool("dry-run", false, "Parse and validate only, do not execute")
	runCmd.Flags().Bool("simple", false, "Use simple completion mode (no tools)")
	runCmd.Flags().String("working-dir", ".", "Working directory for agent execution")
	runCmd.Flags().String("goal", "", "Override pipeline goal attribute")

	_ = viper.BindPFlag("logs_dir", runCmd.Flags().Lookup("logs-dir"))

	rootCmd.AddCommand(runCmd)
}

func runPipeline(cmd *cobra.Command, args []string) error {
	dotFile := args[0]
	model := viper.GetString("model")
	provider := viper.GetString("provider")
	verbose := viper.GetBool("verbose")
	awsProfile := viper.GetString("aws_profile")
	dryRun, _ := cmd.Flags().GetBool("dry-run")
	simple, _ := cmd.Flags().GetBool("simple")
	workDir, _ := cmd.Flags().GetString("working-dir")
	goalOverride, _ := cmd.Flags().GetString("goal")

	// When an AWS profile is specified, configure the default LLM client to
	// use the SDK credential chain so SSO tokens are resolved automatically.
	if awsProfile != "" {
		client := unifiedllm.NewClientFromEnvWithOptions(unifiedllm.ClientFromEnvOptions{
			AWSProfile: awsProfile,
		})
		unifiedllm.SetDefaultClient(client)
	}

	// Resolve working directory to absolute path.
	absWorkDir, err := filepath.Abs(workDir)
	if err != nil {
		return fmt.Errorf("resolving working directory: %w", err)
	}

	// Read and parse the DOT file.
	src, err := os.ReadFile(dotFile)
	if err != nil {
		return fmt.Errorf("reading pipeline file: %w", err)
	}

	graph, err := dotparser.Parse(src)
	if err != nil {
		return fmt.Errorf("parsing pipeline: %w", err)
	}

	if verbose {
		fmt.Fprintf(os.Stderr, "Pipeline: %s (%d nodes, %d edges)\n", graph.Name, len(graph.Nodes), len(graph.Edges))
	}

	// Override goal if specified.
	if goalOverride != "" {
		setGraphAttr(graph, "goal", goalOverride)
	}

	if dryRun {
		fmt.Fprintf(os.Stderr, "Dry run: pipeline %q parsed successfully\n", graph.Name)
		printPipelineSummary(graph)
		return nil
	}

	// Set up logs directory.
	logsDir := viper.GetString("logs_dir")
	if logsDir == "" {
		logsDir = filepath.Join("logs", time.Now().Format("20060102-150405"))
	}

	// Set up event emitter with terminal listener.
	emitter := pipeline.NewEventEmitter()
	emitter.On(terminalEventListener(verbose))

	// Create the LLM backend.
	var backend *pipeline.LLMBackend
	if simple {
		backend = &pipeline.LLMBackend{RunFunc: makeSimpleRunFunc(model)}
	} else {
		backend = &pipeline.LLMBackend{RunFunc: makeAgentRunFunc(model, provider, absWorkDir, emitter)}
	}

	// Build the registry with a real backend.
	interviewer := pipeline.NewCLIInterviewer(os.Stdin, os.Stderr)
	registry := pipeline.DefaultRegistryWithInterviewer(interviewer)
	codergenHandler := pipeline.NewCodergenHandler(backend)
	registry.Register("codergen", codergenHandler)
	registry.SetDefaultHandler(codergenHandler)

	config := &pipeline.RunConfig{
		LogsRoot:     logsDir,
		Registry:     registry,
		Interviewer:  interviewer,
		EventEmitter: emitter,
	}

	// Run the pipeline.
	fmt.Fprintf(os.Stderr, "[pipeline] Starting: %s\n", graph.Name)
	if goalAttr, ok := graph.GraphAttr("goal"); ok {
		fmt.Fprintf(os.Stderr, "[pipeline] Goal: %s\n", goalAttr.Str)
	}

	result, err := pipeline.Run(graph, config)
	if err != nil {
		fmt.Fprintf(os.Stderr, "[pipeline] Failed: %v\n", err)
		return err
	}

	// Print summary.
	printRunSummary(result)
	return nil
}

// makeAgentRunFunc creates a RunFunc that uses a full agentloop session with tools.
// It prepends prior stage output as conversation history for continuity and
// wires session events to the pipeline emitter for observability.
func makeAgentRunFunc(model, provider, workDir string, emitter *pipeline.EventEmitter) func(context.Context, string, *pipeline.Context) (string, error) {
	return func(ctx context.Context, prompt string, pctx *pipeline.Context) (string, error) {
		profile := selectProfile(model, provider)
		env := agentloop.NewLocalExecutionEnvironment(workDir)
		session := agentloop.NewSession(profile, env, nil)
		defer session.Close()

		// Prepend prior stage output as conversation history for continuity.
		// This lets the model know what happened in previous pipeline stages.
		if pctx != nil {
			if lastResponse, ok := pctx.Get("last_response"); ok {
				if respStr, ok := lastResponse.(string); ok && respStr != "" {
					// Create a user/assistant turn pair representing the prior stage.
					priorTurns := []agentloop.Turn{
						agentloop.NewUserTurn("[Prior stage context]"),
						agentloop.NewAssistantTurn(respStr, nil, "", unifiedllm.Usage{}, ""),
					}
					session.PrependHistory(priorTurns)
				}
			}
		}

		// Wire session events to pipeline emitter for observability.
		// Run in a goroutine so we don't block the main loop.
		done := make(chan struct{})
		go func() {
			defer close(done)
			for event := range session.Events() {
				if emitter == nil {
					continue
				}
				// Forward agent loop events to pipeline emitter.
				emitter.Emit(pipeline.Event{
					Type:      pipeline.EventType("agent_" + string(event.Kind)),
					Timestamp: event.Timestamp,
					Data: map[string]any{
						"session_id": event.SessionID,
						"data":       event.Data,
					},
				})
			}
		}()

		if err := session.Submit(ctx, prompt); err != nil {
			return "", err
		}

		// Wait for event forwarding to complete.
		<-done

		// Extract the last assistant text from history.
		history := session.History()
		for i := len(history) - 1; i >= 0; i-- {
			turn := history[i]
			if turn.Kind == agentloop.TurnAssistant {
				return turn.TextContent(), nil
			}
		}
		return "", fmt.Errorf("no assistant response in session history")
	}
}

// makeSimpleRunFunc creates a RunFunc that uses a single LLM completion (no tools).
func makeSimpleRunFunc(model string) func(context.Context, string, *pipeline.Context) (string, error) {
	return func(ctx context.Context, prompt string, _ *pipeline.Context) (string, error) {
		client := unifiedllm.GetDefaultClient()
		resp, err := client.Complete(ctx, unifiedllm.Request{
			Model:    model,
			Messages: []unifiedllm.Message{unifiedllm.UserMessage(prompt)},
		})
		if err != nil {
			return "", err
		}
		return resp.Text(), nil
	}
}

// selectProfile creates the appropriate provider profile for the given model.
func selectProfile(model, provider string) agentloop.ProviderProfile {
	// Auto-detect provider from model catalog if not specified.
	if provider == "" {
		if info := unifiedllm.GetModelInfo(model); info != nil {
			provider = info.Provider
		}
	}

	switch provider {
	case "openai":
		return agentloop.NewOpenAIProfile(model)
	case "gemini":
		return agentloop.NewGeminiProfile(model)
	default:
		// Default to Anthropic.
		return agentloop.NewAnthropicProfile(model)
	}
}

// setGraphAttr sets or replaces a graph-level attribute.
func setGraphAttr(graph *dotparser.Graph, key, value string) {
	for i, attr := range graph.GraphAttrs {
		if attr.Key == key {
			graph.GraphAttrs[i].Value = dotparser.Value{
				Kind: dotparser.ValueString,
				Str:  value,
				Raw:  value,
			}
			return
		}
	}
	graph.GraphAttrs = append(graph.GraphAttrs, dotparser.Attr{
		Key:   key,
		Value: dotparser.Value{Kind: dotparser.ValueString, Str: value, Raw: value},
	})
}

// terminalEventListener returns an event listener that prints pipeline progress.
func terminalEventListener(verbose bool) func(pipeline.Event) {
	stageIndex := 0
	stageStarts := make(map[string]time.Time)

	return func(e pipeline.Event) {
		switch e.Type {
		case pipeline.EventPipelineStarted:
			// Already printed in runPipeline.

		case pipeline.EventStageStarted:
			stageIndex++
			name, _ := e.Data["name"].(string)
			stageStarts[name] = e.Timestamp
			fmt.Fprintf(os.Stderr, "[stage %d] %s...", stageIndex, name)

		case pipeline.EventStageCompleted:
			name, _ := e.Data["name"].(string)
			durationMs, _ := e.Data["duration_ms"].(int64)
			duration := time.Duration(durationMs) * time.Millisecond
			if start, ok := stageStarts[name]; ok {
				duration = e.Timestamp.Sub(start)
			}
			fmt.Fprintf(os.Stderr, " done (success, %.1fs)\n", duration.Seconds())

		case pipeline.EventStageFailed:
			errMsg, _ := e.Data["error"].(string)
			willRetry, _ := e.Data["will_retry"].(bool)
			if willRetry {
				fmt.Fprintf(os.Stderr, " failed (retrying: %s)\n", errMsg)
			} else {
				fmt.Fprintf(os.Stderr, " failed (%s)\n", errMsg)
			}

		case pipeline.EventStageRetrying:
			name, _ := e.Data["name"].(string)
			attempt, _ := e.Data["attempt"].(int)
			fmt.Fprintf(os.Stderr, "[retry] %s (attempt %d)\n", name, attempt)

		case pipeline.EventPipelineCompleted:
			durationMs, _ := e.Data["duration_ms"].(int64)
			duration := time.Duration(durationMs) * time.Millisecond
			fmt.Fprintf(os.Stderr, "[pipeline] Completed in %.1fs\n", duration.Seconds())

		case pipeline.EventPipelineFailed:
			errMsg, _ := e.Data["error"].(string)
			fmt.Fprintf(os.Stderr, "[pipeline] Failed: %s\n", errMsg)

		case pipeline.EventInterviewStarted:
			if verbose {
				question, _ := e.Data["question"].(string)
				stage, _ := e.Data["stage"].(string)
				fmt.Fprintf(os.Stderr, "[interview] %s (stage: %s)\n", question, stage)
			}

		default:
			if verbose {
				fmt.Fprintf(os.Stderr, "[event] %s\n", e.Type)
			}
		}
	}
}

// printPipelineSummary prints a summary of the parsed pipeline graph.
func printPipelineSummary(graph *dotparser.Graph) {
	fmt.Fprintf(os.Stderr, "  Name: %s\n", graph.Name)
	fmt.Fprintf(os.Stderr, "  Nodes: %d\n", len(graph.Nodes))
	fmt.Fprintf(os.Stderr, "  Edges: %d\n", len(graph.Edges))

	if goalAttr, ok := graph.GraphAttr("goal"); ok {
		fmt.Fprintf(os.Stderr, "  Goal: %s\n", goalAttr.Str)
	}

	fmt.Fprintf(os.Stderr, "  Stages:\n")
	for _, node := range graph.Nodes {
		label := node.ID
		if labelAttr, ok := node.Attr("label"); ok && labelAttr.Str != "" {
			label = labelAttr.Str
		}
		shape := "box"
		if shapeAttr, ok := node.Attr("shape"); ok {
			shape = shapeAttr.Str
		}
		fmt.Fprintf(os.Stderr, "    - %s [%s] (%s)\n", node.ID, label, shape)
	}
}

// printRunSummary prints the result of a pipeline run.
func printRunSummary(result *pipeline.RunResult) {
	fmt.Fprintf(os.Stderr, "\n[summary]\n")
	fmt.Fprintf(os.Stderr, "  Completed stages: %d\n", len(result.CompletedNodes))
	for _, nodeID := range result.CompletedNodes {
		status := "?"
		if outcome, ok := result.NodeOutcomes[nodeID]; ok {
			status = outcome.Status.String()
		}
		fmt.Fprintf(os.Stderr, "    - %s: %s\n", nodeID, status)
	}

	if result.FinalOutcome != nil {
		fmt.Fprintf(os.Stderr, "  Final status: %s\n", result.FinalOutcome.Status)
		if result.FinalOutcome.Notes != "" {
			fmt.Fprintf(os.Stderr, "  Notes: %s\n", result.FinalOutcome.Notes)
		}
	}
}
