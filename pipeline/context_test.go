package pipeline

import (
	"sync"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestContext_SetAndGet(t *testing.T) {
	ctx := NewContext()
	ctx.Set("key", "value")
	assert.Equal(t, "value", ctx.GetString("key"))
}

func TestContext_GetDefault(t *testing.T) {
	ctx := NewContext()
	assert.Equal(t, "fallback", ctx.Get("missing", "fallback"))
	assert.Equal(t, "", ctx.GetString("missing"))
}

func TestContext_Has(t *testing.T) {
	ctx := NewContext()
	assert.False(t, ctx.Has("key"))
	ctx.Set("key", "value")
	assert.True(t, ctx.Has("key"))
}

func TestContext_Delete(t *testing.T) {
	ctx := NewContext()
	ctx.Set("key", "value")
	ctx.Delete("key")
	assert.False(t, ctx.Has("key"))
}

func TestContext_AppendLog(t *testing.T) {
	ctx := NewContext()
	ctx.AppendLog("entry1")
	ctx.AppendLog("entry2")
	logs := ctx.Logs()
	assert.Equal(t, []string{"entry1", "entry2"}, logs)
}

func TestContext_Snapshot(t *testing.T) {
	ctx := NewContext()
	ctx.Set("a", "1")
	ctx.Set("b", "2")
	snap := ctx.Snapshot()
	assert.Equal(t, map[string]string{"a": "1", "b": "2"}, snap)

	// Snapshot is a copy, modifying it doesn't affect the context
	snap["c"] = "3"
	assert.False(t, ctx.Has("c"))
}

func TestContext_Clone(t *testing.T) {
	ctx := NewContext()
	ctx.Set("key", "original")
	ctx.AppendLog("log1")

	clone := ctx.Clone()
	assert.Equal(t, "original", clone.GetString("key"))
	assert.Equal(t, []string{"log1"}, clone.Logs())

	// Mutating clone doesn't affect original
	clone.Set("key", "modified")
	clone.Set("new", "value")
	assert.Equal(t, "original", ctx.GetString("key"))
	assert.False(t, ctx.Has("new"))
}

func TestContext_ApplyUpdates(t *testing.T) {
	ctx := NewContext()
	ctx.Set("existing", "keep")
	ctx.ApplyUpdates(map[string]string{
		"a": "1",
		"b": "2",
	})
	assert.Equal(t, "keep", ctx.GetString("existing"))
	assert.Equal(t, "1", ctx.GetString("a"))
	assert.Equal(t, "2", ctx.GetString("b"))
}

func TestContext_ApplyUpdatesNil(t *testing.T) {
	ctx := NewContext()
	ctx.ApplyUpdates(nil) // should not panic
}

func TestContext_ConcurrentAccess(t *testing.T) {
	ctx := NewContext()
	var wg sync.WaitGroup

	for i := 0; i < 100; i++ {
		wg.Add(2)
		go func(i int) {
			defer wg.Done()
			ctx.Set("key", "value")
			ctx.AppendLog("log")
		}(i)
		go func(i int) {
			defer wg.Done()
			_ = ctx.GetString("key")
			_ = ctx.Snapshot()
			_ = ctx.Clone()
		}(i)
	}
	wg.Wait()

	// Just verify it didn't panic or deadlock
	require.NotNil(t, ctx.Snapshot())
}
