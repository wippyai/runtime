package engine

//
//import (
//	"context"
//	"fmt"
//	api "github.com/ponyruntime/pony/api/runtime/lua"
//	"github.com/ponyruntime/pony/internal/closer"
//	lua "github.com/yuin/gopher-lua"
//	"go.uber.org/zap"
//)
//
//type LayerExecutor struct {
//	cvm    *CoroutineVM
//	layers []api.Layer
//}
//
//func NewLayerExecutor(cvm *CoroutineVM) *LayerExecutor {
//	return &LayerExecutor{
//		cvm:    cvm,
//		layers: make([]api.Layer, 0),
//	}
//}
//
//func (e *LayerExecutor) AddLayer(layer api.Layer) {
//	e.layers = append(e.layers, layer)
//}
//
//func (e *LayerExecutor) Execute(
//	ctx context.Context,
//	funcName string,
//	args ...lua.LValue,
//) (lua.LValue, error) {
//	fn, ok := e.cvm.vm.exported[funcName]
//	if !ok {
//		return nil, fmt.Errorf("function %q not found", funcName)
//	}
//
//	// we reset cvm context within this call
//	if ctx != nil {
//		ctx, cleanup := closer.WithContext(ctx)
//		defer func() {
//			e.cvm.vm.state.RemoveContext()
//			if err := cleanup.Close(); err != nil {
//				e.cvm.vm.log.Error("cleanup failed",
//					zap.String("function", funcName),
//					zap.Error(err))
//			}
//		}()
//		e.cvm.vm.state.SetContext(ctx)
//	}
//
//	// Get initial task from CVM
//	task, err := e.cvm.GetTask(thread) // we need to get initial task somehow
//	if err != nil {
//		return nil, err
//	}
//
//	tasks := []*Task{task}
//
//	// Process through layers until complete
//	for len(tasks) > 0 {
//		// Process through each layer
//		var externalTasks []*Task
//
//		for _, layer := range e.layers {
//			var err error
//			tasks, err = layer.Step(e.cvm, tasks...)
//			if err != nil {
//				return nil, err
//			}
//		}
//
//		// If no layer handled tasks, pass to base CVM
//		if len(tasks) > 0 {
//			var err error
//			tasks, err = e.cvm.Step(tasks...)
//			if err != nil {
//				return nil, err
//			}
//		}
//	}
//
//	// Need to get final result from somewhere
//	return nil, nil
//}
