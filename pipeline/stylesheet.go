package pipeline

import (
	"fmt"
	"regexp"
	"strings"

	"github.com/martinemde/attractor/dotparser"
)

// SelectorType discriminates stylesheet selector types.
type SelectorType int

const (
	// SelectorUniversal matches all nodes (*). Specificity: 0.
	SelectorUniversal SelectorType = iota
	// SelectorClass matches nodes by class name (.name). Specificity: 1.
	SelectorClass
	// SelectorID matches nodes by exact ID (#id). Specificity: 2.
	SelectorID
)

// StyleSelector represents a CSS-like selector in a stylesheet rule.
type StyleSelector struct {
	Type  SelectorType
	Value string // class name or node ID (empty for universal)
}

// Specificity returns the selector's specificity weight.
func (s StyleSelector) Specificity() int {
	return int(s.Type)
}

// StyleDeclaration is a property-value pair in a stylesheet rule.
type StyleDeclaration struct {
	Property string
	Value    string
}

// StyleRule is a complete rule with a selector and declarations.
type StyleRule struct {
	Selector     StyleSelector
	Declarations []StyleDeclaration
}

// Stylesheet is a collection of parsed stylesheet rules.
type Stylesheet struct {
	Rules []StyleRule
}

// validPropertyRE matches valid LLM property names.
var validPropertyRE = regexp.MustCompile(`^(llm_model|llm_provider|reasoning_effort)$`)

// classNameRE matches valid class names: [a-z0-9-]+
var classNameRE = regexp.MustCompile(`^[a-z0-9-]+$`)

// identifierRE matches valid identifiers for node IDs.
var identifierRE = regexp.MustCompile(`^[a-zA-Z_][a-zA-Z0-9_]*$`)

// ParseStylesheet parses a CSS-like stylesheet string into a Stylesheet.
func ParseStylesheet(source string) (*Stylesheet, error) {
	source = strings.TrimSpace(source)
	if source == "" {
		return &Stylesheet{}, nil
	}

	stylesheet := &Stylesheet{}
	remaining := source

	for len(remaining) > 0 {
		remaining = strings.TrimSpace(remaining)
		if remaining == "" {
			break
		}

		rule, rest, err := parseRule(remaining)
		if err != nil {
			return nil, err
		}
		stylesheet.Rules = append(stylesheet.Rules, rule)
		remaining = rest
	}

	return stylesheet, nil
}

// parseRule parses a single rule from the source and returns the rule and remaining source.
func parseRule(source string) (StyleRule, string, error) {
	// Parse selector
	selector, rest, err := parseSelector(source)
	if err != nil {
		return StyleRule{}, "", err
	}

	rest = strings.TrimSpace(rest)

	// Expect '{'
	if len(rest) == 0 || rest[0] != '{' {
		return StyleRule{}, "", fmt.Errorf("expected '{' after selector, got %q", truncate(rest, 20))
	}
	rest = strings.TrimSpace(rest[1:])

	// Parse declarations until '}'
	var declarations []StyleDeclaration
	for len(rest) > 0 && rest[0] != '}' {
		decl, remainder, err := parseDeclaration(rest)
		if err != nil {
			return StyleRule{}, "", err
		}
		declarations = append(declarations, decl)
		rest = strings.TrimSpace(remainder)

		// Consume optional semicolon
		if len(rest) > 0 && rest[0] == ';' {
			rest = strings.TrimSpace(rest[1:])
		}
	}

	// Expect '}'
	if len(rest) == 0 || rest[0] != '}' {
		return StyleRule{}, "", fmt.Errorf("expected '}' at end of rule, got %q", truncate(rest, 20))
	}
	rest = rest[1:]

	return StyleRule{
		Selector:     selector,
		Declarations: declarations,
	}, rest, nil
}

// parseSelector parses a selector from the source.
func parseSelector(source string) (StyleSelector, string, error) {
	source = strings.TrimSpace(source)
	if len(source) == 0 {
		return StyleSelector{}, "", fmt.Errorf("expected selector")
	}

	switch source[0] {
	case '*':
		return StyleSelector{Type: SelectorUniversal}, source[1:], nil
	case '.':
		// Class selector
		name, rest := parseIdentifierLike(source[1:], classNameRE)
		if name == "" {
			return StyleSelector{}, "", fmt.Errorf("expected class name after '.'")
		}
		return StyleSelector{Type: SelectorClass, Value: name}, rest, nil
	case '#':
		// ID selector
		name, rest := parseIdentifierLike(source[1:], identifierRE)
		if name == "" {
			return StyleSelector{}, "", fmt.Errorf("expected node ID after '#'")
		}
		return StyleSelector{Type: SelectorID, Value: name}, rest, nil
	default:
		return StyleSelector{}, "", fmt.Errorf("expected selector starting with '*', '.', or '#', got %q", truncate(source, 20))
	}
}

// parseIdentifierLike extracts an identifier-like token that matches the given regex.
func parseIdentifierLike(source string, pattern *regexp.Regexp) (string, string) {
	// Find the end of the identifier-like token
	end := 0
	for end < len(source) {
		c := source[end]
		if !isIdentifierChar(c) {
			break
		}
		end++
	}

	if end == 0 {
		return "", source
	}

	token := source[:end]
	if !pattern.MatchString(token) {
		return "", source
	}

	return token, source[end:]
}

// isIdentifierChar returns true if c can be part of an identifier.
func isIdentifierChar(c byte) bool {
	return (c >= 'a' && c <= 'z') || (c >= 'A' && c <= 'Z') || (c >= '0' && c <= '9') || c == '_' || c == '-'
}

// parseDeclaration parses a property:value declaration.
func parseDeclaration(source string) (StyleDeclaration, string, error) {
	source = strings.TrimSpace(source)

	// Parse property name
	propEnd := strings.IndexAny(source, ":")
	if propEnd < 0 {
		return StyleDeclaration{}, "", fmt.Errorf("expected ':' in declaration")
	}

	property := strings.TrimSpace(source[:propEnd])
	if !validPropertyRE.MatchString(property) {
		return StyleDeclaration{}, "", fmt.Errorf("invalid property %q, expected llm_model, llm_provider, or reasoning_effort", property)
	}

	rest := strings.TrimSpace(source[propEnd+1:])

	// Parse property value (until ';' or '}')
	valueEnd := strings.IndexAny(rest, ";}")
	if valueEnd < 0 {
		return StyleDeclaration{}, "", fmt.Errorf("expected ';' or '}' after property value")
	}

	value := strings.TrimSpace(rest[:valueEnd])
	if value == "" {
		return StyleDeclaration{}, "", fmt.Errorf("empty value for property %q", property)
	}

	return StyleDeclaration{
		Property: property,
		Value:    value,
	}, rest[valueEnd:], nil
}

// truncate returns the first n characters of s, adding "..." if truncated.
func truncate(s string, n int) string {
	if len(s) <= n {
		return s
	}
	return s[:n] + "..."
}

// StylesheetTransform applies model_stylesheet rules to graph nodes.
type StylesheetTransform struct{}

// Apply implements Transform. It reads the model_stylesheet graph attribute,
// parses it, and applies matching rules to nodes that don't already have
// the properties set explicitly.
func (t *StylesheetTransform) Apply(graph *dotparser.Graph) *dotparser.Graph {
	// Get the model_stylesheet attribute
	stylesheetValue, ok := graph.GraphAttr("model_stylesheet")
	if !ok {
		return graph
	}

	stylesheet, err := ParseStylesheet(stylesheetValue.Str)
	if err != nil {
		// If parsing fails, leave the graph unchanged
		// (validation can catch this later if needed)
		return graph
	}

	// Apply stylesheet to each node
	for _, node := range graph.Nodes {
		applyStylesheetToNode(node, stylesheet)
	}

	return graph
}

// applyStylesheetToNode applies all matching rules to a node, respecting specificity.
func applyStylesheetToNode(node *dotparser.Node, stylesheet *Stylesheet) {
	// Collect all matching rules with their specificity
	type match struct {
		rule        StyleRule
		specificity int
		order       int // original order in stylesheet for tie-breaking
	}

	var matches []match

	for i, rule := range stylesheet.Rules {
		if selectorMatchesNode(rule.Selector, node) {
			matches = append(matches, match{
				rule:        rule,
				specificity: rule.Selector.Specificity(),
				order:       i,
			})
		}
	}

	if len(matches) == 0 {
		return
	}

	// Build final property values: later rules of equal specificity override earlier ones
	// Higher specificity always wins
	propertyValues := make(map[string]struct {
		value       string
		specificity int
		order       int
	})

	for _, m := range matches {
		for _, decl := range m.rule.Declarations {
			existing, exists := propertyValues[decl.Property]
			if !exists || m.specificity > existing.specificity ||
				(m.specificity == existing.specificity && m.order > existing.order) {
				propertyValues[decl.Property] = struct {
					value       string
					specificity int
					order       int
				}{decl.Value, m.specificity, m.order}
			}
		}
	}

	// Apply properties only if node doesn't already have them explicitly set
	for property, pv := range propertyValues {
		if _, hasExplicit := node.Attr(property); !hasExplicit {
			node.Attrs = append(node.Attrs, dotparser.Attr{
				Key: property,
				Value: dotparser.Value{
					Kind: dotparser.ValueString,
					Str:  pv.value,
					Raw:  pv.value,
				},
			})
		}
	}
}

// selectorMatchesNode returns true if the selector matches the node.
func selectorMatchesNode(selector StyleSelector, node *dotparser.Node) bool {
	switch selector.Type {
	case SelectorUniversal:
		return true
	case SelectorID:
		return node.ID == selector.Value
	case SelectorClass:
		return nodeHasClass(node, selector.Value)
	default:
		return false
	}
}

// nodeHasClass returns true if the node's class attribute contains the given class.
func nodeHasClass(node *dotparser.Node, className string) bool {
	classAttr, ok := node.Attr("class")
	if !ok {
		return false
	}

	// Classes are comma-separated
	classes := strings.Split(classAttr.Str, ",")
	for _, c := range classes {
		if strings.TrimSpace(c) == className {
			return true
		}
	}
	return false
}
