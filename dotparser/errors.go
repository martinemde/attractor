package dotparser

import "fmt"

// ParseError is the base error type for all dotparser errors.
type ParseError struct {
	Message string
	Pos     Position
	Cause   error
}

func (e *ParseError) Error() string {
	if e.Pos.Line > 0 {
		return fmt.Sprintf("line %d, col %d: %s", e.Pos.Line, e.Pos.Column, e.Message)
	}
	return e.Message
}

func (e *ParseError) Unwrap() error { return e.Cause }

// LexError represents a lexer-level error (unterminated string, invalid character).
type LexError struct{ ParseError }

// SyntaxError represents a grammar-level error (unexpected token).
type SyntaxError struct {
	ParseError
	Expected string
	Got      string
}

func (e *SyntaxError) Error() string {
	if e.Pos.Line > 0 {
		return fmt.Sprintf("line %d, col %d: expected %s, got %s", e.Pos.Line, e.Pos.Column, e.Expected, e.Got)
	}
	return fmt.Sprintf("expected %s, got %s", e.Expected, e.Got)
}

// ValueError represents a value conversion error (bad duration unit, overflow).
type ValueError struct{ ParseError }
