// SPDX-License-Identifier: MPL-2.0

package topology

import (
	"fmt"
	"reflect"
	"strings"
	"testing"

	"github.com/wippyai/runtime/api/event"
	"github.com/wippyai/runtime/internal/version"
	"go.uber.org/zap"

	"github.com/wippyai/runtime/api/payload"
	"github.com/wippyai/runtime/api/registry"
)

// testEntry is a helper to create test entries with less boilerplate
type testEntry struct {
	ns        string
	name      string
	kind      string
	data      string
	dependsOn []string
	groups    []string
}

func (te testEntry) toEntry() registry.Entry {
	meta := make(map[string]any)
	if len(te.dependsOn) > 0 {
		meta[registry.TagDependsOn] = te.dependsOn
	}
	if len(te.groups) > 0 {
		meta[registry.TagGroups] = te.groups
	}

	return registry.Entry{
		ID:   registry.NewID(te.ns, te.name),
		Kind: te.kind,
		Data: payload.NewString(te.data),
		Meta: meta,
	}
}

// MockHistory is a mock implementation of the registry.History interface for testing
type MockHistory struct {
	versions  map[registry.Version]registry.ChangeSet
	head      registry.Version
	callStack []string
}

func NewMockHistory() *MockHistory {
	return &MockHistory{
		versions:  make(map[registry.Version]registry.ChangeSet),
		callStack: make([]string, 0),
	}
}

func (m *MockHistory) Versions() ([]registry.Version, error) {
	m.callStack = append(m.callStack, "Versions")
	vs := make([]registry.Version, 0, len(m.versions))
	for v := range m.versions {
		vs = append(vs, v)
	}
	return vs, nil
}

func (m *MockHistory) Get(v registry.Version) (registry.ChangeSet, error) {
	m.callStack = append(m.callStack, fmt.Sprintf("Get(%d)", v.ID()))
	return m.versions[v], nil
}

func (m *MockHistory) Save(v registry.Version, cs registry.ChangeSet, head bool) error {
	m.callStack = append(m.callStack, "Save")
	m.versions[v] = cs
	if head {
		m.head = v
	}
	return nil
}

func (m *MockHistory) Head() (registry.Version, error) {
	m.callStack = append(m.callStack, "Head")
	return m.head, nil
}

func (m *MockHistory) SetHead(v registry.Version) error {
	m.callStack = append(m.callStack, fmt.Sprintf("SetHead(%d)", v.ID()))
	m.head = v
	return nil
}

// Helper functions for common operations
func verifyState(t *testing.T, got, want registry.State) {
	t.Helper()
	if !reflect.DeepEqual(got, want) {
		t.Errorf("state mismatch:\ngot:  %v\nwant: %v", formatState(got), formatState(want))
	}
}

func verifyCallStack(t *testing.T, got, want []string) {
	t.Helper()
	if !reflect.DeepEqual(got, want) {
		t.Errorf("call stack mismatch:\ngot:  %v\nwant: %v", got, want)
	}
}

// formatState formats a State for error messages
func formatState(state registry.State) string {
	var result strings.Builder
	result.WriteString("\n")
	for _, entry := range state {
		_, _ = fmt.Fprintf(&result, "  {NS: %s, Alias: %s, Kind: %s, Data: %v, DependsOn: %v, Groups: %v}\n",
			entry.ID.NS,
			entry.ID.Name,
			entry.Kind,
			entry.Data.Data(),
			entry.Meta[registry.TagDependsOn],
			entry.Meta[registry.TagGroups],
		)
	}
	return result.String()
}

// formatDelta formats a ChangeSet for error messages
func formatDelta(cs registry.ChangeSet) string {
	var result strings.Builder
	result.WriteString("\n")
	for _, op := range cs {
		_, _ = fmt.Fprintf(&result, "  %s {NS: %s, Alias: %s} (deps: %v)\n",
			op.Kind,
			op.Entry.ID.NS,
			op.Entry.ID.Name,
			op.Entry.Meta[registry.TagDependsOn],
		)
	}
	return result.String()
}

// Helper struct to group operations by kind
type opGroup struct {
	deps    map[string]bool
	entries []registry.Entry
}

// verifyDeltaWithinLevel helper function
func verifyDeltaWithinLevel(t *testing.T, got, want registry.ChangeSet) {
	t.Helper()
	if len(got) != len(want) {
		t.Errorf("unexpected delta length:\ngot:  %d\nwant: %d", len(got), len(want))
		return
	}

	// Group operations by type (Spawn, Update, Delete)
	gotGroups := make(map[event.Kind]opGroup)
	wantGroups := make(map[event.Kind]opGroup)

	// Helper to add to groups
	addToGroup := func(groups map[event.Kind]opGroup, op registry.Operation) {
		group := groups[op.Kind]
		group.entries = append(group.entries, op.Entry)
		if group.deps == nil {
			group.deps = make(map[string]bool)
		}
		if deps, ok := op.Entry.Meta[registry.TagDependsOn].([]string); ok {
			for _, dep := range deps {
				group.deps[dep] = true
			}
		}
		groups[op.Kind] = group
	}

	// Group operations
	for _, op := range got {
		addToGroup(gotGroups, op)
	}
	for _, op := range want {
		addToGroup(wantGroups, op)
	}

	// Compare groups
	for kind, wantGroup := range wantGroups {
		gotGroup, exists := gotGroups[kind]
		if !exists {
			t.Errorf("missing operation kind %s in result", kind)
			continue
		}

		// Compare dependencies
		if !reflect.DeepEqual(gotGroup.deps, wantGroup.deps) {
			t.Errorf("dependency mismatch for %s operations:\ngot:  %v\nwant: %v",
				kind, gotGroup.deps, wantGroup.deps)
		}

		// Compare entries (ignoring order within same dependency level)
		gotEntryMap := make(map[registry.ID]bool)
		for _, entry := range gotGroup.entries {
			gotEntryMap[entry.ID] = true
		}
		wantEntryMap := make(map[registry.ID]bool)
		for _, entry := range wantGroup.entries {
			wantEntryMap[entry.ID] = true
		}

		if !reflect.DeepEqual(gotEntryMap, wantEntryMap) {
			t.Errorf("entry mismatch for %s operations:\ngot:  %v\nwant: %v",
				kind, gotGroup.entries, wantGroup.entries)
		}
	}
}

func TestValidateOperation(t *testing.T) {
	builder := NewStateBuilder(zap.NewNop(), nil)

	baseEntry := testEntry{
		ns:   "test",
		name: "service.api",
		kind: "service",
		data: "original",
	}.toEntry()

	t.Run("Spawn", func(t *testing.T) {
		state := NewStateMap(nil)

		// Valid create
		err := builder.ValidateOperation(state, registry.Operation{
			Kind:  registry.EntryCreate,
			Entry: baseEntry,
		})
		if err != nil {
			t.Errorf("expected no error for valid create, got: %v", err)
		}

		// Invalid - already exists
		state[baseEntry.ID] = baseEntry
		err = builder.ValidateOperation(state, registry.Operation{
			Kind:  registry.EntryCreate,
			Entry: baseEntry,
		})
		if err == nil || !strings.Contains(err.Error(), "already exists") {
			t.Errorf("expected 'already exists' error, got: %v", err)
		}
	})

	t.Run("Update", func(t *testing.T) {
		state := NewStateMap(nil)

		// Invalid - doesn't exist
		err := builder.ValidateOperation(state, registry.Operation{
			Kind:  registry.EntryUpdate,
			Entry: baseEntry,
		})
		if err == nil || !strings.Contains(err.Error(), "does not exist") {
			t.Errorf("expected 'does not exist' error, got: %v", err)
		}

		// Invalid - kind change
		state[baseEntry.ID] = baseEntry
		differentKindEntry := baseEntry
		differentKindEntry.Kind = "different"
		err = builder.ValidateOperation(state, registry.Operation{
			Kind:  registry.EntryUpdate,
			Entry: differentKindEntry,
		})
		if err == nil || !strings.Contains(err.Error(), "cannot change entry kind") {
			t.Errorf("expected 'cannot change entry kind' error, got: %v", err)
		}

		// Valid update
		updatedEntry := baseEntry
		updatedEntry.Data = payload.NewString("updated")
		err = builder.ValidateOperation(state, registry.Operation{
			Kind:  registry.EntryUpdate,
			Entry: updatedEntry,
		})
		if err != nil {
			t.Errorf("expected no error for valid update, got: %v", err)
		}
	})

	t.Run("Delete", func(t *testing.T) {
		state := NewStateMap(nil)

		// Invalid - doesn't exist
		err := builder.ValidateOperation(state, registry.Operation{
			Kind:  registry.EntryDelete,
			Entry: baseEntry,
		})
		if err == nil || !strings.Contains(err.Error(), "cannot delete non-existent") {
			t.Errorf("expected 'cannot delete non-existent' error, got: %v", err)
		}

		// Valid delete
		state[baseEntry.ID] = baseEntry
		err = builder.ValidateOperation(state, registry.Operation{
			Kind:  registry.EntryDelete,
			Entry: baseEntry,
		})
		if err != nil {
			t.Errorf("expected no error for valid delete, got: %v", err)
		}
	})

	t.Run("Invalid Operation", func(t *testing.T) {
		state := NewStateMap(nil)
		err := builder.ValidateOperation(state, registry.Operation{
			Kind:  "invalid",
			Entry: baseEntry,
		})
		if err == nil || !strings.Contains(err.Error(), "unknown operation") {
			t.Errorf("expected 'unknown operation' error, got: %v", err)
		}
	})
}

func TestApplyOperation(t *testing.T) {
	builder := NewStateBuilder(zap.NewNop(), nil)

	baseEntry := testEntry{
		ns:   "test",
		name: "service.api",
		kind: "service",
		data: "original",
	}.toEntry()

	t.Run("Spawn", func(t *testing.T) {
		state := NewStateMap(nil)

		newState, err := builder.ApplyOperation(state, registry.Operation{
			Kind:  registry.EntryCreate,
			Entry: baseEntry,
		})
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}

		// Verify original state unchanged
		if len(state) != 0 {
			t.Error("original state was modified")
		}

		// Verify new state
		if entry, exists := newState[baseEntry.ID]; !exists {
			t.Error("entry not created")
		} else if !reflect.DeepEqual(entry, baseEntry) {
			t.Error("created entry doesn't match original")
		}
	})

	t.Run("Update", func(t *testing.T) {
		state := NewStateMap(nil)
		state[baseEntry.ID] = baseEntry

		updatedEntry := baseEntry
		updatedEntry.Data = payload.NewString("updated")

		newState, err := builder.ApplyOperation(state, registry.Operation{
			Kind:  registry.EntryUpdate,
			Entry: updatedEntry,
		})
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}

		// Verify original state unchanged
		if !reflect.DeepEqual(state[baseEntry.ID], baseEntry) {
			t.Error("original state was modified")
		}

		// Verify new state
		if entry, exists := newState[baseEntry.ID]; !exists {
			t.Error("entry not found after update")
		} else if !reflect.DeepEqual(entry, updatedEntry) {
			t.Error("updated entry doesn't match expected")
		}
	})

	t.Run("Delete", func(t *testing.T) {
		state := NewStateMap(nil)
		state[baseEntry.ID] = baseEntry

		newState, err := builder.ApplyOperation(state, registry.Operation{
			Kind:  registry.EntryDelete,
			Entry: baseEntry,
		})
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}

		// Verify original state unchanged
		if !reflect.DeepEqual(state[baseEntry.ID], baseEntry) {
			t.Error("original state was modified")
		}

		// Verify new state
		if _, exists := newState[baseEntry.ID]; exists {
			t.Error("entry not deleted")
		}
	})

	t.Run("Invalid Operation", func(t *testing.T) {
		state := NewStateMap(nil)
		_, err := builder.ApplyOperation(state, registry.Operation{
			Kind:  "invalid",
			Entry: baseEntry,
		})
		if err == nil || !strings.Contains(err.Error(), "unknown operation") {
			t.Errorf("expected 'unknown operation' error, got: %v", err)
		}
	})
}

func TestGetInverseOperation(t *testing.T) {
	builder := NewStateBuilder(zap.NewNop(), nil)

	baseEntry := testEntry{
		ns:   "test",
		name: "service.api",
		kind: "service",
		data: "original",
	}.toEntry()

	t.Run("Spawn", func(t *testing.T) {
		inverse, err := builder.GetInverseOperation(registry.Operation{
			Kind:  registry.EntryCreate,
			Entry: baseEntry,
		})
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}

		if inverse.Kind != registry.EntryDelete {
			t.Error("inverse of Spawn should be Delete")
		}
		if !reflect.DeepEqual(inverse.Entry, baseEntry) {
			t.Error("inverse operation entry doesn't match original")
		}
	})

	t.Run("Update", func(t *testing.T) {
		updatedEntry := baseEntry
		updatedEntry.Data = payload.NewString("updated")

		inverse, err := builder.GetInverseOperation(registry.Operation{
			Kind:          registry.EntryUpdate,
			Entry:         updatedEntry,
			OriginalEntry: &baseEntry,
		})
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}

		if inverse.Kind != registry.EntryUpdate {
			t.Error("inverse of Update should be Update")
		}
		if !reflect.DeepEqual(inverse.Entry, baseEntry) {
			t.Error("inverse operation should restore original entry")
		}
	})

	t.Run("Delete", func(t *testing.T) {
		inverse, err := builder.GetInverseOperation(registry.Operation{
			Kind:          registry.EntryDelete,
			Entry:         baseEntry,
			OriginalEntry: &baseEntry,
		})
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}

		if inverse.Kind != registry.EntryCreate {
			t.Error("inverse of Delete should be Spawn")
		}
		if !reflect.DeepEqual(inverse.Entry, baseEntry) {
			t.Error("inverse operation should recreate original entry")
		}
	})

	t.Run("Update Non-Existent", func(t *testing.T) {
		_, err := builder.GetInverseOperation(registry.Operation{
			Kind:  registry.EntryUpdate,
			Entry: baseEntry,
		})
		if err == nil || !strings.Contains(err.Error(), "not found") {
			t.Errorf("expected 'not found' error, got: %v", err)
		}
	})

	t.Run("Delete Non-Existent", func(t *testing.T) {
		_, err := builder.GetInverseOperation(registry.Operation{
			Kind:  registry.EntryDelete,
			Entry: baseEntry,
		})
		if err == nil || !strings.Contains(err.Error(), "not found") {
			t.Errorf("expected 'not found' error, got: %v", err)
		}
	})

	t.Run("Invalid Operation", func(t *testing.T) {
		_, err := builder.GetInverseOperation(registry.Operation{
			Kind:  "invalid",
			Entry: baseEntry,
		})
		if err == nil || !strings.Contains(err.Error(), "unknown operation") {
			t.Errorf("expected 'unknown operation' error, got: %v", err)
		}
	})
}

func TestBuildState_Empty(t *testing.T) {
	builder := NewStateBuilder(zap.NewNop(), nil)
	history := NewMockHistory()

	v0 := version.New(registry.RootVersion)
	_ = history.Save(v0, registry.ChangeSet{}, false)

	state, err := builder.BuildState(history, v0)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if len(state) != 0 {
		t.Errorf("expected empty state, got %d entries", len(state))
	}

	// Path(v0, v0) returns empty array, but we still Get v0's changeset
	expectedCallStack := []string{"Save", "Versions", "Get(0)"}
	verifyCallStack(t, history.callStack, expectedCallStack)
}

func TestBuildState_SingleVersion(t *testing.T) {
	builder := NewStateBuilder(zap.NewNop(), nil)
	history := NewMockHistory()

	// Spawn test entries
	entry1 := testEntry{
		ns: "test", name: "service.api",
		kind: "service", data: "data1",
	}.toEntry()

	entry2 := testEntry{
		ns: "test", name: "service.db",
		kind: "service", data: "data2",
	}.toEntry()

	// Setup versions
	v0 := version.New(registry.RootVersion)
	v1 := version.FromParent(v0, 1)

	// Save changes
	_ = history.Save(v0, registry.ChangeSet{}, false)
	_ = history.Save(v1, registry.ChangeSet{
		{Kind: registry.EntryCreate, Entry: entry1},
		{Kind: registry.EntryCreate, Entry: entry2},
	}, false)

	// Build state
	state, err := builder.BuildState(history, v1)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	expectedState := registry.State{entry1, entry2}
	verifyState(t, state, expectedState)

	// Path(v0, v1) now returns [v1] (only changesets to apply)
	expectedCallStack := []string{"Save", "Save", "Versions", "Get(1)"}
	verifyCallStack(t, history.callStack, expectedCallStack)
}

func TestBuildState_MultipleVersions(t *testing.T) {
	builder := NewStateBuilder(zap.NewNop(), nil)
	history := NewMockHistory()

	// Spawn test entries
	entry1 := testEntry{
		ns: "test", name: "service.api",
		kind: "service", data: "data1",
	}.toEntry()

	entry1Updated := testEntry{
		ns: "test", name: "service.api",
		kind: "service", data: "data1-updated",
	}.toEntry()

	entry2 := testEntry{
		ns: "test", name: "service.db",
		kind: "service", data: "data2",
	}.toEntry()

	// Setup versions
	v0 := version.New(registry.RootVersion)
	v1 := version.FromParent(v0, 1)
	v2 := version.FromParent(v1, 2)
	v3 := version.FromParent(v2, 3)

	// Save changes
	_ = history.Save(v0, registry.ChangeSet{}, false)
	_ = history.Save(v1, registry.ChangeSet{
		{Kind: registry.EntryCreate, Entry: entry1},
	}, false)
	_ = history.Save(v2, registry.ChangeSet{
		{Kind: registry.EntryCreate, Entry: entry2},
		{Kind: registry.EntryUpdate, Entry: entry1Updated},
	}, false)
	_ = history.Save(v3, registry.ChangeSet{
		{Kind: registry.EntryDelete, Entry: entry2},
	}, false)

	// Build final state
	state, err := builder.BuildState(history, v3)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	expectedState := registry.State{entry1Updated}
	verifyState(t, state, expectedState)

	// Path(v0, v3) now returns [v1, v2, v3] (no v0)
	expectedCallStack := []string{
		"Save", "Save", "Save", "Save",
		"Versions",
		"Get(1)", "Get(2)", "Get(3)",
	}
	verifyCallStack(t, history.callStack, expectedCallStack)
}

func TestBuildState_ConflictingOperations(t *testing.T) {
	builder := NewStateBuilder(zap.NewNop(), nil)
	history := NewMockHistory()

	entry := testEntry{
		ns: "test", name: "service.api",
		kind: "service", data: "original",
	}.toEntry()

	conflictingEntry := testEntry{
		ns: "test", name: "service.api",
		kind: "different", data: "conflict",
	}.toEntry()

	// Setup versions
	v0 := version.New(registry.RootVersion)
	v1 := version.FromParent(v0, 1)
	v2 := version.FromParent(v1, 2)

	// Save changes with conflict
	_ = history.Save(v0, registry.ChangeSet{}, false)
	_ = history.Save(v1, registry.ChangeSet{
		{Kind: registry.EntryCreate, Entry: entry},
	}, false)
	_ = history.Save(v2, registry.ChangeSet{
		{Kind: registry.EntryCreate, Entry: conflictingEntry}, // Conflicting create
	}, false)

	// Build state should return an error for conflicting operations
	_, err := builder.BuildState(history, v2)
	if err == nil {
		t.Fatal("expected error for conflicting operations, got nil")
	}
}

func TestBuildState_UnreachableVersion(t *testing.T) {
	builder := NewStateBuilder(zap.NewNop(), nil)
	history := NewMockHistory()

	// Spawn disconnected versions
	v0 := version.New(registry.RootVersion)
	v1 := version.FromParent(v0, 1)
	v2 := version.New(2) // Disconnected version

	entry := testEntry{
		ns: "test", name: "service.api",
		kind: "service", data: "data",
	}.toEntry()

	_ = history.Save(v0, registry.ChangeSet{}, false)
	_ = history.Save(v1, registry.ChangeSet{
		{Kind: registry.EntryCreate, Entry: entry},
	}, false)
	_ = history.Save(v2, registry.ChangeSet{}, false)

	// Try to build state to unreachable version
	_, err := builder.BuildState(history, v2)
	if err == nil {
		t.Fatal("expected error for unreachable version")
	}
	if !strings.Contains(err.Error(), "failed to get path") {
		t.Errorf("unexpected error: %v", err)
	}
}

func TestBuildState_IntermediateVersion(t *testing.T) {
	builder := NewStateBuilder(zap.NewNop(), nil)
	history := NewMockHistory()

	// Spawn test entries
	entry1 := testEntry{
		ns: "test", name: "service.api",
		kind: "service", data: "data1",
	}.toEntry()

	entry2 := testEntry{
		ns: "test", name: "service.db",
		kind: "service", data: "data2",
	}.toEntry()

	// Setup versions
	v0 := version.New(registry.RootVersion)
	v1 := version.FromParent(v0, 1)
	v2 := version.FromParent(v1, 2)
	v3 := version.FromParent(v2, 3)

	// Save changes
	_ = history.Save(v0, registry.ChangeSet{}, false)
	_ = history.Save(v1, registry.ChangeSet{
		{Kind: registry.EntryCreate, Entry: entry1},
	}, false)
	_ = history.Save(v2, registry.ChangeSet{
		{Kind: registry.EntryCreate, Entry: entry2},
	}, false)
	_ = history.Save(v3, registry.ChangeSet{
		{Kind: registry.EntryDelete, Entry: entry2},
	}, false)

	// Build state up to intermediate version v2
	state, err := builder.BuildState(history, v2)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	expectedState := registry.State{entry1, entry2}
	verifyState(t, state, expectedState)

	// Path(v0, v2) now returns [v1, v2] (no v3)
	expectedCallStack := []string{
		"Save", "Save", "Save", "Save",
		"Versions",
		"Get(1)", "Get(2)", // Should not get v3
	}
	verifyCallStack(t, history.callStack, expectedCallStack)
}

func TestBuildDelta_Empty(t *testing.T) {
	builder := NewStateBuilder(zap.NewNop(), nil)

	t.Run("Both Empty", func(t *testing.T) {
		delta, err := builder.BuildDelta(registry.State{}, registry.State{})
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		if len(delta) != 0 {
			t.Errorf("expected empty delta, got %d operations", len(delta))
		}
	})

	t.Run("Target Empty", func(t *testing.T) {
		entry := testEntry{
			ns: "test", name: "service",
			kind: "service", data: "data",
		}.toEntry()

		delta, err := builder.BuildDelta(registry.State{}, registry.State{entry})
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}

		expectedDelta := registry.ChangeSet{
			{Kind: registry.EntryCreate, Entry: entry},
		}
		verifyDelta(t, delta, expectedDelta)
	})

	t.Run("To Empty", func(t *testing.T) {
		entry := testEntry{
			ns: "test", name: "service",
			kind: "service", data: "data",
		}.toEntry()

		delta, err := builder.BuildDelta(registry.State{entry}, registry.State{})
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}

		expectedDelta := registry.ChangeSet{
			{Kind: registry.EntryDelete, Entry: entry},
		}
		verifyDelta(t, delta, expectedDelta)
	})
}

func TestBuildDelta_SimpleOperations(t *testing.T) {
	builder := NewStateBuilder(zap.NewNop(), nil)

	// Base entries
	entry1 := testEntry{
		ns: "test", name: "service.api",
		kind: "service", data: "data1",
	}.toEntry()

	entry2 := testEntry{
		ns: "test", name: "service.db",
		kind: "service", data: "data2",
	}.toEntry()

	entry1Updated := testEntry{
		ns: "test", name: "service.api",
		kind: "service", data: "data1-updated",
	}.toEntry()

	t.Run("Spawn", func(t *testing.T) {
		from := registry.State{entry1}
		to := registry.State{entry1, entry2}

		delta, err := builder.BuildDelta(from, to)
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}

		expectedDelta := registry.ChangeSet{
			{Kind: registry.EntryCreate, Entry: entry2},
		}
		verifyDelta(t, delta, expectedDelta)
	})

	t.Run("Update", func(t *testing.T) {
		from := registry.State{entry1}
		to := registry.State{entry1Updated}

		delta, err := builder.BuildDelta(from, to)
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}

		expectedDelta := registry.ChangeSet{
			{Kind: registry.EntryUpdate, Entry: entry1Updated},
		}
		verifyDelta(t, delta, expectedDelta)
	})

	t.Run("Delete", func(t *testing.T) {
		from := registry.State{entry1, entry2}
		to := registry.State{entry1}

		delta, err := builder.BuildDelta(from, to)
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}

		expectedDelta := registry.ChangeSet{
			{Kind: registry.EntryDelete, Entry: entry2},
		}
		verifyDelta(t, delta, expectedDelta)
	})

	t.Run("Mixed Operations", func(t *testing.T) {
		entry3 := testEntry{
			ns: "test", name: "service.cache",
			kind: "service", data: "data3",
		}.toEntry()

		from := registry.State{entry1, entry2}
		to := registry.State{entry1Updated, entry3}

		delta, err := builder.BuildDelta(from, to)
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}

		// Three operations on three different IDs (no inter-dependencies).
		// SortChangeSet normalizes input by (NS, Name, Kind) for determinism,
		// so the output is alphabetic by name: service.api, service.cache,
		// service.db.
		expectedDelta := registry.ChangeSet{
			{Kind: registry.EntryUpdate, Entry: entry1Updated},
			{Kind: registry.EntryCreate, Entry: entry3},
			{Kind: registry.EntryDelete, Entry: entry2},
		}
		verifyDelta(t, delta, expectedDelta)
	})
}

// Helper function to verify ChangeSet equality
func verifyDelta(t *testing.T, got, want registry.ChangeSet) {
	t.Helper()
	if !reflect.DeepEqual(got, want) {
		t.Errorf("unexpected delta:\ngot: %v\nwant: %v", formatDelta(got), formatDelta(want))
	}
}

func TestBuildDelta_Groups(t *testing.T) {
	builder := NewStateBuilder(zap.NewNop(), nil)

	t.Run("Spawn With Group Dependencies", func(t *testing.T) {
		// Spawn entries with group dependencies
		service1 := testEntry{
			ns: "test", name: "service1",
			kind: "service", data: "service1",
			groups: []string{"backend"},
		}.toEntry()

		service2 := testEntry{
			ns: "test", name: "service2",
			kind: "service", data: "service2",
			groups: []string{"backend"},
		}.toEntry()

		frontend := testEntry{
			ns: "test", name: "frontend",
			kind: "service", data: "frontend",
			dependsOn: []string{"group:backend"},
		}.toEntry()

		from := registry.State{}
		to := registry.State{frontend, service1, service2}

		delta, err := builder.BuildDelta(from, to)
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}

		// Verify dependency ordering
		validateDependencyOrder(t, delta, []struct {
			entry           registry.Entry
			mustBeforeNames []string
		}{
			{
				entry:           service1,
				mustBeforeNames: []string{"frontend"},
			},
			{
				entry:           service2,
				mustBeforeNames: []string{"frontend"},
			},
		})

		// Verify all entries are present, allowing any order within backend group
		expectedDelta := registry.ChangeSet{
			{Kind: registry.EntryCreate, Entry: service1},
			{Kind: registry.EntryCreate, Entry: service2},
			{Kind: registry.EntryCreate, Entry: frontend},
		}
		verifyDeltaWithinLevel(t, delta, expectedDelta)
	})

	t.Run("Delete With Group Dependencies", func(t *testing.T) {
		// Spawn entries with group dependencies
		service1 := testEntry{
			ns: "test", name: "service1",
			kind: "service", data: "service1",
			groups: []string{"backend"},
		}.toEntry()

		service2 := testEntry{
			ns: "test", name: "service2",
			kind: "service", data: "service2",
			groups: []string{"backend"},
		}.toEntry()

		frontend := testEntry{
			ns: "test", name: "frontend",
			kind: "service", data: "frontend",
			dependsOn: []string{"group:backend"},
		}.toEntry()

		from := registry.State{frontend, service1, service2}
		to := registry.State{}

		delta, err := builder.BuildDelta(from, to)
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}

		// Verify dependency ordering
		validateDependencyOrder(t, delta, []struct {
			entry           registry.Entry
			mustBeforeNames []string
		}{
			{
				entry:           frontend,
				mustBeforeNames: []string{"service1", "service2"},
			},
		})

		// Verify all entries are present
		expectedDelta := registry.ChangeSet{
			{Kind: registry.EntryDelete, Entry: frontend},
			{Kind: registry.EntryDelete, Entry: service1},
			{Kind: registry.EntryDelete, Entry: service2},
		}
		verifyDeltaWithinLevel(t, delta, expectedDelta)
	})

	t.Run("Mixed Group Operations", func(t *testing.T) {
		// Initial entries
		service1 := testEntry{
			ns: "test", name: "service1",
			kind: "service", data: "service1",
			groups: []string{"backend"},
		}.toEntry()

		service2 := testEntry{
			ns: "test", name: "service2",
			kind: "service", data: "service2",
			groups: []string{"backend"},
		}.toEntry()

		frontend := testEntry{
			ns: "test", name: "frontend",
			kind: "service", data: "frontend",
			dependsOn: []string{"group:backend"},
		}.toEntry()

		// Updated entries
		service1Updated := testEntry{
			ns: "test", name: "service1",
			kind: "service", data: "service1-v2",
			groups: []string{"backend"},
		}.toEntry()

		service3 := testEntry{
			ns: "test", name: "service3",
			kind: "service", data: "service3",
			groups: []string{"backend"},
		}.toEntry()

		frontendUpdated := testEntry{
			ns: "test", name: "frontend",
			kind: "service", data: "frontend-v2",
			dependsOn: []string{"group:backend"},
		}.toEntry()

		from := registry.State{frontend, service1, service2}
		to := registry.State{frontendUpdated, service1Updated, service3}

		delta, err := builder.BuildDelta(from, to)
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}

		// Verify operations maintain group dependencies
		validateDependencyOrder(t, delta, []struct {
			entry           registry.Entry
			mustBeforeNames []string
		}{
			{
				entry:           service1Updated,
				mustBeforeNames: []string{"frontend"},
			},
			{
				entry:           service3,
				mustBeforeNames: []string{"frontend"},
			},
		})

		// Delete and Updates can happen in any order, but creates must respect dependencies
		// No need to verify exact order, just presence and group dependency maintenance
		verifyDeltaWithinLevel(t, delta, registry.ChangeSet{
			{Kind: registry.EntryDelete, Entry: service2},
			{Kind: registry.EntryUpdate, Entry: service1Updated},
			{Kind: registry.EntryCreate, Entry: service3},
			{Kind: registry.EntryUpdate, Entry: frontendUpdated},
		})
	})
}

func TestBuildDelta_NamespaceDependencies(t *testing.T) {
	builder := NewStateBuilder(zap.NewNop(), nil)

	t.Run("Simple Namespace Dependencies", func(t *testing.T) {
		// Spawn entries in different namespaces
		infraDB := testEntry{
			ns: "infra", name: "database",
			kind: "service", data: "db",
		}.toEntry()

		infraCache := testEntry{
			ns: "infra", name: "cache",
			kind: "service", data: "cache",
		}.toEntry()

		appService := testEntry{
			ns: "app", name: "service",
			kind: "service", data: "service",
			dependsOn: []string{"ns:infra"},
		}.toEntry()

		from := registry.State{}
		to := registry.State{appService, infraDB, infraCache}

		delta, err := builder.BuildDelta(from, to)
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}

		// Verify namespace ordering
		validateNamespaceOrder(t, delta, map[string][]string{
			"app": {"infra"}, // app depends on infra
		})

		// Verify all entries are present without enforcing order within namespaces
		expectedDelta := registry.ChangeSet{
			{Kind: registry.EntryCreate, Entry: infraDB},
			{Kind: registry.EntryCreate, Entry: infraCache},
			{Kind: registry.EntryCreate, Entry: appService},
		}
		verifyDeltaWithinLevel(t, delta, expectedDelta)
	})

	t.Run("Mixed Namespace and Direct Dependencies", func(t *testing.T) {
		infraDB := testEntry{
			ns: "infra", name: "database",
			kind: "service", data: "db",
		}.toEntry()

		appAuth := testEntry{
			ns: "app", name: "auth",
			kind: "service", data: "auth",
			dependsOn: []string{"ns:infra"},
		}.toEntry()

		appService := testEntry{
			ns: "app", name: "service",
			kind: "service", data: "service",
			dependsOn: []string{"app:auth"},
		}.toEntry()

		from := registry.State{}
		to := registry.State{appService, appAuth, infraDB}

		delta, err := builder.BuildDelta(from, to)
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}

		// Verify both namespace and direct dependencies
		validateDependencyOrder(t, delta, []struct {
			entry           registry.Entry
			mustBeforeNames []string
		}{
			{
				entry:           infraDB,
				mustBeforeNames: []string{"auth", "service"},
			},
			{
				entry:           appAuth,
				mustBeforeNames: []string{"service"},
			},
		})

		// Verify all entries are present
		expectedDelta := registry.ChangeSet{
			{Kind: registry.EntryCreate, Entry: infraDB},
			{Kind: registry.EntryCreate, Entry: appAuth},
			{Kind: registry.EntryCreate, Entry: appService},
		}
		verifyDeltaWithinLevel(t, delta, expectedDelta)
	})

	t.Run("Delete With Namespace Dependencies", func(t *testing.T) {
		infraDB := testEntry{
			ns: "infra", name: "database",
			kind: "service", data: "db",
		}.toEntry()

		infraCache := testEntry{
			ns: "infra", name: "cache",
			kind: "service", data: "cache",
		}.toEntry()

		appAuth := testEntry{
			ns: "app", name: "auth",
			kind: "service", data: "auth",
			dependsOn: []string{"ns:infra"},
		}.toEntry()

		appService := testEntry{
			ns: "app", name: "service",
			kind: "service", data: "service",
			dependsOn: []string{"ns:infra", "app:auth"},
		}.toEntry()

		from := registry.State{appService, appAuth, infraDB, infraCache}
		to := registry.State{}

		delta, err := builder.BuildDelta(from, to)
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}

		// Verify deletion order respects namespace dependencies
		validateDependencyOrder(t, delta, []struct {
			entry           registry.Entry
			mustBeforeNames []string
		}{
			{
				entry:           appService,
				mustBeforeNames: []string{"auth", "database", "cache"},
			},
			{
				entry:           appAuth,
				mustBeforeNames: []string{"database", "cache"},
			},
		})

		// Verify all deletions are present
		expectedDelta := registry.ChangeSet{
			{Kind: registry.EntryDelete, Entry: appService},
			{Kind: registry.EntryDelete, Entry: appAuth},
			{Kind: registry.EntryDelete, Entry: infraDB},
			{Kind: registry.EntryDelete, Entry: infraCache},
		}
		verifyDeltaWithinLevel(t, delta, expectedDelta)
	})
}

// Helper function to validate namespace-level dependencies
func validateNamespaceOrder(t *testing.T, delta registry.ChangeSet, dependencies map[string][]string) {
	t.Helper()

	// Build map of first occurrence of each namespace
	nsFirstPos := make(map[string]int)
	nsLastPos := make(map[string]int)

	for i, op := range delta {
		ns := op.Entry.ID.NS
		if firstPos, exists := nsFirstPos[ns]; !exists || i < firstPos {
			nsFirstPos[ns] = i
		}
		if lastPos, exists := nsLastPos[ns]; !exists || i > lastPos {
			nsLastPos[ns] = i
		}
	}

	// Check namespace dependencies
	for ns, deps := range dependencies {
		nsPos, exists := nsFirstPos[ns]
		if !exists {
			t.Errorf("namespace %s not found in delta", ns)
			continue
		}

		for _, depNS := range deps {
			depLastPos, exists := nsLastPos[depNS]
			if !exists {
				t.Errorf("dependent namespace %s not found in delta", depNS)
				continue
			}

			if nsPos < depLastPos {
				t.Errorf("namespace order violation: %s (pos %d) must come after all entries in %s (last pos %d)",
					ns, nsPos, depNS, depLastPos)
			}
		}
	}
}

func TestBuildDelta_Dependencies(t *testing.T) {
	builder := NewStateBuilder(zap.NewNop(), nil)

	t.Run("Simple Dependencies", func(t *testing.T) {
		// Spawn entries with dependencies
		service := testEntry{
			ns: "test", name: "service",
			kind: "service", data: "service",
		}.toEntry()

		database := testEntry{
			ns: "test", name: "service.db",
			kind: "component", data: "database",
			dependsOn: []string{"service"},
		}.toEntry()

		cache := testEntry{
			ns: "test", name: "service.cache",
			kind: "component", data: "cache",
			dependsOn: []string{"service"},
		}.toEntry()

		from := registry.State{}
		to := registry.State{service, database, cache}

		delta, err := builder.BuildDelta(from, to)
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}

		// Verify dependency ordering
		validateDependencyOrder(t, delta, []struct {
			entry           registry.Entry
			mustBeforeNames []string
		}{
			{
				entry:           service,
				mustBeforeNames: []string{"service.db", "service.cache"},
			},
		})

		// Also verify all entries are present
		expectedDelta := registry.ChangeSet{
			{Kind: registry.EntryCreate, Entry: service},
			{Kind: registry.EntryCreate, Entry: database},
			{Kind: registry.EntryCreate, Entry: cache},
		}
		verifyDeltaWithinLevel(t, delta, expectedDelta)
	})

	t.Run("Chained Dependencies", func(t *testing.T) {
		// Spawn entries with chained dependencies
		service := testEntry{
			ns: "test", name: "service",
			kind: "service", data: "service",
		}.toEntry()

		database := testEntry{
			ns: "test", name: "service.db",
			kind: "component", data: "database",
			dependsOn: []string{"service"},
		}.toEntry()

		backup := testEntry{
			ns: "test", name: "service.db.backup",
			kind: "component", data: "backup",
			dependsOn: []string{"service.db"},
		}.toEntry()

		from := registry.State{}
		to := registry.State{backup, service, database}

		delta, err := builder.BuildDelta(from, to)
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}

		// Verify dependency ordering
		validateDependencyOrder(t, delta, []struct {
			entry           registry.Entry
			mustBeforeNames []string
		}{
			{
				entry:           service,
				mustBeforeNames: []string{"service.db", "service.db.backup"},
			},
			{
				entry:           database,
				mustBeforeNames: []string{"service.db.backup"},
			},
		})

		expectedDelta := registry.ChangeSet{
			{Kind: registry.EntryCreate, Entry: service},
			{Kind: registry.EntryCreate, Entry: database},
			{Kind: registry.EntryCreate, Entry: backup},
		}
		verifyDeltaWithinLevel(t, delta, expectedDelta)
	})

	t.Run("Delete With Dependencies", func(t *testing.T) {
		// Spawn entries with dependencies
		service := testEntry{
			ns: "test", name: "service",
			kind: "service", data: "service",
		}.toEntry()

		database := testEntry{
			ns: "test", name: "service.db",
			kind: "component", data: "database",
			dependsOn: []string{"service"},
		}.toEntry()

		from := registry.State{service, database}
		to := registry.State{}

		delta, err := builder.BuildDelta(from, to)
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}

		// Database should be deleted before service
		validateDependencyOrder(t, delta, []struct {
			entry           registry.Entry
			mustBeforeNames []string
		}{
			{
				entry:           database,
				mustBeforeNames: []string{"service"},
			},
		})

		expectedDelta := registry.ChangeSet{
			{Kind: registry.EntryDelete, Entry: database},
			{Kind: registry.EntryDelete, Entry: service},
		}
		verifyDeltaWithinLevel(t, delta, expectedDelta)
	})
}

// validateDependencyOrder checks if entries respect their dependency order
func validateDependencyOrder(t *testing.T, delta registry.ChangeSet, checks []struct {
	entry           registry.Entry
	mustBeforeNames []string
}) {
	t.Helper()

	// Build map of entry positions
	posMap := make(map[string]int)
	for i, op := range delta {
		posMap[op.Entry.ID.Name] = i
	}

	// Check each dependency requirement
	for _, check := range checks {
		entryPos, exists := posMap[check.entry.ID.Name]
		if !exists {
			t.Errorf("entry %s not found in delta", check.entry.ID.Name)
			continue
		}

		for _, mustAfterName := range check.mustBeforeNames {
			dependentPos, exists := posMap[mustAfterName]
			if !exists {
				t.Errorf("dependent entry %s not found in delta", mustAfterName)
				continue
			}

			if entryPos > dependentPos {
				t.Errorf("dependency order violation: %s (pos %d) must come before %s (pos %d)",
					check.entry.ID.Name, entryPos,
					mustAfterName, dependentPos)
			}
		}
	}
}

func TestBuildDelta_CircularDependencies(t *testing.T) {
	builder := NewStateBuilder(zap.NewNop(), nil)

	tests := []struct {
		name    string
		entries []registry.Entry
		from    registry.State
		to      registry.State
	}{
		{
			name: "simple circular dependency",
			from: registry.State{},
			to: registry.State{
				makeEntryWithMeta(
					nsID("test", "service.a"),
					"service",
					"a",
					map[string]any{
						registry.TagDependsOn: []string{"service.b"},
					},
				),
				makeEntryWithMeta(
					nsID("test", "service.b"),
					"service",
					"b",
					map[string]any{
						registry.TagDependsOn: []string{"service.a"},
					},
				),
			},
		},
		{
			name: "circular dependency through groups",
			from: registry.State{},
			to: registry.State{
				makeEntryWithMeta(
					nsID("test", "service.a"),
					"service",
					"a",
					map[string]any{
						registry.TagGroups:    []string{"group-a"},
						registry.TagDependsOn: []string{"group:group-b"},
					},
				),
				makeEntryWithMeta(
					nsID("test", "service.b"),
					"service",
					"b",
					map[string]any{
						registry.TagGroups:    []string{"group-b"},
						registry.TagDependsOn: []string{"group:group-a"},
					},
				),
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			delta, err := builder.BuildDelta(tt.from, tt.to)
			if err != nil && !strings.Contains(err.Error(), "cycle detected") {
				t.Errorf("unexpected error type: %v", err)
			}
			if err == nil && len(delta) != len(tt.to) {
				t.Errorf("expected %d operations despite circular dependency, got %d", len(tt.to), len(delta))
			}
		})
	}
}

func TestBuildDelta_ComplexTransformations(t *testing.T) {
	builder := NewStateBuilder(zap.NewNop(), nil)

	t.Run("Mixed Dependency Types", func(t *testing.T) {
		// Base infrastructure
		infraDB := testEntry{
			ns: "infra", name: "database",
			kind: "service", data: "db",
			groups: []string{"storage"},
		}.toEntry()

		infraCache := testEntry{
			ns: "infra", name: "cache",
			kind: "service", data: "cache",
			groups: []string{"storage"},
		}.toEntry()

		// App services
		appAuth := testEntry{
			ns: "app", name: "auth",
			kind: "service", data: "auth",
			dependsOn: []string{"ns:infra", "group:storage"},
		}.toEntry()

		appAPI := testEntry{
			ns: "app", name: "api",
			kind: "service", data: "api",
			dependsOn: []string{"app:auth"},
		}.toEntry()

		// Frontend services
		webUI := testEntry{
			ns: "web", name: "ui",
			kind: "service", data: "ui",
			dependsOn: []string{"app:api"},
		}.toEntry()

		from := registry.State{}
		to := registry.State{webUI, appAPI, appAuth, infraCache, infraDB}

		delta, err := builder.BuildDelta(from, to)
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}

		// Verify dependency ordering
		validateDependencyOrder(t, delta, []struct {
			entry           registry.Entry
			mustBeforeNames []string
		}{
			{
				entry:           infraDB,
				mustBeforeNames: []string{"auth", "api", "ui"},
			},
			{
				entry:           infraCache,
				mustBeforeNames: []string{"auth", "api", "ui"},
			},
			{
				entry:           appAuth,
				mustBeforeNames: []string{"api", "ui"},
			},
			{
				entry:           appAPI,
				mustBeforeNames: []string{"ui"},
			},
		})

		// Verify operations
		expectedDelta := registry.ChangeSet{
			{Kind: registry.EntryCreate, Entry: infraDB},
			{Kind: registry.EntryCreate, Entry: infraCache},
			{Kind: registry.EntryCreate, Entry: appAuth},
			{Kind: registry.EntryCreate, Entry: appAPI},
			{Kind: registry.EntryCreate, Entry: webUI},
		}
		verifyDeltaWithinLevel(t, delta, expectedDelta)
	})

	t.Run("Complex Update Scenario", func(t *testing.T) {
		// Original entries
		infraDB := testEntry{
			ns: "infra", name: "database",
			kind: "service", data: "db-v1",
			groups: []string{"storage"},
		}.toEntry()

		appService := testEntry{
			ns: "app", name: "service",
			kind: "service", data: "service-v1",
			dependsOn: []string{"ns:infra", "group:storage"},
		}.toEntry()

		// Updated entries
		infraDBv2 := testEntry{
			ns: "infra", name: "database",
			kind: "service", data: "db-v2",
			groups: []string{"storage"},
		}.toEntry()

		appServicev2 := testEntry{
			ns: "app", name: "service",
			kind: "service", data: "service-v2",
			dependsOn: []string{"ns:infra", "group:storage"},
		}.toEntry()

		// New entry
		monitoring := testEntry{
			ns: "infra", name: "monitoring",
			kind: "service", data: "monitoring",
			dependsOn: []string{"group:storage"},
		}.toEntry()

		from := registry.State{infraDB, appService}
		to := registry.State{infraDBv2, appServicev2, monitoring}

		delta, err := builder.BuildDelta(from, to)
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}

		// Verify dependency ordering
		validateDependencyOrder(t, delta, []struct {
			entry           registry.Entry
			mustBeforeNames []string
		}{
			{
				entry:           infraDBv2,
				mustBeforeNames: []string{"service", "monitoring"},
			},
		})

		// Verify operations
		expectedDelta := registry.ChangeSet{
			{Kind: registry.EntryUpdate, Entry: infraDBv2},
			{Kind: registry.EntryUpdate, Entry: appServicev2},
			{Kind: registry.EntryCreate, Entry: monitoring},
		}
		verifyDeltaWithinLevel(t, delta, expectedDelta)
	})

	t.Run("Complex Delete Scenario", func(t *testing.T) {
		// Spawn a complex dependency tree
		storage := testEntry{
			ns: "infra", name: "storage",
			kind: "service", data: "storage",
			groups: []string{"core"},
		}.toEntry()

		database := testEntry{
			ns: "infra", name: "database",
			kind: "service", data: "database",
			dependsOn: []string{"group:core"},
		}.toEntry()

		cache := testEntry{
			ns: "infra", name: "cache",
			kind: "service", data: "cache",
			dependsOn: []string{"group:core"},
		}.toEntry()

		auth := testEntry{
			ns: "app", name: "auth",
			kind: "service", data: "auth",
			dependsOn: []string{"ns:infra"},
		}.toEntry()

		api := testEntry{
			ns: "app", name: "api",
			kind: "service", data: "api",
			dependsOn: []string{"app:auth"},
		}.toEntry()

		from := registry.State{api, auth, cache, database, storage}
		to := registry.State{storage} // Keep only core storage

		delta, err := builder.BuildDelta(from, to)
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}

		// Verify dependency ordering
		validateDependencyOrder(t, delta, []struct {
			entry           registry.Entry
			mustBeforeNames []string
		}{
			{
				entry:           api,
				mustBeforeNames: []string{"auth"},
			},
			{
				entry:           auth,
				mustBeforeNames: []string{"database", "cache"},
			},
		})

		// Verify operations - note that order between cache and database doesn't matter
		expectedDelta := registry.ChangeSet{
			{Kind: registry.EntryDelete, Entry: api},
			{Kind: registry.EntryDelete, Entry: auth},
			{Kind: registry.EntryDelete, Entry: cache},
			{Kind: registry.EntryDelete, Entry: database},
		}
		verifyDeltaWithinLevel(t, delta, expectedDelta)
	})

	t.Run("Mixed Operations With Dependencies", func(t *testing.T) {
		// Original entries
		database := testEntry{
			ns: "infra", name: "database",
			kind: "service", data: "db-v1",
		}.toEntry()

		auth := testEntry{
			ns: "app", name: "auth",
			kind: "service", data: "auth-v1",
			dependsOn: []string{"infra:database"},
		}.toEntry()

		api := testEntry{
			ns: "app", name: "api",
			kind: "service", data: "api-v1",
			dependsOn: []string{"app:auth"},
		}.toEntry()

		// Updated/new entries
		databaseV2 := testEntry{
			ns: "infra", name: "database",
			kind: "service", data: "db-v2",
		}.toEntry()

		authV2 := testEntry{
			ns: "app", name: "auth",
			kind: "service", data: "auth-v2",
			dependsOn: []string{"infra:database"},
		}.toEntry()

		monitoring := testEntry{
			ns: "infra", name: "monitoring",
			kind: "service", data: "monitoring",
			dependsOn: []string{"infra:database"},
		}.toEntry()

		from := registry.State{database, auth, api}
		to := registry.State{databaseV2, authV2, monitoring}

		delta, err := builder.BuildDelta(from, to)
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}

		// Verify dependency ordering
		validateDependencyOrder(t, delta, []struct {
			entry           registry.Entry
			mustBeforeNames []string
		}{
			{
				entry:           api,
				mustBeforeNames: []string{"auth", "database"},
			},
			{
				entry:           databaseV2,
				mustBeforeNames: []string{"monitoring", "auth"},
			},
		})

		// Verify operations
		expectedDelta := registry.ChangeSet{
			{Kind: registry.EntryDelete, Entry: api},
			{Kind: registry.EntryUpdate, Entry: databaseV2},
			{Kind: registry.EntryUpdate, Entry: authV2},
			{Kind: registry.EntryCreate, Entry: monitoring},
		}
		verifyDeltaWithinLevel(t, delta, expectedDelta)
	})
}

func TestBuildDelta_RollbackToEmptyState(t *testing.T) {
	builder := NewStateBuilder(zap.NewNop(), nil)

	t.Run("HTTP-like hierarchy rollback", func(t *testing.T) {
		// Simulates HTTP server -> router -> endpoint -> static handler hierarchy
		server := testEntry{
			ns: "app", name: "gateway",
			kind: "http.service", data: "server",
		}.toEntry()

		router := testEntry{
			ns: "app", name: "api",
			kind: "http.router", data: "router",
			dependsOn: []string{"gateway"},
		}.toEntry()

		endpoint := testEntry{
			ns: "app", name: "hello",
			kind: "http.endpoint", data: "endpoint",
			dependsOn: []string{"api"},
		}.toEntry()

		staticHandler := testEntry{
			ns: "app", name: "frontend",
			kind: "http.static", data: "static",
			dependsOn: []string{"gateway"},
		}.toEntry()

		// Rollback scenario: from populated state to empty
		from := registry.State{server, router, endpoint, staticHandler}
		to := registry.State{}

		delta, err := builder.BuildDelta(from, to)
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}

		// All operations should be deletes
		if len(delta) != 4 {
			t.Fatalf("expected 4 delete operations, got %d", len(delta))
		}

		for _, op := range delta {
			if op.Kind != registry.EntryDelete {
				t.Errorf("expected delete operation, got %s", op.Kind)
			}
		}

		// Verify delete order: dependents before dependencies
		// endpoint must be deleted before router
		// router must be deleted before server
		// staticHandler must be deleted before server
		validateDependencyOrder(t, delta, []struct {
			entry           registry.Entry
			mustBeforeNames []string
		}{
			{entry: endpoint, mustBeforeNames: []string{"api", "gateway"}},
			{entry: router, mustBeforeNames: []string{"gateway"}},
			{entry: staticHandler, mustBeforeNames: []string{"gateway"}},
		})
	})

	t.Run("Deep dependency chain rollback", func(t *testing.T) {
		// A -> B -> C -> D (D depends on C, C on B, B on A)
		entryA := testEntry{
			ns: "test", name: "a",
			kind: "service", data: "a",
		}.toEntry()

		entryB := testEntry{
			ns: "test", name: "b",
			kind: "service", data: "b",
			dependsOn: []string{"a"},
		}.toEntry()

		entryC := testEntry{
			ns: "test", name: "c",
			kind: "service", data: "c",
			dependsOn: []string{"b"},
		}.toEntry()

		entryD := testEntry{
			ns: "test", name: "d",
			kind: "service", data: "d",
			dependsOn: []string{"c"},
		}.toEntry()

		from := registry.State{entryA, entryB, entryC, entryD}
		to := registry.State{}

		delta, err := builder.BuildDelta(from, to)
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}

		// Must delete in order: D, C, B, A
		validateDependencyOrder(t, delta, []struct {
			entry           registry.Entry
			mustBeforeNames []string
		}{
			{entry: entryD, mustBeforeNames: []string{"c", "b", "a"}},
			{entry: entryC, mustBeforeNames: []string{"b", "a"}},
			{entry: entryB, mustBeforeNames: []string{"a"}},
		})
	})

	t.Run("Diamond dependency rollback", func(t *testing.T) {
		// Diamond: A -> B, A -> C, B -> D, C -> D
		entryA := testEntry{
			ns: "test", name: "a",
			kind: "service", data: "a",
		}.toEntry()

		entryB := testEntry{
			ns: "test", name: "b",
			kind: "service", data: "b",
			dependsOn: []string{"a"},
		}.toEntry()

		entryC := testEntry{
			ns: "test", name: "c",
			kind: "service", data: "c",
			dependsOn: []string{"a"},
		}.toEntry()

		entryD := testEntry{
			ns: "test", name: "d",
			kind: "service", data: "d",
			dependsOn: []string{"b", "c"},
		}.toEntry()

		from := registry.State{entryA, entryB, entryC, entryD}
		to := registry.State{}

		delta, err := builder.BuildDelta(from, to)
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}

		// D must be deleted first, then B and C (any order), then A
		validateDependencyOrder(t, delta, []struct {
			entry           registry.Entry
			mustBeforeNames []string
		}{
			{entry: entryD, mustBeforeNames: []string{"b", "c", "a"}},
			{entry: entryB, mustBeforeNames: []string{"a"}},
			{entry: entryC, mustBeforeNames: []string{"a"}},
		})
	})

	t.Run("Multiple independent trees rollback", func(t *testing.T) {
		// Tree 1: server1 -> router1
		server1 := testEntry{
			ns: "app1", name: "server",
			kind: "http.service", data: "server1",
		}.toEntry()

		router1 := testEntry{
			ns: "app1", name: "router",
			kind: "http.router", data: "router1",
			dependsOn: []string{"server"},
		}.toEntry()

		// Tree 2: server2 -> router2
		server2 := testEntry{
			ns: "app2", name: "server",
			kind: "http.service", data: "server2",
		}.toEntry()

		router2 := testEntry{
			ns: "app2", name: "router",
			kind: "http.router", data: "router2",
			dependsOn: []string{"server"},
		}.toEntry()

		from := registry.State{server1, router1, server2, router2}
		to := registry.State{}

		delta, err := builder.BuildDelta(from, to)
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}

		// Each router must be deleted before its server
		validateDependencyOrder(t, delta, []struct {
			entry           registry.Entry
			mustBeforeNames []string
		}{
			{entry: router1, mustBeforeNames: []string{"server"}},
			{entry: router2, mustBeforeNames: []string{"server"}},
		})
	})

	t.Run("Namespace dependency rollback", func(t *testing.T) {
		// infra namespace with db and cache
		infraDB := testEntry{
			ns: "infra", name: "db",
			kind: "service", data: "db",
		}.toEntry()

		infraCache := testEntry{
			ns: "infra", name: "cache",
			kind: "service", data: "cache",
		}.toEntry()

		// app namespace depends on infra
		appService := testEntry{
			ns: "app", name: "service",
			kind: "service", data: "service",
			dependsOn: []string{"ns:infra"},
		}.toEntry()

		from := registry.State{infraDB, infraCache, appService}
		to := registry.State{}

		delta, err := builder.BuildDelta(from, to)
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}

		// app:service must be deleted before any infra entry
		validateDependencyOrder(t, delta, []struct {
			entry           registry.Entry
			mustBeforeNames []string
		}{
			{entry: appService, mustBeforeNames: []string{"db", "cache"}},
		})
	})

	t.Run("Group dependency rollback", func(t *testing.T) {
		// Storage group
		db := testEntry{
			ns: "infra", name: "db",
			kind: "service", data: "db",
			groups: []string{"storage"},
		}.toEntry()

		cache := testEntry{
			ns: "infra", name: "cache",
			kind: "service", data: "cache",
			groups: []string{"storage"},
		}.toEntry()

		// Service depends on storage group
		service := testEntry{
			ns: "app", name: "service",
			kind: "service", data: "service",
			dependsOn: []string{"group:storage"},
		}.toEntry()

		from := registry.State{db, cache, service}
		to := registry.State{}

		delta, err := builder.BuildDelta(from, to)
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}

		// service must be deleted before any storage group member
		validateDependencyOrder(t, delta, []struct {
			entry           registry.Entry
			mustBeforeNames []string
		}{
			{entry: service, mustBeforeNames: []string{"db", "cache"}},
		})
	})
}
