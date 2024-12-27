package runtime

import (
	"context"
	"fmt"
	"reflect"
	"sync"
	"testing"

	"github.com/ponyruntime/pony/api/registry"
	"github.com/ponyruntime/pony/api/runtime"
)

// MockRuntime implements ExecutableRuntime interface for testing
type MockRuntime struct {
	addLibraryCalls     map[registry.ID]runtime.LibraryConfig
	updateLibraryCalls  map[registry.ID]runtime.LibraryConfig
	addFunctionCalls    map[registry.ID]runtime.FunctionConfig
	updateFunctionCalls map[registry.ID]runtime.FunctionConfig
	deleteCalls         []registry.ID
	executeCalls        []runtime.Task
	executeReturn       struct {
		res chan *runtime.Result
		err error
	}
	mu sync.Mutex
}

func NewMockRuntime() *MockRuntime {
	return &MockRuntime{
		addLibraryCalls:     make(map[registry.ID]runtime.LibraryConfig),
		updateLibraryCalls:  make(map[registry.ID]runtime.LibraryConfig),
		addFunctionCalls:    make(map[registry.ID]runtime.FunctionConfig),
		updateFunctionCalls: make(map[registry.ID]runtime.FunctionConfig),
		deleteCalls:         make([]registry.ID, 0),
		executeCalls:        make([]runtime.Task, 0),
	}
}

func (m *MockRuntime) AddLibrary(id registry.ID, config runtime.LibraryConfig) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.addLibraryCalls[id] = config
	return nil
}

func (m *MockRuntime) UpdateLibrary(id registry.ID, config runtime.LibraryConfig) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.updateLibraryCalls[id] = config
	return nil
}

func (m *MockRuntime) AddFunction(id registry.ID, config runtime.FunctionConfig) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.addFunctionCalls[id] = config
	return nil
}

func (m *MockRuntime) UpdateFunction(id registry.ID, config runtime.FunctionConfig) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.updateFunctionCalls[id] = config
	return nil
}

func (m *MockRuntime) Delete(id registry.ID) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.deleteCalls = append(m.deleteCalls, id)
	return nil
}

func (m *MockRuntime) Execute(task runtime.Task) (chan *runtime.Result, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.executeCalls = append(m.executeCalls, task)
	return m.executeReturn.res, m.executeReturn.err
}

func (m *MockRuntime) AssertAddLibraryCalled(t *testing.T, id registry.ID, config runtime.LibraryConfig) {
	t.Helper()
	m.mu.Lock()
	defer m.mu.Unlock()
	if _, ok := m.addLibraryCalls[id]; !ok {
		t.Errorf("AddLibrary was not called with ID %s", id)
	}
	if !reflect.DeepEqual(m.addLibraryCalls[id], config) {
		t.Errorf("AddLibrary was called with incorrect config for ID %s", id)
	}
}
func (m *MockRuntime) AssertUpdateLibraryCalled(t *testing.T, id registry.ID, config runtime.LibraryConfig) {
	t.Helper()
	m.mu.Lock()
	defer m.mu.Unlock()
	if _, ok := m.updateLibraryCalls[id]; !ok {
		t.Errorf("UpdateLibrary was not called with ID %s", id)
	}
	if !reflect.DeepEqual(m.updateLibraryCalls[id], config) {
		t.Errorf("UpdateLibrary was called with incorrect config for ID %s", id)
	}
}

func (m *MockRuntime) AssertAddFunctionCalled(t *testing.T, id registry.ID, config runtime.FunctionConfig) {
	t.Helper()
	m.mu.Lock()
	defer m.mu.Unlock()
	if _, ok := m.addFunctionCalls[id]; !ok {
		t.Errorf("AddFunction was not called with ID %s", id)
	}
	if !reflect.DeepEqual(m.addFunctionCalls[id], config) {
		t.Errorf("AddFunction was called with incorrect config for ID %s", id)
	}
}

func (m *MockRuntime) AssertUpdateFunctionCalled(t *testing.T, id registry.ID, config runtime.FunctionConfig) {
	t.Helper()
	m.mu.Lock()
	defer m.mu.Unlock()
	if _, ok := m.updateFunctionCalls[id]; !ok {
		t.Errorf("UpdateFunction was not called with ID %s", id)
	}
	if !reflect.DeepEqual(m.updateLibraryCalls[id], config) {
		t.Errorf("UpdateFunction was called with incorrect config for ID %s", id)
	}
}

func (m *MockRuntime) AssertDeleteCalled(t *testing.T, id registry.ID) {
	t.Helper()
	m.mu.Lock()
	defer m.mu.Unlock()
	for _, calledID := range m.deleteCalls {
		if calledID == id {
			return
		}
	}
	t.Errorf("Delete was not called with ID %s", id)
}

func (m *MockRuntime) AssertExecuteCalled(t *testing.T, task runtime.Task) {
	t.Helper()
	m.mu.Lock()
	defer m.mu.Unlock()
	for _, calledTask := range m.executeCalls {
		if calledTask == task {
			return
		}
	}
	t.Errorf("Execute was not called with task %v", task)
}

func TestNewCompositeRuntime(t *testing.T) {
	t.Run("successful creation", func(t *testing.T) {
		mockRuntime := NewMockRuntime()
		cr, err := NewCompositeRuntime(NamedRuntime{
			Name:    "test",
			Runtime: mockRuntime,
		})

		if err != nil {
			t.Errorf("Unexpected error: %v", err)
		}
		if cr == nil {
			t.Error("CompositeRuntime is nil")
		}
		if len(cr.runtimes) != 1 {
			t.Errorf("Expected 1 runtime, got %d", len(cr.runtimes))
		}
	})

	t.Run("no runtimes provided", func(t *testing.T) {
		cr, err := NewCompositeRuntime()
		if err == nil {
			t.Error("Expected an error, got nil")
		}
		if cr != nil {
			t.Error("CompositeRuntime is not nil")
		}
		if err.Error() != "at least one runtime must be provided" {
			t.Errorf("Unexpected error message: %v", err)
		}
	})

	t.Run("duplicate runtime names", func(t *testing.T) {
		mockRuntime1 := NewMockRuntime()
		mockRuntime2 := NewMockRuntime()
		cr, err := NewCompositeRuntime(
			NamedRuntime{Name: "test", Runtime: mockRuntime1},
			NamedRuntime{Name: "test", Runtime: mockRuntime2},
		)

		if err == nil {
			t.Error("Expected an error, got nil")
		}
		if cr != nil {
			t.Error("CompositeRuntime is not nil")
		}
		if err.Error() != "duplicate runtime name: test" {
			t.Errorf("Unexpected error message: %v", err)
		}
	})
}

func TestCompositeRuntime_AddLibrary(t *testing.T) {
	mockRuntime := NewMockRuntime()
	cr, _ := NewCompositeRuntime(NamedRuntime{
		Name:    "test",
		Runtime: mockRuntime,
	})

	t.Run("successful addition", func(t *testing.T) {
		id := registry.ID("test-lib")
		config := runtime.LibraryConfig{
			Meta: registry.Metadata{
				"runtime": "test",
			},
			Source: "source code",
		}

		err := cr.AddLibrary(id, config)
		if err != nil {
			t.Errorf("Unexpected error: %v", err)
		}

		// Verify routing was stored
		runtime, ok := cr.routing.Load(string(id))
		if !ok {
			t.Error("Routing not stored")
		}
		if runtime != "test" {
			t.Errorf("Expected runtime 'test', got '%s'", runtime)
		}
		mockRuntime.AssertAddLibraryCalled(t, id, config)
	})

	t.Run("missing runtime tag", func(t *testing.T) {
		id := registry.ID("test-lib")
		config := runtime.LibraryConfig{
			Meta:   registry.Metadata{},
			Source: "source code",
		}

		err := cr.AddLibrary(id, config)
		if err == nil {
			t.Fatal("Expected an error for missing runtime tag, got nil")
		}
		expectedMsg := fmt.Sprintf("no runtime specified for ID %s", id)
		if err.Error() != expectedMsg {
			t.Errorf("Expected error message %q, got %q", expectedMsg, err.Error())
		}
	})

	t.Run("runtime not found", func(t *testing.T) {
		id := registry.ID("test-lib")
		config := runtime.LibraryConfig{
			Meta: registry.Metadata{
				"runtime": "nonexistent",
			},
			Source: "source code",
		}

		err := cr.AddLibrary(id, config)
		if err == nil {
			t.Error("Expected an error, got nil")
			return
		}

		if err.Error() != "runtime nonexistent not found for library test-lib" {
			t.Errorf("Unexpected error message: %v", err)
		}
	})

	t.Run("invalid config", func(t *testing.T) {
		id := registry.ID("test-lib")
		config := runtime.LibraryConfig{
			Meta: registry.Metadata{
				"runtime": "test",
			},
		}

		err := cr.AddLibrary(id, config)
		if err == nil {
			t.Error("Expected error, got nil")
		}
	})
}

func TestCompositeRuntime_UpdateLibrary(t *testing.T) {
	mockRuntime := NewMockRuntime()
	cr, _ := NewCompositeRuntime(NamedRuntime{
		Name:    "test",
		Runtime: mockRuntime,
	})

	t.Run("successful update", func(t *testing.T) {
		id := registry.ID("test-lib")
		config := runtime.LibraryConfig{
			Meta: registry.Metadata{
				"runtime": "test",
			},
			Source: "updated source",
		}

		// First add the library
		cr.AddLibrary(id, config)

		// Then update it
		err := cr.UpdateLibrary(id, config)
		if err != nil {
			t.Errorf("Unexpected error: %v", err)
		}
		mockRuntime.AssertUpdateLibraryCalled(t, id, config)
	})
}

func TestCompositeRuntime_Execute(t *testing.T) {
	mockRuntime := NewMockRuntime()
	cr, _ := NewCompositeRuntime(NamedRuntime{
		Name:    "test",
		Runtime: mockRuntime,
	})

	t.Run("successful execution", func(t *testing.T) {
		id := registry.ID("test-func")
		resultChan := make(chan *runtime.Result, 1)
		mockRuntime.executeReturn.res = resultChan
		mockRuntime.executeReturn.err = nil

		task := runtime.Task{
			Context: context.Background(),
			Target:  id,
			Payload: nil,
		}

		// First register the function
		config := runtime.FunctionConfig{
			Meta: registry.Metadata{
				"runtime": "test",
			},
			Source: "source",
			Method: "test",
		}

		cr.AddFunction(id, config)

		// Then execute it
		ch, err := cr.Execute(task)
		if err != nil {
			t.Errorf("Unexpected error: %v", err)
		}
		if ch != resultChan {
			t.Error("Incorrect result channel returned")
		}
		mockRuntime.AssertExecuteCalled(t, task)
	})

	t.Run("execution of unknown function", func(t *testing.T) {
		task := runtime.Task{
			Context: context.Background(),
			Target:  registry.ID("unknown"),
			Payload: nil,
		}

		ch, err := cr.Execute(task)
		if err == nil {
			t.Error("Expected an error, got nil")
		}
		if ch != nil {
			t.Error("Expected nil channel, got a channel")
		}
		if err.Error() != "no runtime specified for ID unknown" {
			t.Errorf("Unexpected error: %v", err)
		}
	})
}

func TestCompositeRuntime_Delete(t *testing.T) {
	mockRuntime := NewMockRuntime()
	cr, _ := NewCompositeRuntime(NamedRuntime{
		Name:    "test",
		Runtime: mockRuntime,
	})

	t.Run("successful deletion", func(t *testing.T) {
		id := registry.ID("test-func")

		// First add something
		config := runtime.FunctionConfig{
			Meta: registry.Metadata{
				"runtime": "test",
			},
			Source: "source",
			Method: "test",
		}

		cr.AddFunction(id, config)

		// Then delete it
		err := cr.Delete(id)
		if err != nil {
			t.Errorf("Unexpected error: %v", err)
		}

		// Verify it was removed from routing
		_, ok := cr.routing.Load(string(id))
		if ok {
			t.Error("Routing not deleted")
		}
		mockRuntime.AssertDeleteCalled(t, id)
	})

	t.Run("delete nonexistent", func(t *testing.T) {
		id := registry.ID("nonexistent")
		err := cr.Delete(id)
		if err == nil {
			t.Error("Expected an error, got nil")
		}
		if err.Error() != "no runtime found for id: nonexistent" {
			t.Errorf("Unexpected error message: %v", err)
		}
	})
}

func TestCompositeRuntime_AddFunction(t *testing.T) {
	mockRuntime := NewMockRuntime()
	cr, _ := NewCompositeRuntime(NamedRuntime{
		Name:    "test",
		Runtime: mockRuntime,
	})

	t.Run("successful addition", func(t *testing.T) {
		id := registry.ID("test-func")
		config := runtime.FunctionConfig{
			Meta: registry.Metadata{
				"runtime": "test",
			},
			Source: "source",
			Method: "test",
		}

		err := cr.AddFunction(id, config)
		if err != nil {
			t.Errorf("Unexpected error: %v", err)
		}

		// Verify routing was stored
		runtime, ok := cr.routing.Load(string(id))
		if !ok {
			t.Error("Routing not stored")
		}
		if runtime != "test" {
			t.Errorf("Expected runtime 'test', got '%s'", runtime)
		}
		mockRuntime.AssertAddFunctionCalled(t, id, config)
	})
	t.Run("invalid config", func(t *testing.T) {
		id := registry.ID("test-func")
		config := runtime.FunctionConfig{
			Meta: registry.Metadata{
				"runtime": "test",
			},
		}
		err := cr.AddFunction(id, config)
		if err == nil {
			t.Error("Expected error, got nil")
		}
	})
}

func TestCompositeRuntime_UpdateFunction(t *testing.T) {
	mockRuntime := NewMockRuntime()
	cr, _ := NewCompositeRuntime(NamedRuntime{
		Name:    "test",
		Runtime: mockRuntime,
	})

	t.Run("successful update", func(t *testing.T) {
		id := registry.ID("test-func")
		config := runtime.FunctionConfig{
			Meta: registry.Metadata{
				"runtime": "test",
			},
			Source: "source",
			Method: "test",
		}

		// First add the function
		cr.AddFunction(id, config)

		// Then update it
		err := cr.UpdateFunction(id, config)
		if err != nil {
			t.Errorf("Unexpected error: %v", err)
		}
		mockRuntime.AssertUpdateFunctionCalled(t, id, config)
	})
}
