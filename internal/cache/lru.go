package lru

import (
	"container/list"
	"sync"
	"time"
)

type Cache[K comparable, V any] struct {
	mu        sync.RWMutex
	capacity  int
	items     map[K]*list.Element
	evictList *list.List
	ttl       time.Duration
}

type entry[K comparable, V any] struct {
	key        K
	value      V
	expiration time.Time
}

// Option defines functional options for Cache
type Option func(*config)

type config struct {
	capacity int
	ttl      time.Duration
}

// WithCapacity sets cache capacity
func WithCapacity(capacity int) Option {
	return func(c *config) {
		c.capacity = capacity
	}
}

// WithTTL sets time-to-live for cache entries
func WithTTL(ttl time.Duration) Option {
	return func(c *config) {
		c.ttl = ttl
	}
}

// New creates a new Cache with the given options
func New[K comparable, V any](opts ...Option) *Cache[K, V] {
	cfg := &config{
		capacity: 1000, // default capacity
	}

	for _, opt := range opts {
		opt(cfg)
	}

	return &Cache[K, V]{
		capacity:  cfg.capacity,
		ttl:       cfg.ttl,
		items:     make(map[K]*list.Element),
		evictList: list.New(),
	}
}

// Get retrieves a value from the cache
func (c *Cache[K, V]) Get(key K) (V, bool) {
	c.mu.Lock()
	defer c.mu.Unlock()

	var zero V
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

// Set adds or updates a value in the cache
func (c *Cache[K, V]) Set(key K, value V) {
	c.mu.Lock()
	defer c.mu.Unlock()

	if element, exists := c.items[key]; exists {
		c.evictList.MoveToFront(element)
		e := element.Value.(*entry[K, V])
		e.value = value
		if c.ttl > 0 {
			e.expiration = time.Now().Add(c.ttl)
		}
		return
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
}

// Delete removes a key from the cache
func (c *Cache[K, V]) Delete(key K) {
	c.mu.Lock()
	defer c.mu.Unlock()

	if element, exists := c.items[key]; exists {
		c.removeElement(element)
	}
}

// Len returns the number of items in the cache
func (c *Cache[K, V]) Len() int {
	c.mu.RLock()
	defer c.mu.RUnlock()
	return c.evictList.Len()
}

func (c *Cache[K, V]) removeElement(element *list.Element) {
	e := element.Value.(*entry[K, V])
	delete(c.items, e.key)
	c.evictList.Remove(element)
}

func (c *Cache[K, V]) evictOldest() {
	if element := c.evictList.Back(); element != nil {
		c.removeElement(element)
	}
}
