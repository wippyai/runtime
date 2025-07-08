package loader

import (
	"context"
	"errors"
	"fmt"
	"reflect"
	"testing"

	"github.com/ponyruntime/pony/api/payload"
	"github.com/ponyruntime/pony/api/registry"
	tr "github.com/ponyruntime/pony/system/payload"
	jsoncodec "github.com/ponyruntime/pony/system/payload/json"
	"github.com/ponyruntime/pony/system/payload/yaml"
)

// TestSuite holds common test setup and utilities
type TestSuite struct {
	transcoder payload.Transcoder
	processor  *EntryProcessor
	validator  *EntryValidator
}

// NewTestSuite creates a new test suite with common setup
func NewTestSuite() *TestSuite {
	transcoder := tr.NewTranscoder()
	jsoncodec.Register(transcoder)
	yaml.Register(transcoder)

	return &TestSuite{
		transcoder: transcoder,
		processor:  NewEntryProcessor(transcoder),
		validator:  NewEntryValidator(),
	}
}

// TestExtractDependenciesToEntries tests the main extraction functionality
func TestExtractDependenciesToEntries(t *testing.T) {
	suite := NewTestSuite()

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

			got, err := suite.processor.ExtractDependenciesToEntries(context.Background(), p)
			if (err != nil) != tt.wantErr {
				t.Errorf("ExtractDependenciesToEntries() error = %v, wantErr %v", err, tt.wantErr)
				return
			}

			if !tt.wantErr {
				assertEntriesEqual(t, got, tt.want)
			}
		})
	}
}

// TestEntryProcessor tests the EntryProcessor functionality
func TestEntryProcessor(t *testing.T) {
	suite := NewTestSuite()

	t.Run("NewEntryProcessor", func(t *testing.T) {
		processor := NewEntryProcessor(suite.transcoder)
		if processor == nil {
			t.Fatal("NewEntryProcessor returned nil")
		}
		if processor.transcoder != suite.transcoder {
			t.Error("transcoder not set correctly")
		}
		if processor.validator == nil {
			t.Error("validator not initialized")
		}
	})

	t.Run("ProcessBatchEntries", func(t *testing.T) {
		content := &FileContent{
			Namespace: "test",
			Meta: registry.Metadata{
				"shared": "value",
			},
			RawEntries: []map[string]interface{}{
				{
					"name": "test-entry",
					"kind": "service",
					"meta": map[string]interface{}{
						"version": "1.0",
					},
				},
			},
		}

		entries, err := suite.processor.processBatchEntries(context.Background(), content)
		if err != nil {
			t.Fatalf("processBatchEntries failed: %v", err)
		}

		if len(entries) != 1 {
			t.Fatalf("expected 1 entry, got %d", len(entries))
		}

		entry := entries[0]
		if entry.ID.NS != "test" {
			t.Errorf("expected namespace 'test', got '%s'", entry.ID.NS)
		}
		if entry.ID.Name != "test-entry" {
			t.Errorf("expected name 'test-entry', got '%s'", entry.ID.Name)
		}
		if entry.Kind != "service" {
			t.Errorf("expected kind 'service', got '%s'", entry.Kind)
		}
	})

	t.Run("ProcessSingleEntry", func(t *testing.T) {
		content := &FileContent{
			Namespace: "test",
			Name:      "single-entry",
			Kind:      "service",
			Meta: registry.Metadata{
				"version": "1.0",
			},
			Data: map[string]interface{}{
				"url":  "http://example.com",
				"port": 8080,
			},
		}

		entry, err := suite.processor.processSingleEntry(context.Background(), content)
		if err != nil {
			t.Fatalf("processSingleEntry failed: %v", err)
		}

		if entry == nil {
			t.Fatal("expected non-nil entry")
		}

		if entry.ID.NS != "test" {
			t.Errorf("expected namespace 'test', got '%s'", entry.ID.NS)
		}
		if entry.ID.Name != "single-entry" {
			t.Errorf("expected name 'single-entry', got '%s'", entry.ID.Name)
		}
		if entry.Kind != "service" {
			t.Errorf("expected kind 'service', got '%s'", entry.Kind)
		}
	})
}

// TestEntryValidator tests the EntryValidator functionality
func TestEntryValidator(t *testing.T) {
	suite := NewTestSuite()

	t.Run("ValidateFileContent", func(t *testing.T) {
		tests := []struct {
			name    string
			content *FileContent
			wantErr bool
		}{
			{
				name: "valid content",
				content: &FileContent{
					Namespace: "test",
					Name:      "test-entry",
					Kind:      "service",
				},
				wantErr: false,
			},
			{
				name:    "nil content",
				content: nil,
				wantErr: true,
			},
			{
				name: "empty namespace",
				content: &FileContent{
					Namespace: "",
					Name:      "test-entry",
					Kind:      "service",
				},
				wantErr: true,
			},
			{
				name: "whitespace namespace",
				content: &FileContent{
					Namespace: "   ",
					Name:      "test-entry",
					Kind:      "service",
				},
				wantErr: true,
			},
			{
				name: "no entries or single entry",
				content: &FileContent{
					Namespace: "test",
				},
				wantErr: true,
			},
		}

		for _, tt := range tests {
			t.Run(tt.name, func(t *testing.T) {
				err := suite.validator.ValidateFileContent(tt.content)
				if (err != nil) != tt.wantErr {
					t.Errorf("ValidateFileContent() error = %v, wantErr %v", err, tt.wantErr)
				}
			})
		}
	})

	t.Run("ValidateRawEntry", func(t *testing.T) {
		tests := []struct {
			name     string
			rawEntry map[string]interface{}
			index    int
			wantErr  bool
		}{
			{
				name: "valid entry",
				rawEntry: map[string]interface{}{
					"name": "test-entry",
					"kind": "service",
				},
				index:   0,
				wantErr: false,
			},
			{
				name:     "nil entry",
				rawEntry: nil,
				index:    0,
				wantErr:  true,
			},
			{
				name: "missing name",
				rawEntry: map[string]interface{}{
					"kind": "service",
				},
				index:   0,
				wantErr: true,
			},
			{
				name: "empty name",
				rawEntry: map[string]interface{}{
					"name": "",
					"kind": "service",
				},
				index:   0,
				wantErr: true,
			},
			{
				name: "whitespace name",
				rawEntry: map[string]interface{}{
					"name": "   ",
					"kind": "service",
				},
				index:   0,
				wantErr: true,
			},
			{
				name: "missing kind",
				rawEntry: map[string]interface{}{
					"name": "test-entry",
				},
				index:   0,
				wantErr: true,
			},
			{
				name: "empty kind",
				rawEntry: map[string]interface{}{
					"name": "test-entry",
					"kind": "",
				},
				index:   0,
				wantErr: true,
			},
		}

		for _, tt := range tests {
			t.Run(tt.name, func(t *testing.T) {
				err := suite.validator.ValidateRawEntry(tt.rawEntry, tt.index)
				if (err != nil) != tt.wantErr {
					t.Errorf("ValidateRawEntry() error = %v, wantErr %v", err, tt.wantErr)
				}
			})
		}
	})

	t.Run("ValidateSingleEntry", func(t *testing.T) {
		tests := []struct {
			name    string
			content *FileContent
			wantErr bool
		}{
			{
				name: "valid single entry",
				content: &FileContent{
					Name: "test-entry",
					Kind: "service",
				},
				wantErr: false,
			},
			{
				name: "missing name",
				content: &FileContent{
					Kind: "service",
				},
				wantErr: true,
			},
			{
				name: "empty name",
				content: &FileContent{
					Name: "",
					Kind: "service",
				},
				wantErr: true,
			},
			{
				name: "missing kind",
				content: &FileContent{
					Name: "test-entry",
				},
				wantErr: true,
			},
			{
				name: "empty kind",
				content: &FileContent{
					Name: "test-entry",
					Kind: "",
				},
				wantErr: true,
			},
		}

		for _, tt := range tests {
			t.Run(tt.name, func(t *testing.T) {
				err := suite.validator.ValidateSingleEntry(tt.content)
				if (err != nil) != tt.wantErr {
					t.Errorf("ValidateSingleEntry() error = %v, wantErr %v", err, tt.wantErr)
				}
			})
		}
	})
}

// TestErrorTypes tests the custom error types
func TestErrorTypes(t *testing.T) {
	t.Run("ValidationError", func(t *testing.T) {
		err := &ValidationError{
			Field:   "name",
			Message: "required field",
			Index:   0,
		}
		expected := "entry[0].name: required field"
		if err.Error() != expected {
			t.Errorf("ValidationError.Error() = %v, want %v", err.Error(), expected)
		}

		errNoIndex := &ValidationError{
			Field:   "namespace",
			Message: "required field",
			Index:   -1,
		}
		expectedNoIndex := "namespace: required field"
		if errNoIndex.Error() != expectedNoIndex {
			t.Errorf("ValidationError.Error() = %v, want %v", errNoIndex.Error(), expectedNoIndex)
		}
	})

	t.Run("ProcessingError", func(t *testing.T) {
		originalErr := fmt.Errorf("original error")
		err := &ProcessingError{
			Operation: "test",
			EntryID:   "test-entry",
			Err:       originalErr,
		}
		expected := "processing error in test for entry test-entry: original error"
		if err.Error() != expected {
			t.Errorf("ProcessingError.Error() = %v, want %v", err.Error(), expected)
		}

		if !errors.Is(err.Unwrap(), originalErr) {
			t.Errorf("ProcessingError.Unwrap() = %v, want %v", err.Unwrap(), originalErr)
		}
	})
}

// TestMergeMeta tests the metadata merging functionality
func TestMergeMeta(t *testing.T) {
	suite := NewTestSuite()

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
			got := suite.processor.mergeMetadata(tt.base, tt.override)
			if !reflect.DeepEqual(got, tt.want) {
				t.Errorf("mergeMetadata() = %v, want %v", got, tt.want)
			}
		})
	}
}

// TestLegacyFunctions tests backward compatibility functions
func TestLegacyFunctions(t *testing.T) {
	suite := NewTestSuite()

	t.Run("Legacy ExtractDependenciesToEntries", func(t *testing.T) {
		input := `{
			"namespace": "test",
			"name": "legacy-test",
			"kind": "service",
			"meta": {
				"version": "1.0"
			}
		}`
		p := payload.NewPayload([]byte(input), payload.JSON)

		entries, err := ExtractDependenciesToEntries(p, suite.transcoder)
		if err != nil {
			t.Fatalf("Legacy ExtractDependenciesToEntries failed: %v", err)
		}

		if len(entries) != 1 {
			t.Fatalf("expected 1 entry, got %d", len(entries))
		}

		entry := entries[0]
		if entry.ID.Name != "legacy-test" {
			t.Errorf("expected name 'legacy-test', got '%s'", entry.ID.Name)
		}
	})

	t.Run("Legacy mergeMeta", func(t *testing.T) {
		base := registry.Metadata{"key": "value"}
		override := registry.Metadata{"key": "override"}

		result := mergeMeta(base, override)
		if result["key"] != "override" {
			t.Errorf("expected 'override', got '%v'", result["key"])
		}
	})
}

// Helper functions

// assertEntriesEqual compares two slices of entries for equality
func assertEntriesEqual(t *testing.T, got, want []registry.Entry) {
	t.Helper()

	if len(got) != len(want) {
		t.Errorf("ExtractDependenciesToEntries() got %d entries, want %d", len(got), len(want))
		// Debug output
		for i, entry := range got {
			t.Logf("Got entry[%d]: ID=%v, Kind=%s", i, entry.ID, entry.Kind)
		}
		return
	}

	for i := range got {
		// Check ID
		if !reflect.DeepEqual(got[i].ID, want[i].ID) {
			t.Errorf("Entry[%d].ID = %v, want %v", i, got[i].ID, want[i].ID)
		}

		// Check Kind
		if got[i].Kind != want[i].Kind {
			t.Errorf("Entry[%d].Kind = %v, want %v", i, got[i].Kind, want[i].Kind)
		}

		// Check Meta
		if !reflect.DeepEqual(got[i].Meta, want[i].Meta) {
			t.Errorf("Entry[%d].Meta = %+v, want %+v", i, got[i].Meta, want[i].Meta)
		}

		// For Data, check that it's not nil
		if got[i].Data == nil {
			t.Errorf("Entry[%d].Data is nil", i)
			continue
		}
	}
}
