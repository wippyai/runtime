package runtime

import (
	"errors"
	"fmt"
	"github.com/ponyruntime/pony/api/runtime"
	"sync"

	"github.com/ponyruntime/pony/api/registry"
)

// ExecutableRuntime combines the Runtime and Executor interfaces.
type ExecutableRuntime interface {
	runtime.Runtime
	runtime.Executor
}

// NamedRuntime pairs a runtime name with an ExecutableRuntime instance.
type NamedRuntime struct {
	Name    string
	Runtime ExecutableRuntime
}

// CompositeRuntime is a runtime that delegates to other runtimes based on the RuntimeTag.
type CompositeRuntime struct {
	runtimes map[string]ExecutableRuntime
	routing  sync.Map // ID (string) -> runtime name (string)
}

// NewCompositeRuntime creates a new CompositeRuntime.
func NewCompositeRuntime(namedRuntimes ...NamedRuntime) (*CompositeRuntime, error) {
	if len(namedRuntimes) == 0 {
		return nil, errors.New("at least one runtime must be provided")
	}

	runtimes := make(map[string]ExecutableRuntime)
	for _, nr := range namedRuntimes {
		if _, exists := runtimes[nr.Name]; exists {
			return nil, fmt.Errorf("duplicate runtime name: %s", nr.Name)
		}
		runtimes[nr.Name] = nr.Runtime
	}

	return &CompositeRuntime{
		runtimes: runtimes,
		routing:  sync.Map{},
	}, nil
}

// getRuntimeName returns the runtime name for the given ID. It uses sync.Map for safe reads.
func (cr *CompositeRuntime) getRuntimeName(id registry.ID, meta registry.Metadata) (string, error) {
	if name, ok := cr.routing.Load(string(id)); ok {
		return name.(string), nil
	}

	// Fallback to metadata if not found in routing table
	runtimeName := meta.StringValue(runtime.RuntimeTag)
	if runtimeName == "" {
		return "", fmt.Errorf("no runtime specified for ID %s", id)
	}

	return runtimeName, nil
}

// AddLibrary adds a library to the appropriate runtime.
func (cr *CompositeRuntime) AddLibrary(id registry.ID, config runtime.LibraryConfig) error {
	runtimeName, err := cr.getRuntimeName(id, config.Meta)
	if err != nil {
		return err
	}

	run, ok := cr.runtimes[runtimeName]
	if !ok {
		return fmt.Errorf("runtime %s not found for library %s", runtimeName, id)
	}

	if err := run.AddLibrary(id, config); err != nil {
		return err
	}

	cr.routing.Store(string(id), runtimeName)
	return nil
}

// UpdateLibrary updates a library in the appropriate runtime.
func (cr *CompositeRuntime) UpdateLibrary(id registry.ID, config runtime.LibraryConfig) error {
	runtimeName, err := cr.getRuntimeName(id, config.Meta)
	if err != nil {
		return err
	}

	run, ok := cr.runtimes[runtimeName]
	if !ok {
		return fmt.Errorf("runtime %s not found for library %s", runtimeName, id)
	}

	if err := run.UpdateLibrary(id, config); err != nil {
		return err
	}
	// sync map update
	cr.routing.Store(string(id), runtimeName)
	return nil
}

// AddFunction adds a function to the appropriate runtime.
func (cr *CompositeRuntime) AddFunction(id registry.ID, config runtime.FunctionConfig) error {
	runtimeName, err := cr.getRuntimeName(id, config.Meta)
	if err != nil {
		return err
	}

	run, ok := cr.runtimes[runtimeName]
	if !ok {
		return fmt.Errorf("runtime %s not found for function %s", runtimeName, id)
	}

	if err := run.AddFunction(id, config); err != nil {
		return err
	}

	// Store in sync.Map
	cr.routing.Store(string(id), runtimeName)
	return nil
}

// UpdateFunction updates a function in the appropriate runtime.
func (cr *CompositeRuntime) UpdateFunction(id registry.ID, config runtime.FunctionConfig) error {
	runtimeName, err := cr.getRuntimeName(id, config.Meta)
	if err != nil {
		return err
	}

	run, ok := cr.runtimes[runtimeName]
	if !ok {
		return fmt.Errorf("runtime %s not found for function %s", runtimeName, id)
	}

	if err := run.UpdateFunction(id, config); err != nil {
		return err
	}
	// sync map update
	cr.routing.Store(string(id), runtimeName)
	return nil
}

// Delete deletes a function or library from the appropriate runtime.
func (cr *CompositeRuntime) Delete(id registry.ID) error {
	// Load from sync.Map to find the run
	runtimeName, ok := cr.routing.Load(string(id))
	if !ok {
		return fmt.Errorf("no runtime found for id: %s", id)
	}

	run, ok := cr.runtimes[runtimeName.(string)]
	if !ok {
		return fmt.Errorf("runtime %s not found for id %s", runtimeName, id)
	}

	if err := run.Delete(id); err != nil {
		return err
	}

	// Delete from sync.Map
	cr.routing.Delete(string(id))
	return nil
}

// Execute executes a task using the appropriate executor.
func (cr *CompositeRuntime) Execute(task runtime.Task) (chan *runtime.Result, error) {
	runtimeName, err := cr.getRuntimeName(task.Target, registry.Metadata{})
	if err != nil {
		return nil, err
	}

	executor, ok := cr.runtimes[runtimeName]
	if !ok {
		return nil, fmt.Errorf("runtime %s not found for target %s", runtimeName, task.Target)
	}
	return executor.Execute(task)
}
