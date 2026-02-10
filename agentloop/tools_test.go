package agentloop

import (
	"encoding/json"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestToolRegistryRegisterAndGet(t *testing.T) {
	reg := NewToolRegistry()
	tool := RegisteredTool{
		Definition: ToolDefinition{
			Name:        "test_tool",
			Description: "A test tool",
			Parameters:  map[string]interface{}{"type": "object"},
		},
		Executor: func(args json.RawMessage, env ExecutionEnvironment) (string, error) {
			return "executed", nil
		},
	}

	reg.Register(tool)
	assert.Equal(t, 1, reg.Count())

	got := reg.Get("test_tool")
	require.NotNil(t, got)
	assert.Equal(t, "test_tool", got.Definition.Name)

	// Unknown tool returns nil.
	assert.Nil(t, reg.Get("nonexistent"))
}

func TestToolRegistryUnregister(t *testing.T) {
	reg := NewToolRegistry()
	reg.Register(RegisteredTool{
		Definition: ToolDefinition{Name: "tool_a"},
	})
	reg.Register(RegisteredTool{
		Definition: ToolDefinition{Name: "tool_b"},
	})
	assert.Equal(t, 2, reg.Count())

	reg.Unregister("tool_a")
	assert.Equal(t, 1, reg.Count())
	assert.Nil(t, reg.Get("tool_a"))
	assert.NotNil(t, reg.Get("tool_b"))
}

func TestToolRegistryDefinitions(t *testing.T) {
	reg := NewToolRegistry()
	reg.Register(RegisteredTool{
		Definition: ToolDefinition{Name: "tool_x", Description: "Tool X"},
	})
	reg.Register(RegisteredTool{
		Definition: ToolDefinition{Name: "tool_y", Description: "Tool Y"},
	})

	defs := reg.Definitions()
	assert.Len(t, defs, 2)
	names := make(map[string]bool)
	for _, d := range defs {
		names[d.Name] = true
	}
	assert.True(t, names["tool_x"])
	assert.True(t, names["tool_y"])
}

func TestToolRegistryNames(t *testing.T) {
	reg := NewToolRegistry()
	reg.Register(RegisteredTool{Definition: ToolDefinition{Name: "alpha"}})
	reg.Register(RegisteredTool{Definition: ToolDefinition{Name: "beta"}})

	names := reg.Names()
	assert.Len(t, names, 2)
	assert.Contains(t, names, "alpha")
	assert.Contains(t, names, "beta")
}

// TestToolRegistryOverwrite verifies that registering a tool with the same
// name replaces the existing one (spec: "latest-wins").
func TestToolRegistryOverwrite(t *testing.T) {
	reg := NewToolRegistry()
	reg.Register(RegisteredTool{
		Definition: ToolDefinition{Name: "tool", Description: "Original"},
	})
	reg.Register(RegisteredTool{
		Definition: ToolDefinition{Name: "tool", Description: "Replacement"},
	})

	assert.Equal(t, 1, reg.Count())
	got := reg.Get("tool")
	require.NotNil(t, got)
	assert.Equal(t, "Replacement", got.Definition.Description)
}

func TestToolRegistryClone(t *testing.T) {
	reg := NewToolRegistry()
	reg.Register(RegisteredTool{
		Definition: ToolDefinition{Name: "original_tool"},
	})

	clone := reg.Clone()
	assert.Equal(t, 1, clone.Count())
	assert.NotNil(t, clone.Get("original_tool"))

	// Modifications to clone don't affect original.
	clone.Register(RegisteredTool{
		Definition: ToolDefinition{Name: "new_tool"},
	})
	assert.Equal(t, 2, clone.Count())
	assert.Equal(t, 1, reg.Count())
}

// TestToolRegistryMergeFrom verifies latest-wins merge behavior.
func TestToolRegistryMergeFrom(t *testing.T) {
	reg1 := NewToolRegistry()
	reg1.Register(RegisteredTool{
		Definition: ToolDefinition{Name: "shared", Description: "From reg1"},
	})
	reg1.Register(RegisteredTool{
		Definition: ToolDefinition{Name: "unique1"},
	})

	reg2 := NewToolRegistry()
	reg2.Register(RegisteredTool{
		Definition: ToolDefinition{Name: "shared", Description: "From reg2"},
	})
	reg2.Register(RegisteredTool{
		Definition: ToolDefinition{Name: "unique2"},
	})

	reg1.MergeFrom(reg2)
	assert.Equal(t, 3, reg1.Count())
	// Shared tool should be from reg2 (latest-wins).
	assert.Equal(t, "From reg2", reg1.Get("shared").Definition.Description)
	assert.NotNil(t, reg1.Get("unique1"))
	assert.NotNil(t, reg1.Get("unique2"))
}

func TestParseToolArguments(t *testing.T) {
	raw := json.RawMessage(`{"name":"test","count":42,"flag":true}`)
	args, err := ParseToolArguments(raw)
	require.NoError(t, err)
	assert.Equal(t, "test", args["name"])
	assert.Equal(t, float64(42), args["count"])
	assert.Equal(t, true, args["flag"])
}

func TestParseToolArgumentsInvalid(t *testing.T) {
	_, err := ParseToolArguments(json.RawMessage(`not json`))
	assert.Error(t, err)
}

func TestGetStringArg(t *testing.T) {
	args := map[string]interface{}{"key": "value", "num": 42}

	s, ok := GetStringArg(args, "key")
	assert.True(t, ok)
	assert.Equal(t, "value", s)

	_, ok = GetStringArg(args, "num")
	assert.False(t, ok)

	_, ok = GetStringArg(args, "missing")
	assert.False(t, ok)
}

func TestGetIntArg(t *testing.T) {
	args := map[string]interface{}{"count": float64(42), "str": "not int"}

	n, ok := GetIntArg(args, "count")
	assert.True(t, ok)
	assert.Equal(t, 42, n)

	_, ok = GetIntArg(args, "str")
	assert.False(t, ok)

	_, ok = GetIntArg(args, "missing")
	assert.False(t, ok)
}

func TestGetBoolArg(t *testing.T) {
	args := map[string]interface{}{"flag": true, "str": "not bool"}

	b, ok := GetBoolArg(args, "flag")
	assert.True(t, ok)
	assert.True(t, b)

	_, ok = GetBoolArg(args, "str")
	assert.False(t, ok)
}
