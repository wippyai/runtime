package config

import (
	"fmt"
	"reflect"
	"sort"
	"sync"
	"testing"

	"github.com/ponyruntime/pony/api/config"
	"github.com/ponyruntime/pony/api/payload"
	transcoder "github.com/ponyruntime/pony/core/payload"

	"github.com/ponyruntime/pony/core/payload/json"
	"github.com/ponyruntime/pony/core/payload/yaml"
)

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

func TestLoader_Load_SingleEntry_YAML(t *testing.T) {
	// Create a default dtt
	dtt := createDefaultTranscoder()

	// Create a new loader with the dtt
	l := NewLoader(dtt)

	// Create a sample YAML payload
	yamlData := `
path: test
kind: test-kind
meta:
  key: value
`
	p := payload.NewPayload(yamlData, payload.Yaml)

	// Load the payload
	err := l.Load(p)
	if err != nil {
		t.Fatalf("unexpected error during Load: %v", err)
	}

	// Check if the entry was loaded correctly
	entries := l.Entries()
	if len(entries) != 1 {
		t.Fatalf("expected 1 entry, got %d", len(entries))
	}

	expectedEntry := config.Entry{
		Path:   "test",
		Kind:   "test-kind",
		Meta:   config.Metadata{"key": "value"},
		Config: p,
	}

	if entries[0].Path != expectedEntry.Path || entries[0].Kind != expectedEntry.Kind || !reflect.DeepEqual(entries[0].Meta, expectedEntry.Meta) {
		t.Errorf("loaded entry does not match expected entry\ngot:  %v\nwant: %v", entries[0], expectedEntry)
	}
}

func TestLoader_Load_MultipleEntries_YAML(t *testing.T) {
	dtt := createDefaultTranscoder()
	l := NewLoader(dtt)

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

	err := l.Load(p1, p2)
	if err != nil {
		t.Fatalf("unexpected error during Load: %v", err)
	}

	entries := l.Entries()
	if len(entries) != 2 {
		t.Fatalf("expected 2 entries, got %d", len(entries))
	}

	expectedEntries := []config.Entry{
		{Path: "entry1", Kind: "kind1", Meta: config.Metadata{"k1": "v1"}, Config: p1},
		{Path: "entry2", Kind: "kind2", Meta: config.Metadata{"k2": "v2"}, Config: p2},
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

func TestLoader_Load_WithPrefix_YAML(t *testing.T) {
	dtt := createDefaultTranscoder()
	l := NewLoader(dtt).WithPrefix("prefix")

	yamlData := `
path: my-entry
kind: my-kind
meta:
  my-key: my-value
`
	p := payload.NewPayload(yamlData, payload.Yaml)

	err := l.Load(p)
	if err != nil {
		t.Fatalf("unexpected error during Load: %v", err)
	}

	entries := l.Entries()
	if len(entries) != 1 {
		t.Fatalf("expected 1 entry, got %d", len(entries))
	}

	expectedEntry := config.Entry{
		Path:   "prefix.my-entry", // Path should have the prefix
		Kind:   "my-kind",
		Meta:   config.Metadata{"my-key": "my-value"},
		Config: p,
	}

	if entries[0].Path != expectedEntry.Path || entries[0].Kind != expectedEntry.Kind || !reflect.DeepEqual(entries[0].Meta, expectedEntry.Meta) {
		t.Errorf("loaded entry does not match expected entry\ngot:  %v\nwant: %v", entries[0], expectedEntry)
	}
}

func TestLoader_Entries_Sorted(t *testing.T) {
	dtt := createDefaultTranscoder()
	l := NewLoader(dtt)

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

	// Load in a non-alphabetical order
	err := l.Load(p1, p2, p3)
	if err != nil {
		t.Fatalf("unexpected error during Load: %v", err)
	}

	entries := l.Entries()
	if len(entries) != 3 {
		t.Fatalf("expected 3 entries, got %d", len(entries))
	}

	// Check if entries are sorted by Path
	if entries[0].Path != "a-entry" || entries[1].Path != "b-entry" || entries[2].Path != "c-entry" {
		t.Errorf("entries are not sorted by Path")
	}
}

func TestLoader_Entries_PrefixOrder(t *testing.T) {
	dtt := createDefaultTranscoder()
	l := NewLoader(dtt)

	// Load a base entry
	baseEntryYAML := `
path: base
kind: base-kind
meta:
  key: base-value
`
	basePayload := payload.NewPayload(baseEntryYAML, payload.Yaml)
	err := l.Load(basePayload)
	if err != nil {
		t.Fatalf("unexpected error during Load: %v", err)
	}

	// Use the base entry as a prefix loader
	prefixLoader := l.WithPrefix("base")

	// Load entries with the prefix
	prefixedEntryYAML := `
path: sub-entry
kind: sub-kind
meta:
  key: sub-value
`
	prefixedPayload := payload.NewPayload(prefixedEntryYAML, payload.Yaml)
	err = prefixLoader.Load(prefixedPayload)
	if err != nil {
		t.Fatalf("unexpected error during Load with prefix: %v", err)
	}

	entries := l.Entries()
	if len(entries) != 2 {
		t.Fatalf("expected 2 entries, got %d", len(entries))
	}

	// Check that the base entry comes before the prefixed entry
	if entries[0].Path != "base" || entries[1].Path != "base.sub-entry" {
		t.Errorf("entries are not in the correct order or prefixed entry is missing")
	}
}

func TestLoader_Load_InvalidEntryFormat(t *testing.T) {
	dtt := createDefaultTranscoder()
	l := NewLoader(dtt)

	// Invalid YAML format
	invalidYAML := `
path: invalid
kind: invalid-kind
meta:
  - key: value # Invalpath: meta should be a map, not a list
`
	p := payload.NewPayload(invalidYAML, payload.Yaml)

	err := l.Load(p)
	if err == nil {
		t.Fatalf("expected an error during Load with invalid format, but got nil")
	}

	expectedErrorMsg := "failed to unmarshal payload as config.Entry"
	if err.Error()[:len(expectedErrorMsg)] != expectedErrorMsg {
		t.Errorf("unexpected error message\ngot:  %s\nwant: %s", err.Error(), expectedErrorMsg)
	}
}

func TestLoader_Load_MissingIDAndKind(t *testing.T) {
	dtt := createDefaultTranscoder()
	l := NewLoader(dtt)

	// Missing Path
	missingIDYAML := `
kind: missing-id-kind
meta:
  key: value
`
	p1 := payload.NewPayload(missingIDYAML, payload.Yaml)
	err1 := l.Load(p1)
	if err1 == nil {
		t.Fatalf("expected an error due to missing Path, but got nil")
	}
	expectedErrorMsg1 := "missing Path in config entry" // Correct error message
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
	err2 := l.Load(p2)
	if err2 == nil {
		t.Fatalf("expected an error due to missing Kind, but got nil")
	}
	expectedErrorMsg2 := "missing Kind in config entry" // Correct error message
	if err2.Error() != expectedErrorMsg2 {
		t.Errorf("unexpected error message for missing Kind\ngot:  %s\nwant: %s", err2.Error(), expectedErrorMsg2)
	}
}

func TestLoader_Reset(t *testing.T) {
	dtt := createDefaultTranscoder()
	l := NewLoader(dtt)

	yamlData := `
path: test
kind: test-kind
meta:
  key: value
`
	p := payload.NewPayload(yamlData, payload.Yaml)
	err := l.Load(p)
	if err != nil {
		t.Fatalf("unexpected error during Load: %v", err)
	}

	if len(l.Entries()) != 1 {
		t.Fatalf("expected 1 entry, got %d", len(l.Entries()))
	}

	l.Reset()

	if len(l.Entries()) != 0 {
		t.Errorf("expected 0 entries after Reset, got %d", len(l.Entries()))
	}
}

func TestLoader_Load_DuplicateID(t *testing.T) {
	dtt := createDefaultTranscoder()
	l := NewLoader(dtt)

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

	err := l.Load(p1)
	if err != nil {
		t.Fatalf("unexpected error during Load: %v", err)
	}
	err = l.Load(p2)
	if err != nil {
		t.Fatalf("unexpected error during Load: %v", err)
	}

	entries := l.Entries()
	if len(entries) != 1 {
		t.Fatalf("expected 1 entry, got %d", len(entries))
	}

	expectedEntry := config.Entry{
		Path:   "dup-entry",
		Kind:   "kind2",                          // Kind from the second payload
		Meta:   config.Metadata{"key": "value2"}, // Meta from the second payload
		Config: p2,                               // Config from the second payload
	}

	if entries[0].Path != expectedEntry.Path || entries[0].Kind != expectedEntry.Kind || !reflect.DeepEqual(entries[0].Meta, expectedEntry.Meta) {
		t.Errorf("loaded entry does not match expected entry\ngot:  %v\nwant: %v", entries[0], expectedEntry)
	}
}

func TestLoader_Load_Concurrent(t *testing.T) {
	dtt := createDefaultTranscoder()
	l := NewLoader(dtt)
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
			err := l.Load(p)
			if err != nil {
				t.Errorf("unexpected error during concurrent Load: %v", err)
			}
		}(i)
	}

	wg.Wait()

	if len(l.Entries()) != 10 {
		t.Errorf("expected 10 entries, got %d", len(l.Entries()))
	}
}

func TestLoader_Load_EmptyPayload(t *testing.T) {
	dtt := createDefaultTranscoder()
	l := NewLoader(dtt)

	p := payload.NewPayload("", payload.Yaml) // Empty data

	err := l.Load(p)
	if err == nil {
		t.Fatalf("expected an error during Load with empty payload, but got nil")
	}
}

// Helper function to sort entries by Path for easier comparison in tests
func sortEntriesByID(entries []config.Entry) {
	sort.Slice(entries, func(i, j int) bool {
		return entries[i].Path < entries[j].Path
	})
}
