package dotparser

import (
	"fmt"
	"strings"
)

// Lexer tokenizes DOT source text into a stream of tokens.
type Lexer struct {
	src    []byte
	pos    int // current byte offset
	line   int // current line (1-based)
	col    int // current column (1-based)
	peeked *Token
}

// NewLexer creates a new Lexer for the given source bytes.
func NewLexer(src []byte) *Lexer {
	return &Lexer{src: src, line: 1, col: 1}
}

// Peek returns the next token without consuming it.
func (l *Lexer) Peek() (Token, error) {
	if l.peeked != nil {
		return *l.peeked, nil
	}
	tok, err := l.scan()
	if err != nil {
		return Token{}, err
	}
	l.peeked = &tok
	return tok, nil
}

// Next returns the next token and advances the lexer.
func (l *Lexer) Next() (Token, error) {
	if l.peeked != nil {
		tok := *l.peeked
		l.peeked = nil
		return tok, nil
	}
	return l.scan()
}

func (l *Lexer) currentPos() Position {
	return Position{Line: l.line, Column: l.col, Offset: l.pos}
}

func (l *Lexer) atEnd() bool {
	return l.pos >= len(l.src)
}

func (l *Lexer) peek() byte {
	if l.atEnd() {
		return 0
	}
	return l.src[l.pos]
}

func (l *Lexer) advance() byte {
	ch := l.src[l.pos]
	l.pos++
	if ch == '\n' {
		l.line++
		l.col = 1
	} else {
		l.col++
	}
	return ch
}

func (l *Lexer) skipWhitespaceAndComments() error {
	for !l.atEnd() {
		ch := l.peek()
		switch {
		case ch == ' ' || ch == '\t' || ch == '\r' || ch == '\n':
			l.advance()
		case ch == '/' && l.pos+1 < len(l.src) && l.src[l.pos+1] == '/':
			// Line comment: skip to end of line
			for !l.atEnd() && l.peek() != '\n' {
				l.advance()
			}
		case ch == '/' && l.pos+1 < len(l.src) && l.src[l.pos+1] == '*':
			// Block comment: skip to */
			startPos := l.currentPos()
			l.advance() // consume /
			l.advance() // consume *
			for {
				if l.atEnd() {
					return &LexError{ParseError{
						Message: "unterminated block comment",
						Pos:     startPos,
					}}
				}
				if l.peek() == '*' && l.pos+1 < len(l.src) && l.src[l.pos+1] == '/' {
					l.advance() // consume *
					l.advance() // consume /
					break
				}
				l.advance()
			}
		default:
			return nil
		}
	}
	return nil
}

func (l *Lexer) scan() (Token, error) {
	if err := l.skipWhitespaceAndComments(); err != nil {
		return Token{}, err
	}

	if l.atEnd() {
		return Token{Kind: TokenEOF, Pos: l.currentPos()}, nil
	}

	pos := l.currentPos()
	ch := l.peek()

	// Single-character tokens
	switch ch {
	case '{':
		l.advance()
		return Token{Kind: TokenLBrace, Literal: "{", Pos: pos}, nil
	case '}':
		l.advance()
		return Token{Kind: TokenRBrace, Literal: "}", Pos: pos}, nil
	case '[':
		l.advance()
		return Token{Kind: TokenLBracket, Literal: "[", Pos: pos}, nil
	case ']':
		l.advance()
		return Token{Kind: TokenRBracket, Literal: "]", Pos: pos}, nil
	case '=':
		l.advance()
		return Token{Kind: TokenEquals, Literal: "=", Pos: pos}, nil
	case ',':
		l.advance()
		return Token{Kind: TokenComma, Literal: ",", Pos: pos}, nil
	case ';':
		l.advance()
		return Token{Kind: TokenSemicolon, Literal: ";", Pos: pos}, nil
	case '.':
		l.advance()
		return Token{Kind: TokenDot, Literal: ".", Pos: pos}, nil
	case '"':
		return l.scanString()
	case '-':
		if l.pos+1 < len(l.src) && l.src[l.pos+1] == '>' {
			l.advance()
			l.advance()
			return Token{Kind: TokenArrow, Literal: "->", Pos: pos}, nil
		}
		if l.pos+1 < len(l.src) && isDigit(l.src[l.pos+1]) {
			return l.scanNumber()
		}
		l.advance()
		return Token{}, &LexError{ParseError{
			Message: fmt.Sprintf("unexpected character '-'"),
			Pos:     pos,
		}}
	}

	if isDigit(ch) {
		return l.scanNumber()
	}

	if isIdentStart(ch) {
		return l.scanIdentifier()
	}

	l.advance()
	return Token{}, &LexError{ParseError{
		Message: fmt.Sprintf("unexpected character %q", ch),
		Pos:     pos,
	}}
}

func (l *Lexer) scanString() (Token, error) {
	pos := l.currentPos()
	l.advance() // consume opening "

	var sb strings.Builder
	for {
		if l.atEnd() {
			return Token{}, &LexError{ParseError{
				Message: "unterminated string",
				Pos:     pos,
			}}
		}
		ch := l.advance()
		if ch == '"' {
			return Token{Kind: TokenString, Literal: sb.String(), Pos: pos}, nil
		}
		if ch == '\\' {
			if l.atEnd() {
				return Token{}, &LexError{ParseError{
					Message: "unterminated string escape",
					Pos:     pos,
				}}
			}
			esc := l.advance()
			switch esc {
			case '"':
				sb.WriteByte('"')
			case '\\':
				sb.WriteByte('\\')
			case 'n':
				sb.WriteByte('\n')
			case 't':
				sb.WriteByte('\t')
			default:
				// Preserve unknown escapes as-is
				sb.WriteByte('\\')
				sb.WriteByte(esc)
			}
			continue
		}
		sb.WriteByte(ch)
	}
}

func (l *Lexer) scanNumber() (Token, error) {
	pos := l.currentPos()
	start := l.pos

	// Optional negative sign
	if !l.atEnd() && l.peek() == '-' {
		l.advance()
	}

	// Consume digits
	for !l.atEnd() && isDigit(l.peek()) {
		l.advance()
	}

	isFloat := false
	// Check for decimal point
	if !l.atEnd() && l.peek() == '.' && l.pos+1 < len(l.src) && isDigit(l.src[l.pos+1]) {
		isFloat = true
		l.advance() // consume '.'
		for !l.atEnd() && isDigit(l.peek()) {
			l.advance()
		}
	}

	literal := string(l.src[start:l.pos])

	if isFloat {
		return Token{Kind: TokenFloat, Literal: literal, Pos: pos}, nil
	}

	return Token{Kind: TokenInteger, Literal: literal, Pos: pos}, nil
}

func (l *Lexer) scanIdentifier() (Token, error) {
	pos := l.currentPos()
	start := l.pos

	for !l.atEnd() && isIdentPart(l.peek()) {
		l.advance()
	}

	literal := string(l.src[start:l.pos])

	if kind, ok := keywords[literal]; ok {
		return Token{Kind: kind, Literal: literal, Pos: pos}, nil
	}

	return Token{Kind: TokenIdentifier, Literal: literal, Pos: pos}, nil
}

func isDigit(ch byte) bool {
	return ch >= '0' && ch <= '9'
}

func isIdentStart(ch byte) bool {
	return (ch >= 'a' && ch <= 'z') || (ch >= 'A' && ch <= 'Z') || ch == '_'
}

func isIdentPart(ch byte) bool {
	return isIdentStart(ch) || isDigit(ch)
}
