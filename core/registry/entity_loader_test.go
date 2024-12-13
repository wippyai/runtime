package registry

import (
	"fmt"
	"reflect"
	"sort"
	"sync"
	"testing"

	"github.com/ponyruntime/pony/api/payload"
	reg "github.com/ponyruntime/pony/api/registry"
	transcoder "github.com/ponyruntime/pony/core/payload"
	"github.com/ponyruntime/pony/core/payload/json"
	"github.com/ponyruntime/pony/core/payload/yaml"
)

// Helper function to sort entries by Path for easier comparison in tests
func sortEntriesByID(entries []reg.Entry) {
	sort.Slice(entries, func(i, j int) bool {
		return entries[i].Path < entries[j].Path
	})
}

// createDefaultTranscoder creates a default dtt with JSON and YAML support
func createDefaultTranscoder() payload.Transcoder {
	dtt := transcoder.NewTranscoder()

	// Register JSON
	dtt.RegisterTranscoder(payload.Json, payload.Golang, 1, &json.ToGolang{})
	dtt.RegisterTranscoder(payload.Golang, payload.Json, 1, &json.FromGolang{})
	dtt.RegisterUnmarshaler(payload.Json, &json.ToGolang{})

	// Register YAML
	dtt.RegisterTranscoder(payload.Yaml, payload.Golang, 1, &yaml.ToGolang{})
	dtt.RegisterTranscoder(payload.Golang, payload.Yaml, 1, &yaml.FromGolang{})
	dtt.RegisterUnmarshaler(payload.Yaml, &yaml.ToGolang{})

	return dtt
}

func TestLoader_Register_SingleEntry_YAML(t *testing.T) {
	// Create a default dtt
	dtt := createDefaultTranscoder()

	// Create a new loader with the dtt
	l := NewEntityLoader(dtt)

	// Create a sample YAML payload
	yamlData := `
path: test
kind: test-kind
meta:
  key: value
`
	p := payload.NewPayload(yamlData, payload.Yaml)

	// Register the payload
	err := l.Register("root/", p)
	if err != nil {
		t.Fatalf("unexpected error during Register: %v", err)
	}

	// Check if the entry was loaded correctly
	entries := l.Entries()
	if len(entries) != 1 {
		t.Fatalf("expected 1 entry, got %d", len(entries))
	}

	expectedEntry := reg.Entry{
		Path: "root/test",
		Kind: "test-kind",
		Meta: reg.Metadata{"key": "value"},
		Data: p,
	}

	if entries[0].Path != expectedEntry.Path || entries[0].Kind != expectedEntry.Kind || !reflect.DeepEqual(entries[0].Meta, expectedEntry.Meta) {
		t.Errorf("loaded entry does not match expected entry\ngot:  %v\nwant: %v", entries[0], expectedEntry)
	}
}

func TestLoader_Register_MultipleEntries_YAML(t *testing.T) {
	dtt := createDefaultTranscoder()
	l := NewEntityLoader(dtt)

	yamlData1 := `
path: entry1
kind: kind1
meta:
  k1: v1
`
	p1 := payload.NewPayload(yamlData1, payload.Yaml)

	yamlData2 := `
path: entry2
kind: kind2
meta:
  k2: v2
`
	p2 := payload.NewPayload(yamlData2, payload.Yaml)

	err := l.Register("root/", p1, p2)
	if err != nil {
		t.Fatalf("unexpected error during Register: %v", err)
	}

	entries := l.Entries()
	if len(entries) != 2 {
		t.Fatalf("expected 2 entries, got %d", len(entries))
	}

	expectedEntries := []reg.Entry{
		{Path: "root/entry1", Kind: "kind1", Meta: reg.Metadata{"k1": "v1"}, Data: p1},
		{Path: "root/entry2", Kind: "kind2", Meta: reg.Metadata{"k2": "v2"}, Data: p2},
	}

	// We need to sort entries for comparison as the order they are loaded in is not guaranteed to be the same order they appear in the entries map
	sortEntriesByID(entries)
	sortEntriesByID(expectedEntries)

	for i := 0; i < 2; i++ {
		if entries[i].Path != expectedEntries[i].Path || entries[i].Kind != expectedEntries[i].Kind || !reflect.DeepEqual(entries[i].Meta, expectedEntries[i].Meta) {
			t.Errorf("loaded entry does not match expected entry\ngot:  %v\nwant: %v", entries[i], expectedEntries[i])
		}
	}
}

func TestLoader_Entries_Sorted(t *testing.T) {
	dtt := createDefaultTranscoder()
	l := NewEntityLoader(dtt)

	yamlData1 := `
path: b-entry
kind: kind-b
meta:
  key: value-b
`
	p1 := payload.NewPayload(yamlData1, payload.Yaml)

	yamlData2 := `
path: a-entry
kind: kind-a
meta:
  key: value-a
`
	p2 := payload.NewPayload(yamlData2, payload.Yaml)

	yamlData3 := `
path: c-entry
kind: kind-c
meta:
  key: value-c
`
	p3 := payload.NewPayload(yamlData3, payload.Yaml)

	// Register in a non-alphabetical order
	err := l.Register("root/", p1, p2, p3)
	if err != nil {
		t.Fatalf("unexpected error during Register: %v", err)
	}

	entries := l.Entries()
	if len(entries) != 3 {
		t.Fatalf("expected 3 entries, got %d", len(entries))
	}

	// Check if entries are sorted by Path
	if entries[0].Path != "root/a-entry" || entries[1].Path != "root/b-entry" || entries[2].Path != "root/c-entry" {
		t.Errorf("entries are not sorted by Path")
	}
}

func TestLoader_Register_InvalidEntryFormat(t *testing.T) {
	dtt := createDefaultTranscoder()
	l := NewEntityLoader(dtt)

	// Invalid YAML format
	invalidYAML := `
path: invalid
kind: invalid-kind
meta:
  - key: value # Invalpath: meta should be a map, not a list
`
	p := payload.NewPayload(invalidYAML, payload.Yaml)

	err := l.Register("root/", p)
	if err == nil {
		t.Fatalf("expected an error during Register with invalid format, but got nil")
	}

	expectedErrorMsg := "failed to unmarshal payload as reg.Entry"
	if err.Error()[:len(expectedErrorMsg)] != expectedErrorMsg {
		t.Errorf("unexpected error message\ngot:  %s\nwant: %s", err.Error(), expectedErrorMsg)
	}
}

func TestLoader_Register_MissingIDAndKind(t *testing.T) {
	dtt := createDefaultTranscoder()
	l := NewEntityLoader(dtt)

	// Missing Path
	missingIDYAML := `
kind: missing-id-kind
meta:
  key: value
`
	p1 := payload.NewPayload(missingIDYAML, payload.Yaml)
	err1 := l.Register("root/", p1)
	if err1 == nil {
		t.Fatalf("expected an error due to missing Path, but got nil")
	}
	expectedErrorMsg1 := "missing Path in reg entry"
	if err1.Error() != expectedErrorMsg1 {
		t.Errorf("unexpected error message for missing Path\ngot:  %s\nwant: %s", err1.Error(), expectedErrorMsg1)
	}

	// Missing Kind
	missingKindYAML := `
path: missing-kind-id
meta:
  key: value
`
	p2 := payload.NewPayload(missingKindYAML, payload.Yaml)
	err2 := l.Register("root/", p2)
	if err2 == nil {
		t.Fatalf("expected an error due to missing Kind, but got nil")
	}
	expectedErrorMsg2 := "missing Kind in reg entry"
	if err2.Error() != expectedErrorMsg2 {
		t.Errorf("unexpected error message for missing Kind\ngot:  %s\nwant: %s", err2.Error(), expectedErrorMsg2)
	}
}

func TestLoader_Reset(t *testing.T) {
	dtt := createDefaultTranscoder()
	l := NewEntityLoader(dtt)

	yamlData := `
path: test
kind: test-kind
meta:
  key: value
`
	p := payload.NewPayload(yamlData, payload.Yaml)
	err := l.Register("root/", p)
	if err != nil {
		t.Fatalf("unexpected error during Register: %v", err)
	}

	if len(l.Entries()) != 1 {
		t.Fatalf("expected 1 entry, got %d", len(l.Entries()))
	}

	l.Reset()

	if len(l.Entries()) != 0 {
		t.Errorf("expected 0 entries after Reset, got %d", len(l.Entries()))
	}
}

func TestLoader_Register_DuplicateID(t *testing.T) {
	dtt := createDefaultTranscoder()
	l := NewEntityLoader(dtt)

	yamlData1 := `
path: dup-entry
kind: kind1
meta:
  key: value1
`
	p1 := payload.NewPayload(yamlData1, payload.Yaml)

	yamlData2 := `
path: dup-entry
kind: kind2
meta:
  key: value2
`
	p2 := payload.NewPayload(yamlData2, payload.Yaml)

	err := l.Register("root/", p1)
	if err != nil {
		t.Fatalf("unexpected error during Register: %v", err)
	}
	err = l.Register("root/", p2)
	if err != nil {
		t.Fatalf("unexpected error during Register: %v", err)
	}

	entries := l.Entries()
	if len(entries) != 1 {
		t.Fatalf("expected 1 entry, got %d", len(entries))
	}

	expectedEntry := reg.Entry{
		Path: "root/dup-entry",
		Kind: "kind2",                       // Kind from the second payload
		Meta: reg.Metadata{"key": "value2"}, // Meta from the second payload
		Data: p2,                            // Data from the second payload
	}

	if entries[0].Path != expectedEntry.Path || entries[0].Kind != expectedEntry.Kind || !reflect.DeepEqual(entries[0].Meta, expectedEntry.Meta) {
		t.Errorf("loaded entry does not match expected entry\ngot:  %v\nwant: %v", entries[0], expectedEntry)
	}
}

func TestLoader_Register_Concurrent(t *testing.T) {
	dtt := createDefaultTranscoder()
	l := NewEntityLoader(dtt)
	var wg sync.WaitGroup

	for i := 0; i < 10; i++ {
		wg.Add(1)
		go func(i int) {
			defer wg.Done()
			yamlData := fmt.Sprintf(`
path: entry-%d
kind: kind-%d
meta:
  key: value-%d
`, i, i, i)
			p := payload.NewPayload(yamlData, payload.Yaml)
			err := l.Register(reg.Path(fmt.Sprintf("root-%d/", i)), p)
			if err != nil {
				t.Errorf("unexpected error during concurrent Register: %v", err)
			}
		}(i)
	}

	wg.Wait()

	if len(l.Entries()) != 10 {
		t.Errorf("expected 10 entries, got %d", len(l.Entries()))
	}
}

func TestLoader_Register_EmptyPayload(t *testing.T) {
	dtt := createDefaultTranscoder()
	l := NewEntityLoader(dtt)

	p := payload.NewPayload("", payload.Yaml) // Empty data

	err := l.Register("root/", p)
	if err == nil {
		t.Fatalf("expected an error during Register with empty payload, but got nil")
	}
}

func TestLoader_Register_JSON(t *testing.T) {
	dtt := createDefaultTranscoder()
	l := NewEntityLoader(dtt)

	jsonData := `{
		"path": "test",
		"kind": "test-kind",
		"meta": {
			"key": "value",
			"number": 123,
			"nested": {
				"inner": "value"
			}
		}
	}`
	p := payload.NewPayload(jsonData, payload.Json)

	err := l.Register("root/", p)
	if err != nil {
		t.Fatalf("unexpected error loading JSON: %v", err)
	}

	entries := l.Entries()
	if len(entries) != 1 {
		t.Fatalf("expected 1 entry, got %d", len(entries))
	}

	entry := entries[0]
	if entry.Path != "root/test" {
		t.Errorf("expected path 'root/test', got %s", entry.Path)
	}
	if entry.Kind != "test-kind" {
		t.Errorf("expected kind 'test-kind', got %s", entry.Kind)
	}

	meta := entry.Meta
	if meta["key"] != "value" {
		t.Errorf("expected meta key 'value', got %v", meta["key"])
	}
	if meta["number"].(float64) != 123 {
		t.Errorf("expected meta number 123, got %v", meta["number"])
	}

	nested, ok := meta["nested"].(map[string]interface{})
	if !ok {
		t.Fatal("nested object not properly parsed")
	}
	if nested["inner"] != "value" {
		t.Errorf("expected nested inner 'value', got %v", nested["inner"])
	}
}

func TestLoader_ConcurrentAccess(t *testing.T) {
	dtt := createDefaultTranscoder()
	l := NewEntityLoader(dtt)
	var wg sync.WaitGroup

	// Concurrent writes
	for i := 0; i < 10; i++ {
		wg.Add(1)
		go func(i int) {
			defer wg.Done()
			yamlData := fmt.Sprintf(`
path: entry-%d
kind: test
meta:
  value: %d
`, i, i)
			p := payload.NewPayload(yamlData, payload.Yaml)
			err := l.Register(reg.Path(fmt.Sprintf("root-%d/", i)), p)
			if err != nil {
				t.Errorf("concurrent write failed: %v", err)
			}
		}(i)
	}

	// Concurrent reads while writing
	for i := 0; i < 5; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			entries := l.Entries()
			// Just accessing entries should not panic
			_ = len(entries)
		}()
	}

	wg.Wait()

	// Verify final state
	entries := l.Entries()
	if len(entries) != 10 {
		t.Errorf("expected 10 entries, got %d", len(entries))
	}

	// Verify entries are unique and contain expected values
	seen := make(map[reg.Path]bool)
	for _, entry := range entries {
		if seen[entry.Path] {
			t.Errorf("duplicate entry found: %s", entry.Path)
		}
		seen[entry.Path] = true
	}
}
