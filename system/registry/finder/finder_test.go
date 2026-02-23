// SPDX-License-Identifier: MPL-2.0

package finder

import (
	"context"
	"errors"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"github.com/wippyai/runtime/api/attrs"
	"github.com/wippyai/runtime/api/registry"
)

// mockRegistry implements a simple in-memory registry for testing
type mockRegistry struct {
	currentFunc func() (registry.Version, error)
	entries     []registry.Entry
}

func (m *mockRegistry) GetAllEntries() ([]registry.Entry, error) {
	return m.entries, nil
}

func (m *mockRegistry) GetEntry(id registry.ID) (registry.Entry, error) {
	for _, entry := range m.entries {
		if entry.ID == id {
			return entry, nil
		}
	}
	return registry.Entry{}, errors.New("not found")
}

func (m *mockRegistry) Current() (registry.Version, error) {
	if m.currentFunc != nil {
		return m.currentFunc()
	}
	return nil, nil //nolint:nilnil // mock stub
}

func (m *mockRegistry) History() registry.History { return nil }
func (m *mockRegistry) Apply(_ context.Context, _ registry.ChangeSet) (registry.Version, error) {
	return nil, nil //nolint:nilnil // mock stub
}
func (m *mockRegistry) ApplyVersion(_ context.Context, _ registry.Version) error { return nil }

// TestFinder_RootFieldMatching tests matching on root fields (.kind, .name, .ns, .id)
func TestFinder_RootFieldMatching(t *testing.T) {
	entries := []registry.Entry{
		{
			ID:   registry.NewID("app", "service-api"),
			Kind: "service",
			Meta: attrs.Bag{"meta.enabled": true},
		},
		{
			ID:   registry.NewID("app", "service-queue"),
			Kind: "service",
			Meta: attrs.Bag{"meta.enabled": true},
		},
		{
			ID:   registry.NewID("storage", "database-users"),
			Kind: "database",
			Meta: attrs.Bag{"meta.enabled": true},
		},
		{
			ID:   registry.NewID("funcs", "handler"),
			Kind: "function.lua",
			Meta: attrs.Bag{"meta.enabled": true},
		},
		{
			ID:   registry.NewID("funcs", "compiled"),
			Kind: "function.bytecode",
			Meta: attrs.Bag{"meta.enabled": true},
		},
	}

	mockReg := &mockRegistry{entries: entries}
	finder := NewFinder(mockReg, nil)

	tests := []struct {
		name     string
		criteria attrs.Bag
		wantIDs  []string
	}{
		{
			name:     "Find by .kind",
			criteria: attrs.Bag{".kind": "service"},
			wantIDs:  []string{"service-api", "service-queue"},
		},
		{
			name:     "Find by .name",
			criteria: attrs.Bag{".name": "service-api"},
			wantIDs:  []string{"service-api"},
		},
		{
			name:     "Find by .ns",
			criteria: attrs.Bag{".ns": "app"},
			wantIDs:  []string{"service-api", "service-queue"},
		},
		{
			name:     "Find by .id (full ID)",
			criteria: attrs.Bag{".id": "app:service-api"},
			wantIDs:  []string{"service-api"},
		},
		{
			name: "Combine .kind and .ns",
			criteria: attrs.Bag{
				".kind": "service",
				".ns":   "app",
			},
			wantIDs: []string{"service-api", "service-queue"},
		},
		{
			name:     "Find by .kind with prefix glob",
			criteria: attrs.Bag{".kind": "function.*"},
			wantIDs:  []string{"handler", "compiled"},
		},
		{
			name:     "Find by .kind with suffix glob",
			criteria: attrs.Bag{".kind": "*.lua"},
			wantIDs:  []string{"handler"},
		},
		{
			name:     "Find by .name with prefix glob",
			criteria: attrs.Bag{".name": "service-*"},
			wantIDs:  []string{"service-api", "service-queue"},
		},
		{
			name:     "Find by .ns with glob",
			criteria: attrs.Bag{".ns": "app*"},
			wantIDs:  []string{"service-api", "service-queue"},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			results, err := finder.Find(tt.criteria)
			require.NoError(t, err)

			var resultIDs []string
			for _, entry := range results {
				resultIDs = append(resultIDs, entry.ID.Name)
			}

			assert.ElementsMatch(t, tt.wantIDs, resultIDs)
		})
	}
}

// TestFinder_MetadataEquality tests exact equality matching on metadata fields
func TestFinder_MetadataEquality(t *testing.T) {
	entries := []registry.Entry{
		{
			ID:   registry.NewID("", "entry-1"),
			Kind: "test",
			Meta: attrs.Bag{
				"enabled": true,
				"port":    8080,
				"env":     "production",
			},
		},
		{
			ID:   registry.NewID("", "entry-2"),
			Kind: "test",
			Meta: attrs.Bag{
				"enabled": false,
				"port":    9000,
				"env":     "staging",
			},
		},
		{
			ID:   registry.NewID("", "entry-3"),
			Kind: "test",
			Meta: attrs.Bag{
				"enabled": true,
				"port":    8080,
				"env":     "staging",
			},
		},
	}

	mockReg := &mockRegistry{entries: entries}
	finder := NewFinder(mockReg, nil)

	tests := []struct {
		name     string
		criteria attrs.Bag
		wantIDs  []string
	}{
		{
			name:     "Boolean equality",
			criteria: attrs.Bag{"meta.enabled": true},
			wantIDs:  []string{"entry-1", "entry-3"},
		},
		{
			name:     "Integer equality",
			criteria: attrs.Bag{"meta.port": 8080},
			wantIDs:  []string{"entry-1", "entry-3"},
		},
		{
			name:     "String equality",
			criteria: attrs.Bag{"meta.env": "production"},
			wantIDs:  []string{"entry-1"},
		},
		{
			name: "Multiple field AND logic",
			criteria: attrs.Bag{
				"meta.enabled": true,
				"meta.env":     "staging",
			},
			wantIDs: []string{"entry-3"},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			results, err := finder.Find(tt.criteria)
			require.NoError(t, err)

			var resultIDs []string
			for _, entry := range results {
				resultIDs = append(resultIDs, entry.ID.Name)
			}

			assert.ElementsMatch(t, tt.wantIDs, resultIDs)
		})
	}
}

// TestFinder_ArrayMatching tests array field matching (AND logic)
func TestFinder_ArrayMatching(t *testing.T) {
	entries := []registry.Entry{
		{
			ID:   registry.NewID("", "service-api"),
			Kind: "service",
			Meta: attrs.Bag{
				"tags": []string{"api", "rest", "users"},
			},
		},
		{
			ID:   registry.NewID("", "service-queue"),
			Kind: "service",
			Meta: attrs.Bag{
				"tags": []string{"queue", "background", "jobs"},
			},
		},
		{
			ID:   registry.NewID("", "service-mixed"),
			Kind: "service",
			Meta: attrs.Bag{
				"tags": []string{"api", "queue"},
			},
		},
	}

	mockReg := &mockRegistry{entries: entries}
	finder := NewFinder(mockReg, nil)

	tests := []struct {
		name     string
		criteria attrs.Bag
		wantIDs  []string
	}{
		{
			name:     "Single tag match (ALL must be present)",
			criteria: attrs.Bag{"meta.tags": []string{"api"}},
			wantIDs:  []string{"service-api", "service-mixed"},
		},
		{
			name:     "Multiple tags match (ALL must be present)",
			criteria: attrs.Bag{"meta.tags": []string{"api", "rest"}},
			wantIDs:  []string{"service-api"},
		},
		{
			name:     "No match when not all tags present",
			criteria: attrs.Bag{"meta.tags": []string{"api", "jobs"}},
			wantIDs:  []string{},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			results, err := finder.Find(tt.criteria)
			require.NoError(t, err)

			var resultIDs []string
			for _, entry := range results {
				resultIDs = append(resultIDs, entry.ID.Name)
			}

			assert.ElementsMatch(t, tt.wantIDs, resultIDs)
		})
	}
}

// TestFinder_RegexMatching tests regex pattern matching
func TestFinder_RegexMatching(t *testing.T) {
	entries := []registry.Entry{
		{
			ID:   registry.NewID("", "service-1"),
			Kind: "service",
			Meta: attrs.Bag{
				"description": "RESTful API service for user management",
				"version":     "v1.2.3",
			},
		},
		{
			ID:   registry.NewID("", "service-2"),
			Kind: "service",
			Meta: attrs.Bag{
				"description": "Background job processing queue",
				"version":     "v2.0.1",
			},
		},
		{
			ID:   registry.NewID("", "database-1"),
			Kind: "database",
			Meta: attrs.Bag{
				"description": "User database with profiles",
				"version":     "14.5",
			},
		},
	}

	mockReg := &mockRegistry{entries: entries}
	finder := NewFinder(mockReg, nil)

	tests := []struct {
		name     string
		criteria attrs.Bag
		wantIDs  []string
	}{
		{
			name:     "Regex on description",
			criteria: attrs.Bag{"~meta.description": ".*database.*"},
			wantIDs:  []string{"database-1"},
		},
		{
			name:     "Regex version pattern v1.x.x",
			criteria: attrs.Bag{"~meta.version": `^v1\.`},
			wantIDs:  []string{"service-1"},
		},
		{
			name:     "Regex version pattern v2.x.x",
			criteria: attrs.Bag{"~meta.version": `^v2\.`},
			wantIDs:  []string{"service-2"},
		},
		{
			name: "Combine regex with root field",
			criteria: attrs.Bag{
				".kind":             "service",
				"~meta.description": ".*API.*",
			},
			wantIDs: []string{"service-1"},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			results, err := finder.Find(tt.criteria)
			require.NoError(t, err)

			var resultIDs []string
			for _, entry := range results {
				resultIDs = append(resultIDs, entry.ID.Name)
			}

			assert.ElementsMatch(t, tt.wantIDs, resultIDs)
		})
	}
}

// TestFinder_ContainsMatching tests substring matching
func TestFinder_ContainsMatching(t *testing.T) {
	entries := []registry.Entry{
		{
			ID:   registry.NewID("", "entry-1"),
			Kind: "test",
			Meta: attrs.Bag{
				"description": "This is an API service",
				"tags":        []string{"production", "api", "backend"},
			},
		},
		{
			ID:   registry.NewID("", "entry-2"),
			Kind: "test",
			Meta: attrs.Bag{
				"description": "Frontend application",
				"tags":        []string{"production", "frontend"},
			},
		},
	}

	mockReg := &mockRegistry{entries: entries}
	finder := NewFinder(mockReg, nil)

	tests := []struct {
		name     string
		criteria attrs.Bag
		wantIDs  []string
	}{
		{
			name:     "Contains in string field",
			criteria: attrs.Bag{"*meta.description": "API"},
			wantIDs:  []string{"entry-1"},
		},
		{
			name:     "Contains in array field",
			criteria: attrs.Bag{"*meta.tags": "backend"},
			wantIDs:  []string{"entry-1"},
		},
		{
			name:     "Partial match in array",
			criteria: attrs.Bag{"*meta.tags": "prod"},
			wantIDs:  []string{"entry-1", "entry-2"},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			results, err := finder.Find(tt.criteria)
			require.NoError(t, err)

			var resultIDs []string
			for _, entry := range results {
				resultIDs = append(resultIDs, entry.ID.Name)
			}

			assert.ElementsMatch(t, tt.wantIDs, resultIDs)
		})
	}
}

// TestFinder_PrefixSuffixMatching tests prefix and suffix matching
func TestFinder_PrefixSuffixMatching(t *testing.T) {
	entries := []registry.Entry{
		{
			ID:   registry.NewID("", "entry-1"),
			Kind: "test",
			Meta: attrs.Bag{
				"version":  "v1.2.3",
				"filename": "service.json",
			},
		},
		{
			ID:   registry.NewID("", "entry-2"),
			Kind: "test",
			Meta: attrs.Bag{
				"version":  "v2.0.1",
				"filename": "config.yaml",
			},
		},
		{
			ID:   registry.NewID("", "entry-3"),
			Kind: "test",
			Meta: attrs.Bag{
				"version":  "1.0.0",
				"filename": "data.json",
			},
		},
	}

	mockReg := &mockRegistry{entries: entries}
	finder := NewFinder(mockReg, nil)

	tests := []struct {
		name     string
		criteria attrs.Bag
		wantIDs  []string
	}{
		{
			name:     "Prefix match on version",
			criteria: attrs.Bag{"^meta.version": "v1"},
			wantIDs:  []string{"entry-1"},
		},
		{
			name:     "Suffix match on filename",
			criteria: attrs.Bag{"$meta.filename": ".json"},
			wantIDs:  []string{"entry-1", "entry-3"},
		},
		{
			name: "Combine prefix and suffix",
			criteria: attrs.Bag{
				"^meta.version":  "v",
				"$meta.filename": ".json",
			},
			wantIDs: []string{"entry-1"},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			results, err := finder.Find(tt.criteria)
			require.NoError(t, err)

			var resultIDs []string
			for _, entry := range results {
				resultIDs = append(resultIDs, entry.ID.Name)
			}

			assert.ElementsMatch(t, tt.wantIDs, resultIDs)
		})
	}
}

// TestFinder_EmptyAndEdgeCases tests empty criteria and edge cases
func TestFinder_EmptyAndEdgeCases(t *testing.T) {
	entries := []registry.Entry{
		{ID: registry.NewID("", "entry-1"), Kind: "test"},
		{ID: registry.NewID("", "entry-2"), Kind: "test"},
	}

	mockReg := &mockRegistry{entries: entries}
	finder := NewFinder(mockReg, nil)

	t.Run("Empty criteria matches all", func(t *testing.T) {
		results, err := finder.Find(attrs.Bag{})
		require.NoError(t, err)
		assert.Equal(t, 2, len(results))
	})

	t.Run("No matches", func(t *testing.T) {
		results, err := finder.Find(attrs.Bag{".kind": "nonexistent"})
		require.NoError(t, err)
		assert.Equal(t, 0, len(results))
	})

	t.Run("Malformed regex is handled gracefully", func(t *testing.T) {
		results, err := finder.Find(attrs.Bag{
			"~meta.field": "[invalid(regex",
			".kind":       "test",
		})
		require.NoError(t, err)
		// Should still match on .kind (regex is skipped with warning)
		assert.Equal(t, 2, len(results))
	})
}

// TestFinder_VersionAwareCaching tests that caching works and invalidates on version change
func TestFinder_VersionAwareCaching(t *testing.T) {
	// Create a versioned mock
	type versionedMock struct {
		*mockRegistry
		version uint
	}

	vm := &versionedMock{
		mockRegistry: &mockRegistry{
			entries: []registry.Entry{
				{ID: registry.NewID("", "entry-1"), Kind: "test", Meta: attrs.Bag{"enabled": true}},
			},
		},
		version: 1,
	}

	vm.currentFunc = func() (registry.Version, error) {
		return &mockVersion{id: vm.version}, nil
	}

	finder := NewFinder(vm.mockRegistry, nil)

	// First query - cache miss
	results1, err := finder.Find(attrs.Bag{"meta.enabled": true})
	require.NoError(t, err)
	assert.Equal(t, 1, len(results1))

	// Same query - should hit cache
	results2, err := finder.Find(attrs.Bag{"meta.enabled": true})
	require.NoError(t, err)
	assert.Equal(t, 1, len(results2))

	// Change version
	vm.version = 2

	// Update entries
	vm.entries = []registry.Entry{
		{ID: registry.NewID("", "entry-1"), Kind: "test", Meta: attrs.Bag{"enabled": true}},
		{ID: registry.NewID("", "entry-2"), Kind: "test", Meta: attrs.Bag{"enabled": true}},
	}

	// Same query after version change - should get new results
	results3, err := finder.Find(attrs.Bag{"meta.enabled": true})
	require.NoError(t, err)
	assert.Equal(t, 2, len(results3), "Cache should be invalidated on version change")
}

// TestFinder_RegexCachePersistence tests that regex cache survives version changes
func TestFinder_RegexCachePersistence(t *testing.T) {
	type versionedMock struct {
		*mockRegistry
		version uint
	}

	vm := &versionedMock{
		mockRegistry: &mockRegistry{
			entries: []registry.Entry{
				{ID: registry.NewID("", "entry-1"), Kind: "test", Meta: attrs.Bag{"desc": "test"}},
			},
		},
		version: 1,
	}

	vm.currentFunc = func() (registry.Version, error) {
		return &mockVersion{id: vm.version}, nil
	}

	finder := NewFinder(vm.mockRegistry, nil).(*memoryFinder)

	// First query with regex
	_, err := finder.Find(attrs.Bag{"~meta.desc": ".*test.*"})
	require.NoError(t, err)

	// Check regex was cached
	_, ok := finder.regexCache.Get(".*test.*")
	assert.True(t, ok, "Regex should be cached")

	// Change version
	vm.version = 2

	// Query again
	_, err = finder.Find(attrs.Bag{"~meta.desc": ".*test.*"})
	require.NoError(t, err)

	// Regex cache should still exist
	_, ok = finder.regexCache.Get(".*test.*")
	assert.True(t, ok, "Regex cache should persist across version changes")
}

// mockVersion for testing
type mockVersion struct {
	id uint
}

func (m *mockVersion) ID() uint                   { return m.id }
func (m *mockVersion) Previous() registry.Version { return nil }
func (m *mockVersion) Next() registry.Version     { return nil }
func (m *mockVersion) String() string             { return "" }

// TestFinder_CacheEviction tests that LRU cache properly evicts old entries
func TestFinder_CacheEviction(t *testing.T) {
	entries := []registry.Entry{
		{ID: registry.NewID("", "entry-1"), Kind: "test", Meta: attrs.Bag{"value": 1}},
		{ID: registry.NewID("", "entry-2"), Kind: "test", Meta: attrs.Bag{"value": 2}},
		{ID: registry.NewID("", "entry-3"), Kind: "test", Meta: attrs.Bag{"value": 3}},
	}

	mockReg := &mockRegistry{entries: entries}

	// Create finder with very small cache (only 2 entries)
	finder := NewFinder(mockReg, nil, WithQueryCacheSize(2))

	// Query 1 - should be cached
	_, err := finder.Find(attrs.Bag{"meta.value": 1})
	require.NoError(t, err)

	// Query 2 - should be cached
	_, err = finder.Find(attrs.Bag{"meta.value": 2})
	require.NoError(t, err)

	// Query 3 - should evict query 1 (oldest)
	_, err = finder.Find(attrs.Bag{"meta.value": 3})
	require.NoError(t, err)

	// At this point, cache should have queries 2 and 3, query 1 should be evicted
	// We can't directly inspect the cache, but we can verify it doesn't crash
	// and behaves correctly
	_, err = finder.Find(attrs.Bag{"meta.value": 1})
	require.NoError(t, err)
}

// TestFinder_RegexCacheEviction tests regex cache LRU eviction
func TestFinder_RegexCacheEviction(t *testing.T) {
	entries := []registry.Entry{
		{ID: registry.NewID("", "entry-1"), Kind: "test", Meta: attrs.Bag{"desc": "pattern1"}},
	}

	mockReg := &mockRegistry{entries: entries}

	// Create finder with very small regex cache (only 2 patterns)
	finder := NewFinder(mockReg, nil, WithRegexCacheSize(2))

	// Use 3 different regex patterns
	_, err := finder.Find(attrs.Bag{"~meta.desc": ".*pattern1.*"})
	require.NoError(t, err)

	_, err = finder.Find(attrs.Bag{"~meta.desc": ".*pattern2.*"})
	require.NoError(t, err)

	// This should evict the first pattern
	_, err = finder.Find(attrs.Bag{"~meta.desc": ".*pattern3.*"})
	require.NoError(t, err)

	// Reusing first pattern should recompile it (but still work)
	_, err = finder.Find(attrs.Bag{"~meta.desc": ".*pattern1.*"})
	require.NoError(t, err)
}

// TestFinder_Fork tests that Fork creates a new finder sharing the regex cache
func TestFinder_Fork(t *testing.T) {
	entries := []registry.Entry{
		{ID: registry.NewID("", "entry-1"), Kind: "test", Meta: attrs.Bag{"desc": "test pattern"}},
	}

	mockReg := &mockRegistry{entries: entries}
	source := NewFinder(mockReg, nil)

	// Use a regex to populate cache
	_, err := source.Find(attrs.Bag{"~meta.desc": ".*pattern.*"})
	require.NoError(t, err)

	// Fork the finder with a different registry
	snapshotEntries := []registry.Entry{
		{ID: registry.NewID("", "snapshot-1"), Kind: "test", Meta: attrs.Bag{"desc": "snapshot pattern"}},
	}
	snapshotReg := &mockRegistry{entries: snapshotEntries}
	forked := Fork(source, snapshotReg, nil)

	// Verify forked finder works with its own registry
	results, err := forked.Find(attrs.Bag{".name": "snapshot-1"})
	require.NoError(t, err)
	assert.Equal(t, 1, len(results))
	assert.Equal(t, "snapshot-1", results[0].ID.Name)

	// Verify regex cache is shared
	sourceFinder := source.(*memoryFinder)
	forkedFinder := forked.(*memoryFinder)
	assert.Same(t, sourceFinder.regexCache, forkedFinder.regexCache, "Regex cache should be shared")

	// Verify query caches are independent
	assert.NotSame(t, sourceFinder.queryCache, forkedFinder.queryCache, "Query caches should be independent")
}

// TestFinder_ForkNonMemoryFinder tests Fork fallback when source is not memoryFinder
func TestFinder_ForkNonMemoryFinder(t *testing.T) {
	entries := []registry.Entry{
		{ID: registry.NewID("", "entry-1"), Kind: "test"},
	}
	mockReg := &mockRegistry{entries: entries}

	// Create a non-memoryFinder (wrap it to hide the type)
	var source registry.Finder = struct{ registry.Finder }{NewFinder(mockReg, nil)}

	forked := Fork(source, mockReg, nil)
	require.NotNil(t, forked)

	// Should work as a fresh finder
	results, err := forked.Find(attrs.Bag{".kind": "test"})
	require.NoError(t, err)
	assert.Equal(t, 1, len(results))
}

// TestFinder_UnprefixedFieldsAreSkipped tests v2 behavior: fields without meta. prefix are skipped
func TestFinder_UnprefixedFieldsAreSkipped(t *testing.T) {
	entries := []registry.Entry{
		{ID: registry.NewID("", "entry-1"), Kind: "test", Meta: attrs.Bag{"enabled": true}},
		{ID: registry.NewID("", "entry-2"), Kind: "test", Meta: attrs.Bag{"enabled": false}},
	}

	mockReg := &mockRegistry{entries: entries}
	finder := NewFinder(mockReg, nil)

	// Unprefixed field should be skipped (with warning)
	results, err := finder.Find(attrs.Bag{"enabled": true})
	require.NoError(t, err)
	assert.Equal(t, 2, len(results), "Unprefixed fields should be skipped, matching all entries")

	// With proper prefix should work
	results, err = finder.Find(attrs.Bag{"meta.enabled": true})
	require.NoError(t, err)
	assert.Equal(t, 1, len(results), "Prefixed fields should match correctly")
}
