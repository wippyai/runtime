package temporal

import (
	"fmt"
	payload "github.com/ponyruntime/pony/api/payload"
	"github.com/ponyruntime/pony/runtime/lua/engine/async"
	"github.com/ponyruntime/pony/runtime/lua/engine/channel"
	lua "github.com/yuin/gopher-lua"
	"go.temporal.io/sdk/client"
	"go.uber.org/zap"
	"log"
)

// Workflow wraps a temporal workflow execution
type Workflow struct {
	log        *zap.Logger
	client     client.Client
	workflowID string
	runID      string
	execution  client.WorkflowRun
}

func CheckWorkflow(l *lua.LState) *Workflow {
	ud := l.CheckUserData(1)
	if v, ok := ud.Value.(*Workflow); ok {
		return v
	}
	l.ArgError(1, "temporal workflow expected")
	return nil
}

func info(l *lua.LState) int {
	w := CheckWorkflow(l)
	if w == nil {
		return 0
	}

	result := l.NewTable()
	l.SetField(result, "workflow_id", lua.LString(w.workflowID))
	l.SetField(result, "run_id", lua.LString(w.runID))
	l.Push(result)
	return 1
}

func signal(l *lua.LState) int {
	w := CheckWorkflow(l)
	if w == nil {
		return 0
	}

	signalName := l.CheckString(2)
	if signalName == "" {
		l.Push(lua.LNil)
		l.Push(lua.LString("signal name required"))
		return 2
	}

	// Debug
	w.log.Info("sending signal",
		zap.String("workflow_id", w.workflowID),
		zap.String("run_id", w.runID),
		zap.String("signal", signalName))

	dtt := payload.GetTranscoder(l.Context())
	args := make([]interface{}, l.GetTop()-2)
	for i := 3; i <= l.GetTop(); i++ {
		val, err := dtt.Transcode(payload.NewPayload(l.Get(i), payload.Lua), payload.Golang)
		if err != nil {
			l.Push(lua.LNil)
			l.Push(lua.LString(fmt.Sprintf("failed to convert argument %d: %v", i-2, err)))
			return 2
		}
		args[i-3] = val.Data()
		w.log.Debug("signal arg", zap.Any("arg", args[i-3]))
	}

	log.Printf("args: %v", args)

	err := w.client.SignalWorkflow(l.Context(), w.workflowID, w.runID, signalName, args[0]) // todo: i dont like it
	if err != nil {
		w.log.Error("signal failed",
			zap.String("workflow_id", w.workflowID),
			zap.String("run_id", w.runID),
			zap.String("signal", signalName),
			zap.Error(err))
		l.Push(lua.LNil)
		l.Push(lua.LString(err.Error()))
		return 2
	}

	w.log.Info("signal sent successfully",
		zap.String("workflow_id", w.workflowID),
		zap.String("run_id", w.runID),
		zap.String("signal", signalName))

	l.Push(lua.LTrue)
	return 1
}

func response(l *lua.LState) int {
	w := CheckWorkflow(l)
	if w == nil {
		return 0
	}

	ch := channel.Named(fmt.Sprintf("workflow_%s_%s", w.workflowID, w.runID), 1)
	go func() {
		var result interface{}
		err := w.execution.Get(l.Context(), &result)
		if err != nil {
			w.log.Error("workflow execution failed",
				zap.String("workflow_id", w.workflowID),
				zap.String("run_id", w.runID),
				zap.Error(err))
			_ = async.Send(l, ch, lua.LNil, false)
			return
		}

		log.Printf("result: %v", result)

		dtt := payload.GetTranscoder(l.Context())
		luaValue, err := dtt.Transcode(payload.NewPayload(result, payload.Golang), payload.Lua)
		if err != nil {
			w.log.Error("failed to convert workflow result",
				zap.String("workflow_id", w.workflowID),
				zap.String("run_id", w.runID),
				zap.Error(err))
			_ = async.Send(l, ch, lua.LNil, false)
			return
		}

		_ = async.Send(l, ch, luaValue.Data().(lua.LValue), true)
		_ = async.Send(l, ch, lua.LNil, false)
	}()

	l.Push(channel.Wrap(l, ch))
	return 1
}

func error(l *lua.LState) int {
	w := CheckWorkflow(l)
	if w == nil {
		return 0
	}

	var result interface{}
	err := w.execution.Get(l.Context(), &result)
	if err != nil {
		l.Push(lua.LString(err.Error()))
		return 1
	}

	l.Push(lua.LNil)
	return 1
}

func registerWorkflow(l *lua.LState) {
	mt := l.NewTypeMetatable("Temporal.Workflow")
	l.SetField(mt, "__index", l.SetFuncs(l.NewTable(), map[string]lua.LGFunction{
		"info":     info,
		"signal":   signal,
		"response": response,
		"error":    error,
	}))
}
