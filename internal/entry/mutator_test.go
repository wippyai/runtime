package entry

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"github.com/wippyai/runtime/api/payload"
	"github.com/wippyai/runtime/api/registry"
)

func TestParsePath(t *testing.T) {
	tests := []struct {
		name         string
		path         string
		wantTarget   string
		wantSegments []string
		wantErr      bool
	}{
		{
			name:         "data with leading dot",
			path:         ".data.field",
			wantTarget:   "data",
			wantSegments: []string{"field"},
		},
		{
			name:         "data without leading dot",
			path:         "data.field",
			wantTarget:   "data",
			wantSegments: []string{"field"},
		},
		{
			name:         "meta with leading dot",
			path:         ".meta.parent",
			wantTarget:   "meta",
			wantSegments: []string{"parent"},
		},
		{
			name:         "meta without leading dot",
			path:         "meta.parent",
			wantTarget:   "meta",
			wantSegments: []string{"parent"},
		},
		{
			name:         "nested data path",
			path:         "data.config.database.host",
			wantTarget:   "data",
			wantSegments: []string{"config", "database", "host"},
		},
		{
			name:         "nested meta path",
			path:         ".meta.nested.field",
			wantTarget:   "meta",
			wantSegments: []string{"nested", "field"},
		},
		{
			name:    "empty path",
			path:    "",
			wantErr: true,
		},
		{
			name:         "bare path treated as data",
			path:         "root",
			wantTarget:   "data",
			wantSegments: []string{"root"},
		},
		{
			name:         "bare path with dot treated as data",
			path:         ".root",
			wantTarget:   "data",
			wantSegments: []string{"root"},
		},
		{
			name:         "nested bare path treated as data",
			path:         "config.database.host",
			wantTarget:   "data",
			wantSegments: []string{"config", "database", "host"},
		},
		{
			name:         "bare nested with leading dot",
			path:         ".storage.path",
			wantTarget:   "data",
			wantSegments: []string{"storage", "path"},
		},
		{
			name:         "data only",
			path:         "data",
			wantTarget:   "data",
			wantSegments: []string{},
		},
		{
			name:         "meta only",
			path:         "meta",
			wantTarget:   "meta",
			wantSegments: []string{},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			target, segments, err := parsePath(tt.path)

			if tt.wantErr {
				assert.Error(t, err)
				return
			}

			require.NoError(t, err)
			assert.Equal(t, tt.wantTarget, target)
			assert.Equal(t, tt.wantSegments, segments)
		})
	}
}

func TestMutator_Set_Data(t *testing.T) {
	mutator := NewMutator(&mockTranscoder{})

	t.Run("set simple field", func(t *testing.T) {
		entry := &registry.Entry{
			ID:   registry.ID{NS: "test", Name: "entry"},
			Data: payload.New(map[string]any{}),
		}

		err := mutator.Set(entry, "data.host", "localhost")
		require.NoError(t, err)

		data := entry.Data.Data().(map[string]any)
		assert.Equal(t, "localhost", data["host"])
	})

	t.Run("set nested field", func(t *testing.T) {
		entry := &registry.Entry{
			ID:   registry.ID{NS: "test", Name: "entry"},
			Data: payload.New(map[string]any{}),
		}

		err := mutator.Set(entry, "data.config.database.host", "localhost")
		require.NoError(t, err)

		data := entry.Data.Data().(map[string]any)
		config := data["config"].(map[string]any)
		database := config["database"].(map[string]any)
		assert.Equal(t, "localhost", database["host"])
	})

	t.Run("set with leading dot", func(t *testing.T) {
		entry := &registry.Entry{
			ID:   registry.ID{NS: "test", Name: "entry"},
			Data: payload.New(map[string]any{}),
		}

		err := mutator.Set(entry, ".data.port", 8080)
		require.NoError(t, err)

		data := entry.Data.Data().(map[string]any)
		assert.Equal(t, 8080, data["port"])
	})

	t.Run("overwrite existing field", func(t *testing.T) {
		entry := &registry.Entry{
			ID: registry.ID{NS: "test", Name: "entry"},
			Data: payload.New(map[string]any{
				"host": "old-host",
			}),
		}

		err := mutator.Set(entry, "data.host", "new-host")
		require.NoError(t, err)

		data := entry.Data.Data().(map[string]any)
		assert.Equal(t, "new-host", data["host"])
	})

	t.Run("create nested path in existing structure", func(t *testing.T) {
		entry := &registry.Entry{
			ID: registry.ID{NS: "test", Name: "entry"},
			Data: payload.New(map[string]any{
				"config": map[string]any{
					"existing": "value",
				},
			}),
		}

		err := mutator.Set(entry, "data.config.new.field", "test")
		require.NoError(t, err)

		data := entry.Data.Data().(map[string]any)
		config := data["config"].(map[string]any)
		assert.Equal(t, "value", config["existing"])
		newMap := config["new"].(map[string]any)
		assert.Equal(t, "test", newMap["field"])
	})

	t.Run("nil data", func(t *testing.T) {
		entry := &registry.Entry{
			ID:   registry.ID{NS: "test", Name: "entry"},
			Data: nil,
		}

		err := mutator.Set(entry, "data.field", "value")
		require.NoError(t, err)

		data := entry.Data.Data().(map[string]any)
		assert.Equal(t, "value", data["field"])
	})

	t.Run("bare path without prefix", func(t *testing.T) {
		entry := &registry.Entry{
			ID:   registry.ID{NS: "test", Name: "entry"},
			Data: payload.New(map[string]any{}),
		}

		err := mutator.Set(entry, "root", "value")
		require.NoError(t, err)

		data := entry.Data.Data().(map[string]any)
		assert.Equal(t, "value", data["root"])
	})

	t.Run("bare path with leading dot", func(t *testing.T) {
		entry := &registry.Entry{
			ID:   registry.ID{NS: "test", Name: "entry"},
			Data: payload.New(map[string]any{}),
		}

		err := mutator.Set(entry, ".storage", "/tmp/data")
		require.NoError(t, err)

		data := entry.Data.Data().(map[string]any)
		assert.Equal(t, "/tmp/data", data["storage"])
	})

	t.Run("nested bare path", func(t *testing.T) {
		entry := &registry.Entry{
			ID:   registry.ID{NS: "test", Name: "entry"},
			Data: payload.New(map[string]any{}),
		}

		err := mutator.Set(entry, "config.database.host", "localhost")
		require.NoError(t, err)

		data := entry.Data.Data().(map[string]any)
		config := data["config"].(map[string]any)
		database := config["database"].(map[string]any)
		assert.Equal(t, "localhost", database["host"])
	})
}

func TestMutator_Set_Meta(t *testing.T) {
	mutator := NewMutator(&mockTranscoder{})

	t.Run("set simple meta field", func(t *testing.T) {
		entry := &registry.Entry{
			ID:   registry.ID{NS: "test", Name: "entry"},
			Meta: registry.Metadata{},
		}

		err := mutator.Set(entry, "meta.parent", "parent:id")
		require.NoError(t, err)

		assert.Equal(t, "parent:id", entry.Meta["parent"])
	})

	t.Run("set with leading dot", func(t *testing.T) {
		entry := &registry.Entry{
			ID:   registry.ID{NS: "test", Name: "entry"},
			Meta: registry.Metadata{},
		}

		err := mutator.Set(entry, ".meta.target_db", "main_db")
		require.NoError(t, err)

		assert.Equal(t, "main_db", entry.Meta["target_db"])
	})

	t.Run("nil meta", func(t *testing.T) {
		entry := &registry.Entry{
			ID:   registry.ID{NS: "test", Name: "entry"},
			Meta: nil,
		}

		err := mutator.Set(entry, "meta.field", "value")
		require.NoError(t, err)

		assert.NotNil(t, entry.Meta)
		assert.Equal(t, "value", entry.Meta["field"])
	})

	t.Run("overwrite existing meta", func(t *testing.T) {
		entry := &registry.Entry{
			ID: registry.ID{NS: "test", Name: "entry"},
			Meta: registry.Metadata{
				"field": "old",
			},
		}

		err := mutator.Set(entry, "meta.field", "new")
		require.NoError(t, err)

		assert.Equal(t, "new", entry.Meta["field"])
	})
}

func TestMutator_Append(t *testing.T) {
	mutator := NewMutator(&mockTranscoder{})

	t.Run("append to new array in data", func(t *testing.T) {
		entry := &registry.Entry{
			ID:   registry.ID{NS: "test", Name: "entry"},
			Data: payload.New(map[string]any{}),
		}

		err := mutator.Append(entry, "data.depends_on", "dep1", "dep2")
		require.NoError(t, err)

		data := entry.Data.Data().(map[string]any)
		deps := data["depends_on"].([]any)
		assert.Equal(t, []any{"dep1", "dep2"}, deps)
	})

	t.Run("append to existing array", func(t *testing.T) {
		entry := &registry.Entry{
			ID: registry.ID{NS: "test", Name: "entry"},
			Data: payload.New(map[string]any{
				"depends_on": []any{"existing"},
			}),
		}

		err := mutator.Append(entry, "data.depends_on", "new1", "new2")
		require.NoError(t, err)

		data := entry.Data.Data().(map[string]any)
		deps := data["depends_on"].([]any)
		assert.Equal(t, []any{"existing", "new1", "new2"}, deps)
	})

	t.Run("append with deduplication", func(t *testing.T) {
		entry := &registry.Entry{
			ID: registry.ID{NS: "test", Name: "entry"},
			Data: payload.New(map[string]any{
				"depends_on": []any{"dep1", "dep2"},
			}),
		}

		err := mutator.Append(entry, "data.depends_on", "dep2", "dep3", "dep1")
		require.NoError(t, err)

		data := entry.Data.Data().(map[string]any)
		deps := data["depends_on"].([]any)
		assert.Equal(t, []any{"dep1", "dep2", "dep3"}, deps)
	})

	t.Run("append to meta array", func(t *testing.T) {
		entry := &registry.Entry{
			ID: registry.ID{NS: "test", Name: "entry"},
			Meta: registry.Metadata{
				"groups": []any{"group1"},
			},
		}

		err := mutator.Append(entry, "meta.groups", "group2", "group3")
		require.NoError(t, err)

		groups := entry.Meta["groups"].([]any)
		assert.Equal(t, []any{"group1", "group2", "group3"}, groups)
	})

	t.Run("append to meta with leading dot", func(t *testing.T) {
		entry := &registry.Entry{
			ID:   registry.ID{NS: "test", Name: "entry"},
			Meta: registry.Metadata{},
		}

		err := mutator.Append(entry, ".meta.depends_on", "dep1")
		require.NoError(t, err)

		deps := entry.Meta["depends_on"].([]any)
		assert.Equal(t, []any{"dep1"}, deps)
	})

	t.Run("error on non-array field", func(t *testing.T) {
		entry := &registry.Entry{
			ID: registry.ID{NS: "test", Name: "entry"},
			Data: payload.New(map[string]any{
				"field": "not-an-array",
			}),
		}

		err := mutator.Append(entry, "data.field", "value")
		assert.Error(t, err)
		assert.Contains(t, err.Error(), "cannot append to non-array")
	})

	t.Run("append to bare path", func(t *testing.T) {
		entry := &registry.Entry{
			ID:   registry.ID{NS: "test", Name: "entry"},
			Data: payload.New(map[string]any{}),
		}

		err := mutator.Append(entry, "depends_on", "dep1", "dep2")
		require.NoError(t, err)

		data := entry.Data.Data().(map[string]any)
		deps := data["depends_on"].([]any)
		assert.Equal(t, []any{"dep1", "dep2"}, deps)
	})

	t.Run("append to bare path with leading dot", func(t *testing.T) {
		entry := &registry.Entry{
			ID: registry.ID{NS: "test", Name: "entry"},
			Data: payload.New(map[string]any{
				"tags": []any{"existing"},
			}),
		}

		err := mutator.Append(entry, ".tags", "new1", "new2")
		require.NoError(t, err)

		data := entry.Data.Data().(map[string]any)
		tags := data["tags"].([]any)
		assert.Equal(t, []any{"existing", "new1", "new2"}, tags)
	})
}

func TestMutator_Delete(t *testing.T) {
	mutator := NewMutator(&mockTranscoder{})

	t.Run("delete from data", func(t *testing.T) {
		entry := &registry.Entry{
			ID: registry.ID{NS: "test", Name: "entry"},
			Data: payload.New(map[string]any{
				"field1": "value1",
				"field2": "value2",
			}),
		}

		err := mutator.Delete(entry, "data.field1")
		require.NoError(t, err)

		data := entry.Data.Data().(map[string]any)
		assert.NotContains(t, data, "field1")
		assert.Contains(t, data, "field2")
	})

	t.Run("delete nested field", func(t *testing.T) {
		entry := &registry.Entry{
			ID: registry.ID{NS: "test", Name: "entry"},
			Data: payload.New(map[string]any{
				"config": map[string]any{
					"field1": "value1",
					"field2": "value2",
				},
			}),
		}

		err := mutator.Delete(entry, "data.config.field1")
		require.NoError(t, err)

		data := entry.Data.Data().(map[string]any)
		config := data["config"].(map[string]any)
		assert.NotContains(t, config, "field1")
		assert.Contains(t, config, "field2")
	})

	t.Run("delete from meta", func(t *testing.T) {
		entry := &registry.Entry{
			ID: registry.ID{NS: "test", Name: "entry"},
			Meta: registry.Metadata{
				"field1": "value1",
				"field2": "value2",
			},
		}

		err := mutator.Delete(entry, "meta.field1")
		require.NoError(t, err)

		assert.NotContains(t, entry.Meta, "field1")
		assert.Contains(t, entry.Meta, "field2")
	})

	t.Run("delete with leading dot", func(t *testing.T) {
		entry := &registry.Entry{
			ID: registry.ID{NS: "test", Name: "entry"},
			Data: payload.New(map[string]any{
				"field": "value",
			}),
		}

		err := mutator.Delete(entry, ".data.field")
		require.NoError(t, err)

		data := entry.Data.Data().(map[string]any)
		assert.NotContains(t, data, "field")
	})

	t.Run("delete non-existent field", func(t *testing.T) {
		entry := &registry.Entry{
			ID:   registry.ID{NS: "test", Name: "entry"},
			Data: payload.New(map[string]any{}),
		}

		err := mutator.Delete(entry, "data.nonexistent")
		require.NoError(t, err) // Should not error
	})

	t.Run("delete from nil meta", func(t *testing.T) {
		entry := &registry.Entry{
			ID:   registry.ID{NS: "test", Name: "entry"},
			Meta: nil,
		}

		err := mutator.Delete(entry, "meta.field")
		require.NoError(t, err) // Should not error
	})
}

func TestMutator_FormatHandling(t *testing.T) {
	mutator := NewMutator(&mockTranscoder{})

	t.Run("golang format stays golang", func(t *testing.T) {
		entry := &registry.Entry{
			ID:   registry.ID{NS: "test", Name: "entry"},
			Data: payload.NewPayload(map[string]any{"field": "value"}, payload.Golang),
		}

		err := mutator.Set(entry, "data.newfield", "newvalue")
		require.NoError(t, err)

		assert.Equal(t, payload.Golang, entry.Data.Format())
	})

	t.Run("non-golang format transcodes to golang", func(t *testing.T) {
		entry := &registry.Entry{
			ID:   registry.ID{NS: "test", Name: "entry"},
			Data: payload.NewPayload(map[string]any{"field": "value"}, payload.JSON),
		}

		err := mutator.Set(entry, "data.newfield", "newvalue")
		require.NoError(t, err)

		// After transcoding, should be Golang format
		assert.Equal(t, payload.Golang, entry.Data.Format())
	})
}

func TestMutator_RealWorldScenarios(t *testing.T) {
	mutator := NewMutator(&mockTranscoder{})

	t.Run("dependency resolution scenario", func(t *testing.T) {
		// Simulate resolving a dependency and updating entry
		entry := &registry.Entry{
			ID: registry.ID{NS: "app.local", Name: "service"},
			Data: payload.New(map[string]any{
				"config": map[string]any{
					"name": "my-service",
				},
			}),
			Meta: registry.Metadata{},
		}

		// Set resolved API URL from dependency
		err := mutator.Set(entry, "data.config.api_url", "https://api.example.com")
		require.NoError(t, err)

		// Add dependency tracking
		err = mutator.Append(entry, "meta.depends_on", "api:service:v1")
		require.NoError(t, err)

		// Set parent
		err = mutator.Set(entry, "meta.parent", "parent:module:id")
		require.NoError(t, err)

		// Verify results
		data := entry.Data.Data().(map[string]any)
		config := data["config"].(map[string]any)
		assert.Equal(t, "https://api.example.com", config["api_url"])
		assert.Equal(t, "my-service", config["name"])

		deps := entry.Meta["depends_on"].([]any)
		assert.Equal(t, []any{"api:service:v1"}, deps)
		assert.Equal(t, "parent:module:id", entry.Meta["parent"])
	})

	t.Run("runtime config override scenario", func(t *testing.T) {
		// Simulate runtime config override from command line
		entry := &registry.Entry{
			ID: registry.ID{NS: "app", Name: "database"},
			Data: payload.New(map[string]any{
				"host": "prod-host",
				"port": 5432,
			}),
		}

		// Override for local development
		err := mutator.Set(entry, ".data.host", "localhost")
		require.NoError(t, err)

		err = mutator.Set(entry, ".data.port", 5433)
		require.NoError(t, err)

		// Verify
		data := entry.Data.Data().(map[string]any)
		assert.Equal(t, "localhost", data["host"])
		assert.Equal(t, 5433, data["port"])
	})

	t.Run("migration target database scenario", func(t *testing.T) {
		// Real example from codebase
		entry := &registry.Entry{
			ID:   registry.ID{NS: "wippy.session.migration", Name: "05_create_artifacts_table"},
			Meta: registry.Metadata{},
		}

		// Set target database as shown in user's example
		err := mutator.Set(entry, ".meta.target_db", "session_db")
		require.NoError(t, err)

		assert.Equal(t, "session_db", entry.Meta["target_db"])
	})
}
