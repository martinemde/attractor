package dotparser

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestParseValueString(t *testing.T) {
	v, err := ParseValue(Token{Kind: TokenString, Literal: "hello"})
	require.NoError(t, err)
	assert.Equal(t, ValueString, v.Kind)
	assert.Equal(t, "hello", v.Str)
}

func TestParseValueInteger(t *testing.T) {
	tests := []struct {
		literal string
		want    int64
	}{
		{"42", 42},
		{"0", 0},
		{"-1", -1},
		{"12345", 12345},
	}
	for _, tt := range tests {
		v, err := ParseValue(Token{Kind: TokenInteger, Literal: tt.literal})
		require.NoError(t, err, "literal: %s", tt.literal)
		assert.Equal(t, ValueInt, v.Kind)
		assert.Equal(t, tt.want, v.Int)
		assert.Equal(t, tt.literal, v.Raw)
	}
}

func TestParseValueFloat(t *testing.T) {
	tests := []struct {
		literal string
		want    float64
	}{
		{"0.5", 0.5},
		{"-3.14", -3.14},
		{"1.0", 1.0},
	}
	for _, tt := range tests {
		v, err := ParseValue(Token{Kind: TokenFloat, Literal: tt.literal})
		require.NoError(t, err, "literal: %s", tt.literal)
		assert.Equal(t, ValueFloat, v.Kind)
		assert.InDelta(t, tt.want, v.Float, 0.001)
	}
}

func TestParseValueBoolTrue(t *testing.T) {
	v, err := ParseValue(Token{Kind: TokenTrue, Literal: "true"})
	require.NoError(t, err)
	assert.Equal(t, ValueBool, v.Kind)
	assert.True(t, v.Bool)
	assert.Equal(t, "true", v.Raw)
}

func TestParseValueBoolFalse(t *testing.T) {
	v, err := ParseValue(Token{Kind: TokenFalse, Literal: "false"})
	require.NoError(t, err)
	assert.Equal(t, ValueBool, v.Kind)
	assert.False(t, v.Bool)
	assert.Equal(t, "false", v.Raw)
}

func TestParseValueIdentifierAsString(t *testing.T) {
	tests := []string{"box", "Mdiamond", "LR", "Msquare"}
	for _, id := range tests {
		v, err := ParseValue(Token{Kind: TokenIdentifier, Literal: id})
		require.NoError(t, err, "identifier: %s", id)
		assert.Equal(t, ValueString, v.Kind)
		assert.Equal(t, id, v.Str)
	}
}

func TestParseValueInvalidToken(t *testing.T) {
	_, err := ParseValue(Token{Kind: TokenArrow, Literal: "->"})
	require.Error(t, err)
	assert.IsType(t, &ValueError{}, err)
}
