// SPDX-License-Identifier: MPL-2.0

package lru

import (
	"container/list"
	"errors"
	"sync"
	"time"
)

// ErrCacheClosed is returned when operations are attempted on a closed cache.
var ErrCacheClosed = errors.New("cache is closed")

// Cache is a simple LRU cache
type Cache[K comparable, V any] struct {
	items       map[K]*list.Element
	evictList   *list.List
	stopCleanup chan struct{}
	onEvict     func(K, V)
	capacity    int
	ttl         time.Duration
	mu          sync.RWMutex
	closed      bool
}

type entry[K comparable, V any] struct {
	key        K
	value      V
	expiration time.Time
}

// Option defines functional options for Cache.
type Option func(*config)

type config struct {
	// onEvict is stored type-erased because Option is non-generic — the
	// typed callback is unwrapped in New once K and V are known and a
	// mismatched type fails loudly there rather than being silently dropped.
	onEvict    any
	capacity   int
	ttl        time.Duration
	gcInterval time.Duration
}

// WithCapacity sets cache capacity.
func WithCapacity(capacity int) Option {
	return func(c *config) {
		c.capacity = capacity
	}
}

// WithTTL sets time-to-live for cache entries.
func WithTTL(ttl time.Duration) Option {
	return func(c *config) {
		c.ttl = ttl
	}
}

// WithGCInterval sets the interval for background garbage collection of
// expired entries.
func WithGCInterval(interval time.Duration) Option {
	return func(c *config) {
		c.gcInterval = interval
	}
}

// WithOnEvict registers a callback fired whenever an entry leaves the cache:
// cap-based eviction, explicit Delete, TTL expiry during Get, or GC
// cleanup. The callback runs synchronously under the cache's write lock, so
// it must be fast and must not call back into the cache. K and V are
// inferred from fn's signature and must match the cache's type parameters;
// a mismatch panics at New time.
func WithOnEvict[K comparable, V any](fn func(K, V)) Option {
	return func(c *config) {
		c.onEvict = fn
	}
}

// New creates a new Cache with the given options
func New[K comparable, V any](opts ...Option) *Cache[K, V] {
	cfg := &config{
		capacity:   1000,        // default capacity
		gcInterval: time.Minute, // default GC interval
	}

	for _, opt := range opts {
		opt(cfg)
	}

	// Ensure capacity is at least 1
	if cfg.capacity < 1 {
		cfg.capacity = 1
	}

	cache := &Cache[K, V]{
		capacity:    cfg.capacity,
		ttl:         cfg.ttl,
		items:       make(map[K]*list.Element),
		evictList:   list.New(),
		stopCleanup: make(chan struct{}),
	}

	if cfg.onEvict != nil {
		fn, ok := cfg.onEvict.(func(K, V))
		if !ok {
			panic("lru: WithOnEvict callback type does not match Cache[K, V]")
		}
		cache.onEvict = fn
	}

	// Serve cleanup goroutine if TTL is enabled
	if cfg.ttl > 0 && cfg.gcInterval > 0 {
		go cache.cleanupLoop(cfg.gcInterval)
	}

	return cache
}

// Internal cleanup loop
func (c *Cache[K, V]) cleanupLoop(interval time.Duration) {
	ticker := time.NewTicker(interval)
	defer ticker.Stop()

	for {
		select {
		case <-ticker.C:
			c.cleanup()
		case <-c.stopCleanup:
			return
		}
	}
}

// Internal cleanup function
func (c *Cache[K, V]) cleanup() {
	if c.ttl <= 0 {
		return
	}

	now := time.Now()
	c.mu.Lock()
	defer c.mu.Unlock()

	for element := c.evictList.Back(); element != nil; {
		next := element.Prev() // Get next before potentially removing element

		e := element.Value.(*entry[K, V])
		if !e.expiration.IsZero() && now.After(e.expiration) {
			c.removeElement(element)
		}

		element = next
	}
}

// Get retrieves a value from the cache.
// Returns zero value and false if the cache is closed.
func (c *Cache[K, V]) Get(key K) (V, bool) {
	c.mu.Lock()
	defer c.mu.Unlock()

	var zero V
	if c.closed {
		return zero, false
	}

	element, exists := c.items[key]
	if !exists {
		return zero, false
	}

	e := element.Value.(*entry[K, V])
	if !e.expiration.IsZero() && time.Now().After(e.expiration) {
		c.removeElement(element)
		return zero, false
	}

	c.evictList.MoveToFront(element)
	return e.value, true
}

// Set adds or updates a value in the cache.
// Returns ErrCacheClosed if the cache is closed.
func (c *Cache[K, V]) Set(key K, value V) error {
	c.mu.Lock()
	defer c.mu.Unlock()

	if c.closed {
		return ErrCacheClosed
	}

	if element, exists := c.items[key]; exists {
		c.evictList.MoveToFront(element)
		e := element.Value.(*entry[K, V])
		e.value = value
		if c.ttl > 0 {
			e.expiration = time.Now().Add(c.ttl)
		}
		return nil
	}

	e := &entry[K, V]{
		key:   key,
		value: value,
	}
	if c.ttl > 0 {
		e.expiration = time.Now().Add(c.ttl)
	}

	element := c.evictList.PushFront(e)
	c.items[key] = element

	if c.evictList.Len() > c.capacity {
		c.evictOldest()
	}

	return nil
}

// Delete removes a key from the cache.
// No-op if the cache is closed.
func (c *Cache[K, V]) Delete(key K) {
	c.mu.Lock()
	defer c.mu.Unlock()

	if c.closed {
		return
	}

	if element, exists := c.items[key]; exists {
		c.removeElement(element)
	}
}

// Len returns the number of items in the cache.
// Returns 0 if the cache is closed.
func (c *Cache[K, V]) Len() int {
	c.mu.RLock()
	defer c.mu.RUnlock()

	if c.closed {
		return 0
	}
	return c.evictList.Len()
}

func (c *Cache[K, V]) removeElement(element *list.Element) {
	e := element.Value.(*entry[K, V])
	delete(c.items, e.key)
	c.evictList.Remove(element)
	if c.onEvict != nil {
		c.onEvict(e.key, e.value)
	}
}

func (c *Cache[K, V]) evictOldest() {
	if element := c.evictList.Back(); element != nil {
		c.removeElement(element)
	}
}

// Close shuts down the cleanup goroutine
func (c *Cache[K, V]) Close() {
	c.mu.Lock()
	defer c.mu.Unlock()

	if !c.closed {
		close(c.stopCleanup)
		c.closed = true
	}
}
