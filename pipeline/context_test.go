package pipeline

import (
	"sync"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestNewContext(t *testing.T) {
	ctx := NewContext()
	require.NotNil(t, ctx)
	assert.NotNil(t, ctx.values)
	assert.NotNil(t, ctx.logs)
	assert.Empty(t, ctx.values)
	assert.Empty(t, ctx.logs)
}

func TestContextSetAndGet(t *testing.T) {
	ctx := NewContext()

	// Set and get string
	ctx.Set("key1", "value1")
	v, ok := ctx.Get("key1")
	assert.True(t, ok)
	assert.Equal(t, "value1", v)

	// Set and get int
	ctx.Set("key2", 42)
	v, ok = ctx.Get("key2")
	assert.True(t, ok)
	assert.Equal(t, 42, v)

	// Get missing key
	v, ok = ctx.Get("missing")
	assert.False(t, ok)
	assert.Nil(t, v)

	// Overwrite existing key
	ctx.Set("key1", "new_value")
	v, ok = ctx.Get("key1")
	assert.True(t, ok)
	assert.Equal(t, "new_value", v)
}

func TestContextGetString(t *testing.T) {
	ctx := NewContext()

	// Missing key returns default
	assert.Equal(t, "default", ctx.GetString("missing", "default"))

	// String value
	ctx.Set("str", "hello")
	assert.Equal(t, "hello", ctx.GetString("str", "default"))

	// Int value converted to string
	ctx.Set("int", 42)
	assert.Equal(t, "42", ctx.GetString("int", "default"))

	// Bool value converted to string
	ctx.Set("bool_true", true)
	ctx.Set("bool_false", false)
	assert.Equal(t, "true", ctx.GetString("bool_true", "default"))
	assert.Equal(t, "false", ctx.GetString("bool_false", "default"))

	// Float value converted to string
	ctx.Set("float", 3.14)
	assert.Equal(t, "3.14", ctx.GetString("float", "default"))
}

func TestContextAppendLog(t *testing.T) {
	ctx := NewContext()

	ctx.AppendLog("entry 1")
	ctx.AppendLog("entry 2")
	ctx.AppendLog("entry 3")

	logs := ctx.Logs()
	assert.Equal(t, []string{"entry 1", "entry 2", "entry 3"}, logs)
}

func TestContextLogsReturnsACopy(t *testing.T) {
	ctx := NewContext()
	ctx.AppendLog("entry 1")

	logs1 := ctx.Logs()
	logs1[0] = "modified"

	logs2 := ctx.Logs()
	assert.Equal(t, "entry 1", logs2[0], "modifying returned slice should not affect context")
}

func TestContextSnapshot(t *testing.T) {
	ctx := NewContext()
	ctx.Set("key1", "value1")
	ctx.Set("key2", 42)

	snap := ctx.Snapshot()
	assert.Equal(t, "value1", snap["key1"])
	assert.Equal(t, 42, snap["key2"])
	assert.Len(t, snap, 2)

	// Modifying snapshot should not affect context
	snap["key1"] = "modified"
	v, _ := ctx.Get("key1")
	assert.Equal(t, "value1", v)
}

func TestContextClone(t *testing.T) {
	ctx := NewContext()
	ctx.Set("key1", "value1")
	ctx.Set("key2", 42)
	ctx.AppendLog("log entry")

	clone := ctx.Clone()

	// Clone has same values
	v, ok := clone.Get("key1")
	assert.True(t, ok)
	assert.Equal(t, "value1", v)

	v, ok = clone.Get("key2")
	assert.True(t, ok)
	assert.Equal(t, 42, v)

	// Clone has same logs
	logs := clone.Logs()
	assert.Equal(t, []string{"log entry"}, logs)

	// Modifying clone should not affect original
	clone.Set("key1", "modified")
	clone.Set("key3", "new")
	clone.AppendLog("new log")

	origV, _ := ctx.Get("key1")
	assert.Equal(t, "value1", origV)

	_, ok = ctx.Get("key3")
	assert.False(t, ok)

	origLogs := ctx.Logs()
	assert.Equal(t, []string{"log entry"}, origLogs)

	// Modifying original should not affect clone
	ctx.Set("key2", 100)
	cloneV, _ := clone.Get("key2")
	assert.Equal(t, 42, cloneV)
}

func TestContextApplyUpdates(t *testing.T) {
	ctx := NewContext()
	ctx.Set("existing", "original")

	updates := map[string]any{
		"existing": "updated",
		"new_key":  "new_value",
	}
	ctx.ApplyUpdates(updates)

	v, _ := ctx.Get("existing")
	assert.Equal(t, "updated", v)

	v, _ = ctx.Get("new_key")
	assert.Equal(t, "new_value", v)
}

func TestContextApplyUpdatesNil(t *testing.T) {
	ctx := NewContext()
	ctx.Set("key", "value")

	// Should not panic with nil updates
	ctx.ApplyUpdates(nil)

	v, ok := ctx.Get("key")
	assert.True(t, ok)
	assert.Equal(t, "value", v)
}

func TestContextThreadSafety(t *testing.T) {
	ctx := NewContext()
	var wg sync.WaitGroup
	numGoroutines := 100
	numOps := 100

	// Concurrent writes
	for i := 0; i < numGoroutines; i++ {
		wg.Add(1)
		go func(id int) {
			defer wg.Done()
			for j := 0; j < numOps; j++ {
				ctx.Set("key", id*numOps+j)
			}
		}(i)
	}

	// Concurrent reads
	for i := 0; i < numGoroutines; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			for j := 0; j < numOps; j++ {
				ctx.Get("key")
			}
		}()
	}

	// Concurrent GetString
	for i := 0; i < numGoroutines; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			for j := 0; j < numOps; j++ {
				ctx.GetString("key", "default")
			}
		}()
	}

	// Concurrent Snapshot
	for i := 0; i < numGoroutines; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			for j := 0; j < numOps; j++ {
				ctx.Snapshot()
			}
		}()
	}

	// Concurrent AppendLog
	for i := 0; i < numGoroutines; i++ {
		wg.Add(1)
		go func(id int) {
			defer wg.Done()
			for j := 0; j < numOps; j++ {
				ctx.AppendLog("log entry")
			}
		}(i)
	}

	// Concurrent Clone
	for i := 0; i < numGoroutines; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			for j := 0; j < numOps; j++ {
				ctx.Clone()
			}
		}()
	}

	wg.Wait()

	// Verify context is still in a consistent state
	_, ok := ctx.Get("key")
	assert.True(t, ok)

	logs := ctx.Logs()
	assert.Equal(t, numGoroutines*numOps, len(logs))
}

func TestContextThreadSafetyMixedOperations(t *testing.T) {
	ctx := NewContext()
	var wg sync.WaitGroup

	// Multiple keys with concurrent access
	keys := []string{"a", "b", "c", "d", "e"}
	numGoroutines := 50
	numOps := 50

	for _, key := range keys {
		for i := 0; i < numGoroutines; i++ {
			wg.Add(1)
			go func(k string, id int) {
				defer wg.Done()
				for j := 0; j < numOps; j++ {
					ctx.Set(k, id*numOps+j)
					ctx.Get(k)
					ctx.GetString(k, "")
				}
			}(key, i)
		}
	}

	wg.Wait()

	// All keys should exist
	for _, key := range keys {
		_, ok := ctx.Get(key)
		assert.True(t, ok, "key %s should exist", key)
	}
}
