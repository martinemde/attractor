package dotparser

import (
	"fmt"
	"strconv"
)

// ParseValue converts a token into a typed Value.
func ParseValue(tok Token) (Value, error) {
	switch tok.Kind {
	case TokenString:
		return Value{
			Kind: ValueString,
			Str:  tok.Literal,
			Raw:  tok.Literal,
		}, nil

	case TokenInteger:
		n, err := strconv.ParseInt(tok.Literal, 10, 64)
		if err != nil {
			return Value{}, &ValueError{ParseError{
				Message: fmt.Sprintf("invalid integer %q: %v", tok.Literal, err),
				Pos:     tok.Pos,
				Cause:   err,
			}}
		}
		return Value{
			Kind: ValueInt,
			Int:  n,
			Raw:  tok.Literal,
		}, nil

	case TokenFloat:
		f, err := strconv.ParseFloat(tok.Literal, 64)
		if err != nil {
			return Value{}, &ValueError{ParseError{
				Message: fmt.Sprintf("invalid float %q: %v", tok.Literal, err),
				Pos:     tok.Pos,
				Cause:   err,
			}}
		}
		return Value{
			Kind:  ValueFloat,
			Float: f,
			Raw:   tok.Literal,
		}, nil

	case TokenTrue:
		return Value{Kind: ValueBool, Bool: true, Raw: "true"}, nil

	case TokenFalse:
		return Value{Kind: ValueBool, Bool: false, Raw: "false"}, nil

	case TokenIdentifier:
		// Bare identifiers in value position are treated as unquoted strings
		// (e.g. shape=box, shape=Mdiamond, rankdir=LR).
		return Value{
			Kind: ValueString,
			Str:  tok.Literal,
			Raw:  tok.Literal,
		}, nil

	default:
		return Value{}, &ValueError{ParseError{
			Message: fmt.Sprintf("unexpected token %s in value position", tok.Kind),
			Pos:     tok.Pos,
		}}
	}
}
