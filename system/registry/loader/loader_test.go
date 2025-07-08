package loader

import (
	"io/fs"
	"reflect"
	"testing"
	"testing/fstest"

	"github.com/ponyruntime/pony/api/payload"
	"github.com/ponyruntime/pony/api/registry"
	tr "github.com/ponyruntime/pony/system/payload"
	"github.com/ponyruntime/pony/system/payload/json"
	"github.com/ponyruntime/pony/system/payload/yaml"
	"github.com/ponyruntime/pony/system/registry/loader/interpolate"
	"go.uber.org/zap"
)

func setupTranscoder() payload.Transcoder {
	transcoder := tr.NewTranscoder()
	json.Register(transcoder)
	yaml.Register(transcoder)
	return transcoder
}

// Creates a filesystem for testing with the given files
func createTestFS(files map[string]string) fs.FS {
	mapFS := fstest.MapFS{}
	for path, content := range files {
		mapFS[path] = &fstest.MapFile{
			Data: []byte(content),
		}
	}
	return mapFS
}

func TestLoader_LoadFolder(t *testing.T) {
	logger := zap.NewNop()
	transcoder := setupTranscoder()
	interpolator := interpolate.NewEntryInterpolator(transcoder,
		interpolate.WithInterpolator(interpolate.LoadVars),
	)

	tests := []struct {
		name    string
		files   map[string]string
		vars    interpolate.Variables
		want    []registry.Entry
		wantErr bool
	}{
		{
			name: "single file single entry",
			files: map[string]string{
				"config.yaml": `
namespace: test
name: service1
kind: service
meta:
  version: "1.0"
data:
  url: http://example.com
`,
			},

			want: []registry.Entry{
				{
					ID: registry.ID{
						NS:   "test",
						Name: "service1",
					},
					Kind: "service",
					Meta: registry.Metadata{
						"version": "1.0",
					},
					Data: payload.NewPayload(map[string]interface{}{
						"namespace": "test",
						"name":      "service1",
						"kind":      "service",
						"meta": map[string]interface{}{
							"version": "1.0",
						},
						"data": map[string]interface{}{
							"url": "http://example.com",
						},
					}, payload.YAML),
				},
			},
			wantErr: false,
		},
		{
			name: "multiple entries with interpolation",
			files: map[string]string{
				"services.yaml": `
namespace: ${env}
meta:
  shared: common
entries:
  - name: service1
    kind: service
    meta:
      version: "1.0"
    data:
      url: http://${host}/service1
  - name: service2
    kind: service
    meta:
      version: "2.0"
    data:
      url: http://${host}/service2
`,
			},
			vars: interpolate.Variables{
				"env":  "production",
				"host": "example.com",
			},
			want: []registry.Entry{
				{
					ID: registry.ID{
						NS:   "production",
						Name: "service1",
					},
					Kind: "service",
					Meta: registry.Metadata{
						"version": "1.0",
						"shared":  "common",
					},
					Data: payload.NewPayload(map[string]interface{}{
						"data": map[string]interface{}{
							"url": "http://example.com/service1",
						},
					}, payload.Golang),
				},
				{
					ID: registry.ID{
						NS:   "production",
						Name: "service2",
					},
					Kind: "service",
					Meta: registry.Metadata{
						"version": "2.0",
						"shared":  "common",
					},
					Data: payload.NewPayload(map[string]interface{}{
						"data": map[string]interface{}{
							"url": "http://example.com/service2",
						},
					}, payload.Golang),
				},
			},
			wantErr: false,
		},
		{
			name: "mixed valid and invalid files",
			files: map[string]string{
				"valid.yaml": `
namespace: test
name: service1
kind: service
`,
				"invalid.yaml": `
invalid: content
`,
			},
			want: []registry.Entry{
				{
					ID: registry.ID{
						NS:   "test",
						Name: "service1",
					},
					Kind: "service",
					Data: payload.NewPayload(map[string]interface{}{
						"namespace": "test",
						"name":      "service1",
						"kind":      "service",
					}, payload.YAML),
				},
			},
			wantErr: false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			// Create in-memory filesystem for testing
			fsys := createTestFS(tt.files)

			// Initialize loader
			loader := NewLoader(transcoder, logger, interpolator)

			// Load entries
			got, err := loader.LoadFS(fsys, tt.vars)

			if tt.wantErr {
				if err == nil {
					t.Errorf("LoadFS() error = nil, wantErr %v", tt.wantErr)
				}
				return
			}

			if err != nil {
				t.Errorf("LoadFS() unexpected error = %v", err)
				return
			}

			if len(got) != len(tt.want) {
				t.Errorf("LoadFS() got %d entries, want %d", len(got), len(tt.want))
				return
			}

			// Compare entries
			for i := range got {
				if !reflect.DeepEqual(got[i].ID, tt.want[i].ID) {
					t.Errorf("Entry[%d].ID = %v, want %v", i, got[i].ID, tt.want[i].ID)
				}
				if got[i].Kind != tt.want[i].Kind {
					t.Errorf("Entry[%d].Kind = %v, want %v", i, got[i].Kind, tt.want[i].Kind)
				}
				if !equalMetadata(got[i].Meta, tt.want[i].Meta) {
					t.Errorf("Entry[%d].Meta = %v, want %v", i, got[i].Meta, tt.want[i].Meta)
				}
			}
		})
	}
}

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
