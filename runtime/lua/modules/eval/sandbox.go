package eval

import (
	"context"
	"fmt"

	"github.com/wippyai/runtime/api/payload"
	"github.com/wippyai/runtime/api/process"
	"github.com/wippyai/runtime/api/registry"
	"github.com/wippyai/runtime/api/resource"
	luaconv "github.com/wippyai/runtime/runtime/lua/engine/payload"
	"github.com/wippyai/runtime/runtime/lua/engine/value"
	"github.com/wippyai/runtime/runtime/lua/evalhost"
	lua "github.com/yuin/gopher-lua"
)

// Sandbox wraps a process for manual stepping.
type Sandbox struct {
	sourceOrID    string
	modules       []string
	process       process.Process
	ctx           context.Context
	started       bool
	transcoder    *CommandTranscoder
	cancelCleanup func()
}

var sandboxMethods = map[string]lua.LGFunction{
	"execute": sandboxExecute,
	"step":    sandboxStep,
	"close":   sandboxClose,
}

func checkSandbox(l *lua.LState, idx int) *Sandbox {
	ud := l.CheckUserData(idx)
	if v, ok := ud.Value.(*Sandbox); ok {
		return v
	}
	l.ArgError(idx, "Sandbox expected")
	return nil
}

// sandboxExecute starts execution: sandbox:execute(method, args...)
func sandboxExecute(l *lua.LState) int {
	sb := checkSandbox(l, 1)
	if sb == nil {
		return 0
	}

	if sb.started {
		l.Push(lua.LNil)
		l.Push(lua.LString("sandbox already started"))
		return 2
	}

	method := l.CheckString(2)

	// Get eval host from context to create the process
	ctx := l.Context()
	host := evalhost.GetHost(ctx)
	if host == nil {
		l.Push(lua.LNil)
		l.Push(lua.LString("eval host not available"))
		return 2
	}

	// Create the process
	var proc process.Process
	var err error

	// Check if sourceOrID looks like a registry ID (contains ":")
	if isRegistryID(sb.sourceOrID) {
		id := registry.ParseID(sb.sourceOrID)
		proc, err = host.CreateProcessFromID(ctx, id)
	} else {
		// Compile and create process from source
		program, compileErr := host.Compile(ctx, evalhost.CompileCmd{
			Source:  sb.sourceOrID,
			Method:  method,
			Modules: sb.modules,
		})
		if compileErr != nil {
			l.Push(lua.LNil)
			l.Push(lua.LString(fmt.Sprintf("compile error: %v", compileErr)))
			return 2
		}
		proc, err = host.CreateProcess(ctx, program)
	}

	if err != nil {
		l.Push(lua.LNil)
		l.Push(lua.LString(fmt.Sprintf("failed to create process: %v", err)))
		return 2
	}

	sb.process = proc
	sb.ctx = ctx
	sb.transcoder = NewCommandTranscoder()

	// Register cleanup with Store so process is closed when parent exits
	if store := resource.GetStore(ctx); store != nil {
		sb.cancelCleanup = store.AddCleanup(func() error {
			if sb.process != nil {
				sb.process.Close()
				sb.process = nil
			}
			return nil
		})
	}

	// Collect args
	var input payload.Payloads
	for i := 3; i <= l.GetTop(); i++ {
		input = append(input, luaconv.ExportPayload(l.Get(i)))
	}

	// Start execution
	if err := proc.Execute(ctx, method, input); err != nil {
		proc.Close()
		l.Push(lua.LNil)
		l.Push(lua.LString(fmt.Sprintf("execute error: %v", err)))
		return 2
	}

	sb.started = true
	l.Push(lua.LTrue)
	return 1
}

// sandboxStep advances the sandbox: sandbox:step(results?) -> step_result
func sandboxStep(l *lua.LState) int {
	sb := checkSandbox(l, 1)
	if sb == nil {
		return 0
	}

	if !sb.started {
		l.Push(lua.LNil)
		l.Push(lua.LString("sandbox not started, call execute first"))
		return 2
	}

	// Get results from previous yields (if any)
	var results *process.YieldResults
	if l.GetTop() >= 2 && l.Get(2).Type() == lua.LTTable {
		resultsTable := l.CheckTable(2)
		results = process.AcquireYieldResults()

		// Extract data and error from table
		if dataVal := resultsTable.RawGetString("data"); dataVal != lua.LNil {
			results.Data = value.ToGoAny(dataVal)
		}
		if errVal := resultsTable.RawGetString("error"); errVal != lua.LNil {
			if errStr, ok := errVal.(lua.LString); ok {
				results.Error = NewEvalError(string(errStr))
			}
		}
	}

	// Step the process
	stepResult, err := sb.process.Step(results)
	if results != nil {
		process.ReleaseYieldResults(results)
	}

	if err != nil {
		// Return error step
		t := l.CreateTable(0, 2)
		t.RawSetString("status", lua.LString("error"))
		t.RawSetString("error", lua.LString(err.Error()))
		l.Push(t)
		return 1
	}

	// Create result table
	t := l.CreateTable(0, 3)

	switch stepResult.Status {
	case process.StepDone:
		t.RawSetString("status", lua.LString("done"))
		if stepResult.Result != nil {
			luaVal := transcodeToLua(sb.ctx, stepResult.Result)
			t.RawSetString("value", luaVal)
		} else {
			t.RawSetString("value", lua.LNil)
		}

	case process.StepIdle:
		t.RawSetString("status", lua.LString("idle"))

	case process.StepContinue:
		t.RawSetString("status", lua.LString("continue"))

		// Transcode yields to Lua tables
		yields := stepResult.GetYields()
		yieldsTable := l.CreateTable(len(yields), 0)
		for i, cmd := range yields {
			cmdTable := sb.transcoder.Transcode(l, cmd)
			yieldsTable.RawSetInt(i+1, cmdTable)
		}
		t.RawSetString("yields", yieldsTable)
	}

	l.Push(t)
	return 1
}

// sandboxClose closes the sandbox: sandbox:close()
func sandboxClose(l *lua.LState) int {
	sb := checkSandbox(l, 1)
	if sb == nil {
		return 0
	}

	// Cancel the Resources cleanup registration (we're closing manually)
	if sb.cancelCleanup != nil {
		sb.cancelCleanup()
		sb.cancelCleanup = nil
	}

	// Close the process
	if sb.process != nil {
		sb.process.Close()
		sb.process = nil
	}
	sb.started = false
	sb.transcoder = nil
	sb.ctx = nil
	return 0
}

// transcodeToLua uses the context transcoder to convert a payload to Lua format.
func transcodeToLua(ctx context.Context, pl payload.Payload) lua.LValue {
	if pl == nil {
		return lua.LNil
	}

	// Already a Lua value
	if pl.Format() == payload.Lua {
		if lv, ok := pl.Data().(lua.LValue); ok {
			return lv
		}
	}

	// Try transcoding via context transcoder
	dtt := payload.GetTranscoder(ctx)
	if dtt != nil {
		transcoded, err := dtt.Transcode(pl, payload.Lua)
		if err == nil {
			if lv, ok := transcoded.Data().(lua.LValue); ok {
				return lv
			}
		}
	}

	// Fallback: return as string representation
	return lua.LString(fmt.Sprintf("%v", pl.Data()))
}

// isRegistryID checks if a string looks like a registry ID vs source code.
// Registry IDs are short identifiers like "lua:component/handler".
// Source code is longer and usually contains newlines.
func isRegistryID(s string) bool {
	// Source code typically has newlines
	for _, c := range s {
		if c == '\n' {
			return false
		}
	}
	// Registry IDs are short, source code is long
	if len(s) > 256 {
		return false
	}
	// Must contain colon (the type separator)
	for _, c := range s {
		if c == ':' {
			return true
		}
	}
	return false
}
