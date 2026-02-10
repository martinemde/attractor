package pipeline

import (
	"sync"
)

// Context is a thread-safe key-value store shared across all stages during
// a pipeline run. It is the primary mechanism for passing data between nodes.
type Context struct {
	mu     sync.RWMutex
	values map[string]string
	logs   []string
}

// NewContext creates an empty context.
func NewContext() *Context {
	return &Context{
		values: make(map[string]string),
	}
}

// Set stores a key-value pair in the context.
func (c *Context) Set(key, value string) {
	c.mu.Lock()
	c.values[key] = value
	c.mu.Unlock()
}

// Get returns the value for a key, or the default if not found.
func (c *Context) Get(key, defaultValue string) string {
	c.mu.RLock()
	v, ok := c.values[key]
	c.mu.RUnlock()
	if !ok {
		return defaultValue
	}
	return v
}

// GetString returns the value for a key, or "" if not found.
func (c *Context) GetString(key string) string {
	return c.Get(key, "")
}

// Has returns true if the key exists in the context.
func (c *Context) Has(key string) bool {
	c.mu.RLock()
	_, ok := c.values[key]
	c.mu.RUnlock()
	return ok
}

// Delete removes a key from the context.
func (c *Context) Delete(key string) {
	c.mu.Lock()
	delete(c.values, key)
	c.mu.Unlock()
}

// AppendLog adds an entry to the append-only run log.
func (c *Context) AppendLog(entry string) {
	c.mu.Lock()
	c.logs = append(c.logs, entry)
	c.mu.Unlock()
}

// Logs returns a copy of the run log entries.
func (c *Context) Logs() []string {
	c.mu.RLock()
	result := make([]string, len(c.logs))
	copy(result, c.logs)
	c.mu.RUnlock()
	return result
}

// Snapshot returns a shallow copy of all key-value pairs.
func (c *Context) Snapshot() map[string]string {
	c.mu.RLock()
	result := make(map[string]string, len(c.values))
	for k, v := range c.values {
		result[k] = v
	}
	c.mu.RUnlock()
	return result
}

// Clone returns a deep copy of this context for parallel branch isolation.
func (c *Context) Clone() *Context {
	c.mu.RLock()
	newCtx := &Context{
		values: make(map[string]string, len(c.values)),
		logs:   make([]string, len(c.logs)),
	}
	for k, v := range c.values {
		newCtx.values[k] = v
	}
	copy(newCtx.logs, c.logs)
	c.mu.RUnlock()
	return newCtx
}

// ApplyUpdates merges a map of key-value pairs into the context.
func (c *Context) ApplyUpdates(updates map[string]string) {
	if len(updates) == 0 {
		return
	}
	c.mu.Lock()
	for k, v := range updates {
		c.values[k] = v
	}
	c.mu.Unlock()
}
