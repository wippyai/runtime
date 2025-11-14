package finder

import (
	"context"
	"errors"
	"testing"

	"github.com/ponyruntime/pony/api/payload"
	"github.com/ponyruntime/pony/api/registry"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// mockRegistry implements a simple in-memory registry for testing
type mockRegistry struct {
	entries []registry.Entry
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

func (m *mockRegistry) Current() (registry.Version, error) { return nil, nil }
func (m *mockRegistry) History() registry.History          { return nil }
func (m *mockRegistry) Apply(_ context.Context, _ registry.ChangeSet) (registry.Version, error) {
	return nil, nil
}
func (m *mockRegistry) ApplyVersion(_ context.Context, _ registry.Version) error { return nil }

// TestFinder_RootFieldMatching tests matching on root fields (.kind, .name, .ns, .id)
func TestFinder_RootFieldMatching(t *testing.T) {
	entries := []registry.Entry{
		{
			ID:   registry.ID{NS: "app", Name: "service-api"},
			Kind: "service",
			Meta: registry.Metadata{"meta.enabled": true},
		},
		{
			ID:   registry.ID{NS: "app", Name: "service-queue"},
			Kind: "service",
			Meta: registry.Metadata{"meta.enabled": true},
		},
		{
			ID:   registry.ID{NS: "storage", Name: "database-users"},
			Kind: "database",
			Meta: registry.Metadata{"meta.enabled": true},
		},
	}

	mockReg := &mockRegistry{entries: entries}
	finder := NewFinder(mockReg, nil)

	tests := []struct {
		name     string
		criteria registry.Metadata
		wantIDs  []string
	}{
		{
			name:     "Find by .kind",
			criteria: registry.Metadata{".kind": "service"},
			wantIDs:  []string{"service-api", "service-queue"},
		},
		{
			name:     "Find by .name",
			criteria: registry.Metadata{".name": "service-api"},
			wantIDs:  []string{"service-api"},
		},
		{
			name:     "Find by .ns",
			criteria: registry.Metadata{".ns": "app"},
			wantIDs:  []string{"service-api", "service-queue"},
		},
		{
			name:     "Find by .id (full ID)",
			criteria: registry.Metadata{".id": "app:service-api"},
			wantIDs:  []string{"service-api"},
		},
		{
			name: "Combine .kind and .ns",
			criteria: registry.Metadata{
				".kind": "service",
				".ns":   "app",
			},
			wantIDs: []string{"service-api", "service-queue"},
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
			ID:   registry.ID{Name: "entry-1"},
			Kind: "test",
			Meta: registry.Metadata{
				"enabled": true,
				"port":    8080,
				"env":     "production",
			},
		},
		{
			ID:   registry.ID{Name: "entry-2"},
			Kind: "test",
			Meta: registry.Metadata{
				"enabled": false,
				"port":    9000,
				"env":     "staging",
			},
		},
		{
			ID:   registry.ID{Name: "entry-3"},
			Kind: "test",
			Meta: registry.Metadata{
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
		criteria registry.Metadata
		wantIDs  []string
	}{
		{
			name:     "Boolean equality",
			criteria: registry.Metadata{"meta.enabled": true},
			wantIDs:  []string{"entry-1", "entry-3"},
		},
		{
			name:     "Integer equality",
			criteria: registry.Metadata{"meta.port": 8080},
			wantIDs:  []string{"entry-1", "entry-3"},
		},
		{
			name:     "String equality",
			criteria: registry.Metadata{"meta.env": "production"},
			wantIDs:  []string{"entry-1"},
		},
		{
			name: "Multiple field AND logic",
			criteria: registry.Metadata{
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
			ID:   registry.ID{Name: "service-api"},
			Kind: "service",
			Meta: registry.Metadata{
				"tags": []string{"api", "rest", "users"},
			},
		},
		{
			ID:   registry.ID{Name: "service-queue"},
			Kind: "service",
			Meta: registry.Metadata{
				"tags": []string{"queue", "background", "jobs"},
			},
		},
		{
			ID:   registry.ID{Name: "service-mixed"},
			Kind: "service",
			Meta: registry.Metadata{
				"tags": []string{"api", "queue"},
			},
		},
	}

	mockReg := &mockRegistry{entries: entries}
	finder := NewFinder(mockReg, nil)

	tests := []struct {
		name     string
		criteria registry.Metadata
		wantIDs  []string
	}{
		{
			name:     "Single tag match (ALL must be present)",
			criteria: registry.Metadata{"meta.tags": []string{"api"}},
			wantIDs:  []string{"service-api", "service-mixed"},
		},
		{
			name:     "Multiple tags match (ALL must be present)",
			criteria: registry.Metadata{"meta.tags": []string{"api", "rest"}},
			wantIDs:  []string{"service-api"},
		},
		{
			name:     "No match when not all tags present",
			criteria: registry.Metadata{"meta.tags": []string{"api", "jobs"}},
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
			ID:   registry.ID{Name: "service-1"},
			Kind: "service",
			Meta: registry.Metadata{
				"description": "RESTful API service for user management",
				"version":     "v1.2.3",
			},
		},
		{
			ID:   registry.ID{Name: "service-2"},
			Kind: "service",
			Meta: registry.Metadata{
				"description": "Background job processing queue",
				"version":     "v2.0.1",
			},
		},
		{
			ID:   registry.ID{Name: "database-1"},
			Kind: "database",
			Meta: registry.Metadata{
				"description": "User database with profiles",
				"version":     "14.5",
			},
		},
	}

	mockReg := &mockRegistry{entries: entries}
	finder := NewFinder(mockReg, nil)

	tests := []struct {
		name     string
		criteria registry.Metadata
		wantIDs  []string
	}{
		{
			name:     "Regex on description",
			criteria: registry.Metadata{"~meta.description": ".*database.*"},
			wantIDs:  []string{"database-1"},
		},
		{
			name:     "Regex version pattern v1.x.x",
			criteria: registry.Metadata{"~meta.version": `^v1\.`},
			wantIDs:  []string{"service-1"},
		},
		{
			name:     "Regex version pattern v2.x.x",
			criteria: registry.Metadata{"~meta.version": `^v2\.`},
			wantIDs:  []string{"service-2"},
		},
		{
			name: "Combine regex with root field",
			criteria: registry.Metadata{
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
			ID:   registry.ID{Name: "entry-1"},
			Kind: "test",
			Meta: registry.Metadata{
				"description": "This is an API service",
				"tags":        []string{"production", "api", "backend"},
			},
		},
		{
			ID:   registry.ID{Name: "entry-2"},
			Kind: "test",
			Meta: registry.Metadata{
				"description": "Frontend application",
				"tags":        []string{"production", "frontend"},
			},
		},
	}

	mockReg := &mockRegistry{entries: entries}
	finder := NewFinder(mockReg, nil)

	tests := []struct {
		name     string
		criteria registry.Metadata
		wantIDs  []string
	}{
		{
			name:     "Contains in string field",
			criteria: registry.Metadata{"*meta.description": "API"},
			wantIDs:  []string{"entry-1"},
		},
		{
			name:     "Contains in array field",
			criteria: registry.Metadata{"*meta.tags": "backend"},
			wantIDs:  []string{"entry-1"},
		},
		{
			name:     "Partial match in array",
			criteria: registry.Metadata{"*meta.tags": "prod"},
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
			ID:   registry.ID{Name: "entry-1"},
			Kind: "test",
			Meta: registry.Metadata{
				"version":  "v1.2.3",
				"filename": "service.json",
			},
		},
		{
			ID:   registry.ID{Name: "entry-2"},
			Kind: "test",
			Meta: registry.Metadata{
				"version":  "v2.0.1",
				"filename": "config.yaml",
			},
		},
		{
			ID:   registry.ID{Name: "entry-3"},
			Kind: "test",
			Meta: registry.Metadata{
				"version":  "1.0.0",
				"filename": "data.json",
			},
		},
	}

	mockReg := &mockRegistry{entries: entries}
	finder := NewFinder(mockReg, nil)

	tests := []struct {
		name     string
		criteria registry.Metadata
		wantIDs  []string
	}{
		{
			name:     "Prefix match on version",
			criteria: registry.Metadata{"^meta.version": "v1"},
			wantIDs:  []string{"entry-1"},
		},
		{
			name:     "Suffix match on filename",
			criteria: registry.Metadata{"$meta.filename": ".json"},
			wantIDs:  []string{"entry-1", "entry-3"},
		},
		{
			name: "Combine prefix and suffix",
			criteria: registry.Metadata{
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
		{ID: registry.ID{Name: "entry-1"}, Kind: "test"},
		{ID: registry.ID{Name: "entry-2"}, Kind: "test"},
	}

	mockReg := &mockRegistry{entries: entries}
	finder := NewFinder(mockReg, nil)

	t.Run("Empty criteria matches all", func(t *testing.T) {
		results, err := finder.Find(registry.Metadata{})
		require.NoError(t, err)
		assert.Equal(t, 2, len(results))
	})

	t.Run("No matches", func(t *testing.T) {
		results, err := finder.Find(registry.Metadata{".kind": "nonexistent"})
		require.NoError(t, err)
		assert.Equal(t, 0, len(results))
	})

	t.Run("Malformed regex is handled gracefully", func(t *testing.T) {
		results, err := finder.Find(registry.Metadata{
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
				{ID: registry.ID{Name: "entry-1"}, Kind: "test", Meta: registry.Metadata{"enabled": true}},
			},
		},
		version: 1,
	}

	vm.mockRegistry.Current = func() (registry.Version, error) {
		return &mockVersion{id: vm.version}, nil
	}

	finder := NewFinder(vm.mockRegistry, nil)

	// First query - cache miss
	results1, err := finder.Find(registry.Metadata{"meta.enabled": true})
	require.NoError(t, err)
	assert.Equal(t, 1, len(results1))

	// Same query - should hit cache
	results2, err := finder.Find(registry.Metadata{"meta.enabled": true})
	require.NoError(t, err)
	assert.Equal(t, 1, len(results2))

	// Change version
	vm.version = 2

	// Update entries
	vm.mockRegistry.entries = []registry.Entry{
		{ID: registry.ID{Name: "entry-1"}, Kind: "test", Meta: registry.Metadata{"enabled": true}},
		{ID: registry.ID{Name: "entry-2"}, Kind: "test", Meta: registry.Metadata{"enabled": true}},
	}

	// Same query after version change - should get new results
	results3, err := finder.Find(registry.Metadata{"meta.enabled": true})
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
				{ID: registry.ID{Name: "entry-1"}, Kind: "test", Meta: registry.Metadata{"desc": "test"}},
			},
		},
		version: 1,
	}

	vm.mockRegistry.Current = func() (registry.Version, error) {
		return &mockVersion{id: vm.version}, nil
	}

	finder := NewFinder(vm.mockRegistry, nil).(*memoryFinder)

	// First query with regex
	_, err := finder.Find(registry.Metadata{"~meta.desc": ".*test.*"})
	require.NoError(t, err)

	// Check regex was cached
	_, ok := finder.regexCache.Load(".*test.*")
	assert.True(t, ok, "Regex should be cached")

	// Change version
	vm.version = 2

	// Query again
	_, err = finder.Find(registry.Metadata{"~meta.desc": ".*test.*"})
	require.NoError(t, err)

	// Regex cache should still exist
	_, ok = finder.regexCache.Load(".*test.*")
	assert.True(t, ok, "Regex cache should persist across version changes")
}

// mockVersion for testing
type mockVersion struct {
	id uint
}

func (m *mockVersion) ID() uint                 { return m.id }
func (m *mockVersion) Parent() registry.Version { return nil }
func (m *mockVersion) String() string           { return "" }

// TestFinder_UnprefixedFieldsAreSkipped tests v2 behavior: fields without meta. prefix are skipped
func TestFinder_UnprefixedFieldsAreSkipped(t *testing.T) {
	entries := []registry.Entry{
		{ID: registry.ID{Name: "entry-1"}, Kind: "test", Meta: registry.Metadata{"enabled": true}},
		{ID: registry.ID{Name: "entry-2"}, Kind: "test", Meta: registry.Metadata{"enabled": false}},
	}

	mockReg := &mockRegistry{entries: entries}
	finder := NewFinder(mockReg, nil)

	// Unprefixed field should be skipped (with warning)
	results, err := finder.Find(registry.Metadata{"enabled": true})
	require.NoError(t, err)
	assert.Equal(t, 2, len(results), "Unprefixed fields should be skipped, matching all entries")

	// With proper prefix should work
	results, err = finder.Find(registry.Metadata{"meta.enabled": true})
	require.NoError(t, err)
	assert.Equal(t, 1, len(results), "Prefixed fields should match correctly")
}

// Benchmark tests
func BenchmarkFinder_CacheHit(b *testing.B) {
	entries := make([]registry.Entry, 100)
	for i := 0; i < 100; i++ {
		entries[i] = registry.Entry{
			ID:   registry.ID{Name: string(rune(i))},
			Kind: "test",
			Meta: registry.Metadata{"enabled": true},
		}
	}

	mockReg := &mockRegistry{entries: entries}
	finder := NewFinder(mockReg, nil)

	// Warm up cache
	finder.Find(registry.Metadata{"meta.enabled": true})

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		finder.Find(registry.Metadata{"meta.enabled": true})
	}
}

func BenchmarkFinder_CacheMiss(b *testing.B) {
	entries := make([]registry.Entry, 100)
	for i := 0; i < 100; i++ {
		entries[i] = registry.Entry{
			ID:   registry.ID{Name: string(rune(i))},
			Kind: "test",
			Meta: registry.Metadata{"count": i},
		}
	}

	mockReg := &mockRegistry{entries: entries}
	finder := NewFinder(mockReg, nil)

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		// Different query each time to force cache miss
		finder.Find(registry.Metadata{"meta.count": i % 100})
	}
}
