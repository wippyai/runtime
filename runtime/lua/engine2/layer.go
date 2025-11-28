package engine2

// Layer processes tasks during Step execution.
// Layers handle internal yields (channels, subscriptions) before
// external yields bubble up to the scheduler.
type Layer interface {
	// Step processes tasks, handling internal operations.
	// Returns tasks that need external handling (e.g., time.sleep).
	Step(proc *Process, tasks ...*Task) ([]*Task, error)
}

// LayerFunc is a function adapter for Layer interface.
type LayerFunc func(proc *Process, tasks ...*Task) ([]*Task, error)

func (f LayerFunc) Step(proc *Process, tasks ...*Task) ([]*Task, error) {
	return f(proc, tasks...)
}
