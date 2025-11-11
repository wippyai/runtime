package context

import (
	"context"
	"sync"
)

// CallContext stores execution-level key-value pairs.
// All keys are write-once: each key can only be set once.
// New keys can be added anytime, but existing keys cannot be overwritten.
// Scope controls inheritance when creating child contexts:
// - ScopeCall keys are NOT inherited (PID, cancel, callbacks)
// - ScopeThread keys ARE inherited (security, values, options)
type CallContext interface {
	// Get retrieves a value by key. Returns (value, exists).
	Get(key *Key) (any, bool)

	// Set stores a value by key. Returns error if key already set (write-once).
	Set(key *Key, value any) error

	// Has checks if a key exists in this context.
	Has(key *Key) bool

	// Iterate calls fn for each key-value pair in this context.
	Iterate(fn func(key *Key, value any))

	// Parent returns the parent CallContext, or nil if none.
	// For metadata/tracing only - values are NOT inherited via Get().
	Parent() CallContext
}

// callContext is the concrete implementation of CallContext.
type callContext struct {
	mu     sync.RWMutex
	values map[*Key]any
	parent CallContext
	cancel context.CancelFunc
}

// NewCallContext creates a new CallContext with optional parent.
// Only ScopeThread keys are inherited from parent (selective copy).
// Auto-creates a cancel function.
func NewCallContext(parent context.Context) (context.Context, CallContext) {
	ctx, cancel := context.WithCancel(parent)
	cc := &callContext{
		values: make(map[*Key]any),
		cancel: cancel,
	}

	// Copy only ScopeThread keys from parent CallContext
	if parentCC := CallFromContext(parent); parentCC != nil {
		cc.parent = parentCC
		parentCC.Iterate(func(key *Key, value any) {
			if key.Scope == ScopeThread {
				cc.values[key] = value
			}
		})
	}

	return WithCallContext(ctx, cc), cc
}

func (c *callContext) Get(key *Key) (any, bool) {
	c.mu.RLock()
	defer c.mu.RUnlock()
	val, exists := c.values[key]
	return val, exists
}

func (c *callContext) Set(key *Key, value any) error {
	c.mu.Lock()
	defer c.mu.Unlock()

	// All keys are write-once
	if _, exists := c.values[key]; exists {
		return &KeyError{Key: key, Message: "key already set"}
	}

	c.values[key] = value
	return nil
}

func (c *callContext) Has(key *Key) bool {
	c.mu.RLock()
	defer c.mu.RUnlock()
	_, exists := c.values[key]
	return exists
}

func (c *callContext) Iterate(fn func(key *Key, value any)) {
	c.mu.RLock()
	defer c.mu.RUnlock()
	for k, v := range c.values {
		fn(k, v)
	}
}

func (c *callContext) Parent() CallContext {
	c.mu.RLock()
	defer c.mu.RUnlock()
	return c.parent
}

// KeyError is returned when trying to overwrite a ScopeCall key
type KeyError struct {
	Key     *Key
	Message string
}

func (e *KeyError) Error() string {
	return e.Message + ": " + e.Key.Name
}

// callContextKey is the context key for storing CallContext.
var callContextKey = &Key{Name: "context.call", Scope: ScopeThread}

// WithCallContext attaches CallContext to the provided context.
func WithCallContext(ctx context.Context, cc CallContext) context.Context {
	return context.WithValue(ctx, callContextKey, cc)
}

// CallFromContext extracts CallContext from context.
// Returns nil if not present.
func CallFromContext(ctx context.Context) CallContext {
	if cc, ok := ctx.Value(callContextKey).(CallContext); ok {
		return cc
	}
	return nil
}
