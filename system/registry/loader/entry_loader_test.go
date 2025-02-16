package loader

import (
	tr "github.com/ponyruntime/pony/system/payload"
	"github.com/ponyruntime/pony/system/payload/json"
	"reflect"
	"testing"

	"github.com/ponyruntime/pony/api/payload"
	"github.com/ponyruntime/pony/api/registry"
)

// equalMetadata compares metadata contents while being lenient with numeric types
func equalMetadata(got, want registry.Metadata) bool {
	if len(got) != len(want) {
		return false
	}

	for k, wantV := range want {
		gotV, exists := got[k]
		if !exists {
			return false
		}

		// Handle slices specially
		if wantSlice, ok := wantV.([]interface{}); ok {
			gotSlice, ok := gotV.([]interface{})
			if !ok || len(gotSlice) != len(wantSlice) {
				return false
			}
			// Compare slice elements
			for i := range wantSlice {
				if !equalValue(gotSlice[i], wantSlice[i]) {
					return false
				}
			}
			continue
		}

		// For non-slice values
		if !equalValue(gotV, wantV) {
			return false
		}
	}
	return true
}

// equalValue compares values while being lenient with numeric types
func equalValue(got, want interface{}) bool {
	// If they're directly equal, no need for special handling
	if reflect.DeepEqual(got, want) {
		return true
	}

	// Handle numeric comparisons
	switch w := want.(type) {
	case int:
		if g, ok := got.(float64); ok {
			return float64(w) == g
		}
	case float64:
		if g, ok := got.(int); ok {
			return w == float64(g)
		}
	case map[string]interface{}:
		if g, ok := got.(map[string]interface{}); ok {
			return equalMetadata(registry.Metadata(g), registry.Metadata(w))
		}
	}

	return false
}

func TestExtractEntries(t *testing.T) {
	// Spawn a test transcoder that handles JSON
	transcoder := tr.NewTranscoder()
	jsonRegister := json.Register
	jsonRegister(transcoder)

	tests := []struct {
		name    string
		input   string
		want    []registry.Entry
		wantErr bool
	}{
		{
			name: "single entry case",
			input: `{
				"namespace": "test",
				"name": "single-entry",
				"kind": "service",
				"meta": {
					"version": "1.0",
					"tags": ["test", "service"]
				},
				"url": "http://example.com",
				"port": 8080
			}`,
			want: []registry.Entry{
				{
					ID: registry.ID{
						NS:   "test",
						Name: "single-entry",
					},
					Kind: "service",
					Meta: registry.Metadata{
						"version": "1.0",
						"tags":    []interface{}{"test", "service"},
					},
					Data: payload.NewPayload(map[string]interface{}{
						"namespace": "test",
						"name":      "single-entry",
						"kind":      "service",
						"meta": map[string]interface{}{
							"version": "1.0",
							"tags":    []interface{}{"test", "service"},
						},
						"url":  "http://example.com",
						"port": float64(8080),
					}, payload.JSON),
				},
			},
			wantErr: false,
		},
		{
			name: "batch entries case",
			input: `{
				"namespace": "test",
				"meta": {
					"shared": "value"
				},
				"entries": [
					{
						"name": "entry1",
						"kind": "service",
						"meta": {
							"version": "1.0"
						},
						"data": {
							"url": "http://example1.com"
						}
					},
					{
						"name": "entry2",
						"kind": "endpoint",
						"meta": {
							"version": "2.0"
						},
						"data": {
							"path": "/api/v2"
						}
					}
				]
			}`,
			want: []registry.Entry{
				{
					ID: registry.ID{
						NS:   "test",
						Name: "entry1",
					},
					Kind: "service",
					Meta: registry.Metadata{
						"version": "1.0",
						"shared":  "value",
					},
					Data: payload.New(map[string]interface{}{
						"data": map[string]interface{}{
							"url": "http://example1.com",
						},
					}),
				},
				{
					ID: registry.ID{
						NS:   "test",
						Name: "entry2",
					},
					Kind: "endpoint",
					Meta: registry.Metadata{
						"version": "2.0",
						"shared":  "value",
					},
					Data: payload.New(map[string]interface{}{
						"data": map[string]interface{}{
							"path": "/api/v2",
						},
					}),
				},
			},
			wantErr: false,
		},
		{
			name: "missing namespace",
			input: `{
				"name": "test",
				"kind": "service"
			}`,
			want:    nil,
			wantErr: true,
		},
		{
			name: "invalid JSON",
			input: `{
				"namespace": "test"
				"invalid": json,
			}`,
			want:    nil,
			wantErr: true,
		},
		{
			name: "empty metadata in batch entry",
			input: `{
				"namespace": "test",
				"entries": [
					{
						"name": "entry1",
						"kind": "service",
						"meta": null,
						"data": {"url": "http://example.com"}
					}
				]
			}`,
			want: []registry.Entry{
				{
					ID: registry.ID{
						NS:   "test",
						Name: "entry1",
					},
					Kind: "service",
					Data: payload.New(map[string]interface{}{
						"data": map[string]interface{}{
							"url": "http://example.com",
						},
					}),
				},
			},
			wantErr: false,
		},
		{
			name: "entry with missing required fields",
			input: `{
				"namespace": "test",
				"entries": [
					{
						"data": {"url": "http://example.com"}
					}
				]
			}`,
			want:    nil,
			wantErr: true,
		},
		{
			name: "complex metadata types",
			input: `{
                "namespace": "test",
                "meta": {
                    "numbers": [1, 2, 3],
                    "nested": {"key": "value"},
                    "bool": true
                },
                "entries": [
                    {
                        "name": "entry1",
                        "kind": "service",
                        "meta": {
                            "arrays": ["a", "b"],
                            "numbers": [4, 5, 6]
                        },
                        "data": {"url": "http://example.com"}
                    }
                ]
            }`,
			want: []registry.Entry{
				{
					ID: registry.ID{
						NS:   "test",
						Name: "entry1",
					},
					Kind: "service",
					Meta: registry.Metadata{
						"arrays":  []interface{}{"a", "b"},
						"numbers": []interface{}{4, 5, 6},
						"nested":  map[string]interface{}{"key": "value"},
						"bool":    true,
					},
					Data: payload.New(map[string]interface{}{
						"data": map[string]interface{}{
							"url": "http://example.com",
						},
					}),
				},
			},
			wantErr: false,
		},
		{
			name: "empty entries array",
			input: `{
				"namespace": "test",
				"entries": []
			}`,
			want:    []registry.Entry{},
			wantErr: false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			// Spawn JSON payload
			p := payload.NewPayload(tt.input, payload.JSON)

			got, err := ExtractEntries(p, transcoder)
			if (err != nil) != tt.wantErr {
				t.Errorf("ExtractEntries() error = %v, wantErr %v", err, tt.wantErr)
				return
			}

			if !tt.wantErr {
				if len(got) != len(tt.want) {
					t.Errorf("ExtractEntries() got %d entries, want %d", len(got), len(tt.want))
					return
				}

				for i := range got {
					// Check ID
					if !reflect.DeepEqual(got[i].ID, tt.want[i].ID) {
						t.Errorf("Entry[%d].ID = %v, want %v", i, got[i].ID, tt.want[i].ID)
					}

					// Check Kind
					if got[i].Kind != tt.want[i].Kind {
						t.Errorf("Entry[%d].Kind = %v, want %v", i, got[i].Kind, tt.want[i].Kind)
					}

					// Check Meta
					if !equalMetadata(got[i].Meta, tt.want[i].Meta) {
						t.Errorf("Entry[%d].Meta = %+v, want %+v", i, got[i].Meta, tt.want[i].Meta)
					}

					// For Data, check format and ensure it's not nil
					if got[i].Data == nil {
						t.Errorf("Entry[%d].Data is nil", i)
						continue
					}
				}
			}
		})
	}
}

func TestMergeMeta(t *testing.T) {
	tests := []struct {
		name     string
		base     registry.Metadata
		override registry.Metadata
		want     registry.Metadata
	}{
		{
			name: "override string slices",
			base: registry.Metadata{
				"tags":    []string{"base1", "base2"},
				"version": "1.0",
			},
			override: registry.Metadata{
				"tags": []string{"override1", "base1"},
				"env":  "prod",
			},
			want: registry.Metadata{
				"tags":    []string{"override1", "base1"}, // Override completely
				"version": "1.0",
				"env":     "prod",
			},
		},
		{
			name: "merge with nil values",
			base: registry.Metadata{
				"key": "value",
			},
			override: nil,
			want: registry.Metadata{
				"key": "value",
			},
		},
		{
			name: "merge empty override",
			base: nil,
			override: registry.Metadata{
				"key": "value",
			},
			want: registry.Metadata{
				"key": "value",
			},
		},
		{
			name: "merge with interface slices",
			base: registry.Metadata{
				"tags": []interface{}{"base1", "base2"},
			},
			override: registry.Metadata{
				"tags": []interface{}{"override1"},
			},
			want: registry.Metadata{
				"tags": []interface{}{"override1"}, // Notify replacement
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := mergeMeta(tt.base, tt.override)
			if !reflect.DeepEqual(got, tt.want) {
				t.Errorf("mergeMeta() = %v, want %v", got, tt.want)
			}
		})
	}
}

func TestMetadataMergingInData(t *testing.T) {
	// Spawn a test transcoder that handles JSON
	transcoder := tr.NewTranscoder()
	jsonRegister := json.Register
	jsonRegister(transcoder)

	tests := []struct {
		name    string
		input   string
		want    []registry.Entry
		wantErr bool
	}{
		{
			name: "metadata should merge into data field",
			input: `{
                "namespace": "test",
                "meta": {
                    "server": "system:gateway",
                    "router": "system:router",
                    "depends_on": ["ns:functions", "ns:system"]
                },
                "entries": [
                    {
                        "name": "api.endpoint",
                        "kind": "http.endpoint",
                        "meta": {
                            "comment": "Test endpoint"
                        },
                        "method": "GET",
                        "path": "/test",
                        "handler": "functions:test.handler"
                    }
                ]
            }`,
			want: []registry.Entry{
				{
					ID: registry.ID{
						NS:   "test",
						Name: "api.endpoint",
					},
					Kind: "http.endpoint",
					Meta: registry.Metadata{
						"comment":    "Test endpoint",
						"server":     "system:gateway",
						"router":     "system:router",
						"depends_on": []interface{}{"ns:functions", "ns:system"},
					},
					Data: payload.New(map[string]interface{}{
						"meta": map[string]interface{}{
							"comment":    "Test endpoint",
							"server":     "system:gateway",
							"router":     "system:router",
							"depends_on": []interface{}{"ns:functions", "ns:system"},
						},
						"method":  "GET",
						"path":    "/test",
						"handler": "functions:test.handler",
					}),
				},
			},
			wantErr: false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			// Spawn JSON payload
			p := payload.NewPayload(tt.input, payload.JSON)

			got, err := ExtractEntries(p, transcoder)
			if (err != nil) != tt.wantErr {
				t.Errorf("ExtractEntries() error = %v, wantErr %v", err, tt.wantErr)
				return
			}

			if !tt.wantErr {
				if len(got) != len(tt.want) {
					t.Errorf("ExtractEntries() got %d entries, want %d", len(got), len(tt.want))
					return
				}

				for i := range got {
					// Check ID
					if !reflect.DeepEqual(got[i].ID, tt.want[i].ID) {
						t.Errorf("Entry[%d].ID = %v, want %v", i, got[i].ID, tt.want[i].ID)
					}

					// Check Kind
					if got[i].Kind != tt.want[i].Kind {
						t.Errorf("Entry[%d].Kind = %v, want %v", i, got[i].Kind, tt.want[i].Kind)
					}

					// Check Meta
					if !equalMetadata(got[i].Meta, tt.want[i].Meta) {
						t.Errorf("Entry[%d].Meta = %+v, want %+v", i, got[i].Meta, tt.want[i].Meta)
					}

					// Check Data content including metadata
					gotData := make(map[string]interface{})
					if err := transcoder.Unmarshal(got[i].Data, &gotData); err != nil {
						t.Errorf("Failed to unmarshal got data: %v", err)
						continue
					}

					wantData := make(map[string]interface{})
					if err := transcoder.Unmarshal(tt.want[i].Data, &wantData); err != nil {
						t.Errorf("Failed to unmarshal want data: %v", err)
						continue
					}

					// Check if metadata is properly merged in data
					gotMeta, ok := gotData["meta"].(map[string]interface{})
					if !ok {
						t.Errorf("Entry[%d].Data.meta is not a map", i)
						continue
					}

					wantMeta, ok := wantData["meta"].(map[string]interface{})
					if !ok {
						t.Errorf("Want Entry[%d].Data.meta is not a map", i)
						continue
					}

					if !equalMetadata(registry.Metadata(gotMeta), registry.Metadata(wantMeta)) {
						t.Errorf("Entry[%d].Data.meta = %+v, want %+v", i, gotMeta, wantMeta)
					}
				}
			}
		})
	}
}
