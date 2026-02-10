package dotparser

import (
	"fmt"
	"strings"
	"unicode"
)

// Parse parses DOT source text and returns a Graph.
// Returns a *SyntaxError, *LexError, or *ValueError on failure.
func Parse(src []byte) (*Graph, error) {
	p := &parser{
		lex:   NewLexer(src),
		nodes: make(map[string]*Node),
	}
	return p.parseGraph()
}

type parser struct {
	lex          *Lexer
	nodeDefaults []Attr
	edgeDefaults []Attr
	nodes        map[string]*Node // dedup by ID
	nodeOrder    []string         // preserve declaration order
	edges        []*Edge
	graphAttrs   []Attr
}

func (p *parser) peek() (Token, error) {
	return p.lex.Peek()
}

func (p *parser) next() (Token, error) {
	return p.lex.Next()
}

func (p *parser) expect(kind TokenKind) (Token, error) {
	tok, err := p.next()
	if err != nil {
		return Token{}, err
	}
	if tok.Kind != kind {
		return Token{}, &SyntaxError{
			ParseError: ParseError{Pos: tok.Pos},
			Expected:   kind.String(),
			Got:        fmt.Sprintf("%s (%q)", tok.Kind, tok.Literal),
		}
	}
	return tok, nil
}

func (p *parser) consumeOptionalSemicolon() error {
	tok, err := p.peek()
	if err != nil {
		return err
	}
	if tok.Kind == TokenSemicolon {
		_, _ = p.next()
	}
	return nil
}

// ensureNode registers a node if it does not already exist.
// Returns the existing or newly created node.
func (p *parser) ensureNode(id string, pos Position) *Node {
	if n, ok := p.nodes[id]; ok {
		return n
	}
	n := &Node{
		ID:    id,
		Attrs: copyAttrs(p.nodeDefaults),
		Pos:   pos,
	}
	p.nodes[id] = n
	p.nodeOrder = append(p.nodeOrder, id)
	return n
}

func (p *parser) parseGraph() (*Graph, error) {
	// Reject 'strict' modifier
	tok, err := p.peek()
	if err != nil {
		return nil, err
	}
	if tok.Kind == TokenIdentifier && tok.Literal == "strict" {
		return nil, &SyntaxError{
			ParseError: ParseError{
				Message: "strict modifier is not supported",
				Pos:     tok.Pos,
			},
			Expected: "'digraph'",
			Got:      "'strict'",
		}
	}

	if _, err := p.expect(TokenDigraph); err != nil {
		return nil, err
	}

	nameTok, err := p.expect(TokenIdentifier)
	if err != nil {
		return nil, err
	}

	if _, err := p.expect(TokenLBrace); err != nil {
		return nil, err
	}

	for {
		tok, err := p.peek()
		if err != nil {
			return nil, err
		}
		if tok.Kind == TokenRBrace || tok.Kind == TokenEOF {
			break
		}
		if err := p.parseStatement(); err != nil {
			return nil, err
		}
	}

	if _, err := p.expect(TokenRBrace); err != nil {
		return nil, err
	}

	// Reject trailing content (one digraph per file)
	tok, err = p.peek()
	if err != nil {
		return nil, err
	}
	if tok.Kind != TokenEOF {
		return nil, &SyntaxError{
			ParseError: ParseError{
				Message: "only one digraph per file is allowed",
				Pos:     tok.Pos,
			},
			Expected: "EOF",
			Got:      fmt.Sprintf("%s (%q)", tok.Kind, tok.Literal),
		}
	}

	// Build ordered node list
	nodes := make([]*Node, 0, len(p.nodeOrder))
	for _, id := range p.nodeOrder {
		nodes = append(nodes, p.nodes[id])
	}

	return &Graph{
		Name:       nameTok.Literal,
		GraphAttrs: p.graphAttrs,
		Nodes:      nodes,
		Edges:      p.edges,
	}, nil
}

func (p *parser) parseStatement() error {
	tok, err := p.peek()
	if err != nil {
		return err
	}

	switch tok.Kind {
	case TokenSemicolon:
		_, _ = p.next()
		return nil

	case TokenGraph:
		return p.parseGraphOrAttrDecl()

	case TokenNode:
		return p.parseNodeDefaultsOrStmt()

	case TokenEdge:
		return p.parseEdgeDefaultsOrStmt()

	case TokenSubgraph:
		return p.parseSubgraph()

	case TokenIdentifier:
		return p.parseIdentifierStatement()

	case TokenRBrace:
		// End of block, don't consume
		return nil

	default:
		return &SyntaxError{
			ParseError: ParseError{Pos: tok.Pos},
			Expected:   "statement",
			Got:        fmt.Sprintf("%s (%q)", tok.Kind, tok.Literal),
		}
	}
}

// parseGraphOrAttrDecl handles 'graph' at the start of a statement.
// Could be: graph [attrs] or graph = value (where 'graph' is used as an attr key).
func (p *parser) parseGraphOrAttrDecl() error {
	_, _ = p.next() // consume 'graph'

	tok, err := p.peek()
	if err != nil {
		return err
	}

	if tok.Kind == TokenLBracket {
		attrs, err := p.parseAttrBlock()
		if err != nil {
			return err
		}
		p.graphAttrs = append(p.graphAttrs, attrs...)
		return p.consumeOptionalSemicolon()
	}

	// 'graph' followed by something else: treat as a graph attr declaration
	// e.g. graph = "value" (though unusual, handle it)
	if tok.Kind == TokenEquals {
		return p.parseGraphAttrDeclValue("graph")
	}

	return &SyntaxError{
		ParseError: ParseError{Pos: tok.Pos},
		Expected:   "'[' or '='",
		Got:        fmt.Sprintf("%s (%q)", tok.Kind, tok.Literal),
	}
}

// parseNodeDefaultsOrStmt handles 'node' at the start of a statement.
func (p *parser) parseNodeDefaultsOrStmt() error {
	tok, _ := p.next() // consume 'node'

	next, err := p.peek()
	if err != nil {
		return err
	}

	if next.Kind == TokenLBracket {
		attrs, err := p.parseAttrBlock()
		if err != nil {
			return err
		}
		p.nodeDefaults = attrs
		return p.consumeOptionalSemicolon()
	}

	// 'node' used as an identifier in edge or node statement (unusual but technically possible)
	if next.Kind == TokenArrow {
		return p.parseEdgeChain(tok.Literal, tok.Pos)
	}

	// Treat as a node stmt with ID "node"
	return p.parseNodeBody(tok.Literal, tok.Pos)
}

// parseEdgeDefaultsOrStmt handles 'edge' at the start of a statement.
func (p *parser) parseEdgeDefaultsOrStmt() error {
	tok, _ := p.next() // consume 'edge'

	next, err := p.peek()
	if err != nil {
		return err
	}

	if next.Kind == TokenLBracket {
		attrs, err := p.parseAttrBlock()
		if err != nil {
			return err
		}
		p.edgeDefaults = attrs
		return p.consumeOptionalSemicolon()
	}

	// 'edge' used as an identifier in edge or node statement
	if next.Kind == TokenArrow {
		return p.parseEdgeChain(tok.Literal, tok.Pos)
	}

	return p.parseNodeBody(tok.Literal, tok.Pos)
}

// parseIdentifierStatement handles an identifier at the start of a statement.
// Disambiguates between GraphAttrDecl, EdgeStmt, and NodeStmt.
func (p *parser) parseIdentifierStatement() error {
	tok, _ := p.next() // consume identifier

	next, err := p.peek()
	if err != nil {
		return err
	}

	switch next.Kind {
	case TokenEquals:
		// GraphAttrDecl: key = value
		return p.parseGraphAttrDeclValue(tok.Literal)
	case TokenArrow:
		// EdgeStmt: A -> B -> C [attrs]
		return p.parseEdgeChain(tok.Literal, tok.Pos)
	default:
		// NodeStmt: A [attrs]
		return p.parseNodeBody(tok.Literal, tok.Pos)
	}
}

// parseGraphAttrDeclValue parses '= Value ;?' after the key has been consumed.
func (p *parser) parseGraphAttrDeclValue(key string) error {
	_, err := p.expect(TokenEquals)
	if err != nil {
		return err
	}

	val, err := p.parseValue()
	if err != nil {
		return err
	}

	p.graphAttrs = append(p.graphAttrs, Attr{Key: key, Value: val})
	return p.consumeOptionalSemicolon()
}

// parseNodeBody parses the optional attribute block and semicolon of a node statement.
func (p *parser) parseNodeBody(id string, pos Position) error {
	var explicit []Attr

	tok, err := p.peek()
	if err != nil {
		return err
	}
	if tok.Kind == TokenLBracket {
		explicit, err = p.parseAttrBlock()
		if err != nil {
			return err
		}
	}

	merged := mergeAttrs(p.nodeDefaults, explicit)

	if existing, ok := p.nodes[id]; ok {
		// Merge attributes into existing node
		existing.Attrs = mergeAttrs(existing.Attrs, explicit)
	} else {
		n := &Node{ID: id, Attrs: merged, Pos: pos}
		p.nodes[id] = n
		p.nodeOrder = append(p.nodeOrder, id)
	}

	return p.consumeOptionalSemicolon()
}

// parseEdgeChain parses '->' Identifier ('->' Identifier)* AttrBlock? ';'?
func (p *parser) parseEdgeChain(firstID string, pos Position) error {
	ids := []string{firstID}
	positions := []Position{pos}

	for {
		tok, err := p.peek()
		if err != nil {
			return err
		}
		if tok.Kind != TokenArrow {
			break
		}
		_, _ = p.next() // consume ->

		// The target can be an identifier or a keyword used as a node ID
		target, err := p.expectNodeID()
		if err != nil {
			return err
		}
		ids = append(ids, target.Literal)
		positions = append(positions, target.Pos)
	}

	var explicit []Attr
	tok, err := p.peek()
	if err != nil {
		return err
	}
	if tok.Kind == TokenLBracket {
		explicit, err = p.parseAttrBlock()
		if err != nil {
			return err
		}
	}

	merged := mergeAttrs(p.edgeDefaults, explicit)

	// Ensure all nodes in the chain exist
	for i, id := range ids {
		p.ensureNode(id, positions[i])
	}

	// Create individual edges for each consecutive pair
	for i := 0; i < len(ids)-1; i++ {
		edge := &Edge{
			From:  ids[i],
			To:    ids[i+1],
			Attrs: copyAttrs(merged),
			Pos:   positions[i],
		}
		p.edges = append(p.edges, edge)
	}

	return p.consumeOptionalSemicolon()
}

// expectNodeID expects an identifier or keyword that can be used as a node ID.
func (p *parser) expectNodeID() (Token, error) {
	tok, err := p.next()
	if err != nil {
		return Token{}, err
	}
	switch tok.Kind {
	case TokenIdentifier, TokenNode, TokenEdge, TokenGraph:
		return tok, nil
	default:
		return Token{}, &SyntaxError{
			ParseError: ParseError{Pos: tok.Pos},
			Expected:   "node identifier",
			Got:        fmt.Sprintf("%s (%q)", tok.Kind, tok.Literal),
		}
	}
}

func (p *parser) parseSubgraph() error {
	_, _ = p.next() // consume 'subgraph'

	// Optional subgraph name
	var subgraphName string
	tok, err := p.peek()
	if err != nil {
		return err
	}
	if tok.Kind == TokenIdentifier {
		nameTok, _ := p.next()
		subgraphName = nameTok.Literal
	}

	if _, err := p.expect(TokenLBrace); err != nil {
		return err
	}

	// Save current defaults (push scope)
	savedNodeDefaults := copyAttrs(p.nodeDefaults)
	savedEdgeDefaults := copyAttrs(p.edgeDefaults)

	// Track nodes added within this subgraph scope
	nodesBefore := make(map[string]bool)
	for id := range p.nodes {
		nodesBefore[id] = true
	}

	// Track graph attrs count to find subgraph-local attrs
	graphAttrsBefore := len(p.graphAttrs)

	for {
		tok, err := p.peek()
		if err != nil {
			return err
		}
		if tok.Kind == TokenRBrace || tok.Kind == TokenEOF {
			break
		}
		if err := p.parseStatement(); err != nil {
			return err
		}
	}

	if _, err := p.expect(TokenRBrace); err != nil {
		return err
	}

	// Check if a label was set via graph attr decl inside the subgraph
	// (e.g., label = "Loop A"). Prefer this over the subgraph name.
	var innerLabel string
	for i := graphAttrsBefore; i < len(p.graphAttrs); i++ {
		if p.graphAttrs[i].Key == "label" {
			innerLabel = p.graphAttrs[i].Value.Str
		}
	}

	// Remove subgraph-scoped attrs from the parent graph attrs
	// (they belong to the subgraph, not the parent graph)
	p.graphAttrs = p.graphAttrs[:graphAttrsBefore]

	// Fall back to subgraph name if no label was set inside
	if innerLabel == "" {
		innerLabel = subgraphName
	}

	// Derive class from subgraph label
	derivedClass := deriveClassName(innerLabel)

	// Append derived class to all nodes added within this subgraph
	if derivedClass != "" {
		for id := range p.nodes {
			if nodesBefore[id] {
				continue
			}
			appendNodeClass(p.nodes[id], derivedClass)
		}
	}

	// Restore defaults (pop scope)
	p.nodeDefaults = savedNodeDefaults
	p.edgeDefaults = savedEdgeDefaults

	return nil
}

func (p *parser) parseAttrBlock() ([]Attr, error) {
	if _, err := p.expect(TokenLBracket); err != nil {
		return nil, err
	}

	var attrs []Attr

	// Handle empty attr block: []
	tok, err := p.peek()
	if err != nil {
		return nil, err
	}
	if tok.Kind == TokenRBracket {
		_, _ = p.next()
		return attrs, nil
	}

	// Parse first attr
	attr, err := p.parseAttr()
	if err != nil {
		return nil, err
	}
	attrs = append(attrs, attr)

	// Parse remaining attrs separated by commas
	for {
		tok, err := p.peek()
		if err != nil {
			return nil, err
		}
		if tok.Kind == TokenRBracket {
			break
		}
		if tok.Kind == TokenComma {
			_, _ = p.next() // consume comma
			// Allow trailing comma before ]
			tok, err = p.peek()
			if err != nil {
				return nil, err
			}
			if tok.Kind == TokenRBracket {
				break
			}
			attr, err := p.parseAttr()
			if err != nil {
				return nil, err
			}
			attrs = append(attrs, attr)
			continue
		}
		// No comma found — could be a separator-less attr or an error
		// Be lenient: try parsing another attr if next looks like key=value
		if tok.Kind == TokenIdentifier || tok.Kind == TokenGraph || tok.Kind == TokenNode || tok.Kind == TokenEdge {
			attr, err := p.parseAttr()
			if err != nil {
				return nil, err
			}
			attrs = append(attrs, attr)
			continue
		}
		break
	}

	if _, err := p.expect(TokenRBracket); err != nil {
		return nil, err
	}

	return attrs, nil
}

func (p *parser) parseAttr() (Attr, error) {
	key, pos, err := p.parseKey()
	if err != nil {
		return Attr{}, err
	}

	if _, err := p.expect(TokenEquals); err != nil {
		return Attr{}, err
	}

	val, err := p.parseValue()
	if err != nil {
		return Attr{}, err
	}

	return Attr{Key: key, Value: val, Pos: pos}, nil
}

// parseKey parses an Identifier or QualifiedId (Identifier ('.' Identifier)*).
func (p *parser) parseKey() (string, Position, error) {
	tok, err := p.next()
	if err != nil {
		return "", Position{}, err
	}

	// Accept keywords as attribute keys (e.g., label, edge, node, graph)
	if tok.Kind != TokenIdentifier && tok.Kind != TokenGraph && tok.Kind != TokenNode && tok.Kind != TokenEdge {
		return "", Position{}, &SyntaxError{
			ParseError: ParseError{Pos: tok.Pos},
			Expected:   "attribute key",
			Got:        fmt.Sprintf("%s (%q)", tok.Kind, tok.Literal),
		}
	}

	key := tok.Literal

	// Check for qualified identifier: key.subkey.subsubkey
	for {
		next, err := p.peek()
		if err != nil {
			return "", Position{}, err
		}
		if next.Kind != TokenDot {
			break
		}
		_, _ = p.next() // consume dot

		part, err := p.next()
		if err != nil {
			return "", Position{}, err
		}
		if part.Kind != TokenIdentifier {
			return "", Position{}, &SyntaxError{
				ParseError: ParseError{Pos: part.Pos},
				Expected:   "identifier after '.'",
				Got:        fmt.Sprintf("%s (%q)", part.Kind, part.Literal),
			}
		}
		key += "." + part.Literal
	}

	return key, tok.Pos, nil
}

func (p *parser) parseValue() (Value, error) {
	tok, err := p.next()
	if err != nil {
		return Value{}, err
	}
	return ParseValue(tok)
}

// mergeAttrs produces a final attribute list by starting with defaults and
// overlaying explicit attrs. Explicit attrs with the same key replace defaults.
func mergeAttrs(defaults, explicit []Attr) []Attr {
	if len(defaults) == 0 {
		return copyAttrs(explicit)
	}
	if len(explicit) == 0 {
		return copyAttrs(defaults)
	}

	overridden := make(map[string]bool, len(explicit))
	for _, a := range explicit {
		overridden[a.Key] = true
	}

	result := make([]Attr, 0, len(defaults)+len(explicit))
	for _, a := range defaults {
		if !overridden[a.Key] {
			result = append(result, a)
		}
	}
	result = append(result, explicit...)
	return result
}

func copyAttrs(attrs []Attr) []Attr {
	if attrs == nil {
		return nil
	}
	cp := make([]Attr, len(attrs))
	copy(cp, attrs)
	return cp
}

// deriveClassName derives a CSS-like class name from a subgraph label.
// Lowercases, replaces spaces with hyphens, strips non-alphanumeric except hyphens.
func deriveClassName(label string) string {
	if label == "" {
		return ""
	}

	// Strip common "cluster_" prefix used in Graphviz subgraph naming
	label = strings.TrimPrefix(label, "cluster_")

	var sb strings.Builder
	for _, r := range strings.ToLower(label) {
		switch {
		case r == ' ' || r == '_':
			sb.WriteRune('-')
		case unicode.IsLetter(r) || unicode.IsDigit(r) || r == '-':
			sb.WriteRune(r)
		}
	}

	// Trim leading/trailing hyphens
	return strings.Trim(sb.String(), "-")
}

// appendNodeClass appends a class name to a node's "class" attribute.
func appendNodeClass(n *Node, class string) {
	for i, attr := range n.Attrs {
		if attr.Key == "class" {
			existing := attr.Value.Str
			if existing == "" {
				n.Attrs[i].Value.Str = class
				n.Attrs[i].Value.Raw = class
			} else {
				n.Attrs[i].Value.Str = existing + "," + class
				n.Attrs[i].Value.Raw = existing + "," + class
			}
			return
		}
	}
	// No class attr yet, add one
	n.Attrs = append(n.Attrs, Attr{
		Key:   "class",
		Value: Value{Kind: ValueString, Str: class, Raw: class},
	})
}
