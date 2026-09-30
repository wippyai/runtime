// SPDX-License-Identifier: MPL-2.0

package code

import (
	"container/list"
	"sync"

	"github.com/wippyai/runtime/api/registry"
)

const (
	// The disk retention policy may be much larger than the resident byte budget.
	defaultCompileMemoryBytes   = 32 << 20
	defaultCompileMemoryEntries = 2048
)

type compileBytesKey struct {
	id          registry.ID
	fingerprint string
}

type compileBytesEntry struct {
	key  compileBytesKey
	data []byte
}

// compileBytesCache owns immutable dumped bytes, not mutable FunctionProtos.
// go-lua 1.6.0 exposes SetTypeInfo and lazily builds runtime type bindings. The
// manager also attaches manifests after loading; separate Undumps preserve that
// isolation while eliminating repeated disk reads and artifact validation.
type compileBytesCache struct {
	mu         sync.Mutex
	entries    map[compileBytesKey]*list.Element
	lru        list.List
	bytes      int
	maxBytes   int
	maxEntries int
}

func newCompileBytesCache(maxBytes, maxEntries int) *compileBytesCache {
	return &compileBytesCache{entries: make(map[compileBytesKey]*list.Element), maxBytes: maxBytes, maxEntries: maxEntries}
}

func (c *compileBytesCache) get(key compileBytesKey) ([]byte, bool) {
	if c == nil {
		return nil, false
	}
	c.mu.Lock()
	defer c.mu.Unlock()
	element := c.entries[key]
	if element == nil {
		return nil, false
	}
	c.lru.MoveToFront(element)
	return element.Value.(compileBytesEntry).data, true
}

func (c *compileBytesCache) put(key compileBytesKey, data []byte) {
	if c == nil || len(data) == 0 || len(data) > c.maxBytes || c.maxEntries <= 0 {
		return
	}
	c.mu.Lock()
	defer c.mu.Unlock()
	if element := c.entries[key]; element != nil {
		c.lru.MoveToFront(element)
		return
	}
	// Own the bytes independently of a caller-supplied cache.Store.
	owned := append([]byte(nil), data...)
	c.entries[key] = c.lru.PushFront(compileBytesEntry{key: key, data: owned})
	c.bytes += len(owned)
	for c.bytes > c.maxBytes || len(c.entries) > c.maxEntries {
		element := c.lru.Back()
		entry := element.Value.(compileBytesEntry)
		delete(c.entries, entry.key)
		c.bytes -= len(entry.data)
		c.lru.Remove(element)
	}
}
