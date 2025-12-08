package sandbox

import (
	"context"
	"fmt"
	"sync"

	clockapi "github.com/wippyai/runtime/api/clock"
	ctxapi "github.com/wippyai/runtime/api/context"
	"github.com/wippyai/runtime/api/payload"
	"github.com/wippyai/runtime/api/process"
	"github.com/wippyai/runtime/runtime/lua/engine/value"
	lua "github.com/yuin/gopher-lua"
)

// Process wraps a process for deterministic stepping in Lua.
type Process struct {
	mu        sync.Mutex
	proc      process.Process
	clock     *MockClock
	ctx       context.Context
	output    process.StepOutput
	lastYield *YieldInfo
	closed    bool
}

// YieldInfo contains information about a yield for Lua inspection.
type YieldInfo struct {
	CmdID int
	Tag   uint64
	Cmd   process.Command
}

// NewProcess creates a new sandbox process wrapper.
func NewProcess(ctx context.Context, proc process.Process, clock *MockClock) *Process {
	// Create fresh frame context for the sandbox process
	sandboxCtx, _ := ctxapi.OpenFrameContext(ctx)

	sp := &Process{
		proc:  proc,
		clock: clock,
		ctx:   sandboxCtx,
	}

	// Install clock as time reference if provided
	if clock != nil {
		_ = clockapi.WithTimeReference(sandboxCtx, clock)
	}

	return sp
}

// Init initializes the process with method and arguments.
func (p *Process) Init(method string, args payload.Payloads) error {
	p.mu.Lock()
	defer p.mu.Unlock()

	if p.closed {
		return ErrProcessClosed
	}

	return p.proc.Init(p.ctx, method, args)
}

// Step advances the process and returns yield info or completion status.
func (p *Process) Step(events []process.Event) (*StepResult, error) {
	p.mu.Lock()
	defer p.mu.Unlock()

	if p.closed {
		return nil, ErrProcessClosed
	}

	p.output.Reset()
	err := p.proc.Step(events, &p.output)

	result := &StepResult{
		Status:     p.output.Status(),
		YieldCount: p.output.Count(),
	}

	if p.output.IsDone() {
		result.Result = p.output.Result()
	}

	// Capture yields for inspection
	if p.output.Count() > 0 {
		yields := p.output.Yields()
		p.lastYield = &YieldInfo{
			CmdID: int(yields[0].Cmd.CmdID()),
			Tag:   yields[0].Tag,
			Cmd:   yields[0].Cmd,
		}
		result.Yields = make([]YieldInfo, len(yields))
		for i, y := range yields {
			result.Yields[i] = YieldInfo{
				CmdID: int(y.Cmd.CmdID()),
				Tag:   y.Tag,
				Cmd:   y.Cmd,
			}
		}
	} else {
		p.lastYield = nil
	}

	return result, err
}

// Close releases process resources.
func (p *Process) Close() {
	p.mu.Lock()
	defer p.mu.Unlock()

	if p.closed {
		return
	}
	p.closed = true
	p.proc.Close()
}

// Clock returns the mock clock if any.
func (p *Process) Clock() *MockClock {
	return p.clock
}

// IsClosed returns true if process is closed.
func (p *Process) IsClosed() bool {
	p.mu.Lock()
	defer p.mu.Unlock()
	return p.closed
}

// StepResult contains the result of a step operation.
type StepResult struct {
	Status     process.StepStatus
	YieldCount int
	Yields     []YieldInfo
	Result     payload.Payload
}

// Lua methods for SandboxProcess

var processMethods = map[string]lua.LGoFunc{
	"init":    processInit,
	"step":    processStep,
	"close":   processClose,
	"clock":   processGetClock,
	"is_done": processIsDone,
}

func checkProcess(l *lua.LState) *Process {
	ud := l.CheckUserData(1)
	if p, ok := ud.Value.(*Process); ok {
		return p
	}
	l.ArgError(1, "eval.sandbox.Process expected")
	return nil
}

func processInit(l *lua.LState) int {
	p := checkProcess(l)
	if p == nil {
		return 0
	}

	method := l.OptString(2, "")

	var args payload.Payloads
	if l.GetTop() >= 3 && l.Get(3).Type() == lua.LTTable {
		argsTable := l.CheckTable(3)
		argsTable.ForEach(func(_, v lua.LValue) {
			args = append(args, payload.NewPayload(v, payload.Lua))
		})
	}

	err := p.Init(method, args)
	if err != nil {
		l.Push(lua.LNil)
		l.Push(lua.WrapErrorWithLua(l, err, "init failed").
			WithKind(lua.KindInternal).WithRetryable(false))
		return 2
	}

	l.Push(lua.LTrue)
	l.Push(lua.LNil)
	return 2
}

func processStep(l *lua.LState) int {
	p := checkProcess(l)
	if p == nil {
		return 0
	}

	// Build events from optional table argument
	var events []process.Event
	if l.GetTop() >= 2 && l.Get(2).Type() == lua.LTTable {
		eventsTable := l.CheckTable(2)
		eventsTable.ForEach(func(_, v lua.LValue) {
			if eventTbl, ok := v.(*lua.LTable); ok {
				event := process.Event{}
				if tag := eventTbl.RawGetString("tag"); tag != lua.LNil {
					if n, ok := tag.(lua.LNumber); ok {
						event.Tag = uint64(n)
					}
				}
				if data := eventTbl.RawGetString("data"); data != lua.LNil {
					event.Data = value.ToGoAny(data)
				}
				if errVal := eventTbl.RawGetString("error"); errVal != lua.LNil {
					if errStr, ok := errVal.(lua.LString); ok {
						event.Error = lua.NewLuaError(l, string(errStr))
					}
				}
				event.Type = process.EventYieldComplete
				events = append(events, event)
			}
		})
	}

	result, err := p.Step(events)
	if err != nil {
		// Create result table even on error
		resultTbl := l.CreateTable(0, 4)
		resultTbl.RawSetString("status", lua.LString("error"))
		resultTbl.RawSetString("error", lua.LString(err.Error()))
		l.Push(resultTbl)
		return 1
	}

	// Build result table
	resultTbl := l.CreateTable(0, 4)

	switch result.Status {
	case process.StepContinue:
		resultTbl.RawSetString("status", lua.LString("continue"))
	case process.StepIdle:
		resultTbl.RawSetString("status", lua.LString("idle"))
	case process.StepDone:
		resultTbl.RawSetString("status", lua.LString("done"))
		if result.Result != nil {
			resultTbl.RawSetString("result", transcodeToLua(l, result.Result))
		}
	}

	resultTbl.RawSetString("yield_count", lua.LNumber(result.YieldCount))

	// Add yields info if any
	if len(result.Yields) > 0 {
		yieldsTbl := l.CreateTable(len(result.Yields), 0)
		for i, y := range result.Yields {
			yieldTbl := l.CreateTable(0, 3)
			yieldTbl.RawSetString("cmd_id", lua.LNumber(y.CmdID))
			if y.Tag != 0 {
				yieldTbl.RawSetString("tag", lua.LNumber(y.Tag))
			}
			// Add command-specific info
			addCommandInfo(l, yieldTbl, y.Cmd)
			yieldsTbl.RawSetInt(i+1, yieldTbl)
		}
		resultTbl.RawSetString("yields", yieldsTbl)
	}

	l.Push(resultTbl)
	return 1
}

func addCommandInfo(_ *lua.LState, tbl *lua.LTable, cmd process.Command) {
	// Use type assertion to extract command-specific fields
	if c, ok := cmd.(interface{ Duration() int64 }); ok {
		tbl.RawSetString("duration", lua.LNumber(c.Duration()))
	}
}

func processClose(l *lua.LState) int {
	p := checkProcess(l)
	if p == nil {
		return 0
	}
	p.Close()
	return 0
}

func processGetClock(l *lua.LState) int {
	p := checkProcess(l)
	if p == nil {
		return 0
	}
	clock := p.Clock()
	if clock == nil {
		l.Push(lua.LNil)
		return 1
	}
	value.PushTypedUserData(l, clock, clockTypeName)
	return 1
}

func processIsDone(l *lua.LState) int {
	p := checkProcess(l)
	if p == nil {
		return 0
	}
	l.Push(lua.LBool(p.IsClosed()))
	return 1
}

// transcodeToLua converts a payload to Lua value using context transcoder.
func transcodeToLua(l *lua.LState, pl payload.Payload) lua.LValue {
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
	ctx := l.Context()
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

// anyToLua converts a Go value to Lua value.
func anyToLua(_ *lua.LState, v any) lua.LValue {
	if v == nil {
		return lua.LNil
	}
	switch val := v.(type) {
	case lua.LValue:
		return val
	case bool:
		return lua.LBool(val)
	case int:
		return lua.LNumber(val)
	case int64:
		return lua.LNumber(val)
	case uint64:
		return lua.LNumber(val)
	case float64:
		return lua.LNumber(val)
	case string:
		return lua.LString(val)
	case []byte:
		return lua.LString(val)
	case error:
		return lua.LString(val.Error())
	default:
		return lua.LString(fmt.Sprintf("%v", val))
	}
}
