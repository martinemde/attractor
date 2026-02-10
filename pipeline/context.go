package pipeline

import (
	"context"
	"fmt"
	"maps"
	"sync"
)

// Context is a thread-safe key-value store shared across all stages during a pipeline run.
// It is the primary mechanism for passing data between nodes.
type Context struct {
	mu     sync.RWMutex
	values map[string]any
	logs   []string
	goCtx  context.Context // Go context for cancellation propagation
}

// NewContext creates a new empty Context.
func NewContext() *Context {
	return &Context{
		values: make(map[string]any),
		logs:   make([]string, 0),
	}
}

// Set stores a value in the context.
func (c *Context) Set(key string, value any) {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.values[key] = value
}

// Get retrieves a value from the context.
// Returns the value and true if found, or nil and false if not found.
func (c *Context) Get(key string) (any, bool) {
	c.mu.RLock()
	defer c.mu.RUnlock()
	v, ok := c.values[key]
	return v, ok
}

// GetString retrieves a string value from the context.
// Returns the default value if the key is not found or cannot be converted to string.
func (c *Context) GetString(key, defaultValue string) string {
	c.mu.RLock()
	defer c.mu.RUnlock()
	v, ok := c.values[key]
	if !ok {
		return defaultValue
	}
	return toString(v)
}

// AppendLog adds an entry to the run log.
func (c *Context) AppendLog(entry string) {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.logs = append(c.logs, entry)
}

// Logs returns a copy of the log entries.
func (c *Context) Logs() []string {
	c.mu.RLock()
	defer c.mu.RUnlock()
	result := make([]string, len(c.logs))
	copy(result, c.logs)
	return result
}

// SetGoCtx sets the Go context for cancellation propagation.
func (c *Context) SetGoCtx(ctx context.Context) {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.goCtx = ctx
}

// GoCtx returns the Go context for cancellation propagation.
// Returns context.Background() if no Go context has been set.
func (c *Context) GoCtx() context.Context {
	c.mu.RLock()
	defer c.mu.RUnlock()
	if c.goCtx == nil {
		return context.Background()
	}
	return c.goCtx
}

// Snapshot returns a shallow copy of all context values.
// The returned map is safe to read without holding locks.
func (c *Context) Snapshot() map[string]any {
	c.mu.RLock()
	defer c.mu.RUnlock()
	result := make(map[string]any, len(c.values))
	maps.Copy(result, c.values)
	return result
}

// Clone creates a deep copy of the context for parallel branch isolation.
func (c *Context) Clone() *Context {
	c.mu.RLock()
	defer c.mu.RUnlock()

	newCtx := &Context{
		values: make(map[string]any, len(c.values)),
		logs:   make([]string, len(c.logs)),
	}
	maps.Copy(newCtx.values, c.values)
	copy(newCtx.logs, c.logs)
	return newCtx
}

// ApplyUpdates merges a map of updates into the context.
func (c *Context) ApplyUpdates(updates map[string]any) {
	if updates == nil {
		return
	}
	c.mu.Lock()
	defer c.mu.Unlock()
	maps.Copy(c.values, updates)
}

// toString converts any value to its string representation.
func toString(v any) string {
	switch val := v.(type) {
	case string:
		return val
	case bool:
		if val {
			return "true"
		}
		return "false"
	case int:
		return fmt.Sprintf("%d", val)
	case int64:
		return fmt.Sprintf("%d", val)
	case float64:
		return fmt.Sprintf("%g", val)
	case fmt.Stringer:
		return val.String()
	default:
		return fmt.Sprintf("%v", v)
	}
}
