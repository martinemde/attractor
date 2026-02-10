package pipeline

import (
	"strings"
)

// EvaluateCondition evaluates a condition expression against an outcome and context.
// The condition grammar is: Clause ( '&&' Clause )*, where Clause = Key Operator Literal.
// Supported operators are '=' (equals) and '!=' (not equals).
// Empty conditions return true.
func EvaluateCondition(condition string, outcome *Outcome, ctx *Context) bool {
	condition = strings.TrimSpace(condition)
	if condition == "" {
		return true
	}

	for clause := range strings.SplitSeq(condition, "&&") {
		clause = strings.TrimSpace(clause)
		if clause == "" {
			continue
		}
		if !evaluateClause(clause, outcome, ctx) {
			return false
		}
	}
	return true
}

// evaluateClause evaluates a single clause against an outcome and context.
func evaluateClause(clause string, outcome *Outcome, ctx *Context) bool {
	// Check for != operator first (before = to avoid matching the = in !=)
	if key, value, ok := strings.Cut(clause, "!="); ok {
		return resolveKey(strings.TrimSpace(key), outcome, ctx) != unquote(strings.TrimSpace(value))
	}

	// Check for = operator
	if key, value, ok := strings.Cut(clause, "="); ok {
		return resolveKey(strings.TrimSpace(key), outcome, ctx) == unquote(strings.TrimSpace(value))
	}

	// Bare key: check if truthy (non-empty string)
	key := strings.TrimSpace(clause)
	return isTruthy(resolveKey(key, outcome, ctx))
}

// resolveKey resolves a key to its string value from outcome or context.
// Keys can be:
//   - "outcome" -> outcome status
//   - "preferred_label" -> outcome preferred label
//   - "context.*" -> context value (with or without context. prefix)
//   - bare key -> direct context lookup
func resolveKey(key string, outcome *Outcome, ctx *Context) string {
	switch key {
	case "outcome":
		if outcome == nil {
			return ""
		}
		return outcome.Status.String()
	case "preferred_label":
		if outcome == nil {
			return ""
		}
		return outcome.PreferredLabel
	}

	// Handle context.* prefix
	if path, ok := strings.CutPrefix(key, "context."); ok {
		if ctx != nil {
			// Try with full key first (context.foo)
			if v, ok := ctx.Get(key); ok {
				return toString(v)
			}
			// Try without prefix (foo)
			if v, ok := ctx.Get(path); ok {
				return toString(v)
			}
		}
		return ""
	}

	// Direct context lookup for unqualified keys
	if ctx != nil {
		if v, ok := ctx.Get(key); ok {
			return toString(v)
		}
	}
	return ""
}

// unquote removes surrounding quotes from a string value if present.
func unquote(s string) string {
	if len(s) >= 2 {
		if (s[0] == '"' && s[len(s)-1] == '"') || (s[0] == '\'' && s[len(s)-1] == '\'') {
			return s[1 : len(s)-1]
		}
	}
	return s
}

// isTruthy returns true if the string represents a truthy value.
// Empty strings and "false" are falsy, everything else is truthy.
func isTruthy(s string) bool {
	s = strings.ToLower(strings.TrimSpace(s))
	return s != "" && s != "false" && s != "0"
}
