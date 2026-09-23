// SPDX-License-Identifier: MPL-2.0

// Package admission serializes local name decisions across registration scopes.
package admission

import (
	"context"
	"sync"
)

type entry struct {
	mu   sync.Mutex
	refs int
}

// Coordinator keeps admission decisions for each name independent.
type Coordinator struct {
	names map[string]*entry
	mu    sync.Mutex
}

// Acquire locks one name. Waiting callers retain its entry, so eviction
// cannot split the lock. The returned release function must run exactly once.
func (c *Coordinator) Acquire(name string) func() {
	if c == nil {
		return func() {}
	}
	c.mu.Lock()
	if c.names == nil {
		c.names = make(map[string]*entry)
	}
	e := c.names[name]
	if e == nil {
		e = &entry{}
		c.names[name] = e
	}
	e.refs++
	c.mu.Unlock()
	e.mu.Lock()
	return func() {
		e.mu.Unlock()
		c.mu.Lock()
		e.refs--
		if e.refs == 0 {
			delete(c.names, name)
		}
		c.mu.Unlock()
	}
}

type contextKey struct{}

func WithContext(ctx context.Context, c *Coordinator) context.Context {
	return context.WithValue(ctx, contextKey{}, c)
}

func FromContext(ctx context.Context) *Coordinator {
	c, _ := ctx.Value(contextKey{}).(*Coordinator)
	return c
}
