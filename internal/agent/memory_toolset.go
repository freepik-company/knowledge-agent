package agent

import (
	"context"
	"sync"
)

// contextHolder holds the current request context for permission checks
// Thread-safe for concurrent requests
type contextHolder struct {
	mu  sync.RWMutex
	ctx context.Context
}

// GetContext returns the current context (thread-safe)
func (c *contextHolder) GetContext() context.Context {
	c.mu.RLock()
	defer c.mu.RUnlock()
	return c.ctx
}

// SetContext updates the current context (thread-safe)
func (c *contextHolder) SetContext(ctx context.Context) {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.ctx = ctx
}
