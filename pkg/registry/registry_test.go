package registry

import (
	"context"
	"errors"
	"fmt"
	"reflect"
	"strings"
	"sync"
	"testing"

	"go.uber.org/zap"

	"github.com/ponyruntime/pony/api/payload"
	"github.com/ponyruntime/pony/api/registry"
	"github.com/ponyruntime/pony/internal/version"
	"github.com/ponyruntime/pony/pkg/registry/history"
)

// MockRunner is a mock implementation of the registry.Runner interface for testing.
type MockRunner struct {
	newState      registry.State
	err           error
	callStack     []string
	lastState     registry.State
	lastChangeSet registry.ChangeSet
	RunFunc       func(state registry.State, changes registry.ChangeSet) (registry.State, error)
}

func (m *MockRunner) Transition(ctx context.Context, state registry.State, changes registry.ChangeSet) (registry.State, error) {
	m.callStack = append(m.callStack, "Transition")
	m.lastState = state
	m.lastChangeSet = changes
	if m.RunFunc != nil {
		return m.RunFunc(state, changes)
	}
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
	history := history.NewMemory()
	runner := NewMockRunner()
	stateBuilder := NewStateBuilder(zap.NewNop())

	r := NewRegistry(history, runner, stateBuilder, zap.NewNop())

	if _, ok := r.(*reg); !ok {
		t.Errorf("Expected type *reg, got %T", r)
	}

	reg := r.(*reg)
	if reg.history != history {
		t.Errorf("Expected history to be %v, got %v", history, reg.history)
	}

	if reg.runner != runner {
		t.Errorf("Expected runner to be %v, got %v", runner, reg.runner)
	}

	if _, ok := reg.builder.(*StateBuilder); !ok {
		t.Errorf("Expected builder to be of type *StateBuilder, got %T", reg.builder)
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
		{ID: "/foo", Kind: "test", Data: payload.NewString("data1")},
		{ID: "/bar", Kind: "test", Data: payload.NewString("data2")},
	}

	reg := &reg{
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
		if state[i].ID != entries[i].ID || state[i].Kind != entries[i].Kind {
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
	entry1 := registry.Entry{ID: "/foo", Kind: "test", Data: payload.New("data1")}
	entry2 := registry.Entry{ID: "/bar", Kind: "test", Data: payload.New("data2")}

	state := registry.State{entry1, entry2}

	reg := &reg{
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
	history := history.NewMemory()
	_ = history.Save(v0, registry.ChangeSet{}, true)
	runner := NewMockRunner()
	stateBuilder := NewStateBuilder(zap.NewNop())

	reg := NewRegistry(history, runner, stateBuilder, zap.NewNop()).(*reg)

	changes := registry.ChangeSet{
		{
			Kind: registry.Create,
			Entry: registry.Entry{
				ID:   "/foo",
				Kind: "test",
				Data: payload.New("data"),
			},
		},
	}

	// Mock the runner to return a new state
	runner.newState = registry.State{changes[0].Entry}

	newVersion, err := reg.Apply(context.Background(), changes)
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

	expectedRunnerStack := []string{"Transition"}
	if !reflect.DeepEqual(runner.callStack, expectedRunnerStack) {
		t.Errorf("Expected runner call stack: %v, got: %v", expectedRunnerStack, runner.callStack)
	}
}

func TestInMemoryRegistry_Apply_RunnerError(t *testing.T) {
	v0 := version.New(registry.RootVersion)
	history := history.NewMemory()
	_ = history.Save(v0, registry.ChangeSet{}, true)
	runner := NewMockRunner()
	stateBuilder := NewStateBuilder(zap.NewNop())

	reg := NewRegistry(history, runner, stateBuilder, zap.NewNop()).(*reg)
	runner.err = errors.New("runner error, failed to rollback: runner error")

	_, err := reg.Apply(context.Background(), registry.ChangeSet{})
	if err == nil {
		t.Errorf("Expected error, got nil")
		return
	}

	expectedPrefix := "failed to apply changes: "
	if !strings.HasPrefix(err.Error(), expectedPrefix) {
		t.Errorf("Expected error to start with: '%v', got: '%v'", expectedPrefix, err)
	}
}

func TestInMemoryRegistry_ApplyVersion(t *testing.T) {
	v0 := version.New(registry.RootVersion)
	v1 := version.FromParent(v0, 1)
	v2 := version.FromParent(v1, 2)

	history := history.NewMemory()
	_ = history.Save(v0, registry.ChangeSet{}, true)
	_ = history.Save(v1, registry.ChangeSet{
		{Kind: registry.Create, Entry: registry.Entry{ID: "/foo", Kind: "test", Data: payload.New("data1")}},
	}, false)
	_ = history.Save(v2, registry.ChangeSet{
		{Kind: registry.Update, Entry: registry.Entry{ID: "/foo", Kind: "test", Data: payload.New("data2")}},
	}, false)

	runner := NewMockRunner()
	stateBuilder := NewStateBuilder(zap.NewNop())

	reg := NewRegistry(history, runner, stateBuilder, zap.NewNop()).(*reg)
	reg.currentVersion = v2 // Set current version to v2
	// Set initial state to v2 state
	reg.state = registry.State{
		{ID: "/foo", Kind: "test", Data: payload.New("data2")},
	}

	// Mock the runner to return a new state - v1 state
	runner.newState = registry.State{
		{ID: "/foo", Kind: "test", Data: payload.New("data1")},
	}

	err := reg.ApplyVersion(context.Background(), v1)
	if err != nil {
		t.Fatalf("Unexpected error: %v", err)
	}

	if !reflect.DeepEqual(reg.state, runner.newState) {
		t.Errorf("Expected state: %v, got: %v", runner.newState, reg.state)
	}

	if !reflect.DeepEqual(reg.currentVersion, v1) {
		t.Errorf("Expected current version: %v, got: %v", v1, reg.currentVersion)
	}

	expectedRunnerStack := []string{"Transition"}
	if !reflect.DeepEqual(runner.callStack, expectedRunnerStack) {
		t.Errorf("Expected runner call stack: %v, got: %v", expectedRunnerStack, runner.callStack)
	}

	// Verify that runner received the correct state and changes
	expectedStateBeforeRun := registry.State{
		{ID: "/foo", Kind: "test", Data: payload.New("data2")},
	}
	if !reflect.DeepEqual(runner.lastState, expectedStateBeforeRun) {
		t.Errorf("Expected runner to receive state: %v, got: %v", expectedStateBeforeRun, runner.lastState)
	}

	expectedChanges := registry.ChangeSet{
		{Kind: registry.Update, Entry: registry.Entry{ID: "/foo", Kind: "test", Data: payload.New("data1")}},
	}
	if !reflect.DeepEqual(runner.lastChangeSet, expectedChanges) {
		t.Errorf("Expected runner to receive changes: %v, got: %v", expectedChanges, runner.lastChangeSet)
	}
}

// Mock for History that returns an error on Save
type ErrorHistory struct {
	history.MemoryStorage // Correctly embed MemoryStorage
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
		MemoryStorage: *history.NewMemory(),
		versions:      make(map[registry.Version]registry.ChangeSet),
	}
}

func TestInMemoryRegistry_Apply_HistorySaveError(t *testing.T) {
	v0 := version.New(registry.RootVersion)
	history := NewErrorHistory()
	_ = history.Save(v0, registry.ChangeSet{}, true) // Set up an initial head version
	runner := NewMockRunner()
	stateBuilder := NewStateBuilder(zap.NewNop())

	reg := NewRegistry(history, runner, stateBuilder, zap.NewNop()).(*reg)

	// Mock the runner to return a new state (so we can test rollback)
	runner.newState = registry.State{
		{ID: "/foo", Kind: "test", Data: payload.New("data")},
	}

	// Attempt to apply changes, which should fail due to the history error
	_, err := reg.Apply(context.Background(), registry.ChangeSet{
		{
			Kind: registry.Create,
			Entry: registry.Entry{
				ID:   "/foo",
				Kind: "test",
				Data: payload.New("data"),
			},
		},
	})

	if err == nil {
		t.Errorf("Expected error, got nil")
		return
	}

	expectedErrorMsg := "failed to save new version: history error, recovered"
	if err.Error() != expectedErrorMsg {
		t.Errorf("Expected error message: '%v', got: '%v'", expectedErrorMsg, err.Error())
	}

	// Verify that the runner's Transition method was called only once (no rollback)
	expectedRunnerStack := []string{"Transition", "Transition"}
	if !reflect.DeepEqual(runner.callStack, expectedRunnerStack) {
		t.Errorf("Expected runner call stack: %v, got: %v", expectedRunnerStack, runner.callStack)
	}
}

// CustomizableMockRunner is a mock implementation of registry.Runner for testing
// that allows setting a custom Run function.
type CustomizableMockRunner struct {
	RunFunc func(state registry.State, changes registry.ChangeSet) (registry.State, error)
}

// Run calls the custom RunFunc if set, otherwise returns an error.
func (m *CustomizableMockRunner) Transition(_ context.Context, state registry.State, changes registry.ChangeSet) (registry.State, error) {
	if m.RunFunc != nil {
		return m.RunFunc(state, changes)
	}
	return nil, errors.New("RunFunc not set")
}

func TestInMemoryRegistry_ConcurrentApply(t *testing.T) {
	v0 := version.New(registry.RootVersion)
	history := history.NewMemory()
	_ = history.Save(v0, registry.ChangeSet{}, true)
	runner := &CustomizableMockRunner{} // Use the new customizable mock
	stateBuilder := NewStateBuilder(zap.NewNop())
	reg := NewRegistry(history, runner, stateBuilder, zap.NewNop()).(*reg)

	var wg sync.WaitGroup
	numGoroutines := 10
	changesPerRoutine := 5

	// Mock runner behavior: append changes to state
	runner.RunFunc = func(state registry.State, changes registry.ChangeSet) (registry.State, error) {
		for _, change := range changes {
			state = append(state, change.Entry)
		}
		return state, nil
	}

	for i := 0; i < numGoroutines; i++ {
		wg.Add(1)
		go func(routineID int) {
			defer wg.Done()
			for j := 0; j < changesPerRoutine; j++ {
				change := registry.ChangeSet{
					{
						Kind: registry.Create,
						Entry: registry.Entry{
							ID:   registry.ID(fmt.Sprintf("/entry/%d/%d", routineID, j)),
							Kind: "test",
							Data: payload.New(fmt.Sprintf("data-%d-%d", routineID, j)),
						},
					},
				}
				_, err := reg.Apply(context.Background(), change)
				if err != nil {
					t.Errorf("RaiseError in Apply: %v", err)
					return
				}
			}
		}(i)
	}

	wg.Wait()

	// Verify final state
	finalState, err := reg.GetAllEntries()
	if err != nil {
		t.Fatalf("RaiseError getting final state: %v", err)
	}

	if len(finalState) != numGoroutines*changesPerRoutine {
		t.Errorf("Expected %d entries, got %d", numGoroutines*changesPerRoutine, len(finalState))
	}

	// Verify current version
	currentVersion, err := reg.Current()
	if err != nil {
		t.Fatalf("RaiseError getting current version: %v", err)
	}

	if currentVersion.ID() != uint(numGoroutines*changesPerRoutine) {
		t.Errorf("Expected current version Name %d, got %d", numGoroutines*changesPerRoutine, currentVersion.ID())
	}
}

// TestInMemoryRegistry_Apply_Rollback_Success tests the successful rollback scenario
// when saving the new version to history fails.
func TestInMemoryRegistry_Apply_Rollback_Success(t *testing.T) {
	v0 := version.New(registry.RootVersion)
	// Use ErrorHistory to simulate a history save error
	history := NewErrorHistory()
	_ = history.Save(v0, registry.ChangeSet{}, true)

	runner := NewMockRunner()
	stateBuilder := NewStateBuilder(zap.NewNop()) // Use the real StateBuilder
	reg := NewRegistry(history, runner, stateBuilder, zap.NewNop()).(*reg)

	// Initial state
	initialState := registry.State{
		{ID: "/initial", Kind: "test", Data: payload.New("initial_data")},
	}
	reg.state = initialState
	reg.currentVersion = v0

	// Changes that will be applied but should be rolled back
	changes := registry.ChangeSet{
		{
			Kind: registry.Create,
			Entry: registry.Entry{
				ID:   "/foo",
				Kind: "test",
				Data: payload.New("data"),
			},
		},
	}

	// Mock the runner to return a new state that includes the changes
	newState := append(initialState, changes[0].Entry)
	runner.newState = newState

	// Mock the runner's Transition to apply the rollback changeset correctly
	runner.RunFunc = func(state registry.State, cs registry.ChangeSet) (registry.State, error) {
		// Handle initial application of changes
		if reflect.DeepEqual(cs, changes) {
			return newState, nil
		}

		// Handle rollback
		if len(cs) == 1 && cs[0].Kind == registry.Delete && cs[0].Entry.ID == "/foo" &&
			reflect.DeepEqual(state, newState) {
			return initialState, nil
		}

		return state, fmt.Errorf("unexpected Transition call with state: %v, changeset: %v", state, cs)
	}

	// Attempt to apply changes, which should fail due to the history error
	_, err := reg.Apply(context.Background(), changes)
	if err == nil {
		t.Errorf("Expected error, got nil")
		return
	}

	expectedErrorMsg := "failed to save new version: history error, recovered"
	if err.Error() != expectedErrorMsg {
		t.Errorf("Expected error message: '%v', got: '%v'", expectedErrorMsg, err.Error())
	}

	// Verify that the state has been rolled back to the initial state
	if !reflect.DeepEqual(reg.state, initialState) {
		t.Errorf("Expected state to be rolled back to: %v, got: %v", initialState, reg.state)
	}

	// Verify that the current version is still v0
	if reg.currentVersion != v0 {
		t.Errorf("Expected current version to remain v0, got: %v", reg.currentVersion)
	}

	// Verify that the runner's Transition method was called twice (rollback happened)
	expectedRunnerStack := []string{"Transition", "Transition"}
	if !reflect.DeepEqual(runner.callStack, expectedRunnerStack) {
		t.Errorf("Expected runner call stack: %v, got: %v", expectedRunnerStack, runner.callStack)
	}
}

// TestInMemoryRegistry_Apply_Rollback_Failure tests the scenario where the rollback
// itself fails after a history save error.
func TestInMemoryRegistry_Apply_Rollback_Failure(t *testing.T) {
	v0 := version.New(registry.RootVersion)
	history := NewErrorHistory()
	_ = history.Save(v0, registry.ChangeSet{}, true)

	runner := NewMockRunner()
	stateBuilder := NewStateBuilder(zap.NewNop())
	reg := NewRegistry(history, runner, stateBuilder, zap.NewNop()).(*reg)

	initialState := registry.State{
		{ID: "/initial", Kind: "test", Data: payload.New("initial_data")},
	}
	reg.state = initialState
	reg.currentVersion = v0

	changes := registry.ChangeSet{
		{
			Kind: registry.Create,
			Entry: registry.Entry{
				ID:   "/foo",
				Kind: "test",
				Data: payload.New("data"),
			},
		},
	}

	newState := append(initialState, changes[0].Entry)

	rollbackErr := errors.New("rollback failed")
	runner.RunFunc = func(state registry.State, cs registry.ChangeSet) (registry.State, error) {
		// Handle initial application of changes
		if reflect.DeepEqual(cs, changes) {
			return newState, nil
		}

		// Handle rollback failure
		if len(cs) == 1 && cs[0].Kind == registry.Delete && cs[0].Entry.ID == "/foo" &&
			reflect.DeepEqual(state, newState) {
			return state, rollbackErr
		}

		return state, fmt.Errorf("unexpected Transition call with state: %v, changeset: %v", state, cs)
	}

	_, err := reg.Apply(context.Background(), changes)
	if err == nil {
		t.Errorf("Expected error, got nil")
		return
	}

	expectedErrorMsg := fmt.Sprintf("failed to save new version: history error, failed to rollback: %v", rollbackErr)
	if err.Error() != expectedErrorMsg {
		t.Errorf("Expected error message: '%v', got: '%v'", expectedErrorMsg, err.Error())
	}

	// Verify that the state is NOT rolled back (it's in the intermediate state)
	if reflect.DeepEqual(reg.state, initialState) {
		t.Errorf("Expected state to NOT be rolled back, got initial state: %v", reg.state)
	}

	if !reflect.DeepEqual(reg.state, newState) {
		t.Errorf("Expected state to be in intermediate state: %v, got: %v", reg.state, newState)
	}

	// Verify that the current version is still v0
	if reg.currentVersion != v0 {
		t.Errorf("Expected current version to remain v0, got: %v", reg.currentVersion)
	}

	expectedRunnerStack := []string{"Transition", "Transition"}
	if !reflect.DeepEqual(runner.callStack, expectedRunnerStack) {
		t.Errorf("Expected runner call stack: %v, got: %v", expectedRunnerStack, runner.callStack)
	}
}
