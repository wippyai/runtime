// layered.go
package engine

//
//// Layer defines a function type that can intercept and modify VM step operations
//type Layer func(vm VM, tasks ...*Task) ([]*Task, error)
//
//// VMLayer represents a layer that can be wrapped around a VM
//type VMLayer interface {
//	VM
//	GetWrapped() VM
//}
//
//// StepInterceptorLayer implements a VM layer that adds step interception
//type StepInterceptorLayer struct {
//	vm    *CoroutineVM
//	layer Layer
//}
//
//// NewStepInterceptorLayer creates a new layer with step interception
//func NewStepInterceptorLayer(vm *CoroutineVM, layer Layer) *StepInterceptorLayer {
//	return &StepInterceptorLayer{
//		vm:    vm,
//		layer: layer,
//	}
//}
//
//// todo: register code and not func names?
//// todo: detect func names automatically on vm level?
//
//func (l *StepInterceptorLayer) Execute(ctx context.Context, name string, args ...lua.LValue) (lua.LValue, error) {
//	prev := l.vm.PushContext(ctx)
//	defer l.vm.PushContext(prev)
//
//	// Create initial coroutine
//	fn, ok := l.vm.vm.State().GetGlobal(name).(*lua.LFunction)
//	if !ok {
//		return nil, fmt.Errorf("function %q not found", name)
//	}
//
//	//if ctx != nil {
//	//	ctx, cleanup := closer.WithContext(ctx)
//	//	defer func() {
//	//		v.state.RemoveContext()
//	//		if err := cleanup.Close(); err != nil {
//	//			v.log.Error("cleanup failed",
//	//				zap.String("function", funcName),
//	//				zap.Error(err))
//	//		}
//	//	}()
//	//	v.state.SetContext(ctx)
//	//}
//
//	task, err := l.vm.createCoroutine(fn)
//	if err != nil {
//		return nil, fmt.Errorf("failed to create coroutine: %w", err)
//	}
//	task.Resumed = args
//
//	// Execute with layer processing
//	var result lua.LValue
//	for {
//		tasks, err := l.vm.Step(task)
//		if err != nil {
//			return nil, fmt.Errorf("execution error: %w", err)
//		}
//
//		// If no more tasks and we have a result, we're done
//		if len(tasks) == 0 {
//			// Task completed, return its result
//			if task.State == lua.ResumeOK {
//				result = task.Result
//				break
//			}
//			return nil, fmt.Errorf("coroutine terminated without result")
//		}
//
//		// Apply layer
//		tasks, err = l.layer(l.vm, tasks...)
//		if err != nil {
//			return nil, fmt.Errorf("layer error: %w", err)
//		}
//
//		// Any remaining tasks at this layer are an error - they should have been handled by a layer
//		if len(tasks) > 0 {
//			return nil, fmt.Errorf("unhandled tasks detected: no layer claimed responsibility")
//		}
//	}
//
//	return result, nil
//}
//
//func (l *StepInterceptorLayer) Close() {
//	l.vm.Close()
//}
//
//func (l *StepInterceptorLayer) GetWrapped() VM {
//	return l.vm
//}
//
//// LayeredVM represents a VM with support for multiple layers of functionality
//type LayeredVM struct {
//	base   *CoroutineVM
//	layers []VMLayer
//}
//
//// NewLayeredVM creates a new LayeredVM with the given base VM
//func NewLayeredVM(base *CoroutineVM) *LayeredVM {
//	return &LayeredVM{
//		base:   base,
//		layers: make([]VMLayer, 0),
//	}
//}
//
//// AddLayer adds a new layer to the VM
//func (l *LayeredVM) AddLayer(layer VMLayer) {
//	l.layers = append(l.layers, layer)
//}
//
//// Execute implements the VM interface
//func (l *LayeredVM) Execute(ctx context.Context, name string, args ...lua.LValue) (lua.LValue, error) {
//	// Get the outermost layer or use base VM
//	var executor VM
//	if len(l.layers) > 0 {
//		executor = l.layers[len(l.layers)-1]
//	} else {
//		executor = l.base
//	}
//
//	return executor.Execute(ctx, name, args...)
//}
//
//// Close implements the VM interface
//func (l *LayeredVM) Close() {
//	// Close from outermost to innermost
//	for i := len(l.layers) - 1; i >= 0; i-- {
//		l.layers[i].Close()
//	}
//	l.base.Close()
//}
