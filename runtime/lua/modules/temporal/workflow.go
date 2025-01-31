package temporal

import (
	"github.com/ponyruntime/pony/api/runtime/temporal"
	lua "github.com/yuin/gopher-lua"
	"go.uber.org/zap"
)

// WorkflowExecution wraps a temporal workflow execution
type WorkflowExecution struct {
	execution temporal.WorkflowExecution
	log       *zap.Logger
}

func checkWorkflowExecution(l *lua.LState) *WorkflowExecution {
	ud := l.CheckUserData(1)
	if v, ok := ud.Value.(*WorkflowExecution); ok {
		return v
	}
	l.ArgError(1, "workflow execution expected")
	return nil
}

// Creates new workflow execution userdata
func newWorkflow(l *lua.LState, execution temporal.WorkflowExecution, log *zap.Logger) *lua.LUserData {
	ud := l.NewUserData()
	ud.Value = &WorkflowExecution{
		execution: execution,
		log:       log,
	}

	// Set workflow methods
	mt := l.NewTable()
	l.SetField(mt, "__index", l.SetFuncs(l.NewTable(), map[string]lua.LGFunction{
		"id":     getWorkflowID,
		"run_id": getRunID,
		"error":  getError,
		"signal": signalWorkflow,
	}))
	l.SetMetatable(ud, mt)

	return ud
}

// Workflow execution methods

func getWorkflowID(l *lua.LState) int {
	we := checkWorkflowExecution(l)
	l.Push(lua.LString(we.execution.GetID()))
	return 1
}

func getRunID(l *lua.LState) int {
	we := checkWorkflowExecution(l)
	l.Push(lua.LString(we.execution.GetRunID()))
	return 1
}

func getError(l *lua.LState) int {
	we := checkWorkflowExecution(l)
	if err := we.execution.Error(); err != nil {
		l.Push(lua.LString(err.Error()))
	} else {
		l.Push(lua.LNil)
	}
	return 1
}

func signalWorkflow(l *lua.LState) int {
	we := checkWorkflowExecution(l)
	signalName := l.CheckString(2)
	args := l.CheckAny(3)

	err := we.execution.Signal(l.Context(), signalName, args)
	if err != nil {
		l.Push(lua.LFalse)
		l.Push(lua.LString(err.Error()))
		return 2
	}

	l.Push(lua.LTrue)
	return 1
}
