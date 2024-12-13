package registry

import (
	"fmt"
	"github.com/ponyruntime/pony/api/payload"
	"github.com/ponyruntime/pony/api/registry"
	"github.com/ponyruntime/pony/internal/version"
	"go.uber.org/zap"
	"reflect"
	"testing"
)

// MockHistory is a mock implementation of the registry.History interface for testing.
type MockHistory struct {
	versions  map[registry.Version]registry.ChangeSet
	head      registry.Version
	callStack []string
}

func (m *MockHistory) Versions() ([]registry.Version, error) {
	m.callStack = append(m.callStack, "Versions")
	var vs []registry.Version
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

func NewMockHistory() *MockHistory {
	return &MockHistory{
		versions:  make(map[registry.Version]registry.ChangeSet),
		callStack: make([]string, 0),
	}
}

func TestStateBuilder_BuildState_HappyPath(t *testing.T) {
	// Create versions using version.New and version.FromParent.
	v0 := version.New(registry.RootVersion) // Root version
	v1 := version.FromParent(v0, 1)
	v2 := version.FromParent(v1, 2)
	v3 := version.FromParent(v2, 3)

	// Create some sample entries.
	entry1 := registry.Entry{Path: "/path/1", Kind: "kind1", Data: payload.New("data1")}
	entry2 := registry.Entry{Path: "/path/2", Kind: "kind2", Data: payload.New("data2")}
	entry3 := registry.Entry{Path: "/path/3", Kind: "kind3", Data: payload.New("data3")}

	// Create the mock history.
	history := NewMockHistory()
	_ = history.Save(v0, registry.ChangeSet{}, false)
	_ = history.Save(v1, registry.ChangeSet{
		{Kind: registry.Create, Entry: entry1},
	}, false)
	_ = history.Save(v2, registry.ChangeSet{
		{Kind: registry.Update, Entry: entry1}, // Update entry1
		{Kind: registry.Create, Entry: entry2},
	}, false)
	_ = history.Save(v3, registry.ChangeSet{
		{Kind: registry.Delete, Entry: entry2}, // Delete entry2
		{Kind: registry.Create, Entry: entry3},
	}, false)

	// Create the StateBuilder.
	builder := NewStateBuilder(zap.NewNop())

	// Build the state up to v3.
	targetVersion := v3
	state, err := builder.BuildState(history, targetVersion)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	// Verify the state.
	expectedState := registry.State{entry1, entry3} // entry2 was deleted
	if !reflect.DeepEqual(state, expectedState) {
		t.Errorf("unexpected state.\ngot: %v\nwant: %v", state, expectedState)
	}

	// Also very call stack
	expectedCallStack := []string{"Save", "Save", "Save", "Save", "Versions", "Get(0)", "Get(1)", "Get(2)", "Get(3)"}
	if !reflect.DeepEqual(history.callStack, expectedCallStack) {
		t.Errorf("unexpected call stack.\ngot: %v\nwant: %v", history.callStack, expectedCallStack)
	}
}

func TestStateBuilder_BuildState_EmptyHistory(t *testing.T) {
	v0 := version.New(registry.RootVersion)

	history := NewMockHistory()
	_ = history.Save(v0, registry.ChangeSet{}, false)

	builder := NewStateBuilder(zap.NewNop())

	// Build state from an empty history
	state, err := builder.BuildState(history, v0)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	expectedState := registry.State{} // Should be an empty state
	if !reflect.DeepEqual(state, expectedState) {
		t.Errorf("unexpected state.\ngot: %v\nwant: %v", state, expectedState)
	}

	expectedCallStack := []string{"Save", "Versions", "Get(0)"}
	if !reflect.DeepEqual(history.callStack, expectedCallStack) {
		t.Errorf("unexpected call stack.\ngot: %v\nwant: %v", history.callStack, expectedCallStack)
	}
}

func TestStateBuilder_BuildState_UpdateDeleteNonExistent(t *testing.T) {
	v0 := version.New(registry.RootVersion)
	v1 := version.FromParent(v0, 1)
	v2 := version.FromParent(v1, 2)

	entry1 := registry.Entry{Path: "/path/1", Kind: "kind1", Data: payload.New("data1")}

	history := NewMockHistory()
	_ = history.Save(v0, registry.ChangeSet{}, false)
	_ = history.Save(v1, registry.ChangeSet{
		{Kind: registry.Update, Entry: entry1}, // Update on non-existent entry
	}, false)
	_ = history.Save(v2, registry.ChangeSet{
		{Kind: registry.Delete, Entry: entry1}, // Delete on non-existent entry
	}, false)

	builder := NewStateBuilder(zap.NewNop())

	// Build state up to v2
	state, err := builder.BuildState(history, v2)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	expectedState := registry.State{} // No entries should exist
	if !reflect.DeepEqual(state, expectedState) {
		t.Errorf("unexpected state.\ngot: %v\nwant: %v", state, expectedState)
	}

	expectedCallStack := []string{"Save", "Save", "Save", "Versions", "Get(0)", "Get(1)", "Get(2)"}
	if !reflect.DeepEqual(history.callStack, expectedCallStack) {
		t.Errorf("unexpected call stack.\ngot: %v\nwant: %v", history.callStack, expectedCallStack)
	}
}

type ErrorMockHistory struct {
	*MockHistory
	err error
}

func (m *ErrorMockHistory) Versions() ([]registry.Version, error) {
	return nil, m.err
}

func TestStateBuilder_BuildState_HistoryError(t *testing.T) {
	expectedError := fmt.Errorf("history error")
	history := &ErrorMockHistory{
		MockHistory: NewMockHistory(),
		err:         expectedError,
	}

	builder := NewStateBuilder(zap.NewNop())

	// Attempt to build state from a history that returns an error
	_, err := builder.BuildState(history, version.New(1))
	if err == nil {
		t.Fatalf("expected error, got nil")
	}

	if err.Error() != fmt.Sprintf("failed to get versions from history: %v", expectedError) {
		t.Errorf("unexpected error.\ngot: %v\nwant: %v", err, expectedError)
	}
}

func TestStateBuilder_BuildState_ConflictingCreates(t *testing.T) {
	v0 := version.New(registry.RootVersion)
	v1 := version.FromParent(v0, 1)
	v2 := version.FromParent(v1, 2)

	entry1 := registry.Entry{Path: "/path/1", Kind: "kind1", Data: payload.New("data1")}

	history := NewMockHistory()
	_ = history.Save(v0, registry.ChangeSet{}, false)
	_ = history.Save(v1, registry.ChangeSet{
		{Kind: registry.Create, Entry: entry1},
	}, false)
	_ = history.Save(v2, registry.ChangeSet{
		{Kind: registry.Create, Entry: entry1}, // Conflicting create
	}, false)

	builder := NewStateBuilder(zap.NewNop())

	// Build state up to v2
	state, err := builder.BuildState(history, v2)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	expectedState := registry.State{entry1} // Only the first create should take effect
	if !reflect.DeepEqual(state, expectedState) {
		t.Errorf("unexpected state.\ngot: %v\nwant: %v", state, expectedState)
	}

	expectedCallStack := []string{"Save", "Save", "Save", "Versions", "Get(0)", "Get(1)", "Get(2)"}
	if !reflect.DeepEqual(history.callStack, expectedCallStack) {
		t.Errorf("unexpected call stack.\ngot: %v\nwant: %v", history.callStack, expectedCallStack)
	}
}

func TestStateBuilder_BuildState_IntermediateVersion(t *testing.T) {
	v0 := version.New(registry.RootVersion)
	v1 := version.FromParent(v0, 1)
	v2 := version.FromParent(v1, 2)
	v3 := version.FromParent(v2, 3)

	entry1 := registry.Entry{Path: "/path/1", Kind: "kind1", Data: payload.New("data1")}
	entry2 := registry.Entry{Path: "/path/2", Kind: "kind2", Data: payload.New("data2")}

	history := NewMockHistory()
	_ = history.Save(v0, registry.ChangeSet{}, false)
	_ = history.Save(v1, registry.ChangeSet{
		{Kind: registry.Create, Entry: entry1},
	}, false)
	_ = history.Save(v2, registry.ChangeSet{
		{Kind: registry.Update, Entry: entry1},
		{Kind: registry.Create, Entry: entry2},
	}, false)
	_ = history.Save(v3, registry.ChangeSet{
		{Kind: registry.Delete, Entry: entry2},
	}, false)

	builder := NewStateBuilder(zap.NewNop())

	// Build state up to v2 (intermediate version)
	state, err := builder.BuildState(history, v2)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	expectedState := registry.State{entry1, entry2} // Should reflect state at v2
	if !reflect.DeepEqual(state, expectedState) {
		t.Errorf("unexpected state.\ngot: %v\nwant: %v", state, expectedState)
	}

	expectedCallStack := []string{"Save", "Save", "Save", "Save", "Versions", "Get(0)", "Get(1)", "Get(2)"}
	if !reflect.DeepEqual(history.callStack, expectedCallStack) {
		t.Errorf("unexpected call stack.\ngot: %v\nwant: %v", history.callStack, expectedCallStack)
	}
}

func TestStateBuilder_BuildState_UnreachableVersion(t *testing.T) {
	v0 := version.New(registry.RootVersion)
	v1 := version.FromParent(v0, 1)
	v2 := version.New(2) // Create a version not connected to the main branch

	history := NewMockHistory()
	_ = history.Save(v0, registry.ChangeSet{}, false)
	_ = history.Save(v1, registry.ChangeSet{}, false)
	_ = history.Save(v2, registry.ChangeSet{}, false) // This version is not reachable

	builder := NewStateBuilder(zap.NewNop())

	// Attempt to build state to the unreachable version
	_, err := builder.BuildState(history, v2)
	if err == nil {
		t.Fatalf("expected error, got nil")
	}

	expectedErrMsg := fmt.Sprintf("failed to get path from root to version %v: no path found", v2)
	if err.Error() != expectedErrMsg {
		t.Errorf("unexpected error message.\ngot: %v\nwant: %v", err, expectedErrMsg)
	}

	expectedCallStack := []string{"Save", "Save", "Save", "Versions"}
	if !reflect.DeepEqual(history.callStack, expectedCallStack) {
		t.Errorf("unexpected call stack.\ngot: %v\nwant: %v", history.callStack, expectedCallStack)
	}
}

type GetErrorMockHistory struct {
	*MockHistory
	getError error
}

func (m *GetErrorMockHistory) Get(v registry.Version) (registry.ChangeSet, error) {
	if v.ID() == 1 {
		return nil, m.getError
	}
	return m.MockHistory.Get(v)
}

func TestStateBuilder_BuildState_GetError(t *testing.T) {
	v0 := version.New(registry.RootVersion)
	v1 := version.FromParent(v0, 1)

	entry1 := registry.Entry{Path: "/path/1", Kind: "kind1", Data: payload.New("data1")}

	history := &GetErrorMockHistory{
		MockHistory: NewMockHistory(),
		getError:    fmt.Errorf("get error"),
	}

	_ = history.Save(v0, registry.ChangeSet{}, false)
	_ = history.Save(v1, registry.ChangeSet{
		{Kind: registry.Create, Entry: entry1},
	}, false)

	builder := NewStateBuilder(zap.NewNop())

	// Attempt to build state, expecting an error from history.Get()
	_, err := builder.BuildState(history, v1)
	if err == nil {
		t.Fatalf("expected error, got nil")
	}

	expectedErrMsg := fmt.Sprintf("failed to get changeset for version %v: %v", v1, history.getError)
	if err.Error() != expectedErrMsg {
		t.Errorf("unexpected error.\ngot: %v\nwant: %v", err, expectedErrMsg)
	}
}

func TestStateBuilder_BuildDelta_SimpleCreates(t *testing.T) {
	v0 := version.New(registry.RootVersion)
	v1 := version.FromParent(v0, 1)

	entry1 := registry.Entry{Path: "/path/1", Kind: "kind1", Data: payload.New("data1")}

	history := NewMockHistory()
	_ = history.Save(v0, registry.ChangeSet{}, false)
	_ = history.Save(v1, registry.ChangeSet{
		{Kind: registry.Create, Entry: entry1},
	}, false)

	builder := NewStateBuilder(zap.NewNop())

	delta, err := builder.BuildDelta(history, v0, v1)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	expectedDelta := registry.ChangeSet{
		{Kind: registry.Create, Entry: entry1},
	}

	if !reflect.DeepEqual(delta, expectedDelta) {
		t.Errorf("unexpected delta.\ngot: %v\nwant: %v", delta, expectedDelta)
	}
	expectedCallStack := []string{"Save", "Save", "Versions", "Get(0)", "Versions", "Get(0)", "Get(1)"}
	if !reflect.DeepEqual(history.callStack, expectedCallStack) {
		t.Errorf("unexpected call stack.\ngot: %v\nwant: %v", history.callStack, expectedCallStack)
	}
}

func TestStateBuilder_BuildDelta_SimpleUpdates(t *testing.T) {
	v0 := version.New(registry.RootVersion)
	v1 := version.FromParent(v0, 1)
	v2 := version.FromParent(v1, 2)

	entry1 := registry.Entry{Path: "/path/1", Kind: "kind1", Data: payload.New("data1")}
	entry1Updated := registry.Entry{Path: "/path/1", Kind: "kind1", Data: payload.New("data2")}

	history := NewMockHistory()
	_ = history.Save(v0, registry.ChangeSet{}, false)
	_ = history.Save(v1, registry.ChangeSet{
		{Kind: registry.Create, Entry: entry1},
	}, false)
	_ = history.Save(v2, registry.ChangeSet{
		{Kind: registry.Update, Entry: entry1Updated},
	}, false)

	builder := NewStateBuilder(zap.NewNop())

	delta, err := builder.BuildDelta(history, v1, v2)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	expectedDelta := registry.ChangeSet{
		{Kind: registry.Update, Entry: entry1Updated},
	}

	if !reflect.DeepEqual(delta, expectedDelta) {
		t.Errorf("unexpected delta.\ngot: %v\nwant: %v", delta, expectedDelta)
	}
	expectedCallStack := []string{"Save", "Save", "Save", "Versions", "Get(0)", "Get(1)", "Versions", "Get(0)", "Get(1)", "Get(2)"}
	if !reflect.DeepEqual(history.callStack, expectedCallStack) {
		t.Errorf("unexpected call stack.\ngot: %v\nwant: %v", history.callStack, expectedCallStack)
	}
}

func TestStateBuilder_BuildDelta_SimpleDeletes(t *testing.T) {
	v0 := version.New(registry.RootVersion)
	v1 := version.FromParent(v0, 1)
	v2 := version.FromParent(v1, 2)

	entry1 := registry.Entry{Path: "/path/1", Kind: "kind1", Data: payload.New("data1")}

	history := NewMockHistory()
	_ = history.Save(v0, registry.ChangeSet{}, false)
	_ = history.Save(v1, registry.ChangeSet{
		{Kind: registry.Create, Entry: entry1},
	}, false)
	_ = history.Save(v2, registry.ChangeSet{
		{Kind: registry.Delete, Entry: entry1},
	}, false)

	builder := NewStateBuilder(zap.NewNop())

	delta, err := builder.BuildDelta(history, v1, v2)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	expectedDelta := registry.ChangeSet{
		{Kind: registry.Delete, Entry: entry1},
	}

	if !reflect.DeepEqual(delta, expectedDelta) {
		t.Errorf("unexpected delta.\ngot: %v\nwant: %v", delta, expectedDelta)
	}
	expectedCallStack := []string{"Save", "Save", "Save", "Versions", "Get(0)", "Get(1)", "Versions", "Get(0)", "Get(1)", "Get(2)"}
	if !reflect.DeepEqual(history.callStack, expectedCallStack) {
		t.Errorf("unexpected call stack.\ngot: %v\nwant: %v", history.callStack, expectedCallStack)
	}
}

func TestStateBuilder_BuildDelta_MixedOperations(t *testing.T) {
	v0 := version.New(registry.RootVersion)
	v1 := version.FromParent(v0, 1)
	v2 := version.FromParent(v1, 2)

	entry1 := registry.Entry{Path: "/path/1", Kind: "kind1", Data: payload.New("data1")}
	entry2 := registry.Entry{Path: "/path/2", Kind: "kind2", Data: payload.New("data2")}
	entry2Updated := registry.Entry{Path: "/path/2", Kind: "kind2", Data: payload.New("data2Updated")}

	history := NewMockHistory()
	_ = history.Save(v0, registry.ChangeSet{}, false)
	_ = history.Save(v1, registry.ChangeSet{
		{Kind: registry.Create, Entry: entry1},
		{Kind: registry.Create, Entry: entry2},
	}, false)
	_ = history.Save(v2, registry.ChangeSet{
		{Kind: registry.Delete, Entry: entry1},
		{Kind: registry.Update, Entry: entry2Updated},
	}, false)

	builder := NewStateBuilder(zap.NewNop())

	delta, err := builder.BuildDelta(history, v1, v2)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	expectedDelta := registry.ChangeSet{
		{Kind: registry.Delete, Entry: entry1},
		{Kind: registry.Update, Entry: entry2Updated},
	}

	if !reflect.DeepEqual(delta, expectedDelta) {
		t.Errorf("unexpected delta.\ngot: %v\nwant: %v", delta, expectedDelta)
	}
	expectedCallStack := []string{"Save", "Save", "Save", "Versions", "Get(0)", "Get(1)", "Versions", "Get(0)", "Get(1)", "Get(2)"}
	if !reflect.DeepEqual(history.callStack, expectedCallStack) {
		t.Errorf("unexpected call stack.\ngot: %v\nwant: %v", history.callStack, expectedCallStack)
	}
}

func TestStateBuilder_BuildDelta_NoChanges(t *testing.T) {
	v0 := version.New(registry.RootVersion)
	v1 := version.FromParent(v0, 1)

	entry1 := registry.Entry{Path: "/path/1", Kind: "kind1", Data: payload.New("data1")}

	history := NewMockHistory()
	_ = history.Save(v0, registry.ChangeSet{}, false)
	_ = history.Save(v1, registry.ChangeSet{
		{Kind: registry.Create, Entry: entry1},
	}, false)

	builder := NewStateBuilder(zap.NewNop())

	delta, err := builder.BuildDelta(history, v0, v1)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	expectedDelta := registry.ChangeSet{
		{Kind: registry.Create, Entry: entry1},
	}

	if !reflect.DeepEqual(delta, expectedDelta) {
		t.Errorf("unexpected delta.\ngot: %v\nwant: %v", delta, expectedDelta)
	}
	expectedCallStack := []string{"Save", "Save", "Versions", "Get(0)", "Versions", "Get(0)", "Get(1)"}
	if !reflect.DeepEqual(history.callStack, expectedCallStack) {
		t.Errorf("unexpected call stack.\ngot: %v\nwant: %v", history.callStack, expectedCallStack)
	}
}

func TestStateBuilder_BuildDelta_NoChanges_SameVersion(t *testing.T) {
	v0 := version.New(registry.RootVersion)
	v1 := version.FromParent(v0, 1)

	entry1 := registry.Entry{Path: "/path/1", Kind: "kind1", Data: payload.New("data1")}

	history := NewMockHistory()
	_ = history.Save(v0, registry.ChangeSet{}, false)
	_ = history.Save(v1, registry.ChangeSet{
		{Kind: registry.Create, Entry: entry1},
	}, false)

	builder := NewStateBuilder(zap.NewNop())

	// Compare v1 to itself
	delta, err := builder.BuildDelta(history, v1, v1)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	expectedDelta := registry.ChangeSet{} // Expecting an empty ChangeSet

	if !reflect.DeepEqual(delta, expectedDelta) {
		t.Errorf("unexpected delta.\ngot: %v\nwant: %v", delta, expectedDelta)
	}

	expectedCallStack := []string{"Save", "Save", "Versions", "Get(0)", "Get(1)", "Versions", "Get(0)", "Get(1)"}
	if !reflect.DeepEqual(history.callStack, expectedCallStack) {
		t.Errorf("unexpected call stack.\ngot: %v\nwant: %v", history.callStack, expectedCallStack)
	}
}

func TestStateBuilder_BuildDelta_NoChanges_IdenticalStates(t *testing.T) {
	v0 := version.New(registry.RootVersion)
	v1 := version.FromParent(v0, 1)
	v2 := version.FromParent(v1, 2) // v2 will have the same state as v1

	entry1 := registry.Entry{Path: "/path/1", Kind: "kind1", Data: payload.New("data1")}

	history := NewMockHistory()
	_ = history.Save(v0, registry.ChangeSet{}, false)
	_ = history.Save(v1, registry.ChangeSet{
		{Kind: registry.Create, Entry: entry1},
	}, false)
	_ = history.Save(v2, registry.ChangeSet{}, false) // No changes in v2

	builder := NewStateBuilder(zap.NewNop())

	// Compare v1 to v2 (identical states)
	delta, err := builder.BuildDelta(history, v1, v2)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	expectedDelta := registry.ChangeSet{} // Expecting an empty ChangeSet

	if !reflect.DeepEqual(delta, expectedDelta) {
		t.Errorf("unexpected delta.\ngot: %v\nwant: %v", delta, expectedDelta)
	}

	expectedCallStack := []string{"Save", "Save", "Save", "Versions", "Get(0)", "Get(1)", "Versions", "Get(0)", "Get(1)", "Get(2)"}
	if !reflect.DeepEqual(history.callStack, expectedCallStack) {
		t.Errorf("unexpected call stack.\ngot: %v\nwant: %v", history.callStack, expectedCallStack)
	}
}
