package temporal

import (
	"fmt"
	"github.com/ponyruntime/pony/api/payload"
	"github.com/ponyruntime/pony/service/temporal/client"
	lua "github.com/yuin/gopher-lua"
	temporal "go.temporal.io/sdk/client"
	"go.uber.org/zap"
)

// Client wraps a temporal client for Lua access
type Client struct {
	client *client.Client
	log    *zap.Logger
}

func CheckClient(l *lua.LState) *Client {
	ud := l.CheckUserData(1)
	if v, ok := ud.Value.(*Client); ok {
		return v
	}
	l.ArgError(1, "temporal client expected")
	return nil
}

func CheckTemporalClient(l *lua.LState) temporal.Client {
	c := CheckClient(l)
	if c == nil {
		l.ArgError(1, "temporal client not initialized")
		return nil
	}

	tClient, err := c.client.GetClient()
	if err != nil {
		l.ArgError(1, err.Error())
		return nil
	}

	return tClient
}

// Client methods
func healthcheck(l *lua.LState) int {
	c := CheckTemporalClient(l)
	_, err := c.CheckHealth(l.Context(), &temporal.CheckHealthRequest{})
	if err != nil {
		l.Push(lua.LNil)
		l.Push(lua.LString(err.Error()))
		return 2
	}

	l.Push(lua.LBool(true))
	return 1
}

// Client methods
func execute(l *lua.LState) int {
	c := CheckClient(l)
	if c == nil {
		return 0
	}

	workflowType := l.CheckString(2)
	if workflowType == "" {
		l.Push(lua.LNil)
		l.Push(lua.LString("workflow type required"))
		return 2
	}

	options := l.CheckTable(3)
	if options == nil {
		l.Push(lua.LNil)
		l.Push(lua.LString("options table required"))
		return 2
	}

	startOptions := temporal.StartWorkflowOptions{
		TaskQueue: options.RawGetString("task_queue").String(),
	}

	dtt := payload.GetTranscoder(l.Context())
	args := make([]interface{}, l.GetTop()-3)
	for i := 4; i <= l.GetTop(); i++ {
		val, err := dtt.Transcode(payload.NewPayload(l.Get(i), payload.Lua), payload.Golang)
		if err != nil {
			l.Push(lua.LNil)
			l.Push(lua.LString(fmt.Sprintf("failed to convert argument %d: %v", i-3, err)))
			return 2
		}
		args[i-4] = val.Data()
	}

	tClient := CheckTemporalClient(l)
	execution, err := tClient.ExecuteWorkflow(l.Context(), startOptions, workflowType, args...)
	if err != nil {
		l.Push(lua.LNil)
		l.Push(lua.LString(err.Error()))
		return 2
	}

	ud := l.NewUserData()
	ud.Value = &Workflow{
		log:        c.log,
		client:     tClient,
		workflowID: execution.GetID(),
		runID:      execution.GetRunID(),
		execution:  execution,
	}
	l.SetMetatable(ud, l.GetTypeMetatable("Temporal.Workflow"))
	l.Push(ud)
	return 1
}

func getWorkflow(l *lua.LState) int {
	c := CheckClient(l)
	if c == nil {
		return 0
	}

	workflowID := l.CheckString(2)
	if workflowID == "" {
		l.Push(lua.LNil)
		l.Push(lua.LString("workflow ID required"))
		return 2
	}

	runID := l.OptString(3, "")

	tClient := CheckTemporalClient(l)
	handle := tClient.GetWorkflow(l.Context(), workflowID, runID)

	ud := l.NewUserData()
	ud.Value = &Workflow{
		log:        c.log,
		client:     tClient,
		workflowID: workflowID,
		runID:      runID,
		execution:  handle,
	}
	l.SetMetatable(ud, l.GetTypeMetatable("Temporal.Workflow"))
	l.Push(ud)
	return 1
}

// Register client methods
func registerClient(l *lua.LState) {
	mt := l.NewTypeMetatable("Temporal.Client")
	l.SetField(mt, "__index", l.SetFuncs(l.NewTable(), map[string]lua.LGFunction{
		"healthcheck":  healthcheck,
		"execute":      execute,
		"get_workflow": getWorkflow,
	}))
}
