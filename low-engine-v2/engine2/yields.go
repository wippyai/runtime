package engine2

import (
	"time"

	"github.com/wippyai/runtime/low-engine-v2/clock"
	"github.com/wippyai/runtime/low-engine-v2/scheduler"
	lua "github.com/yuin/gopher-lua"
)

// SleepYield is yielded by time.sleep to request the scheduler pause execution.
type SleepYield struct {
	Duration time.Duration
}

func (y *SleepYield) String() string       { return "<sleep_yield>" }
func (y *SleepYield) Type() lua.LValueType { return lua.LTUserData }

// ToCommand converts SleepYield to a scheduler.Command.
func (y *SleepYield) ToCommand() scheduler.Command {
	return clock.SleepCmd{Duration: y.Duration}
}

// YieldConverter converts Lua yield values to scheduler commands.
type YieldConverter interface {
	ToCommand() scheduler.Command
}

// ConvertYieldToCommand attempts to convert a Lua yield value to a scheduler command.
func ConvertYieldToCommand(value lua.LValue) scheduler.Command {
	if converter, ok := value.(YieldConverter); ok {
		return converter.ToCommand()
	}
	return nil
}

// BindTimeSleep binds a time.sleep function that yields SleepYield.
func BindTimeSleep(l *lua.LState) {
	// Get or create time module
	timeModule := l.GetGlobal("time")
	if timeModule == lua.LNil {
		timeModule = l.NewTable()
		l.SetGlobal("time", timeModule)
	}
	timeTbl := timeModule.(*lua.LTable)

	// Add time constants (in nanoseconds)
	l.SetField(timeTbl, "NANOSECOND", lua.LNumber(time.Nanosecond))
	l.SetField(timeTbl, "MICROSECOND", lua.LNumber(time.Microsecond))
	l.SetField(timeTbl, "MILLISECOND", lua.LNumber(time.Millisecond))
	l.SetField(timeTbl, "SECOND", lua.LNumber(time.Second))
	l.SetField(timeTbl, "MINUTE", lua.LNumber(time.Minute))
	l.SetField(timeTbl, "HOUR", lua.LNumber(time.Hour))

	// Add sleep function that yields
	l.SetField(timeTbl, "sleep", l.NewFunction(func(l *lua.LState) int {
		arg := l.Get(1)
		var duration time.Duration

		switch v := arg.(type) {
		case lua.LNumber:
			// Treat as nanoseconds (matches time.SECOND etc)
			duration = time.Duration(v)
		case lua.LString:
			var err error
			duration, err = time.ParseDuration(string(v))
			if err != nil {
				l.RaiseError("invalid duration: %s", err.Error())
				return 0
			}
		default:
			l.RaiseError("sleep requires number or string, got %s", arg.Type().String())
			return 0
		}

		// Yield with SleepYield
		yield := &SleepYield{Duration: duration}
		l.Push(yield)
		return -1
	}))
}
