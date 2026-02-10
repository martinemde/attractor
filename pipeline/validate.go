package pipeline

import (
	"errors"
	"fmt"
	"strings"

	"github.com/martinemde/attractor/dotparser"
)

// Severity represents the severity level of a diagnostic.
type Severity int

const (
	// SeverityError indicates a problem that prevents pipeline execution.
	SeverityError Severity = iota
	// SeverityWarning indicates a potential issue but the pipeline can still execute.
	SeverityWarning
	// SeverityInfo indicates an informational note.
	SeverityInfo
)

// String returns the string representation of the severity.
func (s Severity) String() string {
	switch s {
	case SeverityError:
		return "ERROR"
	case SeverityWarning:
		return "WARNING"
	case SeverityInfo:
		return "INFO"
	default:
		return "UNKNOWN"
	}
}

// Diagnostic represents a validation issue found in a pipeline graph.
type Diagnostic struct {
	// Rule is the rule identifier (e.g., "start_node").
	Rule string
	// Severity is the severity level of the diagnostic.
	Severity Severity
	// Message is a human-readable description of the issue.
	Message string
	// NodeID is the related node ID (optional).
	NodeID string
	// Edge is the related edge as [from, to] (optional).
	Edge [2]string
	// Fix is a suggested fix for the issue (optional).
	Fix string
}

// LintRule is the interface for validation rules.
type LintRule interface {
	// Name returns the rule identifier.
	Name() string
	// Apply runs the rule against a graph and returns any diagnostics.
	Apply(graph *dotparser.Graph) []Diagnostic
}

// BuiltInRules contains all built-in lint rules.
var BuiltInRules = []LintRule{
	&StartNodeRule{},
	&TerminalNodeRule{},
	&ReachabilityRule{},
	&EdgeTargetExistsRule{},
	&StartNoIncomingRule{},
	&ExitNoOutgoingRule{},
	&ConditionSyntaxRule{},
	&TypeKnownRule{},
	&FidelityValidRule{},
	&RetryTargetExistsRule{},
	&GoalGateHasRetryRule{},
	&PromptOnLLMNodesRule{},
}

// Validate runs all built-in lint rules plus any extra rules against the graph.
func Validate(graph *dotparser.Graph, extraRules ...LintRule) []Diagnostic {
	rules := make([]LintRule, 0, len(BuiltInRules)+len(extraRules))
	rules = append(rules, BuiltInRules...)
	rules = append(rules, extraRules...)

	var diagnostics []Diagnostic
	for _, rule := range rules {
		diagnostics = append(diagnostics, rule.Apply(graph)...)
	}
	return diagnostics
}

// ValidateOrError runs validation and returns an error if any ERROR-severity diagnostics exist.
func ValidateOrError(graph *dotparser.Graph, extraRules ...LintRule) ([]Diagnostic, error) {
	diagnostics := Validate(graph, extraRules...)

	var errMsgs []string
	for _, d := range diagnostics {
		if d.Severity == SeverityError {
			errMsgs = append(errMsgs, fmt.Sprintf("%s: %s", d.Rule, d.Message))
		}
	}

	if len(errMsgs) > 0 {
		return diagnostics, errors.New("validation failed: " + strings.Join(errMsgs, "; "))
	}
	return diagnostics, nil
}

// ---------- Built-in Lint Rules ----------

// StartNodeRule checks that exactly one start node exists.
type StartNodeRule struct{}

func (r *StartNodeRule) Name() string { return "start_node" }

func (r *StartNodeRule) Apply(graph *dotparser.Graph) []Diagnostic {
	var startNodes []*dotparser.Node

	// Look for shape=Mdiamond nodes
	for _, node := range graph.Nodes {
		if shape, ok := node.Attr("shape"); ok && shape.Str == "Mdiamond" {
			startNodes = append(startNodes, node)
		}
	}

	// If no Mdiamond, check for id=start or id=Start
	if len(startNodes) == 0 {
		if node := graph.NodeByID("start"); node != nil {
			startNodes = append(startNodes, node)
		} else if node := graph.NodeByID("Start"); node != nil {
			startNodes = append(startNodes, node)
		}
	}

	if len(startNodes) == 0 {
		return []Diagnostic{{
			Rule:     r.Name(),
			Severity: SeverityError,
			Message:  "pipeline must have exactly one start node (shape=Mdiamond or id=start/Start)",
			Fix:      "add a node with shape=Mdiamond or id=start",
		}}
	}

	if len(startNodes) > 1 {
		nodeIDs := make([]string, len(startNodes))
		for i, n := range startNodes {
			nodeIDs[i] = n.ID
		}
		return []Diagnostic{{
			Rule:     r.Name(),
			Severity: SeverityError,
			Message:  fmt.Sprintf("pipeline must have exactly one start node, found %d: %s", len(startNodes), strings.Join(nodeIDs, ", ")),
			Fix:      "remove extra start nodes to leave only one",
		}}
	}

	return nil
}

// TerminalNodeRule checks that at least one terminal node exists.
type TerminalNodeRule struct{}

func (r *TerminalNodeRule) Name() string { return "terminal_node" }

func (r *TerminalNodeRule) Apply(graph *dotparser.Graph) []Diagnostic {
	// Look for shape=Msquare nodes
	for _, node := range graph.Nodes {
		if shape, ok := node.Attr("shape"); ok && shape.Str == "Msquare" {
			return nil
		}
	}

	// Fallback to id=exit or id=end
	if graph.NodeByID("exit") != nil || graph.NodeByID("end") != nil {
		return nil
	}

	return []Diagnostic{{
		Rule:     r.Name(),
		Severity: SeverityError,
		Message:  "pipeline must have at least one terminal node (shape=Msquare or id=exit/end)",
		Fix:      "add a node with shape=Msquare or id=exit",
	}}
}

// ReachabilityRule checks that all nodes are reachable from the start node.
type ReachabilityRule struct{}

func (r *ReachabilityRule) Name() string { return "reachability" }

func (r *ReachabilityRule) Apply(graph *dotparser.Graph) []Diagnostic {
	startNode := findStartNodeForValidation(graph)
	if startNode == nil {
		// start_node rule will catch this
		return nil
	}

	// BFS from start node
	visited := make(map[string]bool)
	queue := []string{startNode.ID}
	visited[startNode.ID] = true

	for len(queue) > 0 {
		current := queue[0]
		queue = queue[1:]

		for _, edge := range graph.EdgesFrom(current) {
			if !visited[edge.To] {
				visited[edge.To] = true
				queue = append(queue, edge.To)
			}
		}
	}

	var diagnostics []Diagnostic
	for _, node := range graph.Nodes {
		if !visited[node.ID] {
			diagnostics = append(diagnostics, Diagnostic{
				Rule:     r.Name(),
				Severity: SeverityError,
				Message:  fmt.Sprintf("node %q is not reachable from start node", node.ID),
				NodeID:   node.ID,
				Fix:      "add an edge from a reachable node to this node, or remove the node",
			})
		}
	}

	return diagnostics
}

// EdgeTargetExistsRule checks that all edge targets reference existing nodes.
type EdgeTargetExistsRule struct{}

func (r *EdgeTargetExistsRule) Name() string { return "edge_target_exists" }

func (r *EdgeTargetExistsRule) Apply(graph *dotparser.Graph) []Diagnostic {
	nodeSet := make(map[string]bool)
	for _, node := range graph.Nodes {
		nodeSet[node.ID] = true
	}

	var diagnostics []Diagnostic
	for _, edge := range graph.Edges {
		if !nodeSet[edge.From] {
			diagnostics = append(diagnostics, Diagnostic{
				Rule:     r.Name(),
				Severity: SeverityError,
				Message:  fmt.Sprintf("edge source node %q does not exist", edge.From),
				Edge:     [2]string{edge.From, edge.To},
				Fix:      "add the missing node or fix the edge reference",
			})
		}
		if !nodeSet[edge.To] {
			diagnostics = append(diagnostics, Diagnostic{
				Rule:     r.Name(),
				Severity: SeverityError,
				Message:  fmt.Sprintf("edge target node %q does not exist", edge.To),
				Edge:     [2]string{edge.From, edge.To},
				Fix:      "add the missing node or fix the edge reference",
			})
		}
	}

	return diagnostics
}

// StartNoIncomingRule checks that the start node has no incoming edges.
type StartNoIncomingRule struct{}

func (r *StartNoIncomingRule) Name() string { return "start_no_incoming" }

func (r *StartNoIncomingRule) Apply(graph *dotparser.Graph) []Diagnostic {
	startNode := findStartNodeForValidation(graph)
	if startNode == nil {
		return nil
	}

	incomingEdges := graph.EdgesTo(startNode.ID)
	if len(incomingEdges) > 0 {
		var sources []string
		for _, e := range incomingEdges {
			sources = append(sources, e.From)
		}
		return []Diagnostic{{
			Rule:     r.Name(),
			Severity: SeverityError,
			Message:  fmt.Sprintf("start node %q has incoming edges from: %s", startNode.ID, strings.Join(sources, ", ")),
			NodeID:   startNode.ID,
			Fix:      "remove incoming edges to the start node",
		}}
	}

	return nil
}

// ExitNoOutgoingRule checks that exit nodes have no outgoing edges.
type ExitNoOutgoingRule struct{}

func (r *ExitNoOutgoingRule) Name() string { return "exit_no_outgoing" }

func (r *ExitNoOutgoingRule) Apply(graph *dotparser.Graph) []Diagnostic {
	exitNodes := findExitNodesForValidation(graph)
	if len(exitNodes) == 0 {
		return nil
	}

	var diagnostics []Diagnostic
	for _, exitNode := range exitNodes {
		outgoingEdges := graph.EdgesFrom(exitNode.ID)
		if len(outgoingEdges) > 0 {
			var targets []string
			for _, e := range outgoingEdges {
				targets = append(targets, e.To)
			}
			diagnostics = append(diagnostics, Diagnostic{
				Rule:     r.Name(),
				Severity: SeverityError,
				Message:  fmt.Sprintf("exit node %q has outgoing edges to: %s", exitNode.ID, strings.Join(targets, ", ")),
				NodeID:   exitNode.ID,
				Fix:      "remove outgoing edges from the exit node",
			})
		}
	}

	return diagnostics
}

// ConditionSyntaxRule checks that edge condition expressions are syntactically valid.
type ConditionSyntaxRule struct{}

func (r *ConditionSyntaxRule) Name() string { return "condition_syntax" }

func (r *ConditionSyntaxRule) Apply(graph *dotparser.Graph) []Diagnostic {
	var diagnostics []Diagnostic

	for _, edge := range graph.Edges {
		condAttr, ok := edge.Attr("condition")
		if !ok || condAttr.Str == "" {
			continue
		}

		if err := validateConditionSyntax(condAttr.Str); err != nil {
			diagnostics = append(diagnostics, Diagnostic{
				Rule:     r.Name(),
				Severity: SeverityError,
				Message:  fmt.Sprintf("invalid condition syntax on edge %s->%s: %v", edge.From, edge.To, err),
				Edge:     [2]string{edge.From, edge.To},
				Fix:      "fix the condition expression syntax",
			})
		}
	}

	return diagnostics
}

// validateConditionSyntax checks if a condition string is syntactically valid.
// It checks that each clause has a valid operator and non-empty key.
func validateConditionSyntax(condition string) error {
	condition = strings.TrimSpace(condition)
	if condition == "" {
		return nil
	}

	for clause := range strings.SplitSeq(condition, "&&") {
		clause = strings.TrimSpace(clause)
		if clause == "" {
			continue
		}

		// Check for valid operators: != or =
		hasNotEquals := strings.Contains(clause, "!=")
		hasEquals := strings.Contains(clause, "=") && !hasNotEquals

		if hasNotEquals {
			parts := strings.SplitN(clause, "!=", 2)
			if len(parts) != 2 || strings.TrimSpace(parts[0]) == "" {
				return fmt.Errorf("invalid clause: %q", clause)
			}
		} else if hasEquals {
			parts := strings.SplitN(clause, "=", 2)
			if len(parts) != 2 || strings.TrimSpace(parts[0]) == "" {
				return fmt.Errorf("invalid clause: %q", clause)
			}
		}
		// Bare keys (no operator) are allowed
	}

	return nil
}

// TypeKnownRule checks that node type values are recognized handler types.
type TypeKnownRule struct{}

func (r *TypeKnownRule) Name() string { return "type_known" }

// knownHandlerTypes lists all handler types from the spec Section 2.8.
var knownHandlerTypes = map[string]bool{
	"start":              true,
	"exit":               true,
	"codergen":           true,
	"wait.human":         true,
	"conditional":        true,
	"parallel":           true,
	"parallel.fan_in":    true,
	"tool":               true,
	"stack.manager_loop": true,
}

func (r *TypeKnownRule) Apply(graph *dotparser.Graph) []Diagnostic {
	var diagnostics []Diagnostic

	for _, node := range graph.Nodes {
		typeAttr, ok := node.Attr("type")
		if !ok || typeAttr.Str == "" {
			continue
		}

		if !knownHandlerTypes[typeAttr.Str] {
			diagnostics = append(diagnostics, Diagnostic{
				Rule:     r.Name(),
				Severity: SeverityWarning,
				Message:  fmt.Sprintf("node %q has unknown type %q", node.ID, typeAttr.Str),
				NodeID:   node.ID,
				Fix:      "use a known handler type or register a custom handler",
			})
		}
	}

	return diagnostics
}

// FidelityValidRule checks that fidelity mode values are valid.
type FidelityValidRule struct{}

func (r *FidelityValidRule) Name() string { return "fidelity_valid" }

func (r *FidelityValidRule) Apply(graph *dotparser.Graph) []Diagnostic {
	var diagnostics []Diagnostic

	// Check graph-level fidelity
	if fidelityAttr, ok := graph.GraphAttr("default_fidelity"); ok && fidelityAttr.Str != "" {
		if !IsValidFidelity(fidelityAttr.Str) {
			diagnostics = append(diagnostics, Diagnostic{
				Rule:     r.Name(),
				Severity: SeverityWarning,
				Message:  fmt.Sprintf("invalid graph-level default_fidelity value %q", fidelityAttr.Str),
				Fix:      "use one of: full, truncate, compact, summary:low, summary:medium, summary:high",
			})
		}
	}

	// Check node-level fidelity
	for _, node := range graph.Nodes {
		if fidelityAttr, ok := node.Attr("fidelity"); ok && fidelityAttr.Str != "" {
			if !IsValidFidelity(fidelityAttr.Str) {
				diagnostics = append(diagnostics, Diagnostic{
					Rule:     r.Name(),
					Severity: SeverityWarning,
					Message:  fmt.Sprintf("node %q has invalid fidelity value %q", node.ID, fidelityAttr.Str),
					NodeID:   node.ID,
					Fix:      "use one of: full, truncate, compact, summary:low, summary:medium, summary:high",
				})
			}
		}
	}

	// Check edge-level fidelity
	for _, edge := range graph.Edges {
		if fidelityAttr, ok := edge.Attr("fidelity"); ok && fidelityAttr.Str != "" {
			if !IsValidFidelity(fidelityAttr.Str) {
				diagnostics = append(diagnostics, Diagnostic{
					Rule:     r.Name(),
					Severity: SeverityWarning,
					Message:  fmt.Sprintf("edge %s->%s has invalid fidelity value %q", edge.From, edge.To, fidelityAttr.Str),
					Edge:     [2]string{edge.From, edge.To},
					Fix:      "use one of: full, truncate, compact, summary:low, summary:medium, summary:high",
				})
			}
		}
	}

	return diagnostics
}

// RetryTargetExistsRule checks that retry_target and fallback_retry_target reference existing nodes.
type RetryTargetExistsRule struct{}

func (r *RetryTargetExistsRule) Name() string { return "retry_target_exists" }

func (r *RetryTargetExistsRule) Apply(graph *dotparser.Graph) []Diagnostic {
	nodeSet := make(map[string]bool)
	for _, node := range graph.Nodes {
		nodeSet[node.ID] = true
	}

	var diagnostics []Diagnostic

	// Check graph-level retry targets
	if attr, ok := graph.GraphAttr("retry_target"); ok && attr.Str != "" {
		if !nodeSet[attr.Str] {
			diagnostics = append(diagnostics, Diagnostic{
				Rule:     r.Name(),
				Severity: SeverityWarning,
				Message:  fmt.Sprintf("graph-level retry_target %q does not exist", attr.Str),
				Fix:      "fix the retry_target to reference an existing node",
			})
		}
	}

	if attr, ok := graph.GraphAttr("fallback_retry_target"); ok && attr.Str != "" {
		if !nodeSet[attr.Str] {
			diagnostics = append(diagnostics, Diagnostic{
				Rule:     r.Name(),
				Severity: SeverityWarning,
				Message:  fmt.Sprintf("graph-level fallback_retry_target %q does not exist", attr.Str),
				Fix:      "fix the fallback_retry_target to reference an existing node",
			})
		}
	}

	// Check node-level retry targets
	for _, node := range graph.Nodes {
		if attr, ok := node.Attr("retry_target"); ok && attr.Str != "" {
			if !nodeSet[attr.Str] {
				diagnostics = append(diagnostics, Diagnostic{
					Rule:     r.Name(),
					Severity: SeverityWarning,
					Message:  fmt.Sprintf("node %q has retry_target %q which does not exist", node.ID, attr.Str),
					NodeID:   node.ID,
					Fix:      "fix the retry_target to reference an existing node",
				})
			}
		}

		if attr, ok := node.Attr("fallback_retry_target"); ok && attr.Str != "" {
			if !nodeSet[attr.Str] {
				diagnostics = append(diagnostics, Diagnostic{
					Rule:     r.Name(),
					Severity: SeverityWarning,
					Message:  fmt.Sprintf("node %q has fallback_retry_target %q which does not exist", node.ID, attr.Str),
					NodeID:   node.ID,
					Fix:      "fix the fallback_retry_target to reference an existing node",
				})
			}
		}
	}

	return diagnostics
}

// GoalGateHasRetryRule checks that nodes with goal_gate=true have a retry target.
type GoalGateHasRetryRule struct{}

func (r *GoalGateHasRetryRule) Name() string { return "goal_gate_has_retry" }

func (r *GoalGateHasRetryRule) Apply(graph *dotparser.Graph) []Diagnostic {
	var diagnostics []Diagnostic

	// Check if graph has retry targets set
	graphHasRetry := false
	if attr, ok := graph.GraphAttr("retry_target"); ok && attr.Str != "" {
		graphHasRetry = true
	}
	if attr, ok := graph.GraphAttr("fallback_retry_target"); ok && attr.Str != "" {
		graphHasRetry = true
	}

	for _, node := range graph.Nodes {
		goalGateAttr, ok := node.Attr("goal_gate")
		if !ok || !goalGateAttr.Bool {
			continue
		}

		// Check if node has its own retry targets
		hasNodeRetry := false
		if attr, ok := node.Attr("retry_target"); ok && attr.Str != "" {
			hasNodeRetry = true
		}
		if attr, ok := node.Attr("fallback_retry_target"); ok && attr.Str != "" {
			hasNodeRetry = true
		}

		if !hasNodeRetry && !graphHasRetry {
			diagnostics = append(diagnostics, Diagnostic{
				Rule:     r.Name(),
				Severity: SeverityWarning,
				Message:  fmt.Sprintf("node %q has goal_gate=true but no retry_target or fallback_retry_target", node.ID),
				NodeID:   node.ID,
				Fix:      "add retry_target or fallback_retry_target to the node or graph",
			})
		}
	}

	return diagnostics
}

// PromptOnLLMNodesRule checks that LLM nodes (codergen handler) have a prompt or label.
type PromptOnLLMNodesRule struct{}

func (r *PromptOnLLMNodesRule) Name() string { return "prompt_on_llm_nodes" }

func (r *PromptOnLLMNodesRule) Apply(graph *dotparser.Graph) []Diagnostic {
	var diagnostics []Diagnostic

	for _, node := range graph.Nodes {
		if !isCodergenNode(node) {
			continue
		}

		hasPrompt := false
		if attr, ok := node.Attr("prompt"); ok && attr.Str != "" {
			hasPrompt = true
		}

		hasLabel := false
		if attr, ok := node.Attr("label"); ok && attr.Str != "" {
			hasLabel = true
		}

		if !hasPrompt && !hasLabel {
			diagnostics = append(diagnostics, Diagnostic{
				Rule:     r.Name(),
				Severity: SeverityWarning,
				Message:  fmt.Sprintf("LLM node %q has no prompt or label attribute", node.ID),
				NodeID:   node.ID,
				Fix:      "add a prompt or label attribute to provide instructions for the LLM",
			})
		}
	}

	return diagnostics
}

// isCodergenNode returns true if the node resolves to the codergen handler.
// Resolution: explicit type=codergen, or shape=box, or default (no shape).
func isCodergenNode(node *dotparser.Node) bool {
	// Check explicit type
	if typeAttr, ok := node.Attr("type"); ok && typeAttr.Str != "" {
		return typeAttr.Str == "codergen"
	}

	// Check shape
	shapeAttr, ok := node.Attr("shape")
	if !ok {
		// Default shape is box, which maps to codergen
		return true
	}

	// box maps to codergen
	if shapeAttr.Str == "box" {
		return true
	}

	// Other shapes map to other handlers
	_, isOtherHandler := shapeToHandlerType[shapeAttr.Str]
	if isOtherHandler {
		return false
	}

	// Unknown shape falls back to default (codergen)
	return true
}

// ---------- Helper Functions ----------

// findStartNodeForValidation locates the start node for validation purposes.
func findStartNodeForValidation(graph *dotparser.Graph) *dotparser.Node {
	// First, look for shape=Mdiamond
	for _, node := range graph.Nodes {
		if shape, ok := node.Attr("shape"); ok && shape.Str == "Mdiamond" {
			return node
		}
	}

	// Fallback to ID-based lookup
	if node := graph.NodeByID("start"); node != nil {
		return node
	}
	if node := graph.NodeByID("Start"); node != nil {
		return node
	}

	return nil
}

// findExitNodesForValidation locates all exit nodes for validation purposes.
func findExitNodesForValidation(graph *dotparser.Graph) []*dotparser.Node {
	var exitNodes []*dotparser.Node

	// Look for shape=Msquare nodes
	for _, node := range graph.Nodes {
		if shape, ok := node.Attr("shape"); ok && shape.Str == "Msquare" {
			exitNodes = append(exitNodes, node)
		}
	}

	// If no Msquare, check for id=exit or id=end
	if len(exitNodes) == 0 {
		if node := graph.NodeByID("exit"); node != nil {
			exitNodes = append(exitNodes, node)
		}
		if node := graph.NodeByID("end"); node != nil {
			exitNodes = append(exitNodes, node)
		}
	}

	return exitNodes
}
