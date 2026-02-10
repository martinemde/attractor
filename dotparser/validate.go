package dotparser

import (
	"fmt"
	"strings"
)

// Severity represents the severity level of a validation diagnostic.
type Severity int

const (
	// Error means the pipeline will not execute.
	Error Severity = iota
	// Warning means the pipeline will execute but behavior may be unexpected.
	Warning
	// Info is an informational note.
	Info
)

func (s Severity) String() string {
	switch s {
	case Error:
		return "ERROR"
	case Warning:
		return "WARNING"
	case Info:
		return "INFO"
	default:
		return fmt.Sprintf("Severity(%d)", int(s))
	}
}

// Diagnostic is a single validation finding.
type Diagnostic struct {
	Rule     string      // rule identifier (e.g., "start_node")
	Severity Severity    // ERROR, WARNING, or INFO
	Message  string      // human-readable description
	NodeID   string      // related node ID (optional)
	Edge     *EdgeRef    // related edge as (from, to) (optional)
	Fix      string      // suggested fix (optional)
}

// EdgeRef identifies an edge by its endpoints.
type EdgeRef struct {
	From string
	To   string
}

func (d Diagnostic) String() string {
	var b strings.Builder
	fmt.Fprintf(&b, "[%s] %s: %s", d.Severity, d.Rule, d.Message)
	if d.NodeID != "" {
		fmt.Fprintf(&b, " (node: %s)", d.NodeID)
	}
	if d.Edge != nil {
		fmt.Fprintf(&b, " (edge: %s -> %s)", d.Edge.From, d.Edge.To)
	}
	if d.Fix != "" {
		fmt.Fprintf(&b, " -- fix: %s", d.Fix)
	}
	return b.String()
}

// LintRule is the interface for a single validation rule.
type LintRule interface {
	Name() string
	Apply(g *Graph) []Diagnostic
}

// ValidationError is returned by ValidateOrError when error-severity diagnostics exist.
type ValidationError struct {
	Diagnostics []Diagnostic
}

func (e *ValidationError) Error() string {
	var msgs []string
	for _, d := range e.Diagnostics {
		msgs = append(msgs, d.String())
	}
	return fmt.Sprintf("validation failed with %d error(s):\n  %s", len(e.Diagnostics), strings.Join(msgs, "\n  "))
}

// Validate runs all built-in rules (and any extra rules) against the graph.
// Returns all diagnostics regardless of severity.
func Validate(g *Graph, extraRules ...LintRule) []Diagnostic {
	rules := builtInRules()
	rules = append(rules, extraRules...)

	var diagnostics []Diagnostic
	for _, rule := range rules {
		diagnostics = append(diagnostics, rule.Apply(g)...)
	}
	return diagnostics
}

// ValidateOrError runs Validate and returns an error if any error-severity
// diagnostics are found. Non-error diagnostics are still returned.
func ValidateOrError(g *Graph, extraRules ...LintRule) ([]Diagnostic, error) {
	diagnostics := Validate(g, extraRules...)

	var errors []Diagnostic
	for _, d := range diagnostics {
		if d.Severity == Error {
			errors = append(errors, d)
		}
	}
	if len(errors) > 0 {
		return diagnostics, &ValidationError{Diagnostics: errors}
	}
	return diagnostics, nil
}

// builtInRules returns the standard set of lint rules from section 7.2.
func builtInRules() []LintRule {
	return []LintRule{
		startNodeRule{},
		terminalNodeRule{},
		edgeTargetExistsRule{},
		startNoIncomingRule{},
		exitNoOutgoingRule{},
		reachabilityRule{},
		conditionSyntaxRule{},
		stylesheetSyntaxRule{},
		typeKnownRule{},
		fidelityValidRule{},
		retryTargetExistsRule{},
		goalGateHasRetryRule{},
		promptOnLLMNodesRule{},
	}
}

// --- Helper functions ---

// isStartNode returns true if the node is a start node.
func isStartNode(n *Node) bool {
	if shape, ok := n.Attr("shape"); ok && shape.Str == "Mdiamond" {
		return true
	}
	return n.ID == "start" || n.ID == "Start"
}

// isTerminalNode returns true if the node is a terminal/exit node.
func isTerminalNode(n *Node) bool {
	if shape, ok := n.Attr("shape"); ok && shape.Str == "Msquare" {
		return true
	}
	return n.ID == "exit" || n.ID == "end" || n.ID == "Exit" || n.ID == "End"
}

// resolveHandlerType returns the handler type for a node based on shape/type attrs.
func resolveHandlerType(n *Node) string {
	if typ, ok := n.Attr("type"); ok && typ.Str != "" {
		return typ.Str
	}
	shape := "box" // default
	if s, ok := n.Attr("shape"); ok && s.Str != "" {
		shape = s.Str
	}
	return shapeToHandler(shape)
}

// shapeToHandler maps shape attribute values to handler type strings.
func shapeToHandler(shape string) string {
	switch shape {
	case "Mdiamond":
		return "start"
	case "Msquare":
		return "exit"
	case "box":
		return "codergen"
	case "hexagon":
		return "wait.human"
	case "diamond":
		return "conditional"
	case "component":
		return "parallel"
	case "tripleoctagon":
		return "parallel.fan_in"
	case "parallelogram":
		return "tool"
	case "house":
		return "stack.manager_loop"
	default:
		return ""
	}
}

// knownHandlerTypes is the set of recognized handler type values.
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

// validFidelityModes is the set of valid fidelity mode values.
var validFidelityModes = map[string]bool{
	"full":           true,
	"truncate":       true,
	"compact":        true,
	"summary:low":    true,
	"summary:medium": true,
	"summary:high":   true,
}

// --- Rule implementations ---

// start_node: Pipeline must have exactly one start node.
type startNodeRule struct{}

func (startNodeRule) Name() string { return "start_node" }

func (startNodeRule) Apply(g *Graph) []Diagnostic {
	var starts []*Node
	for _, n := range g.Nodes {
		if isStartNode(n) {
			starts = append(starts, n)
		}
	}
	switch len(starts) {
	case 0:
		return []Diagnostic{{
			Rule:     "start_node",
			Severity: Error,
			Message:  "pipeline must have exactly one start node (shape=Mdiamond or id=start/Start)",
			Fix:      "add a node with shape=Mdiamond",
		}}
	case 1:
		return nil
	default:
		var diags []Diagnostic
		for _, n := range starts {
			diags = append(diags, Diagnostic{
				Rule:     "start_node",
				Severity: Error,
				Message:  fmt.Sprintf("multiple start nodes found; %q is one of %d", n.ID, len(starts)),
				NodeID:   n.ID,
				Fix:      "ensure exactly one node has shape=Mdiamond or id=start/Start",
			})
		}
		return diags
	}
}

// terminal_node: Pipeline must have at least one terminal node.
type terminalNodeRule struct{}

func (terminalNodeRule) Name() string { return "terminal_node" }

func (terminalNodeRule) Apply(g *Graph) []Diagnostic {
	for _, n := range g.Nodes {
		if isTerminalNode(n) {
			return nil
		}
	}
	return []Diagnostic{{
		Rule:     "terminal_node",
		Severity: Error,
		Message:  "pipeline must have at least one terminal node (shape=Msquare or id=exit/end)",
		Fix:      "add a node with shape=Msquare",
	}}
}

// edge_target_exists: Every edge target must reference an existing node ID.
type edgeTargetExistsRule struct{}

func (edgeTargetExistsRule) Name() string { return "edge_target_exists" }

func (edgeTargetExistsRule) Apply(g *Graph) []Diagnostic {
	nodeSet := make(map[string]bool, len(g.Nodes))
	for _, n := range g.Nodes {
		nodeSet[n.ID] = true
	}

	var diags []Diagnostic
	for _, e := range g.Edges {
		if !nodeSet[e.From] {
			diags = append(diags, Diagnostic{
				Rule:     "edge_target_exists",
				Severity: Error,
				Message:  fmt.Sprintf("edge source %q does not reference an existing node", e.From),
				Edge:     &EdgeRef{From: e.From, To: e.To},
				Fix:      fmt.Sprintf("declare node %q or fix the edge source", e.From),
			})
		}
		if !nodeSet[e.To] {
			diags = append(diags, Diagnostic{
				Rule:     "edge_target_exists",
				Severity: Error,
				Message:  fmt.Sprintf("edge target %q does not reference an existing node", e.To),
				Edge:     &EdgeRef{From: e.From, To: e.To},
				Fix:      fmt.Sprintf("declare node %q or fix the edge target", e.To),
			})
		}
	}
	return diags
}

// start_no_incoming: The start node must have no incoming edges.
type startNoIncomingRule struct{}

func (startNoIncomingRule) Name() string { return "start_no_incoming" }

func (startNoIncomingRule) Apply(g *Graph) []Diagnostic {
	var diags []Diagnostic
	for _, n := range g.Nodes {
		if !isStartNode(n) {
			continue
		}
		incoming := g.EdgesTo(n.ID)
		if len(incoming) > 0 {
			diags = append(diags, Diagnostic{
				Rule:     "start_no_incoming",
				Severity: Error,
				Message:  fmt.Sprintf("start node %q must have no incoming edges, but has %d", n.ID, len(incoming)),
				NodeID:   n.ID,
				Fix:      "remove all edges pointing to the start node",
			})
		}
	}
	return diags
}

// exit_no_outgoing: The exit node must have no outgoing edges.
type exitNoOutgoingRule struct{}

func (exitNoOutgoingRule) Name() string { return "exit_no_outgoing" }

func (exitNoOutgoingRule) Apply(g *Graph) []Diagnostic {
	var diags []Diagnostic
	for _, n := range g.Nodes {
		if !isTerminalNode(n) {
			continue
		}
		outgoing := g.EdgesFrom(n.ID)
		if len(outgoing) > 0 {
			diags = append(diags, Diagnostic{
				Rule:     "exit_no_outgoing",
				Severity: Error,
				Message:  fmt.Sprintf("terminal node %q must have no outgoing edges, but has %d", n.ID, len(outgoing)),
				NodeID:   n.ID,
				Fix:      "remove all edges originating from the terminal node",
			})
		}
	}
	return diags
}

// reachability: All nodes must be reachable from the start node via BFS.
type reachabilityRule struct{}

func (reachabilityRule) Name() string { return "reachability" }

func (reachabilityRule) Apply(g *Graph) []Diagnostic {
	// Find start node
	var startID string
	for _, n := range g.Nodes {
		if isStartNode(n) {
			startID = n.ID
			break
		}
	}
	if startID == "" {
		// start_node rule will catch this; skip reachability.
		return nil
	}

	// Build adjacency list
	adj := make(map[string][]string)
	for _, e := range g.Edges {
		adj[e.From] = append(adj[e.From], e.To)
	}

	// BFS from start
	visited := make(map[string]bool)
	queue := []string{startID}
	visited[startID] = true
	for len(queue) > 0 {
		cur := queue[0]
		queue = queue[1:]
		for _, next := range adj[cur] {
			if !visited[next] {
				visited[next] = true
				queue = append(queue, next)
			}
		}
	}

	var diags []Diagnostic
	for _, n := range g.Nodes {
		if !visited[n.ID] {
			diags = append(diags, Diagnostic{
				Rule:     "reachability",
				Severity: Error,
				Message:  fmt.Sprintf("node %q is not reachable from start node %q", n.ID, startID),
				NodeID:   n.ID,
				Fix:      fmt.Sprintf("add an edge path from %q to %q or remove the unreachable node", startID, n.ID),
			})
		}
	}
	return diags
}

// condition_syntax: Edge condition expressions must parse correctly.
type conditionSyntaxRule struct{}

func (conditionSyntaxRule) Name() string { return "condition_syntax" }

func (conditionSyntaxRule) Apply(g *Graph) []Diagnostic {
	var diags []Diagnostic
	for _, e := range g.Edges {
		cond, ok := e.Attr("condition")
		if !ok || cond.Str == "" {
			continue
		}
		if err := validateConditionExpr(cond.Str); err != nil {
			diags = append(diags, Diagnostic{
				Rule:     "condition_syntax",
				Severity: Error,
				Message:  fmt.Sprintf("invalid condition on edge %s -> %s: %v", e.From, e.To, err),
				Edge:     &EdgeRef{From: e.From, To: e.To},
				Fix:      "fix the condition expression syntax",
			})
		}
	}
	return diags
}

// validateConditionExpr checks whether a condition expression string is syntactically valid
// per section 10 of the spec: ConditionExpr ::= Clause ( '&&' Clause )*
func validateConditionExpr(expr string) error {
	clauses := strings.Split(expr, "&&")
	for _, clause := range clauses {
		clause = strings.TrimSpace(clause)
		if clause == "" {
			return fmt.Errorf("empty clause in condition expression")
		}
		if err := validateClause(clause); err != nil {
			return err
		}
	}
	return nil
}

// validateClause checks a single clause: Key Operator Literal
func validateClause(clause string) error {
	// Try != first (before =) since != contains =
	if idx := strings.Index(clause, "!="); idx >= 0 {
		key := strings.TrimSpace(clause[:idx])
		val := strings.TrimSpace(clause[idx+2:])
		if key == "" {
			return fmt.Errorf("missing key in clause %q", clause)
		}
		if val == "" {
			return fmt.Errorf("missing value in clause %q", clause)
		}
		return validateConditionKey(key)
	}
	if idx := strings.Index(clause, "="); idx >= 0 {
		key := strings.TrimSpace(clause[:idx])
		val := strings.TrimSpace(clause[idx+1:])
		if key == "" {
			return fmt.Errorf("missing key in clause %q", clause)
		}
		if val == "" {
			return fmt.Errorf("missing value in clause %q", clause)
		}
		return validateConditionKey(key)
	}
	// Bare key (truthy check) — validate the key itself
	return validateConditionKey(strings.TrimSpace(clause))
}

// validateConditionKey checks that a condition key is a valid key per section 10.
func validateConditionKey(key string) error {
	if key == "" {
		return fmt.Errorf("empty key")
	}
	// Valid keys: outcome, preferred_label, context.*, or unqualified keys for direct context lookup
	if key == "outcome" || key == "preferred_label" {
		return nil
	}
	if strings.HasPrefix(key, "context.") {
		path := key[len("context."):]
		if path == "" {
			return fmt.Errorf("empty path after 'context.' in key %q", key)
		}
		return validateDottedPath(path)
	}
	// Unqualified key for direct context lookup — must be a valid identifier path
	return validateDottedPath(key)
}

// validateDottedPath checks that a dotted path like "foo.bar" consists of valid identifiers.
func validateDottedPath(path string) error {
	parts := strings.Split(path, ".")
	for _, part := range parts {
		if part == "" {
			return fmt.Errorf("empty segment in path %q", path)
		}
		if !isValidIdentifier(part) {
			return fmt.Errorf("invalid identifier %q in path %q", part, path)
		}
	}
	return nil
}

// isValidIdentifier checks if s matches [A-Za-z_][A-Za-z0-9_]*.
func isValidIdentifier(s string) bool {
	if s == "" {
		return false
	}
	for i := 0; i < len(s); i++ {
		if i == 0 {
			if !isIdentStart(s[i]) {
				return false
			}
		} else {
			if !isIdentPart(s[i]) {
				return false
			}
		}
	}
	return true
}

// stylesheet_syntax: The model_stylesheet attribute must parse as valid stylesheet rules.
type stylesheetSyntaxRule struct{}

func (stylesheetSyntaxRule) Name() string { return "stylesheet_syntax" }

func (stylesheetSyntaxRule) Apply(g *Graph) []Diagnostic {
	ss, ok := g.GraphAttr("model_stylesheet")
	if !ok || ss.Str == "" {
		return nil
	}
	if err := validateStylesheet(ss.Str); err != nil {
		return []Diagnostic{{
			Rule:     "stylesheet_syntax",
			Severity: Error,
			Message:  fmt.Sprintf("invalid model_stylesheet: %v", err),
			Fix:      "fix the stylesheet syntax",
		}}
	}
	return nil
}

// validateStylesheet checks that a stylesheet string follows the grammar:
// Stylesheet ::= Rule+
// Rule       ::= Selector '{' Declaration ( ';' Declaration )* ';'? '}'
func validateStylesheet(src string) error {
	p := &stylesheetParser{src: src}
	p.skipWhitespace()
	if p.pos >= len(p.src) {
		return fmt.Errorf("stylesheet is empty")
	}
	for p.pos < len(p.src) {
		if err := p.parseRule(); err != nil {
			return err
		}
		p.skipWhitespace()
	}
	return nil
}

type stylesheetParser struct {
	src string
	pos int
}

func (p *stylesheetParser) skipWhitespace() {
	for p.pos < len(p.src) && (p.src[p.pos] == ' ' || p.src[p.pos] == '\t' || p.src[p.pos] == '\n' || p.src[p.pos] == '\r') {
		p.pos++
	}
}

func (p *stylesheetParser) parseRule() error {
	// Selector
	p.skipWhitespace()
	if p.pos >= len(p.src) {
		return fmt.Errorf("expected selector at position %d", p.pos)
	}
	if err := p.parseSelector(); err != nil {
		return err
	}

	// '{'
	p.skipWhitespace()
	if p.pos >= len(p.src) || p.src[p.pos] != '{' {
		return fmt.Errorf("expected '{' after selector at position %d", p.pos)
	}
	p.pos++

	// Declarations
	parsedDecl := false
	for {
		p.skipWhitespace()
		if p.pos >= len(p.src) {
			return fmt.Errorf("unterminated rule block")
		}
		if p.src[p.pos] == '}' {
			break
		}
		if err := p.parseDeclaration(); err != nil {
			return err
		}
		parsedDecl = true
		p.skipWhitespace()
		// Optional semicolon
		if p.pos < len(p.src) && p.src[p.pos] == ';' {
			p.pos++
		}
	}
	_ = parsedDecl

	// '}'
	if p.pos >= len(p.src) || p.src[p.pos] != '}' {
		return fmt.Errorf("expected '}' at position %d", p.pos)
	}
	p.pos++
	return nil
}

func (p *stylesheetParser) parseSelector() error {
	if p.pos >= len(p.src) {
		return fmt.Errorf("expected selector")
	}
	ch := p.src[p.pos]
	switch {
	case ch == '*':
		p.pos++
		return nil
	case ch == '#':
		p.pos++
		return p.parseIdent("identifier after '#'")
	case ch == '.':
		p.pos++
		return p.parseClassName()
	default:
		return fmt.Errorf("invalid selector character %q at position %d; expected '*', '#', or '.'", string(ch), p.pos)
	}
}

func (p *stylesheetParser) parseIdent(desc string) error {
	start := p.pos
	for p.pos < len(p.src) {
		ch := p.src[p.pos]
		if isIdentPart(ch) {
			p.pos++
		} else {
			break
		}
	}
	if p.pos == start {
		return fmt.Errorf("expected %s at position %d", desc, p.pos)
	}
	return nil
}

func (p *stylesheetParser) parseClassName() error {
	// ClassName ::= [a-z0-9-]+
	start := p.pos
	for p.pos < len(p.src) {
		ch := p.src[p.pos]
		if (ch >= 'a' && ch <= 'z') || isDigit(ch) || ch == '-' {
			p.pos++
		} else {
			break
		}
	}
	if p.pos == start {
		return fmt.Errorf("expected class name at position %d", p.pos)
	}
	return nil
}

var validStylesheetProperties = map[string]bool{
	"llm_model":        true,
	"llm_provider":     true,
	"reasoning_effort": true,
}

func (p *stylesheetParser) parseDeclaration() error {
	// Property ':' PropertyValue
	start := p.pos
	for p.pos < len(p.src) {
		ch := p.src[p.pos]
		if (ch >= 'a' && ch <= 'z') || (ch >= 'A' && ch <= 'Z') || ch == '_' {
			p.pos++
		} else {
			break
		}
	}
	if p.pos == start {
		return fmt.Errorf("expected property name at position %d", p.pos)
	}
	prop := p.src[start:p.pos]
	if !validStylesheetProperties[prop] {
		return fmt.Errorf("unknown stylesheet property %q at position %d; valid properties are llm_model, llm_provider, reasoning_effort", prop, start)
	}

	p.skipWhitespace()
	if p.pos >= len(p.src) || p.src[p.pos] != ':' {
		return fmt.Errorf("expected ':' after property %q at position %d", prop, p.pos)
	}
	p.pos++

	p.skipWhitespace()
	if err := p.parsePropertyValue(); err != nil {
		return err
	}
	return nil
}

func (p *stylesheetParser) parsePropertyValue() error {
	if p.pos >= len(p.src) {
		return fmt.Errorf("expected property value at position %d", p.pos)
	}
	// Value is everything up to ';' or '}', trimmed
	start := p.pos
	for p.pos < len(p.src) && p.src[p.pos] != ';' && p.src[p.pos] != '}' {
		p.pos++
	}
	val := strings.TrimSpace(p.src[start:p.pos])
	if val == "" {
		return fmt.Errorf("empty property value at position %d", start)
	}
	return nil
}

// type_known: Node type values should be recognized by the handler registry.
type typeKnownRule struct{}

func (typeKnownRule) Name() string { return "type_known" }

func (typeKnownRule) Apply(g *Graph) []Diagnostic {
	var diags []Diagnostic
	for _, n := range g.Nodes {
		typ, ok := n.Attr("type")
		if !ok || typ.Str == "" {
			continue
		}
		if !knownHandlerTypes[typ.Str] {
			diags = append(diags, Diagnostic{
				Rule:     "type_known",
				Severity: Warning,
				Message:  fmt.Sprintf("node %q has unrecognized type %q", n.ID, typ.Str),
				NodeID:   n.ID,
				Fix:      "use a recognized handler type or register a custom handler",
			})
		}
	}
	return diags
}

// fidelity_valid: Fidelity mode values must be valid.
type fidelityValidRule struct{}

func (fidelityValidRule) Name() string { return "fidelity_valid" }

func (fidelityValidRule) Apply(g *Graph) []Diagnostic {
	var diags []Diagnostic

	// Check graph-level default_fidelity
	if fid, ok := g.GraphAttr("default_fidelity"); ok && fid.Str != "" {
		if !validFidelityModes[fid.Str] {
			diags = append(diags, Diagnostic{
				Rule:     "fidelity_valid",
				Severity: Warning,
				Message:  fmt.Sprintf("graph attribute default_fidelity has invalid value %q", fid.Str),
				Fix:      "use one of: full, truncate, compact, summary:low, summary:medium, summary:high",
			})
		}
	}

	// Check node-level fidelity
	for _, n := range g.Nodes {
		if fid, ok := n.Attr("fidelity"); ok && fid.Str != "" {
			if !validFidelityModes[fid.Str] {
				diags = append(diags, Diagnostic{
					Rule:     "fidelity_valid",
					Severity: Warning,
					Message:  fmt.Sprintf("node %q has invalid fidelity value %q", n.ID, fid.Str),
					NodeID:   n.ID,
					Fix:      "use one of: full, truncate, compact, summary:low, summary:medium, summary:high",
				})
			}
		}
	}

	// Check edge-level fidelity
	for _, e := range g.Edges {
		if fid, ok := e.Attr("fidelity"); ok && fid.Str != "" {
			if !validFidelityModes[fid.Str] {
				diags = append(diags, Diagnostic{
					Rule:     "fidelity_valid",
					Severity: Warning,
					Message:  fmt.Sprintf("edge %s -> %s has invalid fidelity value %q", e.From, e.To, fid.Str),
					Edge:     &EdgeRef{From: e.From, To: e.To},
					Fix:      "use one of: full, truncate, compact, summary:low, summary:medium, summary:high",
				})
			}
		}
	}

	return diags
}

// retry_target_exists: retry_target and fallback_retry_target must reference existing nodes.
type retryTargetExistsRule struct{}

func (retryTargetExistsRule) Name() string { return "retry_target_exists" }

func (retryTargetExistsRule) Apply(g *Graph) []Diagnostic {
	nodeSet := make(map[string]bool, len(g.Nodes))
	for _, n := range g.Nodes {
		nodeSet[n.ID] = true
	}

	var diags []Diagnostic

	// Check graph-level retry_target and fallback_retry_target
	for _, key := range []string{"retry_target", "fallback_retry_target"} {
		if v, ok := g.GraphAttr(key); ok && v.Str != "" {
			if !nodeSet[v.Str] {
				diags = append(diags, Diagnostic{
					Rule:     "retry_target_exists",
					Severity: Warning,
					Message:  fmt.Sprintf("graph attribute %s references non-existent node %q", key, v.Str),
					Fix:      fmt.Sprintf("ensure node %q exists or fix the %s value", v.Str, key),
				})
			}
		}
	}

	// Check node-level retry_target and fallback_retry_target
	for _, n := range g.Nodes {
		for _, key := range []string{"retry_target", "fallback_retry_target"} {
			if v, ok := n.Attr(key); ok && v.Str != "" {
				if !nodeSet[v.Str] {
					diags = append(diags, Diagnostic{
						Rule:     "retry_target_exists",
						Severity: Warning,
						Message:  fmt.Sprintf("node %q attribute %s references non-existent node %q", n.ID, key, v.Str),
						NodeID:   n.ID,
						Fix:      fmt.Sprintf("ensure node %q exists or fix the %s value", v.Str, key),
					})
				}
			}
		}
	}

	return diags
}

// goal_gate_has_retry: Nodes with goal_gate=true should have a retry_target or fallback_retry_target.
type goalGateHasRetryRule struct{}

func (goalGateHasRetryRule) Name() string { return "goal_gate_has_retry" }

func (goalGateHasRetryRule) Apply(g *Graph) []Diagnostic {
	// Check if graph-level fallbacks exist
	_, hasGraphRetry := g.GraphAttr("retry_target")
	_, hasGraphFallback := g.GraphAttr("fallback_retry_target")
	graphHasRetry := hasGraphRetry || hasGraphFallback

	var diags []Diagnostic
	for _, n := range g.Nodes {
		gg, ok := n.Attr("goal_gate")
		if !ok || !gg.Bool {
			continue
		}
		_, hasNodeRetry := n.Attr("retry_target")
		_, hasNodeFallback := n.Attr("fallback_retry_target")
		if !hasNodeRetry && !hasNodeFallback && !graphHasRetry {
			diags = append(diags, Diagnostic{
				Rule:     "goal_gate_has_retry",
				Severity: Warning,
				Message:  fmt.Sprintf("node %q has goal_gate=true but no retry_target or fallback_retry_target", n.ID),
				NodeID:   n.ID,
				Fix:      "add retry_target or fallback_retry_target attribute to the node or graph",
			})
		}
	}
	return diags
}

// prompt_on_llm_nodes: Nodes that resolve to codergen handler should have a prompt or label.
type promptOnLLMNodesRule struct{}

func (promptOnLLMNodesRule) Name() string { return "prompt_on_llm_nodes" }

func (promptOnLLMNodesRule) Apply(g *Graph) []Diagnostic {
	var diags []Diagnostic
	for _, n := range g.Nodes {
		handler := resolveHandlerType(n)
		if handler != "codergen" {
			continue
		}
		_, hasPrompt := n.Attr("prompt")
		_, hasLabel := n.Attr("label")
		if !hasPrompt && !hasLabel {
			diags = append(diags, Diagnostic{
				Rule:     "prompt_on_llm_nodes",
				Severity: Warning,
				Message:  fmt.Sprintf("LLM node %q has no prompt or label attribute", n.ID),
				NodeID:   n.ID,
				Fix:      "add a prompt or label attribute to provide instructions for the LLM",
			})
		}
	}
	return diags
}
