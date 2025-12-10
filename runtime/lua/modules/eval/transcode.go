package eval

import (
	clockapi "github.com/wippyai/runtime/api/clock"
	"github.com/wippyai/runtime/api/dispatcher"
	lua "github.com/yuin/gopher-lua"
)

// TranscodeFunc converts a dispatcher.Command to a Lua table.
type TranscodeFunc func(*lua.LState, dispatcher.Command) *lua.LTable

// CommandTranscoder converts dispatcher commands to Lua tables.
type CommandTranscoder struct {
	transcoders map[dispatcher.CommandID]TranscodeFunc
}

// NewCommandTranscoder creates a transcoder with built-in transcoders.
func NewCommandTranscoder() *CommandTranscoder {
	t := &CommandTranscoder{
		transcoders: make(map[dispatcher.CommandID]TranscodeFunc),
	}
	t.registerBuiltins()
	return t
}

// Register adds a transcoder for a command ID.
func (t *CommandTranscoder) Register(id dispatcher.CommandID, fn TranscodeFunc) {
	t.transcoders[id] = fn
}

// Transcode converts a command to a Lua table.
func (t *CommandTranscoder) Transcode(l *lua.LState, cmd dispatcher.Command) *lua.LTable {
	if fn, ok := t.transcoders[cmd.CmdID()]; ok {
		return fn(l, cmd)
	}
	// Default: just return ID and type "unknown"
	tbl := l.CreateTable(0, 2)
	tbl.RawSetString("id", lua.LNumber(cmd.CmdID()))
	tbl.RawSetString("type", lua.LString("unknown"))
	return tbl
}

func (t *CommandTranscoder) registerBuiltins() {
	// Clock commands
	t.Register(clockapi.Sleep, transcodeSleep)
	t.Register(clockapi.TickerStart, transcodeTickerStart)
	t.Register(clockapi.TickerStop, transcodeTickerStop)
	t.Register(clockapi.TimerStart, transcodeTimerStart)
	t.Register(clockapi.TimerWait, transcodeTimerWait)
	t.Register(clockapi.TimerStop, transcodeTimerStop)
	t.Register(clockapi.TimerReset, transcodeTimerReset)
}

func transcodeSleep(l *lua.LState, cmd dispatcher.Command) *lua.LTable {
	sleep := cmd.(clockapi.SleepCmd)
	tbl := l.CreateTable(0, 3)
	tbl.RawSetString("id", lua.LNumber(clockapi.Sleep))
	tbl.RawSetString("type", lua.LString("sleep"))
	tbl.RawSetString("duration", lua.LNumber(sleep.Duration))
	return tbl
}

func transcodeTickerStart(l *lua.LState, cmd dispatcher.Command) *lua.LTable {
	ticker := cmd.(clockapi.TickerStartCmd)
	tbl := l.CreateTable(0, 3)
	tbl.RawSetString("id", lua.LNumber(clockapi.TickerStart))
	tbl.RawSetString("type", lua.LString("ticker_start"))
	tbl.RawSetString("duration", lua.LNumber(ticker.Duration))
	return tbl
}

func transcodeTickerStop(l *lua.LState, cmd dispatcher.Command) *lua.LTable {
	ticker := cmd.(clockapi.TickerStopCmd)
	tbl := l.CreateTable(0, 3)
	tbl.RawSetString("id", lua.LNumber(clockapi.TickerStop))
	tbl.RawSetString("type", lua.LString("ticker_stop"))
	tbl.RawSetString("ticker_id", lua.LNumber(ticker.TickerID))
	return tbl
}

func transcodeTimerStart(l *lua.LState, cmd dispatcher.Command) *lua.LTable {
	timer := cmd.(clockapi.TimerStartCmd)
	tbl := l.CreateTable(0, 3)
	tbl.RawSetString("id", lua.LNumber(clockapi.TimerStart))
	tbl.RawSetString("type", lua.LString("timer_start"))
	tbl.RawSetString("duration", lua.LNumber(timer.Duration))
	return tbl
}

func transcodeTimerWait(l *lua.LState, cmd dispatcher.Command) *lua.LTable {
	timer := cmd.(clockapi.TimerWaitCmd)
	tbl := l.CreateTable(0, 3)
	tbl.RawSetString("id", lua.LNumber(clockapi.TimerWait))
	tbl.RawSetString("type", lua.LString("timer_wait"))
	tbl.RawSetString("timer_id", lua.LNumber(timer.TimerID))
	return tbl
}

func transcodeTimerStop(l *lua.LState, cmd dispatcher.Command) *lua.LTable {
	timer := cmd.(clockapi.TimerStopCmd)
	tbl := l.CreateTable(0, 3)
	tbl.RawSetString("id", lua.LNumber(clockapi.TimerStop))
	tbl.RawSetString("type", lua.LString("timer_stop"))
	tbl.RawSetString("timer_id", lua.LNumber(timer.TimerID))
	return tbl
}

func transcodeTimerReset(l *lua.LState, cmd dispatcher.Command) *lua.LTable {
	timer := cmd.(clockapi.TimerResetCmd)
	tbl := l.CreateTable(0, 4)
	tbl.RawSetString("id", lua.LNumber(clockapi.TimerReset))
	tbl.RawSetString("type", lua.LString("timer_reset"))
	tbl.RawSetString("timer_id", lua.LNumber(timer.TimerID))
	tbl.RawSetString("duration", lua.LNumber(timer.Duration))
	return tbl
}
