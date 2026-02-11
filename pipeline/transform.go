package pipeline

import (
	"strings"

	"github.com/martinemde/attractor/dotparser"
)

// Transform is the interface for AST transforms that modify the pipeline graph
// after parsing and before execution. Transforms enable preprocessing,
// optimization, and structural rewriting without modifying the original DOT file.
type Transform interface {
	// Apply transforms the graph and returns the (potentially modified) graph.
	// The transform may modify the input graph in place.
	Apply(graph *dotparser.Graph) *dotparser.Graph
}

// TransformFunc is an adapter to use a function as a Transform.
type TransformFunc func(graph *dotparser.Graph) *dotparser.Graph

// Apply implements Transform.
func (f TransformFunc) Apply(graph *dotparser.Graph) *dotparser.Graph {
	return f(graph)
}

// VariableExpansionTransform expands $goal in node prompt attributes
// to the graph-level goal attribute value.
type VariableExpansionTransform struct{}

// Apply replaces $goal in all node prompt attributes with the graph-level goal attribute.
func (t *VariableExpansionTransform) Apply(graph *dotparser.Graph) *dotparser.Graph {
	// Get the graph-level goal attribute
	goalValue, hasGoal := graph.GraphAttr("goal")
	if !hasGoal {
		return graph
	}
	goalStr := goalValue.Str

	// Replace $goal in all node prompt attributes
	for _, node := range graph.Nodes {
		for i, attr := range node.Attrs {
			if attr.Key == "prompt" && strings.Contains(attr.Value.Str, "$goal") {
				newStr := strings.ReplaceAll(attr.Value.Str, "$goal", goalStr)
				node.Attrs[i].Value.Str = newStr
				node.Attrs[i].Value.Raw = newStr
			}
		}
	}

	return graph
}

// ApplyTransforms applies a slice of transforms to a graph in order.
func ApplyTransforms(graph *dotparser.Graph, transforms []Transform) *dotparser.Graph {
	for _, t := range transforms {
		graph = t.Apply(graph)
	}
	return graph
}

// DefaultTransforms returns the standard transforms that are automatically
// applied to every pipeline execution. Per attractor-spec.md Section 8.5,
// the stylesheet is applied as a transform after parsing and before validation.
// Variable expansion is applied after stylesheet for $goal substitution.
//
// Order matters: StylesheetTransform runs first to populate node attributes,
// then VariableExpansionTransform expands $goal in prompt attributes.
func DefaultTransforms() []Transform {
	return []Transform{
		&StylesheetTransform{},
		&VariableExpansionTransform{},
	}
}
