package dotparser

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func collectTokens(t *testing.T, src string) []Token {
	t.Helper()
	lex := NewLexer([]byte(src))
	var tokens []Token
	for {
		tok, err := lex.Next()
		require.NoError(t, err)
		tokens = append(tokens, tok)
		if tok.Kind == TokenEOF {
			break
		}
	}
	return tokens
}

func TestLexerPunctuation(t *testing.T) {
	tokens := collectTokens(t, "{ } [ ] = , ; .")
	expected := []TokenKind{
		TokenLBrace, TokenRBrace, TokenLBracket, TokenRBracket,
		TokenEquals, TokenComma, TokenSemicolon, TokenDot, TokenEOF,
	}
	require.Len(t, tokens, len(expected))
	for i, tok := range tokens {
		assert.Equal(t, expected[i], tok.Kind, "token %d", i)
	}
}

func TestLexerArrow(t *testing.T) {
	tokens := collectTokens(t, "->")
	require.Len(t, tokens, 2)
	assert.Equal(t, TokenArrow, tokens[0].Kind)
	assert.Equal(t, "->", tokens[0].Literal)
}

func TestLexerIdentifiers(t *testing.T) {
	cases := []string{"foo", "_bar", "Plan123", "A_b_C"}
	for _, id := range cases {
		tokens := collectTokens(t, id)
		require.Len(t, tokens, 2, "input: %s", id) // identifier + EOF
		assert.Equal(t, TokenIdentifier, tokens[0].Kind, "input: %s", id)
		assert.Equal(t, id, tokens[0].Literal, "input: %s", id)
	}
}

func TestLexerKeywords(t *testing.T) {
	tests := []struct {
		input string
		kind  TokenKind
	}{
		{"digraph", TokenDigraph},
		{"graph", TokenGraph},
		{"node", TokenNode},
		{"edge", TokenEdge},
		{"subgraph", TokenSubgraph},
		{"true", TokenTrue},
		{"false", TokenFalse},
	}
	for _, tt := range tests {
		tokens := collectTokens(t, tt.input)
		require.Len(t, tokens, 2, "input: %s", tt.input)
		assert.Equal(t, tt.kind, tokens[0].Kind, "input: %s", tt.input)
	}
}

func TestLexerStrings(t *testing.T) {
	tests := []struct {
		input   string
		literal string
	}{
		{`"hello"`, "hello"},
		{`""`, ""},
		{`"say \"hi\""`, `say "hi"`},
		{`"a\\b"`, `a\b`},
		{`"line1\nline2"`, "line1\nline2"},
		{`"tab\there"`, "tab\there"},
		{`"multi\nline\nstring"`, "multi\nline\nstring"},
	}
	for _, tt := range tests {
		tokens := collectTokens(t, tt.input)
		require.Len(t, tokens, 2, "input: %s", tt.input)
		assert.Equal(t, TokenString, tokens[0].Kind, "input: %s", tt.input)
		assert.Equal(t, tt.literal, tokens[0].Literal, "input: %s", tt.input)
	}
}

func TestLexerUnterminatedString(t *testing.T) {
	lex := NewLexer([]byte(`"hello`))
	_, err := lex.Next()
	require.Error(t, err)
	assert.IsType(t, &LexError{}, err)
}

func TestLexerIntegers(t *testing.T) {
	tests := []struct {
		input   string
		literal string
	}{
		{"0", "0"},
		{"42", "42"},
		{"12345", "12345"},
	}
	for _, tt := range tests {
		tokens := collectTokens(t, tt.input)
		require.Len(t, tokens, 2, "input: %s", tt.input)
		assert.Equal(t, TokenInteger, tokens[0].Kind, "input: %s", tt.input)
		assert.Equal(t, tt.literal, tokens[0].Literal, "input: %s", tt.input)
	}
}

func TestLexerNegativeNumber(t *testing.T) {
	tokens := collectTokens(t, "-42")
	require.Len(t, tokens, 2)
	assert.Equal(t, TokenInteger, tokens[0].Kind)
	assert.Equal(t, "-42", tokens[0].Literal)

	tokens = collectTokens(t, "-3.14")
	require.Len(t, tokens, 2)
	assert.Equal(t, TokenFloat, tokens[0].Kind)
	assert.Equal(t, "-3.14", tokens[0].Literal)
}

func TestLexerFloats(t *testing.T) {
	tests := []struct {
		input   string
		literal string
	}{
		{"0.5", "0.5"},
		{"3.14", "3.14"},
	}
	for _, tt := range tests {
		tokens := collectTokens(t, tt.input)
		require.Len(t, tokens, 2, "input: %s", tt.input)
		assert.Equal(t, TokenFloat, tokens[0].Kind, "input: %s", tt.input)
		assert.Equal(t, tt.literal, tokens[0].Literal, "input: %s", tt.input)
	}
}

func TestLexerBareDurationIsIntegerThenIdentifier(t *testing.T) {
	// Bare durations like "900s" are not valid DOT; they split into two tokens.
	tests := []struct {
		input     string
		wantInt   string
		wantIdent string
	}{
		{"900s", "900", "s"},
		{"250ms", "250", "ms"},
		{"15m", "15", "m"},
		{"2h", "2", "h"},
		{"1d", "1", "d"},
	}
	for _, tt := range tests {
		tokens := collectTokens(t, tt.input)
		require.Len(t, tokens, 3, "input: %s", tt.input) // integer, identifier, EOF
		assert.Equal(t, TokenInteger, tokens[0].Kind, "input: %s", tt.input)
		assert.Equal(t, tt.wantInt, tokens[0].Literal, "input: %s", tt.input)
		assert.Equal(t, TokenIdentifier, tokens[1].Kind, "input: %s", tt.input)
		assert.Equal(t, tt.wantIdent, tokens[1].Literal, "input: %s", tt.input)
	}
}

func TestLexerLineComments(t *testing.T) {
	tokens := collectTokens(t, "A // comment\nB")
	require.Len(t, tokens, 3) // A, B, EOF
	assert.Equal(t, "A", tokens[0].Literal)
	assert.Equal(t, "B", tokens[1].Literal)
}

func TestLexerBlockComments(t *testing.T) {
	tokens := collectTokens(t, "A /* block\ncomment */ B")
	require.Len(t, tokens, 3) // A, B, EOF
	assert.Equal(t, "A", tokens[0].Literal)
	assert.Equal(t, "B", tokens[1].Literal)
}

func TestLexerUnterminatedBlockComment(t *testing.T) {
	lex := NewLexer([]byte("A /* unterminated"))
	_, err := lex.Next() // gets A
	require.NoError(t, err)
	_, err = lex.Next() // should fail
	require.Error(t, err)
	assert.IsType(t, &LexError{}, err)
}

func TestLexerPosition(t *testing.T) {
	tokens := collectTokens(t, "A\nB C")
	require.Len(t, tokens, 4) // A, B, C, EOF
	assert.Equal(t, 1, tokens[0].Pos.Line)
	assert.Equal(t, 1, tokens[0].Pos.Column)
	assert.Equal(t, 2, tokens[1].Pos.Line)
	assert.Equal(t, 1, tokens[1].Pos.Column)
	assert.Equal(t, 2, tokens[2].Pos.Line)
	assert.Equal(t, 3, tokens[2].Pos.Column)
}

func TestLexerArrowVsNegative(t *testing.T) {
	tokens := collectTokens(t, "A->B")
	require.Len(t, tokens, 4) // A, ->, B, EOF
	assert.Equal(t, TokenIdentifier, tokens[0].Kind)
	assert.Equal(t, TokenArrow, tokens[1].Kind)
	assert.Equal(t, TokenIdentifier, tokens[2].Kind)
}

func TestLexerEmpty(t *testing.T) {
	tokens := collectTokens(t, "")
	require.Len(t, tokens, 1)
	assert.Equal(t, TokenEOF, tokens[0].Kind)
}

func TestLexerInvalidChar(t *testing.T) {
	lex := NewLexer([]byte("@"))
	_, err := lex.Next()
	require.Error(t, err)
	assert.IsType(t, &LexError{}, err)
}

func TestLexerFullStatement(t *testing.T) {
	tokens := collectTokens(t, `start [shape=Mdiamond, label="Start"]`)
	expected := []TokenKind{
		TokenIdentifier, TokenLBracket,
		TokenIdentifier, TokenEquals, TokenIdentifier, TokenComma,
		TokenIdentifier, TokenEquals, TokenString,
		TokenRBracket, TokenEOF,
	}
	require.Len(t, tokens, len(expected))
	for i, tok := range tokens {
		assert.Equal(t, expected[i], tok.Kind, "token %d: %s", i, tok.Literal)
	}
	assert.Equal(t, "start", tokens[0].Literal)
	assert.Equal(t, "shape", tokens[2].Literal)
	assert.Equal(t, "Mdiamond", tokens[4].Literal)
	assert.Equal(t, "label", tokens[6].Literal)
	assert.Equal(t, "Start", tokens[8].Literal)
}

func TestLexerPeek(t *testing.T) {
	lex := NewLexer([]byte("A B"))

	// Peek should not advance
	tok, err := lex.Peek()
	require.NoError(t, err)
	assert.Equal(t, "A", tok.Literal)

	// Peek again returns the same token
	tok2, err := lex.Peek()
	require.NoError(t, err)
	assert.Equal(t, tok, tok2)

	// Next consumes the peeked token
	tok3, err := lex.Next()
	require.NoError(t, err)
	assert.Equal(t, "A", tok3.Literal)

	// Next should now return B
	tok4, err := lex.Next()
	require.NoError(t, err)
	assert.Equal(t, "B", tok4.Literal)
}

func TestLexerNumberFollowedByAlpha(t *testing.T) {
	// "5mm" should lex as integer "5" then identifier "mm"
	tokens := collectTokens(t, "5mm")
	require.Len(t, tokens, 3) // 5, mm, EOF
	assert.Equal(t, TokenInteger, tokens[0].Kind)
	assert.Equal(t, "5", tokens[0].Literal)
	assert.Equal(t, TokenIdentifier, tokens[1].Kind)
	assert.Equal(t, "mm", tokens[1].Literal)
}
