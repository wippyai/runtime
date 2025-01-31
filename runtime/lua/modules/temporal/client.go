package temporal

import (
	"github.com/ponyruntime/pony/api/runtime/temporal"
	lua "github.com/yuin/gopher-lua"
	"go.uber.org/zap"
)

// Client wraps a temporal client for Lua access
type Client struct {
	client temporal.Client
	log    *zap.Logger
}

// checkClient ensures argument is a temporal client userdata
func checkClient(l *lua.LState) *Client {
	ud := l.CheckUserData(1)
	if v, ok := ud.Value.(*Client); ok {
		return v
	}
	l.ArgError(1, "temporal client expected")
	return nil
}

// Creates new client userdata
func newClient(l *lua.LState, client temporal.Client, log *zap.Logger) *lua.LUserData {
	ud := l.NewUserData()
	ud.Value = &Client{
		client: client,
		log:    log,
	}

	// Set client methods
	mt := l.NewTable()
	l.SetField(mt, "__index", l.SetFuncs(l.NewTable(), map[string]lua.LGFunction{
		"execute": executeWorkflow,
	}))
	l.SetMetatable(ud, mt)

	return ud
}

// executeWorkflow implements client:execute() in Lua
// returns workflow execution or (nil, error)
func executeWorkflow(l *lua.LState) int {
	c := checkClient(l)
	workflowID := l.CheckString(2)
	opts := l.CheckTable(3)

	// Extract task queue option
	taskQueue := opts.RawGetString("task_queue").String()
	if taskQueue == "" {
		l.Push(lua.LNil)
		l.Push(lua.LString("task_queue is required"))
		return 2
	}

	// Start workflow execution
	execution, err := c.client.ExecuteWorkflow(l.Context(), workflowID, map[string]interface{}{
		"task_queue": taskQueue,
	})
	if err != nil {
		l.Push(lua.LNil)
		l.Push(lua.LString(err.Error()))
		return 2
	}

	// Return workflow execution userdata
	l.Push(newWorkflow(l, execution, c.log.With(zap.String("workflow_id", workflowID))))
	return 1
}
