package pipeline

import (
	"testing"

	"github.com/stretchr/testify/assert"
)

func TestEvaluateConditionEmpty(t *testing.T) {
	// Empty condition always returns true
	assert.True(t, EvaluateCondition("", nil, nil))
	assert.True(t, EvaluateCondition("  ", nil, nil))
	assert.True(t, EvaluateCondition("\t\n", nil, nil))
}

func TestEvaluateConditionOutcome(t *testing.T) {
	tests := []struct {
		name      string
		condition string
		outcome   *Outcome
		expected  bool
	}{
		{
			name:      "outcome equals success",
			condition: "outcome=success",
			outcome:   Success(),
			expected:  true,
		},
		{
			name:      "outcome equals fail",
			condition: "outcome=fail",
			outcome:   Fail("error"),
			expected:  true,
		},
		{
			name:      "outcome equals partial_success",
			condition: "outcome=partial_success",
			outcome:   PartialSuccess("notes"),
			expected:  true,
		},
		{
			name:      "outcome equals retry",
			condition: "outcome=retry",
			outcome:   Retry("reason"),
			expected:  true,
		},
		{
			name:      "outcome equals skipped",
			condition: "outcome=skipped",
			outcome:   Skipped(),
			expected:  true,
		},
		{
			name:      "outcome not equals success when fail",
			condition: "outcome!=success",
			outcome:   Fail("error"),
			expected:  true,
		},
		{
			name:      "outcome not equals success when success",
			condition: "outcome!=success",
			outcome:   Success(),
			expected:  false,
		},
		{
			name:      "outcome mismatch",
			condition: "outcome=success",
			outcome:   Fail("error"),
			expected:  false,
		},
		{
			name:      "nil outcome",
			condition: "outcome=success",
			outcome:   nil,
			expected:  false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := EvaluateCondition(tt.condition, tt.outcome, nil)
			assert.Equal(t, tt.expected, result)
		})
	}
}

func TestEvaluateConditionPreferredLabel(t *testing.T) {
	tests := []struct {
		name      string
		condition string
		outcome   *Outcome
		expected  bool
	}{
		{
			name:      "preferred_label equals",
			condition: "preferred_label=approve",
			outcome:   Success().WithPreferredLabel("approve"),
			expected:  true,
		},
		{
			name:      "preferred_label not equals when different",
			condition: "preferred_label!=reject",
			outcome:   Success().WithPreferredLabel("approve"),
			expected:  true,
		},
		{
			name:      "preferred_label not equals when same",
			condition: "preferred_label!=approve",
			outcome:   Success().WithPreferredLabel("approve"),
			expected:  false,
		},
		{
			name:      "preferred_label mismatch",
			condition: "preferred_label=approve",
			outcome:   Success().WithPreferredLabel("reject"),
			expected:  false,
		},
		{
			name:      "preferred_label empty outcome",
			condition: "preferred_label=approve",
			outcome:   Success(),
			expected:  false,
		},
		{
			name:      "nil outcome",
			condition: "preferred_label=approve",
			outcome:   nil,
			expected:  false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := EvaluateCondition(tt.condition, tt.outcome, nil)
			assert.Equal(t, tt.expected, result)
		})
	}
}

func TestEvaluateConditionContext(t *testing.T) {
	ctx := NewContext()
	ctx.Set("tests_passed", "true")
	ctx.Set("count", 5)
	ctx.Set("status", "ready")
	ctx.Set("context.nested", "value")

	tests := []struct {
		name      string
		condition string
		ctx       *Context
		expected  bool
	}{
		{
			name:      "context.* key equals",
			condition: "context.tests_passed=true",
			ctx:       ctx,
			expected:  true,
		},
		{
			name:      "context.* key not equals when different",
			condition: "context.tests_passed!=false",
			ctx:       ctx,
			expected:  true,
		},
		{
			name:      "context.* key not equals when same",
			condition: "context.tests_passed!=true",
			ctx:       ctx,
			expected:  false,
		},
		{
			name:      "context.* numeric comparison",
			condition: "context.count=5",
			ctx:       ctx,
			expected:  true,
		},
		{
			name:      "context.* missing key equals empty",
			condition: "context.missing=",
			ctx:       ctx,
			expected:  true,
		},
		{
			name:      "context.* missing key not equals value",
			condition: "context.missing=something",
			ctx:       ctx,
			expected:  false,
		},
		{
			name:      "bare key lookup",
			condition: "status=ready",
			ctx:       ctx,
			expected:  true,
		},
		{
			name:      "bare key mismatch",
			condition: "status=not_ready",
			ctx:       ctx,
			expected:  false,
		},
		{
			name:      "nested context key",
			condition: "context.context.nested=value",
			ctx:       ctx,
			expected:  true,
		},
		{
			name:      "nil context",
			condition: "context.key=value",
			ctx:       nil,
			expected:  false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := EvaluateCondition(tt.condition, nil, tt.ctx)
			assert.Equal(t, tt.expected, result)
		})
	}
}

func TestEvaluateConditionAnd(t *testing.T) {
	ctx := NewContext()
	ctx.Set("tests_passed", "true")
	ctx.Set("coverage", "80")

	tests := []struct {
		name      string
		condition string
		outcome   *Outcome
		ctx       *Context
		expected  bool
	}{
		{
			name:      "two clauses both true",
			condition: "outcome=success && context.tests_passed=true",
			outcome:   Success(),
			ctx:       ctx,
			expected:  true,
		},
		{
			name:      "two clauses first false",
			condition: "outcome=fail && context.tests_passed=true",
			outcome:   Success(),
			ctx:       ctx,
			expected:  false,
		},
		{
			name:      "two clauses second false",
			condition: "outcome=success && context.tests_passed=false",
			outcome:   Success(),
			ctx:       ctx,
			expected:  false,
		},
		{
			name:      "three clauses all true",
			condition: "outcome=success && context.tests_passed=true && context.coverage=80",
			outcome:   Success(),
			ctx:       ctx,
			expected:  true,
		},
		{
			name:      "three clauses one false",
			condition: "outcome=success && context.tests_passed=true && context.coverage=90",
			outcome:   Success(),
			ctx:       ctx,
			expected:  false,
		},
		{
			name:      "empty clause in middle",
			condition: "outcome=success &&  && context.tests_passed=true",
			outcome:   Success(),
			ctx:       ctx,
			expected:  true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := EvaluateCondition(tt.condition, tt.outcome, tt.ctx)
			assert.Equal(t, tt.expected, result)
		})
	}
}

func TestEvaluateConditionWhitespace(t *testing.T) {
	ctx := NewContext()
	ctx.Set("key", "value")

	tests := []struct {
		name      string
		condition string
		expected  bool
	}{
		{
			name:      "spaces around operator",
			condition: "key = value",
			expected:  true,
		},
		{
			name:      "spaces around not equals",
			condition: "key != other",
			expected:  true,
		},
		{
			name:      "tabs and spaces",
			condition: "  key\t=\tvalue  ",
			expected:  true,
		},
		{
			name:      "spaces around &&",
			condition: "key=value  &&  outcome=success",
			expected:  true,
		},
		{
			name:      "leading and trailing whitespace",
			condition: "   key=value   ",
			expected:  true,
		},
	}

	outcome := Success()

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := EvaluateCondition(tt.condition, outcome, ctx)
			assert.Equal(t, tt.expected, result)
		})
	}
}

func TestEvaluateConditionQuotedValues(t *testing.T) {
	ctx := NewContext()
	ctx.Set("message", "hello world")
	ctx.Set("empty", "")

	tests := []struct {
		name      string
		condition string
		expected  bool
	}{
		{
			name:      "double quoted value",
			condition: `message="hello world"`,
			expected:  true,
		},
		{
			name:      "single quoted value",
			condition: `message='hello world'`,
			expected:  true,
		},
		{
			name:      "empty double quoted value",
			condition: `empty=""`,
			expected:  true,
		},
		{
			name:      "empty single quoted value",
			condition: `empty=''`,
			expected:  true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := EvaluateCondition(tt.condition, nil, ctx)
			assert.Equal(t, tt.expected, result)
		})
	}
}

func TestEvaluateConditionBareKey(t *testing.T) {
	ctx := NewContext()
	ctx.Set("truthy_string", "yes")
	ctx.Set("empty_string", "")
	ctx.Set("false_string", "false")
	ctx.Set("zero_string", "0")
	ctx.Set("true_string", "true")

	tests := []struct {
		name      string
		condition string
		expected  bool
	}{
		{
			name:      "truthy string",
			condition: "truthy_string",
			expected:  true,
		},
		{
			name:      "empty string is falsy",
			condition: "empty_string",
			expected:  false,
		},
		{
			name:      "false string is falsy",
			condition: "false_string",
			expected:  false,
		},
		{
			name:      "zero string is falsy",
			condition: "zero_string",
			expected:  false,
		},
		{
			name:      "true string is truthy",
			condition: "true_string",
			expected:  true,
		},
		{
			name:      "missing key is falsy",
			condition: "missing_key",
			expected:  false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := EvaluateCondition(tt.condition, nil, ctx)
			assert.Equal(t, tt.expected, result)
		})
	}
}

func TestResolveKey(t *testing.T) {
	ctx := NewContext()
	ctx.Set("foo", "bar")
	ctx.Set("context.explicit", "explicit_value")

	outcome := Success().WithPreferredLabel("next")

	tests := []struct {
		name     string
		key      string
		outcome  *Outcome
		ctx      *Context
		expected string
	}{
		{
			name:     "outcome key",
			key:      "outcome",
			outcome:  outcome,
			ctx:      ctx,
			expected: "success",
		},
		{
			name:     "preferred_label key",
			key:      "preferred_label",
			outcome:  outcome,
			ctx:      ctx,
			expected: "next",
		},
		{
			name:     "context prefix with existing key",
			key:      "context.foo",
			outcome:  outcome,
			ctx:      ctx,
			expected: "bar",
		},
		{
			name:     "context prefix with explicit context key",
			key:      "context.context.explicit",
			outcome:  outcome,
			ctx:      ctx,
			expected: "explicit_value",
		},
		{
			name:     "bare key lookup",
			key:      "foo",
			outcome:  outcome,
			ctx:      ctx,
			expected: "bar",
		},
		{
			name:     "missing key",
			key:      "missing",
			outcome:  outcome,
			ctx:      ctx,
			expected: "",
		},
		{
			name:     "nil outcome for outcome key",
			key:      "outcome",
			outcome:  nil,
			ctx:      ctx,
			expected: "",
		},
		{
			name:     "nil context for context key",
			key:      "context.foo",
			outcome:  outcome,
			ctx:      nil,
			expected: "",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := resolveKey(tt.key, tt.outcome, tt.ctx)
			assert.Equal(t, tt.expected, result)
		})
	}
}

func TestIsTruthy(t *testing.T) {
	tests := []struct {
		input    string
		expected bool
	}{
		{"", false},
		{"  ", false},
		{"false", false},
		{"FALSE", false},
		{"False", false},
		{"0", false},
		{"true", true},
		{"TRUE", true},
		{"True", true},
		{"1", true},
		{"yes", true},
		{"anything", true},
		{" hello ", true},
	}

	for _, tt := range tests {
		t.Run(tt.input, func(t *testing.T) {
			assert.Equal(t, tt.expected, isTruthy(tt.input))
		})
	}
}

func TestUnquote(t *testing.T) {
	tests := []struct {
		input    string
		expected string
	}{
		{`"hello"`, "hello"},
		{`'hello'`, "hello"},
		{`hello`, "hello"},
		{`""`, ""},
		{`''`, ""},
		{`"`, `"`},
		{`'`, `'`},
		{`"unmatched'`, `"unmatched'`},
		{`'unmatched"`, `'unmatched"`},
	}

	for _, tt := range tests {
		t.Run(tt.input, func(t *testing.T) {
			assert.Equal(t, tt.expected, unquote(tt.input))
		})
	}
}
