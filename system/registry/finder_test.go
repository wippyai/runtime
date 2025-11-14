package registry

import (
	"context"
	"errors"
	"testing"

	"github.com/ponyruntime/pony/api/registry"
	"github.com/stretchr/testify/assert"
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

// Other registry methods not needed for this test
func (m *mockRegistry) Current() (registry.Version, error) { return nil, nil }
func (m *mockRegistry) History() registry.History          { return nil }
func (m *mockRegistry) Apply(_ context.Context, _ registry.ChangeSet) (registry.Version, error) {
	return nil, nil
}
func (m *mockRegistry) ApplyVersion(_ context.Context, _ registry.Version) error { return nil }

// TestFinder_Find tests the advanced finding capabilities
func TestFinder_Find(t *testing.T) {
	// Create test entries
	entries := []registry.Entry{
		{
			ID:   registry.ID{Name: "service-api"},
			Kind: "service",
			Meta: registry.Metadata{
				"description": "RESTful API service for user management",
				"tags":        []string{"api", "rest", "users"},
				"enabled":     true,
				"port":        8080,
				"version":     "v1.2.3",
			},
		},
		{
			ID:   registry.ID{Name: "service-queue"},
			Kind: "service",
			Meta: registry.Metadata{
				"description": "Background job processing queue",
				"tags":        []string{"queue", "background", "jobs"},
				"enabled":     true,
				"port":        9000,
				"version":     "v2.0.1",
			},
		},
		{
			ID:   registry.ID{Name: "database-users"},
			Kind: "database",
			Meta: registry.Metadata{
				"description": "User database with profiles and preferences",
				"engine":      "postgres",
				"enabled":     true,
				"version":     "14.5",
			},
		},
		{
			ID:   registry.ID{Name: "storage-uploads"},
			Kind: "storage",
			Meta: registry.Metadata{
				"description": "File storage service for user uploads",
				"type":        "s3",
				"enabled":     false,
				"region":      "us-west-2",
			},
		},
	}

	// Create mock registry
	mockReg := &mockRegistry{entries: entries}

	// Create finder
	finder := NewFinder(mockReg, nil)

	// Test cases
	tests := []struct {
		name     string
		criteria registry.Metadata
		want     int // Expected number of results
		wantIDs  []string
	}{
		{
			name: "Find by .kind",
			criteria: registry.Metadata{
				".kind": "service",
			},
			want:    2,
			wantIDs: []string{"service-api", "service-queue"},
		},
		{
			name: "Find by .name",
			criteria: registry.Metadata{
				".name": "service-api",
			},
			want:    1,
			wantIDs: []string{"service-api"},
		},
		{
			name: "Find by exact metadata match",
			criteria: registry.Metadata{
				"enabled": true,
			},
			want:    3,
			wantIDs: []string{"service-api", "service-queue", "database-users"},
		},
		{
			name: "Find by regex on description",
			criteria: registry.Metadata{
				"~description": ".*database.*",
			},
			want:    1,
			wantIDs: []string{"database-users"},
		},
		{
			name: "Find with contains matcher",
			criteria: registry.Metadata{
				"*description": "API",
			},
			want:    1,
			wantIDs: []string{"service-api"},
		},
		{
			name: "Find with prefix matcher",
			criteria: registry.Metadata{
				"^version": "v1",
			},
			want:    1,
			wantIDs: []string{"service-api"},
		},
		{
			name: "Find with suffix matcher",
			criteria: registry.Metadata{
				"$version": "3",
			},
			want:    1,
			wantIDs: []string{"service-api"},
		},
		{
			name: "Combine multiple matchers",
			criteria: registry.Metadata{
				".kind":        "service",
				"enabled":      true,
				"~description": ".*queue.*",
			},
			want:    1,
			wantIDs: []string{"service-queue"},
		},
		{
			name: "Match array elements",
			criteria: registry.Metadata{
				"tags": []string{"api", "rest"},
			},
			want:    1,
			wantIDs: []string{"service-api"},
		},
		{
			name: "Partial tag match",
			criteria: registry.Metadata{
				"*tags": "api",
			},
			want:    1,
			wantIDs: []string{"service-api"},
		},
		{
			name: "No matches",
			criteria: registry.Metadata{
				".kind":   "nonexistent",
				"enabled": true,
			},
			want:    0,
			wantIDs: []string{},
		},
	}

	// Run tests
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			results, err := finder.Find(tt.criteria)

			// Verify no error
			assert.NoError(t, err)

			// Verify count matches
			assert.Equal(t, tt.want, len(results), "Expected %d results, got %d", tt.want, len(results))

			// Verify correct IDs returned
			var resultIDs []string
			for _, entry := range results {
				resultIDs = append(resultIDs, entry.ID.Name)
			}

			// Check that all expected IDs are present
			for _, id := range tt.wantIDs {
				assert.Contains(t, resultIDs, id, "Result should contain ID %s", id)
			}

			// Check that there are no unexpected IDs
			assert.Equal(t, len(tt.wantIDs), len(resultIDs), "Number of results should match expected")
		})
	}
}

// TestFinder_MalformedRegex tests that invalid regex patterns are handled gracefully
func TestFinder_MalformedRegex(t *testing.T) {
	entries := []registry.Entry{
		{
			ID:   registry.ID{Name: "test-entry"},
			Kind: "test",
			Meta: registry.Metadata{
				"description": "Test entry for regex",
			},
		},
		{
			ID:   registry.ID{Name: "test-entry-2"},
			Kind: "other",
			Meta: registry.Metadata{
				"description": "Another entry",
			},
		},
	}

	mockReg := &mockRegistry{entries: entries}
	finder := NewFinder(mockReg, nil)

	// Test with malformed regex pattern combined with valid matcher
	results, err := finder.Find(registry.Metadata{
		"~description": "[invalid(regex", // Invalid regex is silently ignored
		".kind":        "test",           // This should still match
	})

	// Should not error, malformed regex is ignored but other matchers work
	assert.NoError(t, err)
	assert.Equal(t, 1, len(results), "Valid matchers should still work")
	assert.Equal(t, "test-entry", results[0].ID.Name)
}

// TestFinder_EmptyMetadata tests searching with empty metadata
func TestFinder_EmptyMetadata(t *testing.T) {
	entries := []registry.Entry{
		{
			ID:   registry.ID{Name: "entry-1"},
			Kind: "test",
			Meta: registry.Metadata{"key": "value"},
		},
		{
			ID:   registry.ID{Name: "entry-2"},
			Kind: "other",
			Meta: registry.Metadata{"key": "value"},
		},
	}

	mockReg := &mockRegistry{entries: entries}
	finder := NewFinder(mockReg, nil)

	// Search with empty metadata should return all entries
	results, err := finder.Find(registry.Metadata{})

	assert.NoError(t, err)
	assert.Equal(t, 2, len(results), "Empty metadata should match all entries")
}

// TestFinder_NoMatches tests that no results are returned when nothing matches
func TestFinder_NoMatches(t *testing.T) {
	entries := []registry.Entry{
		{
			ID:   registry.ID{Name: "entry-1"},
			Kind: "test",
			Meta: registry.Metadata{"enabled": true},
		},
	}

	mockReg := &mockRegistry{entries: entries}
	finder := NewFinder(mockReg, nil)

	// Search for something that doesn't exist
	results, err := finder.Find(registry.Metadata{
		".kind":   "nonexistent",
		"enabled": false,
	})

	assert.NoError(t, err)
	assert.Equal(t, 0, len(results), "Should return no results when nothing matches")
}
