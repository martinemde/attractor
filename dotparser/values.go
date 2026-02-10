package dotparser

import (
	"fmt"
	"strconv"
	"strings"
	"time"
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

	case TokenDuration:
		d, err := parseDuration(tok.Literal)
		if err != nil {
			return Value{}, &ValueError{ParseError{
				Message: fmt.Sprintf("invalid duration %q: %v", tok.Literal, err),
				Pos:     tok.Pos,
				Cause:   err,
			}}
		}
		return Value{
			Kind:     ValueDuration,
			Duration: d,
			Raw:      tok.Literal,
		}, nil

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

// parseDuration parses a duration string like "900s", "250ms", "15m", "2h", "1d".
func parseDuration(s string) (time.Duration, error) {
	// Find where the numeric part ends and the suffix begins
	i := 0
	if i < len(s) && s[i] == '-' {
		i++
	}
	for i < len(s) && s[i] >= '0' && s[i] <= '9' {
		i++
	}

	numStr := s[:i]
	suffix := strings.ToLower(s[i:])

	n, err := strconv.ParseInt(numStr, 10, 64)
	if err != nil {
		return 0, fmt.Errorf("invalid numeric part %q: %w", numStr, err)
	}

	switch suffix {
	case "ms":
		return time.Duration(n) * time.Millisecond, nil
	case "s":
		return time.Duration(n) * time.Second, nil
	case "m":
		return time.Duration(n) * time.Minute, nil
	case "h":
		return time.Duration(n) * time.Hour, nil
	case "d":
		return time.Duration(n) * 24 * time.Hour, nil
	default:
		return 0, fmt.Errorf("unknown duration suffix %q", suffix)
	}
}
