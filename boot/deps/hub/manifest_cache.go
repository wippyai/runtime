// SPDX-License-Identifier: MPL-2.0

package hub

import (
	"context"
	"time"

	lru "github.com/wippyai/runtime/internal/cache"
)

// DefaultManifestCacheCapacity bounds the number of distinct module
// manifests held in memory. Sized to absorb dozens of plugin churn
// cycles without growing without bound; the LRU evicts the
// least-recently-used entry once the limit is reached.
const DefaultManifestCacheCapacity = 256

// DefaultManifestCacheTTL bounds the wall-clock age of a cached
// manifest. The hub publishes a (org, name, version) triple exactly
// once, but a long-running runtime can outlive a replace event; the
// TTL lets the cache re-validate against the hub even without an
// explicit Refresh and pairs with the LRU cap as a second bound.
const DefaultManifestCacheTTL = time.Hour

// DefaultManifestCacheGCInterval is how often the background sweep
// drops expired entries. Short enough that an expired entry can't
// linger long after its TTL; long enough that the goroutine is
// effectively idle on a quiet runtime.
const DefaultManifestCacheGCInterval = 5 * time.Minute

// ManifestCache wraps a ManifestProvider with a capacity-bounded LRU.
// Module manifests are immutable for a given (org, name, version) triple
// — the hub signs and persists each version exactly once — so a cache
// hit can be returned without revisiting the network. Capacity is
// enforced by internal/cache (LRU); the oldest entry is dropped once
// the limit is reached, so a long-running runtime that churns through
// installs cannot grow this cache without bound.
//
// Version-list queries (ListAllVersions) are intentionally not cached:
// the set of published versions grows over time and the caller already
// short-circuits via lockedVersions when a pin satisfies the constraint.
type ManifestCache struct {
	inner ManifestProvider
	store *lru.Cache[string, *ModuleManifest]
}

// NewManifestCache wraps the given provider with the default LRU:
// DefaultManifestCacheCapacity entries, DefaultManifestCacheTTL age
// bound, and DefaultManifestCacheGCInterval background sweep.
func NewManifestCache(provider ManifestProvider) *ManifestCache {
	return NewManifestCacheWithOptions(
		provider,
		DefaultManifestCacheCapacity,
		DefaultManifestCacheTTL,
		DefaultManifestCacheGCInterval,
	)
}

// NewManifestCacheWithOptions wraps the given provider with an LRU of
// the requested capacity / ttl / gc interval. A non-positive capacity
// falls back to 1. A non-positive ttl disables TTL expiry (the cap
// stays in effect). A non-positive gc interval disables the
// background sweep — expired entries are still dropped on Get.
func NewManifestCacheWithOptions(provider ManifestProvider, capacity int, ttl, gcInterval time.Duration) *ManifestCache {
	opts := []lru.Option{lru.WithCapacity(capacity)}
	if ttl > 0 {
		opts = append(opts, lru.WithTTL(ttl))
	}
	if gcInterval > 0 {
		opts = append(opts, lru.WithGCInterval(gcInterval))
	}
	return &ManifestCache{
		inner: provider,
		store: lru.New[string, *ModuleManifest](opts...),
	}
}

// GetManifest serves from the LRU when the (org, name, constraint)
// triple is cached; otherwise it falls through to the wrapped provider
// and stores the result. Both the constraint key and the resolved
// version key are populated so subsequent calls with either form hit.
func (c *ManifestCache) GetManifest(ctx context.Context, org, module, constraint string) (*ModuleManifest, error) {
	key := manifestCacheKey(org, module, constraint)
	if cached, hit := c.store.Get(key); hit {
		return cached, nil
	}

	manifest, err := c.inner.GetManifest(ctx, org, module, constraint)
	if err != nil {
		return nil, err
	}
	if manifest == nil {
		return nil, nil
	}

	_ = c.store.Set(key, manifest)
	if resolved := manifestCacheKey(org, module, manifest.Version); resolved != key {
		_ = c.store.Set(resolved, manifest)
	}

	return manifest, nil
}

// ListAllVersions defers to the wrapped provider without caching — the
// set of published versions changes whenever a new release lands on
// the hub.
func (c *ManifestCache) ListAllVersions(ctx context.Context, org, module string) ([]VersionInfo, error) {
	return c.inner.ListAllVersions(ctx, org, module)
}

// Refresh bypasses the cache, fetches the manifest from the wrapped
// provider, and updates the cache entry. Use after a digest mismatch
// is observed against the lockfile so the next GetManifest sees the
// hub's current view of (org, name, constraint).
func (c *ManifestCache) Refresh(ctx context.Context, org, module, constraint string) (*ModuleManifest, error) {
	manifest, err := c.inner.GetManifest(ctx, org, module, constraint)
	if err != nil {
		return nil, err
	}
	if manifest == nil {
		key := manifestCacheKey(org, module, constraint)
		c.store.Delete(key)
		return nil, nil
	}

	key := manifestCacheKey(org, module, constraint)
	_ = c.store.Set(key, manifest)
	if resolved := manifestCacheKey(org, module, manifest.Version); resolved != key {
		_ = c.store.Set(resolved, manifest)
	}
	return manifest, nil
}

// Len reports the number of cached manifests. Exposed for tests and
// for operator visibility.
func (c *ManifestCache) Len() int {
	return c.store.Len()
}

// Close releases the cache's background resources. Safe to call once
// at process shutdown.
func (c *ManifestCache) Close() {
	c.store.Close()
}

func manifestCacheKey(org, name, version string) string {
	return org + "/" + name + "@" + version
}
