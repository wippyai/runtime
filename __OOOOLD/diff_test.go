package __OOOOLD

import (
	"github.com/ponyruntime/pony/api/events"
	"github.com/ponyruntime/pony/api/payload"
	"github.com/ponyruntime/pony/api/registry"
	"sort"
	"testing"
)

// Helper function to create a registry.Entry for testing.
func makeTestEntry(path registry.Path, configData string) registry.Entry {
	return registry.Entry{
		Path: path,
		Data: payload.NewString(configData),
	}
}

// Helper function to create a registry.Operation for testing.
func makeTestAction(kind events.Kind, entry registry.Entry) registry.Operation {
	return registry.Operation{
		Kind:  kind,
		Entry: entry,
	}
}

// Helper function to sort entries for testing.
func sortTestEntries(entries []registry.Entry) {
	sort.Slice(entries, func(i, j int) bool {
		return entries[i].Path < entries[j].Path
	})
}

// Test helper for diffEntries
func testDiffEntries(t *testing.T, prevEntries, newEntries []registry.Entry, wantActions []registry.Operation) {
	t.Helper()
	gotActions := diffEntries(prevEntries, newEntries)

	if len(gotActions) != len(wantActions) {
		t.Fatalf("diffEntries() len(actions) = %v, want %v", len(gotActions), len(wantActions))
	}

	for i, gotAction := range gotActions {
		wantAction := wantActions[i]
		if gotAction.Kind != wantAction.Kind {
			t.Errorf("diffEntries() action[%d].Kind = %v, want %v", i, gotAction.Kind, wantAction.Kind)
		}
		if gotAction.Entry.Path != wantAction.Entry.Path {
			t.Errorf("diffEntries() action[%d].Entry.Path = %v, want %v", i, gotAction.Entry.Path, wantAction.Entry.Path)
		}
		if gotAction.Entry.Data.Format() != wantAction.Entry.Data.Format() {
			t.Errorf("diffEntries() action[%d].Entry.Data.Format() = %v, want %v", i, gotAction.Entry.Data.Format(), wantAction.Entry.Data.Format())
		}
		if gotAction.Entry.Data.Data() != wantAction.Entry.Data.Data() {
			t.Errorf("diffEntries() action[%d].Entry.Data.Data() = %v, want %v", i, gotAction.Entry.Data.Data(), wantAction.Entry.Data.Data())
		}
	}
}
