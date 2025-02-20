package process

import (
	"github.com/ponyruntime/pony/api/payload"
	"github.com/ponyruntime/pony/api/process"
	lua "github.com/yuin/gopher-lua"
	"go.uber.org/zap"
)

// Module represents the process info module
type Module struct {
	log *zap.Logger
}

// NewProcessContextModule creates a new process module
func NewProcessContextModule(log *zap.Logger) *Module {
	return &Module{
		log: log,
	}
}

// Name returns the module name
func (m *Module) Name() string {
	return "process"
}

// Loader is the entry point for loading the module into Lua
func (m *Module) Loader(l *lua.LState) int {
	// Create module table
	mod := l.NewTable()

	// Register functions
	l.SetFuncs(mod, map[string]lua.LGFunction{
		"info":       m.info,
		"pid":        m.pid,
		"input_args": m.initArgs,
	})

	l.Push(mod)
	return 1
}

// checkProcess validates context and returns process context if valid
func (m *Module) checkProcess(l *lua.LState) (*process.Context, bool) {
	ctx := l.Context()
	if ctx == nil {
		l.Push(lua.LNil)
		l.Push(lua.LString("no context found"))
		return nil, false
	}

	procCtx := process.GetContext(ctx)
	if procCtx == nil {
		l.Push(lua.LNil)
		l.Push(lua.LString("no process context found"))
		return nil, false
	}

	return procCtx, true
}

// info returns comprehensive process information
func (m *Module) info(l *lua.LState) int {
	procCtx, ok := m.checkProcess(l)
	if !ok {
		return 2 // error values already pushed by checkProcess
	}

	// Create info table
	info := l.NewTable()

	// Set node info
	if procCtx.PID.Node != "" {
		info.RawSetString("node", lua.LString(procCtx.PID.Node))
	} else {
		info.RawSetString("node", lua.LNil)
	}

	// Set host
	info.RawSetString("host", lua.LString(procCtx.PID.Host))

	// Set registry ID info
	regid := l.NewTable()
	regid.RawSetString("namespace", lua.LString(procCtx.PID.ID.NS))
	regid.RawSetString("name", lua.LString(procCtx.PID.ID.Name))
	info.RawSetString("registry_id", regid)
	info.RawSetString("uniq_id", lua.LString(procCtx.PID.UniqID))
	info.RawSetString("start_time", lua.LNumber(procCtx.Start.Unix()))
	info.RawSetString("trap_exits", lua.LBool(procCtx.TrapExits))

	l.Push(info)
	return 1
}

// pid returns the string representation of the process ID
func (m *Module) pid(l *lua.LState) int {
	procCtx, ok := m.checkProcess(l)
	if !ok {
		return 2 // error values already pushed by checkProcess
	}

	l.Push(lua.LString(procCtx.PID.String()))
	return 1
}

// initArgs returns the process initialization arguments
func (m *Module) initArgs(l *lua.LState) int {
	procCtx, ok := m.checkProcess(l)
	if !ok {
		return 2 // error values already pushed by checkProcess
	}

	if procCtx.Input == nil || len(procCtx.Input) == 0 {
		l.Push(lua.LNil)
		return 1
	}

	dtt := payload.GetTranscoder(l.Context())
	if dtt == nil {
		l.Push(lua.LNil)
		l.Push(lua.LString("no transcoder found"))
		return 2
	}

	// Create args table
	args := l.NewTable()
	for i, arg := range procCtx.Input {
		lv, err := dtt.Transcode(arg, payload.Lua)
		if err != nil {
			m.log.Error("failed to transcode payload", zap.Error(err))
			l.Push(lua.LNil)
			l.Push(lua.LString("failed to transcode payload"))

			args.RawSetInt(i+1, lua.LNil)
			continue
		}

		args.RawSetInt(i+1, lv.Data().(lua.LValue))
	}

	l.Push(args)
	return 1
}
