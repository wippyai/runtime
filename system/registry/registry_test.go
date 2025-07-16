package registry

import (
	"context"
	"fmt"
	"reflect"
	"strings"
	"sync"
	"testing"
	"testing/fstest"

	"github.com/pkg/errors"
	"github.com/ponyruntime/pony/internal/version"
	transcoder "github.com/ponyruntime/pony/system/payload"
	"github.com/ponyruntime/pony/system/payload/json"
	"github.com/ponyruntime/pony/system/payload/yaml"
	"github.com/ponyruntime/pony/system/registry/loader"
	"github.com/ponyruntime/pony/system/registry/loader/interpolate"
	"github.com/stretchr/testify/assert"

	"go.uber.org/zap"

	"github.com/ponyruntime/pony/api/payload"
	"github.com/ponyruntime/pony/api/registry"
	"github.com/ponyruntime/pony/system/registry/history"
	"github.com/ponyruntime/pony/system/registry/topology"
)

// MockRunner is a mock implementation of the registry.process interface for testing.
type MockRunner struct {
	newState      registry.State
	err           error
	callStack     []string
	lastState     registry.State
	lastChangeSet registry.ChangeSet
	RunFunc       func(state registry.State, changes registry.ChangeSet) (registry.State, error)
}

func (m *MockRunner) Transition(_ context.Context, state registry.State, changes registry.ChangeSet) (registry.State, error) {
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
	hist := history.NewMemory()
	runner := NewMockRunner()
	stateBuilder := topology.NewStateBuilder(zap.NewNop())

	r := NewRegistry(hist, runner, stateBuilder, zap.NewNop())

	if _, ok := r.(*reg); !ok {
		t.Errorf("Expected type *reg, got %T", r)
	}

	reg := r.(*reg)
	if reg.history != hist {
		t.Errorf("Expected hist to be %v, got %v", hist, reg.history)
	}

	if reg.runner != runner {
		t.Errorf("Expected runner to be %v, got %v", runner, reg.runner)
	}

	if _, ok := reg.builder.(*topology.StateBuilder); !ok {
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
		{ID: registry.ID{Name: "/foo"}, Kind: "test", Data: payload.NewString("data1")},
		{ID: registry.ID{Name: "/bar"}, Kind: "test", Data: payload.NewString("data2")},
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
	entry1 := registry.Entry{ID: registry.ID{Name: "/foo"}, Kind: "test", Data: payload.New("data1")}
	entry2 := registry.Entry{ID: registry.ID{Name: "/bar"}, Kind: "test", Data: payload.New("data2")}

	state := registry.State{entry1, entry2}

	reg := &reg{
		state: state,
		mu:    sync.RWMutex{},
	}

	retrievedEntry, err := reg.GetEntry(registry.ID{Name: "/foo"})
	if err != nil {
		t.Fatalf("Unexpected error: %v", err)
	}

	if !reflect.DeepEqual(retrievedEntry, entry1) {
		t.Errorf("Expected entry: %v, got: %v", entry1, retrievedEntry)
	}

	_, err = reg.GetEntry(registry.ID{Name: "/baz"})
	if err == nil {
		t.Errorf("Expected error for non-existent entry")
	}
}

func TestInMemoryRegistry_Apply(t *testing.T) {
	v0 := version.New(registry.RootVersion)
	hist := history.NewMemory()
	_ = hist.Save(v0, registry.ChangeSet{}, true)
	runner := NewMockRunner()
	stateBuilder := topology.NewStateBuilder(zap.NewNop())

	reg := NewRegistry(hist, runner, stateBuilder, zap.NewNop()).(*reg)

	changes := registry.ChangeSet{
		{
			Kind: registry.Create,
			Entry: registry.Entry{
				ID:   registry.ID{Name: "/foo"},
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

	head, _ := hist.Head()
	if newVersion.ID() != 1 {
		t.Errorf("Expected new version to be v1, got: %v", newVersion)
	}

	if !reflect.DeepEqual(head, newVersion) {
		t.Errorf("Expected new version to be head: %v, got: %v", head, newVersion)
	}

	savedChanges, _ := hist.Get(newVersion)
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
	hist := history.NewMemory()
	_ = hist.Save(v0, registry.ChangeSet{}, true)
	runner := NewMockRunner()
	stateBuilder := topology.NewStateBuilder(zap.NewNop())

	reg := NewRegistry(hist, runner, stateBuilder, zap.NewNop()).(*reg)
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

	hist := history.NewMemory()
	_ = hist.Save(v0, registry.ChangeSet{}, true)
	_ = hist.Save(v1, registry.ChangeSet{
		{Kind: registry.Create, Entry: registry.Entry{ID: registry.ID{Name: "/foo"}, Kind: "test", Data: payload.New("data1")}},
	}, false)
	_ = hist.Save(v2, registry.ChangeSet{
		{Kind: registry.Update, Entry: registry.Entry{ID: registry.ID{Name: "/foo"}, Kind: "test", Data: payload.New("data2")}},
	}, false)

	runner := NewMockRunner()
	stateBuilder := topology.NewStateBuilder(zap.NewNop())

	reg := NewRegistry(hist, runner, stateBuilder, zap.NewNop()).(*reg)
	reg.currentVersion = v2 // Set current version to v2
	// Set initial state to v2 state
	reg.state = registry.State{
		{ID: registry.ID{Name: "/foo"}, Kind: "test", Data: payload.New("data2")},
	}

	// Mock the runner to return a new state - v1 state
	runner.newState = registry.State{
		{ID: registry.ID{Name: "/foo"}, Kind: "test", Data: payload.New("data1")},
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
}

// Mock for History that returns an error on Save
type ErrorHistory struct {
	history.MemoryStorage // Correctly embed MemoryStorage
	versions              map[registry.Version]registry.ChangeSet
}

func NewErrorHistory() *ErrorHistory {
	return &ErrorHistory{
		MemoryStorage: *history.NewMemory(),
		versions:      make(map[registry.Version]registry.ChangeSet),
	}
}

// Override Save to return an error
func (h *ErrorHistory) Save(v registry.Version, cs registry.ChangeSet, _ bool) error {
	if h.versions == nil {
		h.versions = make(map[registry.Version]registry.ChangeSet)
	}
	h.versions[v] = cs
	return errors.New("history error")
}

func (h *ErrorHistory) Versions() ([]registry.Version, error) {
	vs := make([]registry.Version, 0, len(h.versions))
	for v := range h.versions {
		vs = append(vs, v)
	}
	return vs, nil
}

func (h *ErrorHistory) Get(v registry.Version) (registry.ChangeSet, error) {
	if cs, ok := h.versions[v]; ok {
		return cs, nil
	}
	return nil, fmt.Errorf("version not found: %v", v)
}

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

func TestInMemoryRegistry_Apply_HistorySaveError(t *testing.T) {
	v0 := version.New(registry.RootVersion)
	hist := NewErrorHistory()
	_ = hist.Save(v0, registry.ChangeSet{}, true)
	runner := NewMockRunner()
	stateBuilder := topology.NewStateBuilder(zap.NewNop())

	reg := NewRegistry(hist, runner, stateBuilder, zap.NewNop()).(*reg)

	// Mock the runner to return a new state (so we can test rollback)
	runner.newState = registry.State{
		{ID: registry.ID{Name: "/foo"}, Kind: "test", Data: payload.New("data")},
	}

	// Attempt to apply changes, which should fail due to the hist error
	_, err := reg.Apply(context.Background(), registry.ChangeSet{
		{
			Kind: registry.Create,
			Entry: registry.Entry{
				ID:   registry.ID{Name: "/foo"},
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

	// Verify that the runner's Transition method was called twice (for rollback)
	expectedRunnerStack := []string{"Transition", "Transition"}
	if !reflect.DeepEqual(runner.callStack, expectedRunnerStack) {
		t.Errorf("Expected runner call stack: %v, got: %v", expectedRunnerStack, runner.callStack)
	}
}

// CustomizableMockRunner is a mock implementation of registry.process for testing
// that allows setting a custom Run function.
type CustomizableMockRunner struct {
	RunFunc func(state registry.State, changes registry.ChangeSet) (registry.State, error)
}

func (m *CustomizableMockRunner) Transition(_ context.Context, state registry.State, changes registry.ChangeSet) (registry.State, error) {
	if m.RunFunc != nil {
		return m.RunFunc(state, changes)
	}
	return nil, errors.New("RunFunc not set")
}

func TestInMemoryRegistry_ConcurrentApply(t *testing.T) {
	v0 := version.New(registry.RootVersion)
	hist := history.NewMemory()
	_ = hist.Save(v0, registry.ChangeSet{}, true)
	runner := &CustomizableMockRunner{}
	stateBuilder := topology.NewStateBuilder(zap.NewNop())
	reg := NewRegistry(hist, runner, stateBuilder, zap.NewNop()).(*reg)

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
							ID:   registry.ID{Name: fmt.Sprintf("/entry/%d/%d", routineID, j)},
							Kind: "test",
							Data: payload.New(fmt.Sprintf("data-%d-%d", routineID, j)),
						},
					},
				}
				_, err := reg.Apply(context.Background(), change)
				if err != nil {
					t.Errorf("Error in Apply: %v", err)
					return
				}
			}
		}(i)
	}

	wg.Wait()

	// Verify the final state
	finalState, err := reg.GetAllEntries()
	if err != nil {
		t.Fatalf("Error getting final state: %v", err)
	}

	if len(finalState) != numGoroutines*changesPerRoutine {
		t.Errorf("Expected %d entries, got %d", numGoroutines*changesPerRoutine, len(finalState))
	}

	// Verify current version
	currentVersion, err := reg.Current()
	if err != nil {
		t.Fatalf("Error getting current version: %v", err)
	}

	//nolint:gosec // used in tests
	if int(currentVersion.ID()) != numGoroutines*changesPerRoutine {
		t.Errorf("Expected current version Process %d, got %d", numGoroutines*changesPerRoutine, currentVersion.ID())
	}
}

// TestInMemoryRegistry_Apply_Rollback_Success tests the successful rollback scenario
// when saving the new version to history fails.
func TestInMemoryRegistry_Apply_Rollback_Success(t *testing.T) {
	v0 := version.New(registry.RootVersion)
	hist := NewErrorHistory()
	_ = hist.Save(v0, registry.ChangeSet{}, true)

	runner := NewMockRunner()
	stateBuilder := topology.NewStateBuilder(zap.NewNop())
	reg := NewRegistry(hist, runner, stateBuilder, zap.NewNop()).(*reg)

	initialState := registry.State{
		{ID: registry.ID{Name: "/initial"}, Kind: "test", Data: payload.New("initial_data")},
	}
	reg.state = initialState
	reg.currentVersion = v0

	changes := registry.ChangeSet{
		{
			Kind: registry.Create,
			Entry: registry.Entry{
				ID:   registry.ID{Name: "/foo"},
				Kind: "test",
				Data: payload.New("data"),
			},
		},
	}

	// Mock the runner to return a new state that includes the changes
	//nolint:gocritic // new array used only for comparison
	newState := append(initialState, changes[0].Entry)
	runner.newState = newState

	// Mock the runner's Transition to apply the rollback changeset correctly
	runner.RunFunc = func(state registry.State, cs registry.ChangeSet) (registry.State, error) {
		// Handle initial application of changes
		if reflect.DeepEqual(cs, changes) {
			return newState, nil
		}

		// Handle rollback
		if len(cs) == 1 && cs[0].Kind == registry.Delete && cs[0].Entry.ID.Name == "/foo" &&
			reflect.DeepEqual(state, newState) {
			return initialState, nil
		}

		return state, fmt.Errorf("unexpected Transition call with state: %v, changeset: %v", state, cs)
	}

	// Attempt to apply changes, which should fail due to the hist error
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
	hist := NewErrorHistory()
	_ = hist.Save(v0, registry.ChangeSet{}, true)

	runner := NewMockRunner()
	stateBuilder := topology.NewStateBuilder(zap.NewNop())
	reg := NewRegistry(hist, runner, stateBuilder, zap.NewNop()).(*reg)

	initialState := registry.State{
		{ID: registry.ID{Name: "/initial"}, Kind: "test", Data: payload.New("initial_data")},
	}
	reg.state = initialState
	reg.currentVersion = v0

	changes := registry.ChangeSet{
		{
			Kind: registry.Create,
			Entry: registry.Entry{
				ID:   registry.ID{Name: "/foo"},
				Kind: "test",
				Data: payload.New("data"),
			},
		},
	}

	//nolint:gocritic // new array used only for comparison
	newState := append(initialState, changes[0].Entry)

	rollbackErr := errors.New("rollback failed")
	runner.RunFunc = func(state registry.State, cs registry.ChangeSet) (registry.State, error) {
		// Handle initial application of changes
		if reflect.DeepEqual(cs, changes) {
			return newState, nil
		}

		// Handle rollback failure
		if len(cs) == 1 && cs[0].Kind == registry.Delete && cs[0].Entry.ID.Name == "/foo" &&
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
		t.Errorf("Expected state to be in intermediate state: %v, got: %v", newState, reg.state)
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

func createTestTranscoder() payload.Transcoder {
	tr := transcoder.NewTranscoder()

	// Register JSON
	tr.RegisterTranscoder(payload.JSON, payload.Golang, 1, &json.ToGolang{})
	tr.RegisterTranscoder(payload.Golang, payload.JSON, 1, &json.FromGolang{})
	tr.RegisterUnmarshaler(payload.JSON, &json.ToGolang{})

	// Register YAML
	tr.RegisterTranscoder(payload.YAML, payload.Golang, 1, &yaml.ToGolang{})
	tr.RegisterTranscoder(payload.Golang, payload.YAML, 1, &yaml.FromGolang{})
	tr.RegisterUnmarshaler(payload.YAML, &yaml.ToGolang{})

	return tr
}

// TestInMemoryRegistry_InitFromFolder tests initializing registry state from a folder
func TestInMemoryRegistry_InitFromFolder(t *testing.T) {
	// 1. Setup memory filesystem

	mapFS := fstest.MapFS{
		"listener/database.yaml": &fstest.MapFile{Data: []byte(`
namespace: default
name: database_url
kind: listener
data:
  host: localhost
  port: 5432
`)},
		"service/api.yaml": &fstest.MapFile{Data: []byte(`
namespace: default
name: api_service
kind: service
data:
  url: http://localhost:8080
`)},
	}

	// 2. Initialize components
	hist := history.NewMemory()
	runner := &CustomizableMockRunner{}
	stateBuilder := topology.NewStateBuilder(zap.NewNop())
	dtt := createTestTranscoder()
	folderLoader := loader.NewLoader(dtt, zap.NewNop(), interpolate.NewEntryInterpolator(dtt, interpolate.WithInterpolator(interpolate.LoadVars)))

	reg := NewRegistry(hist, runner, stateBuilder, zap.NewNop()).(*reg)

	// 3. Load entries from the folder
	entries, err := folderLoader.LoadFS(context.Background(), mapFS)
	if err != nil {
		t.Fatalf("failed to load entries from folder: %v", err)
	}

	// 4. Mock process to apply loaded entries to the state
	runner.RunFunc = func(state registry.State, changes registry.ChangeSet) (registry.State, error) {
		newState := state
		for _, change := range changes {
			switch change.Kind {
			case registry.Create:
				newState = append(newState, change.Entry)
			case registry.Update:
				found := false
				for i, entry := range newState {
					if entry.ID == change.Entry.ID {
						newState[i] = change.Entry
						found = true
						break
					}
				}
				if !found {
					return state, fmt.Errorf("entry not found for update: %s", change.Entry.ID)
				}
			case registry.Delete:
				for i, entry := range newState {
					if entry.ID == change.Entry.ID {
						newState = append(newState[:i], newState[i+1:]...)
						break
					}
				}
			default:
				return state, fmt.Errorf("unsupported operation kind: %s", change.Kind)
			}
		}
		return newState, nil
	}

	// 5. Apply the loaded entries as the initial ChangeSet
	initialChangeSet, _ := topology.CreateChangeSetFromEntries(entries)

	newVersion, err := reg.Apply(context.Background(), initialChangeSet)
	if err != nil {
		t.Fatalf("failed to apply initial ChangeSet: %v", err)
	}

	// 6. Verify the state
	if newVersion.ID() != 1 {
		t.Errorf("Expected current version to be 1, got: %v", newVersion.ID())
	}

	expectedState := registry.State{
		{
			ID:   registry.ID{NS: "default", Name: "database_url"},
			Kind: "listener",
			Data: payload.New(map[string]interface{}{
				"namespace": "default",
				"name":      "database_url",
				"kind":      "listener",
				"data": map[string]interface{}{
					"host": "localhost",
					"port": float64(5432), // YAML numbers are unmarshaled as float64
				},
			}),
		},
		{
			ID:   registry.ID{NS: "default", Name: "api_service"},
			Kind: "service",
			Data: payload.New(map[string]interface{}{
				"namespace": "default",
				"name":      "api_service",
				"kind":      "service",
				"data": map[string]interface{}{
					"url": "http://localhost:8080",
				},
			}),
		},
	}

	currentState, err := reg.GetAllEntries()
	if err != nil {
		t.Fatalf("failed to get all entries: %v", err)
	}

	if len(currentState) != len(expectedState) {
		t.Fatalf("Expected state length %d, got %d", len(expectedState), len(currentState))
	}

	for _, expectedEntry := range expectedState {
		found := false
		for _, currentEntry := range currentState {
			if currentEntry.ID == expectedEntry.ID {
				found = true
				assert.Equal(t, expectedEntry.Kind, currentEntry.Kind, "Kind mismatch for Process: %s", expectedEntry.ID)

				// Compare Data field using assert.Equal for deep comparison of maps
				var expectedData, currentData map[string]interface{}
				err = dtt.Unmarshal(expectedEntry.Data, &expectedData)
				assert.NoError(t, err, "Error unmarshalling expected data")
				err = dtt.Unmarshal(currentEntry.Data, &currentData)
				assert.NoError(t, err, "Error unmarshalling current data")

				assert.Equal(t, expectedData, currentData, "Data mismatch for Process: %s", expectedEntry.ID)
				break
			}
		}
		if !found {
			t.Errorf("Expected entry not found in state: %s", expectedEntry.ID)
		}
	}
}

func TestInMemoryRegistry_Current(t *testing.T) {
	hist := history.NewMemory()
	runner := NewMockRunner()
	stateBuilder := topology.NewStateBuilder(zap.NewNop())

	reg := NewRegistry(hist, runner, stateBuilder, zap.NewNop()).(*reg)

	// Test when no current version is set
	_, err := reg.Current()
	if err == nil {
		t.Error("Expected error when no current version is set")
	}
	if !strings.Contains(err.Error(), "no current version") {
		t.Errorf("Expected error message to contain 'no current version', got: %v", err)
	}

	// Test when current version is set
	v0 := version.New(registry.RootVersion)
	reg.currentVersion = v0

	version, err := reg.Current()
	if err != nil {
		t.Errorf("Unexpected error: %v", err)
	}
	if version != v0 {
		t.Errorf("Expected version %v, got %v", v0, version)
	}
}

func TestInMemoryRegistry_History(t *testing.T) {
	hist := history.NewMemory()
	runner := NewMockRunner()
	stateBuilder := topology.NewStateBuilder(zap.NewNop())

	reg := NewRegistry(hist, runner, stateBuilder, zap.NewNop())

	// Test that History() returns the correct history instance
	if reg.History() != hist {
		t.Errorf("Expected history to be %v, got %v", hist, reg.History())
	}
}

func TestInMemoryRegistry_Rollback(t *testing.T) {
	hist := history.NewMemory()
	runner := NewMockRunner()
	stateBuilder := topology.NewStateBuilder(zap.NewNop())

	reg := NewRegistry(hist, runner, stateBuilder, zap.NewNop()).(*reg)

	// Test successful rollback
	fromState := registry.State{
		{ID: registry.ID{Name: "/foo"}, Kind: "test", Data: payload.NewString("data1")},
	}
	toState := registry.State{
		{ID: registry.ID{Name: "/bar"}, Kind: "test", Data: payload.NewString("data2")},
	}

	runner.RunFunc = func(_ registry.State, _ registry.ChangeSet) (registry.State, error) {
		return toState, nil
	}

	err := reg.rollback(context.Background(), fromState, toState)
	if err != nil {
		t.Errorf("Unexpected error during rollback: %v", err)
	}

	// Test rollback failure
	runner.RunFunc = func(_ registry.State, _ registry.ChangeSet) (registry.State, error) {
		return nil, errors.New("rollback failed")
	}

	err = reg.rollback(context.Background(), fromState, toState)
	if err == nil {
		t.Error("Expected error during failed rollback")
	}
	if !strings.Contains(err.Error(), "rollback failed") {
		t.Errorf("Expected error message to contain 'rollback failed', got: %v", err)
	}
}

func TestInMemoryRegistry_TransitionState(t *testing.T) {
	hist := history.NewMemory()
	runner := NewMockRunner()
	stateBuilder := topology.NewStateBuilder(zap.NewNop())

	reg := NewRegistry(hist, runner, stateBuilder, zap.NewNop()).(*reg)

	// Test successful transition
	fromState := registry.State{
		{ID: registry.ID{Name: "/foo"}, Kind: "test", Data: payload.NewString("data1")},
	}
	toState := registry.State{
		{ID: registry.ID{Name: "/bar"}, Kind: "test", Data: payload.NewString("data2")},
	}

	runner.RunFunc = func(_ registry.State, _ registry.ChangeSet) (registry.State, error) {
		return toState, nil
	}

	newState, err := reg.transitionState(context.Background(), fromState, toState)
	if err != nil {
		t.Errorf("Unexpected error during transition: %v", err)
	}
	if !reflect.DeepEqual(newState, toState) {
		t.Errorf("Expected state %v, got %v", toState, newState)
	}

	// Test transition with no changes
	runner.RunFunc = func(_ registry.State, _ registry.ChangeSet) (registry.State, error) {
		return fromState, nil
	}

	newState, err = reg.transitionState(context.Background(), fromState, fromState)
	if err != nil {
		t.Errorf("Unexpected error during transition with no changes: %v", err)
	}
	if !reflect.DeepEqual(newState, fromState) {
		t.Errorf("Expected state %v, got %v", fromState, newState)
	}

	// Test transition failure
	runner.RunFunc = func(_ registry.State, _ registry.ChangeSet) (registry.State, error) {
		return nil, errors.New("transition failed")
	}

	_, err = reg.transitionState(context.Background(), fromState, toState)
	if err == nil {
		t.Error("Expected error during failed transition")
	}
	if !strings.Contains(err.Error(), "transition failed") {
		t.Errorf("Expected error message to contain 'transition failed', got: %v", err)
	}
}
