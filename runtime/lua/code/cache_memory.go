// SPDX-License-Identifier: MPL-2.0

package code

import (
	"sync"

	"github.com/wippyai/runtime/api/registry"
	lru "github.com/wippyai/runtime/internal/cache"
)

type compileBytesKey struct {
	id          registry.ID
	fingerprint string
}

// compileBytesCache owns immutable dumped bytes, not mutable FunctionProtos.
// go-lua exposes SetTypeInfo and lazily builds runtime type bindings. The manager
// also attaches manifests after loading, so separate Undumps preserve isolation.
// The shared LRU owns recency and entry limits; mu serializes byte accounting.
type compileBytesCache struct {
	entries  *lru.Cache[compileBytesKey, []byte]
	mu       sync.Mutex
	bytes    int
	maxBytes int
}

func newCompileBytesCache(maxBytes, maxEntries int) *compileBytesCache {
	if maxBytes <= 0 || maxEntries <= 0 {
		return nil
	}
	c := &compileBytesCache{maxBytes: maxBytes}
	c.entries = lru.New[compileBytesKey, []byte](
		lru.WithCapacity(maxEntries),
		lru.WithOnEvict(func(_ compileBytesKey, data []byte) { c.bytes -= len(data) }),
	)
	return c
}

func (c *compileBytesCache) get(key compileBytesKey) ([]byte, bool) {
	if c == nil {
		return nil, false
	}
	return c.entries.Get(key)
}

func (c *compileBytesCache) put(key compileBytesKey, data []byte) {
	if c == nil || len(data) == 0 || len(data) > c.maxBytes {
		return
	}
	c.mu.Lock()
	defer c.mu.Unlock()
	if _, exists := c.entries.Get(key); exists {
		return
	}
	// Own the bytes independently of a caller-supplied cache.Store.
	owned := append([]byte(nil), data...)
	c.bytes += len(owned)
	_ = c.entries.Set(key, owned)
	for c.bytes > c.maxBytes {
		c.entries.EvictOldest()
	}
}
