package command

//
//// Layer implements the command processing middleware
//type Layer struct{}
//
//// NewCommandLayer creates a new command processing layer
//func NewCommandLayer() *Layer {
//	return &Layer{}
//}
//
//// InitUnitOfWork initializes the command context for a new unit of work
//func (l *Layer) InitUnitOfWork(uw engine.UnitOfWork) {
//	// Configure UoW scope for the layer
//	uw.Values().Set(cmdContext, &layerContext{
//		queue: list.New(),
//		results: list.New(),
//	})
//}
//
//// Step implements the engine.Layer interface
//func (l *Layer) Step(cvm engine.CVM, tasks ...*engine.Task) ([]*engine.Task, error) {
//	// Get unit of work from VM context
//	ctx := cvm.State().Context()
//
//	// Process any queued results
//	if err := processResults(ctx); err != nil {
//		return nil, fmt.Errorf("process results error: %w", err)
//	}
//
//	// Process tasks through chain
//	outTasks, err := cvm.Step(tasks...)
//	if err != nil {
//		return nil, fmt.Errorf("step error: %w", err)
//	}
//
//	// Check for command yields
//	var processableTasks []*engine.Task
//	for _, task := range outTasks {
//		if len(task.Yielded) == 0 {
//			processableTasks = append(processableTasks, task)
//			continue
//		}
//
//		// Check if last yield is a command
//		if cmd, ok := isCommand(task.Yielded[len(task.Yielded)-1]); ok {
//			// Schedule using context-based approach
//			if err := Schedule(ctx, cmd); err != nil {
//				return nil, fmt.Errorf("schedule error: %w", err)
//			}
//			continue
//		}
//
//		processableTasks = append(processableTasks, task)
//	}
//
//	return processableTasks, nil
//}
//
//// isCommand checks if a value is a Command instance
//func isCommand(v lua.LValue) (*Command, bool) {
//	if ud, ok := v.(*lua.LUserData); ok {
//		if cmd, ok := ud.Value.(*Command); ok {
//			return cmd, true
//		}
//	}
//
//	return nil, false
//}
