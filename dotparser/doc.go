// Package dotparser implements a parser for the Attractor DOT subset.
//
// Attractor accepts a strict subset of the Graphviz DOT language: one directed
// graph per file, bare identifiers for node IDs, directed edges only (->),
// typed attribute values, and optional semicolons. Both // line and /* block */
// comments are stripped before parsing.
//
// The parser is structured as a hand-rolled recursive-descent parser with three
// layers:
//
//   - Lexer: converts raw bytes into a token stream, stripping comments and
//     whitespace.
//   - Parser: consumes tokens according to the BNF grammar and builds an AST.
//   - AST types: the output data structures (Graph, Node, Edge, Attr, Value).
//
// Usage:
//
//	graph, err := dotparser.Parse(dotSource)
//	if err != nil {
//	    log.Fatal(err)
//	}
//	fmt.Println(graph.Name, len(graph.Nodes), len(graph.Edges))
//
// The supported grammar is defined in attractor-spec.md Section 2.2.
package dotparser
