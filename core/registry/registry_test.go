package registry

import (
	"errors"
	"fmt"
	"reflect"
	"sync"
	"testing"

	"go.uber.org/zap"

	"github.com/ponyruntime/pony/api/payload"
	"github.com/ponyruntime/pony/api/registry"
	"github.com/ponyruntime/pony/core/registry/storage"
	"github.com/ponyruntime/pony/internal/version"
)

// MockRunner is a mock implementation of the registry.Runner interface for testing.
type MockRunner struct {
	newState      registry.State
	err           error
	callStack     []string
	lastState     registry.State
	lastChangeSet registry.ChangeSet
}

func (m *MockRunner) Run(state registry.State, changes registry.ChangeSet) (registry.State, error) {
	m.callStack = append(m.callStack, "Run")
	m.lastState = state
	m.lastChangeSet = changes
	if m.err != nil {
		return state, m.err
	}
	return m.newState, nil
}

func NewMockRunner() *MockRunner {
	return &MockRunner{
		callStack: make([]string, 0),
	}
}

func TestNewRegistry(t *testing.T) {
	history := storage.NewMemory()
	runner := NewMockRunner()
	stateBuilder := NewStateBuilder(zap.NewNop())

	r := NewRegistry(history, runner, stateBuilder)

	if _, ok := r.(*memreg); !ok {
		t.Errorf("Expected type *memreg, got %T", r)
	}

	reg := r.(*memreg)
	if reg.history != history {
		t.Errorf("Expected history to be %v, got %v", history, reg.history)
	}

	if reg.runner != runner {
		t.Errorf("Expected runner to be %v, got %v", runner, reg.runner)
	}

	if _, ok := reg.stateBuilder.(*StateBuilder); !ok {
		t.Errorf("Expected stateBuilder to be of type *StateBuilder, got %T", reg.stateBuilder)
	}

	if reg.state == nil {
		t.Errorf("Expected state to be initialized, got nil")
	}

	if reg.currentVersion != nil {
		t.Errorf("Expected currentVersion to be nil, got %v", reg.currentVersion)
	}
}

func TestInMemoryRegistry_GetAllEntries(t *testing.T) {
	state := registry.State{
		{Path: "/foo", Kind: "test", Data: payload.NewString("data1")},
		{Path: "/bar", Kind: "test", Data: payload.NewString("data2")},
	}

	reg := &memreg{
		state: state,
		mu:    sync.RWMutex{},
	}

	entries, err := reg.GetAllEntries()
	if err != nil {
		t.Fatalf("Unexpected error: %v", err)
	}

	if len(entries) != len(state) {
		t.Errorf("Expected %d entries, got %d", len(state), len(entries))
	}

	for i := range state {
		if state[i].Path != entries[i].Path || state[i].Kind != entries[i].Kind {
			t.Errorf("Expected entry at index %d to be %v, got %v", i, state[i], entries[i])
		}

		// Access string value using Data() and type assertion
		expectedData, ok := state[i].Data.Data().(string)
		if !ok {
			t.Fatalf("Expected state Data to be a string, got: %T", state[i].Data.Data())
		}

		actualData, ok := entries[i].Data.Data().(string)
		if !ok {
			t.Fatalf("Expected entries Data to be a string, got: %T", entries[i].Data.Data())
		}

		if expectedData != actualData {
			t.Errorf("Expected data at index %d to be %v, got %v", i, expectedData, actualData)
		}
	}
}

func TestInMemoryRegistry_GetEntry(t *testing.T) {
	entry1 := registry.Entry{Path: "/foo", Kind: "test", Data: payload.New("data1")}
	entry2 := registry.Entry{Path: "/bar", Kind: "test", Data: payload.New("data2")}

	state := registry.State{entry1, entry2}

	reg := &memreg{
		state: state,
		mu:    sync.RWMutex{},
	}

	retrievedEntry, err := reg.GetEntry("/foo")
	if err != nil {
		t.Fatalf("Unexpected error: %v", err)
	}

	if !reflect.DeepEqual(retrievedEntry, entry1) {
		t.Errorf("Expected entry: %v, got: %v", entry1, retrievedEntry)
	}

	_, err = reg.GetEntry("/baz")
	if err == nil {
		t.Errorf("Expected error for non-existent entry")
	}
}

func TestInMemoryRegistry_Apply(t *testing.T) {
	v0 := version.New(registry.RootVersion)
	history := storage.NewMemory()
	_ = history.Save(v0, registry.ChangeSet{}, true)
	runner := NewMockRunner()
	stateBuilder := NewStateBuilder(zap.NewNop())

	reg := NewRegistry(history, runner, stateBuilder).(*memreg)

	changes := registry.ChangeSet{
		{
			Kind: registry.Create,
			Entry: registry.Entry{
				Path: "/foo",
				Kind: "test",
				Data: payload.New("data"),
			},
		},
	}

	// Mock the runner to return a new state
	runner.newState = registry.State{changes[0].Entry}

	newVersion, err := reg.Apply(changes)
	if err != nil {
		t.Fatalf("Unexpected error: %v", err)
	}

	head, _ := history.Head()
	if newVersion.ID() != 1 {
		t.Errorf("Expected new version to be v1, got: %v", newVersion)
	}

	if !reflect.DeepEqual(head, newVersion) {
		t.Errorf("Expected new version to be head: %v, got: %v", head, newVersion)
	}

	savedChanges, _ := history.Get(newVersion)
	if !reflect.DeepEqual(savedChanges, changes) {
		t.Errorf("Expected saved changes: %v, got: %v", changes, savedChanges)
	}

	// Verify that the state is updated from the runner
	if !reflect.DeepEqual(reg.state, runner.newState) {
		t.Errorf("Expected state to be updated from runner: %v, got: %v", runner.newState, reg.state)
	}

	if !reflect.DeepEqual(reg.currentVersion, newVersion) {
		t.Errorf("Expected current version: %v, got: %v", newVersion, reg.currentVersion)
	}

	expectedRunnerStack := []string{"Run"}
	if !reflect.DeepEqual(runner.callStack, expectedRunnerStack) {
		t.Errorf("Expected runner call stack: %v, got: %v", expectedRunnerStack, runner.callStack)
	}
}

func TestInMemoryRegistry_Apply_RunnerError(t *testing.T) {
	v0 := version.New(registry.RootVersion)
	history := storage.NewMemory()
	_ = history.Save(v0, registry.ChangeSet{}, true)
	runner := NewMockRunner()
	stateBuilder := NewStateBuilder(zap.NewNop())

	reg := NewRegistry(history, runner, stateBuilder).(*memreg)
	runner.err = errors.New("runner error")

	_, err := reg.Apply(registry.ChangeSet{})
	if err == nil {
		t.Errorf("Expected error, got nil")
	}
	if err.Error() != fmt.Sprintf("failed to apply changes: %v", runner.err) {
		t.Errorf("Expected error: %v, got: %v", runner.err, err)
	}
}

func TestInMemoryRegistry_ApplyVersion(t *testing.T) {
	v0 := version.New(registry.RootVersion)
	v1 := version.FromParent(v0, 1)
	v2 := version.FromParent(v1, 2)

	history := storage.NewMemory()
	_ = history.Save(v0, registry.ChangeSet{}, true)
	_ = history.Save(v1, registry.ChangeSet{
		{Kind: registry.Create, Entry: registry.Entry{Path: "/foo", Kind: "test", Data: payload.New("data1")}},
	}, false)
	_ = history.Save(v2, registry.ChangeSet{
		{Kind: registry.Update, Entry: registry.Entry{Path: "/foo", Kind: "test", Data: payload.New("data2")}},
	}, false)

	runner := NewMockRunner()
	stateBuilder := NewStateBuilder(zap.NewNop())

	reg := NewRegistry(history, runner, stateBuilder).(*memreg)
	reg.currentVersion = v2 // Set current version to v2
	// Set initial state to v2 state
	reg.state = registry.State{
		{Path: "/foo", Kind: "test", Data: payload.New("data2")},
	}

	// Mock the runner to return a new state - v1 state
	runner.newState = registry.State{
		{Path: "/foo", Kind: "test", Data: payload.New("data1")},
	}

	err := reg.ApplyVersion(v1)
	if err != nil {
		t.Fatalf("Unexpected error: %v", err)
	}

	if !reflect.DeepEqual(reg.state, runner.newState) {
		t.Errorf("Expected state: %v, got: %v", runner.newState, reg.state)
	}

	if !reflect.DeepEqual(reg.currentVersion, v1) {
		t.Errorf("Expected current version: %v, got: %v", v1, reg.currentVersion)
	}

	expectedRunnerStack := []string{"Run"}
	if !reflect.DeepEqual(runner.callStack, expectedRunnerStack) {
		t.Errorf("Expected runner call stack: %v, got: %v", expectedRunnerStack, runner.callStack)
	}

	// Verify that runner received the correct state and changes
	expectedStateBeforeRun := registry.State{
		{Path: "/foo", Kind: "test", Data: payload.New("data2")},
	}
	if !reflect.DeepEqual(runner.lastState, expectedStateBeforeRun) {
		t.Errorf("Expected runner to receive state: %v, got: %v", expectedStateBeforeRun, runner.lastState)
	}

	expectedChanges := registry.ChangeSet{
		{Kind: registry.Update, Entry: registry.Entry{Path: "/foo", Kind: "test", Data: payload.New("data1")}},
	}
	if !reflect.DeepEqual(runner.lastChangeSet, expectedChanges) {
		t.Errorf("Expected runner to receive changes: %v, got: %v", expectedChanges, runner.lastChangeSet)
	}
}

func TestInMemoryRegistry_ApplyVersion_RunnerError(t *testing.T) {
	v0 := version.New(registry.RootVersion)
	v1 := version.FromParent(v0, 1)
	history := storage.NewMemory()
	_ = history.Save(v0, registry.ChangeSet{}, true)
	_ = history.Save(v1, registry.ChangeSet{}, false)

	runner := NewMockRunner()
	stateBuilder := NewStateBuilder(zap.NewNop())
	reg := NewRegistry(history, runner, stateBuilder).(*memreg)
	reg.currentVersion = v1

	runner.err = errors.New("runner error")

	err := reg.ApplyVersion(v0)
	if err == nil {
		t.Errorf("Expected error, got nil")
	}
	if err.Error() != fmt.Sprintf("failed to apply changes for version %v: %v", v0, runner.err) {
		t.Errorf("Expected error: %v, got: %v", runner.err, err)
	}
}

func TestInMemoryRegistry_Apply_GetHeadHistoryError(t *testing.T) {
	history := storage.NewMemory() // No initial version saved
	runner := NewMockRunner()
	stateBuilder := NewStateBuilder(zap.NewNop())

	reg := NewRegistry(history, runner, stateBuilder).(*memreg)

	_, err := reg.Apply(registry.ChangeSet{})
	if err == nil {
		t.Errorf("Expected error, got nil")
	}
	if err.Error() != "failed to get head version: no head version set" {
		t.Errorf("Expected error: %v, got: %v", "failed to get head version: no head version set", err.Error())
	}
}

// Mock for History that returns an error on Save
type ErrorHistory struct {
	storage.MemoryStorage // Correctly embed MemoryStorage
	// Add a map to store versions for Versions() method
	versions map[registry.Version]registry.ChangeSet
}

// Override Save to return an error
func (h *ErrorHistory) Save(v registry.Version, cs registry.ChangeSet, head bool) error {
	// Also save to versions map
	if h.versions == nil {
		h.versions = make(map[registry.Version]registry.ChangeSet)
	}
	h.versions[v] = cs

	return errors.New("history error")
}

// Implement Versions()
func (h *ErrorHistory) Versions() ([]registry.Version, error) {
	var vs []registry.Version
	for v := range h.versions {
		vs = append(vs, v)
	}
	return vs, nil
}

// Implement Get()
func (h *ErrorHistory) Get(v registry.Version) (registry.ChangeSet, error) {
	if cs, ok := h.versions[v]; ok {
		return cs, nil
	}
	return nil, fmt.Errorf("version not found: %v", v)
}

// Implement Head() - return error if no head, otherwise return latest saved version
func (h *ErrorHistory) Head() (registry.Version, error) {
	if len(h.versions) == 0 {
		return nil, errors.New("no head version set")
	}
	var head registry.Version
	for v := range h.versions {
		if head == nil || v.ID() > head.ID() {
			head = v
		}
	}
	return head, nil
}

func NewErrorHistory() *ErrorHistory {
	return &ErrorHistory{
		MemoryStorage: *storage.NewMemory(),
		versions:      make(map[registry.Version]registry.ChangeSet),
	}
}

func TestInMemoryRegistry_Apply_HistorySaveError(t *testing.T) {
	v0 := version.New(registry.RootVersion)
	history := NewErrorHistory()
	_ = history.Save(v0, registry.ChangeSet{}, true) // Set up an initial head version
	runner := NewMockRunner()
	stateBuilder := NewStateBuilder(zap.NewNop())

	reg := NewRegistry(history, runner, stateBuilder).(*memreg)

	// Mock the runner to return a new state (so we can test rollback)
	runner.newState = registry.State{
		{Path: "/foo", Kind: "test", Data: payload.New("data")},
	}

	// Attempt to apply changes, which should fail due to the history error
	_, err := reg.Apply(registry.ChangeSet{
		{
			Kind: registry.Create,
			Entry: registry.Entry{
				Path: "/foo",
				Kind: "test",
				Data: payload.New("data"),
			},
		},
	})

	if err == nil {
		t.Errorf("Expected error, got nil")
	}

	expectedErrorMsg := "failed to save new version: history error, rolled back"
	if err.Error() != expectedErrorMsg {
		t.Errorf("Expected error message: '%v', got: '%v'", expectedErrorMsg, err.Error())
	}

	// Verify that the runner's Run method was called only twice (has rollback)
	expectedRunnerStack := []string{"Run", "Run"}
	if !reflect.DeepEqual(runner.callStack, expectedRunnerStack) {
		t.Errorf("Expected runner call stack: %v, got: %v", expectedRunnerStack, runner.callStack)
	}
}
