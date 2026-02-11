package dotparser

// TokenKind identifies the type of a lexical token.
type TokenKind int

const (
	TokenEOF TokenKind = iota
	TokenIdentifier  // [A-Za-z_][A-Za-z0-9_]*
	TokenString      // "..." with escape processing
	TokenInteger     // -?[0-9]+
	TokenFloat       // -?[0-9]*.[0-9]+
	TokenArrow       // ->
	TokenLBrace      // {
	TokenRBrace      // }
	TokenLBracket    // [
	TokenRBracket    // ]
	TokenEquals      // =
	TokenComma       // ,
	TokenSemicolon   // ;
	TokenDot         // .

	// Keywords (identifier text checked against keyword map)
	TokenDigraph  // digraph
	TokenGraph    // graph
	TokenNode     // node
	TokenEdge     // edge
	TokenSubgraph // subgraph
	TokenTrue     // true
	TokenFalse    // false
)

var tokenNames = map[TokenKind]string{
	TokenEOF:        "EOF",
	TokenIdentifier: "identifier",
	TokenString:     "string",
	TokenInteger:    "integer",
	TokenFloat:      "float",
	TokenArrow:      "'->'",
	TokenLBrace:     "'{'",
	TokenRBrace:     "'}'",
	TokenLBracket:   "'['",
	TokenRBracket:   "']'",
	TokenEquals:     "'='",
	TokenComma:      "','",
	TokenSemicolon:  "';'",
	TokenDot:        "'.'",
	TokenDigraph:    "'digraph'",
	TokenGraph:      "'graph'",
	TokenNode:       "'node'",
	TokenEdge:       "'edge'",
	TokenSubgraph:   "'subgraph'",
	TokenTrue:       "'true'",
	TokenFalse:      "'false'",
}

func (k TokenKind) String() string {
	if name, ok := tokenNames[k]; ok {
		return name
	}
	return "unknown"
}

// Token is a single lexical unit produced by the Lexer.
type Token struct {
	Kind    TokenKind
	Literal string // text content (decoded for strings, raw for others)
	Pos     Position
}

// keywords maps keyword strings to their token kinds.
var keywords = map[string]TokenKind{
	"digraph":  TokenDigraph,
	"graph":    TokenGraph,
	"node":     TokenNode,
	"edge":     TokenEdge,
	"subgraph": TokenSubgraph,
	"true":     TokenTrue,
	"false":    TokenFalse,
}
