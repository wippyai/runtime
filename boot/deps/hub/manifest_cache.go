// SPDX-License-Identifier: MPL-2.0

package hub

import (
	"context"
	"sync"
)

// ManifestCache wraps a ManifestProvider with an in-process cache. Module
// manifests are immutable for a given (org, name, version) triple — the hub
// signs and persists each version exactly once — so a cache hit on that key
// can safely return without revisiting the network.
//
// Version-list queries (ListAllVersions) are intentionally NOT cached: the
// set of published versions grows over time and the caller already short-
// circuits via lockedVersions when a pin satisfies the constraint.
type ManifestCache struct {
	inner ManifestProvider
	store map[string]*ModuleManifest
	mu    sync.RWMutex
}

// NewManifestCache wraps the given provider with an in-process manifest cache.
func NewManifestCache(provider ManifestProvider) *ManifestCache {
	return &ManifestCache{
		inner: provider,
		store: make(map[string]*ModuleManifest),
	}
}

// GetManifest serves from the in-process cache when the (org, name, constraint)
// triple was seen before and the cached entry matches the resolved version
// exactly. Anything else falls through to the wrapped provider.
func (c *ManifestCache) GetManifest(ctx context.Context, org, module, constraint string) (*ModuleManifest, error) {
	key := manifestCacheKey(org, module, constraint)

	c.mu.RLock()
	cached, hit := c.store[key]
	c.mu.RUnlock()
	if hit {
		return cached, nil
	}

	manifest, err := c.inner.GetManifest(ctx, org, module, constraint)
	if err != nil {
		return nil, err
	}
	if manifest == nil {
		return nil, nil
	}

	c.mu.Lock()
	c.store[key] = manifest
	if resolved := manifestCacheKey(org, module, manifest.Version); resolved != key {
		c.store[resolved] = manifest
	}
	c.mu.Unlock()

	return manifest, nil
}

// ListAllVersions defers to the wrapped provider without caching — the set of
// published versions changes whenever a new release lands on the hub.
func (c *ManifestCache) ListAllVersions(ctx context.Context, org, module string) ([]VersionInfo, error) {
	return c.inner.ListAllVersions(ctx, org, module)
}

// Evict drops every cached manifest for a (org, name) pair. Use after a
// digest mismatch is observed so the next GetManifest goes back to the
// underlying provider instead of returning the same stale entry.
func (c *ManifestCache) Evict(org, name string) {
	prefix := org + "/" + name + "@"

	c.mu.Lock()
	defer c.mu.Unlock()
	for key := range c.store {
		if len(key) >= len(prefix) && key[:len(prefix)] == prefix {
			delete(c.store, key)
		}
	}
}

func manifestCacheKey(org, name, version string) string {
	return org + "/" + name + "@" + version
}
