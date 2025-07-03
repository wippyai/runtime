package loader

import (
	"reflect"
	"testing"

	"github.com/ponyruntime/pony/api/payload"
	"github.com/ponyruntime/pony/api/registry"
	tr "github.com/ponyruntime/pony/system/payload"
	jsoncodec "github.com/ponyruntime/pony/system/payload/json"
	"github.com/ponyruntime/pony/system/payload/yaml"
)

func TestExtractDependenciesToEntries(t *testing.T) {
	// Spawn a test transcoder that handles JSON and YAML
	transcoder := tr.NewTranscoder()
	jsoncodec.Register(transcoder)
	yaml.Register(transcoder)

	tests := []struct {
		name    string
		input   string
		format  payload.Format
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
			format: payload.JSON,
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
					Data: payload.New(map[string]interface{}{
						"namespace": "test",
						"name":      "single-entry",
						"kind":      "service",
						"meta": map[string]interface{}{
							"version": "1.0",
							"tags":    []interface{}{"test", "service"},
						},
						"url":  "http://example.com",
						"port": float64(8080),
					}),
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
			format: payload.JSON,
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
						"name": "entry1",
						"kind": "service",
						"meta": map[string]interface{}{
							"version": "1.0",
							"shared":  "value",
						},
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
						"name": "entry2",
						"kind": "endpoint",
						"meta": map[string]interface{}{
							"version": "2.0",
							"shared":  "value",
						},
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
			format:  payload.JSON,
			want:    nil,
			wantErr: true,
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
			format:  payload.JSON,
			want:    nil,
			wantErr: true,
		},
		{
			name: "empty entries array",
			input: `{
				"namespace": "test",
				"entries": []
			}`,
			format:  payload.JSON,
			want:    []registry.Entry{},
			wantErr: false,
		},
		{
			name: "YAML format test",
			input: `
namespace: test-yaml
entries:
 - name: service1
   kind: service
   meta:
     version: "1.0"
   config:
     port: 8080
 - name: service2
   kind: service
   meta:
     version: "2.0"
   config:
     port: 8081
`,
			format: payload.YAML,
			want: []registry.Entry{
				{
					ID:   registry.ID{NS: "test-yaml", Name: "service1"},
					Kind: "service",
					Meta: registry.Metadata{
						"version": "1.0",
					},
					Data: payload.New(map[string]interface{}{
						"name": "service1",
						"kind": "service",
						"meta": map[string]interface{}{
							"version": "1.0",
						},
						"config": map[string]interface{}{
							"port": 8080,
						},
					}),
				},
				{
					ID:   registry.ID{NS: "test-yaml", Name: "service2"},
					Kind: "service",
					Meta: registry.Metadata{
						"version": "2.0",
					},
					Data: payload.New(map[string]interface{}{
						"name": "service2",
						"kind": "service",
						"meta": map[string]interface{}{
							"version": "2.0",
						},
						"config": map[string]interface{}{
							"port": 8081,
						},
					}),
				},
			},
			wantErr: false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			p := payload.NewPayload([]byte(tt.input), tt.format)

			got, err := ExtractDependenciesToEntries(p, transcoder)
			if (err != nil) != tt.wantErr {
				t.Errorf("ExtractDependenciesToEntries() error = %v, wantErr %v", err, tt.wantErr)
				return
			}

			if !tt.wantErr {
				if len(got) != len(tt.want) {
					t.Errorf("ExtractDependenciesToEntries() got %d entries, want %d", len(got), len(tt.want))
					// Debug output
					for i, entry := range got {
						t.Logf("Got entry[%d]: ID=%v, Kind=%s", i, entry.ID, entry.Kind)
					}
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
					if !reflect.DeepEqual(got[i].Meta, tt.want[i].Meta) {
						t.Errorf("Entry[%d].Meta = %+v, want %+v", i, got[i].Meta, tt.want[i].Meta)
					}

					// For Data, check that it's not nil
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
