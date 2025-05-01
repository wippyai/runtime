package inspect

import (
	"fmt"
	"strings"

	lua "github.com/yuin/gopher-lua"
)

// StackFrame represents a single frame in the Lua stack
type StackFrame struct {
	Level       int
	Source      string
	CurrentLine int
	Name        string
	FuncType    string    // What type of function (Lua, Go, main, etc)
	Locals      []Local   // Local variables
	Upvalues    []Upvalue // Upvalue information
}

// Local represents a local variable
type Local struct {
	Name  string
	Value lua.LValue
}

// Upvalue represents an upvalue
type Upvalue struct {
	Name  string
	Value lua.LValue
}

// StackTrace represents a complete stack trace
type StackTrace struct {
	ThreadID string
	Frames   []StackFrame
}

func (st StackTrace) String() string {
	var sb strings.Builder
	_, _ = fmt.Fprintf(&sb, "Thread: %s\n", st.ThreadID)
	for _, frame := range st.Frames {
		_, _ = fmt.Fprintf(&sb, "  %s\n", frame.String())
	}
	return sb.String()
}

func (sf StackFrame) String() string {
	var sb strings.Builder
	_, _ = fmt.Fprintf(&sb, "[%d] %s:%d (%s)", sf.Level, sf.Source, sf.CurrentLine, sf.Name)

	if len(sf.Locals) > 0 {
		sb.WriteString("\n    Locals:")
		for _, local := range sf.Locals {
			_, _ = fmt.Fprintf(&sb, "\n      %s = %v", local.Name, local.Value)
		}
	}

	if len(sf.Upvalues) > 0 {
		sb.WriteString("\n    Upvalues:")
		for _, upvalue := range sf.Upvalues {
			_, _ = fmt.Fprintf(&sb, "\n      %s = %v", upvalue.Name, upvalue.Value)
		}
	}

	return sb.String()
}

// GetStackTrace captures a complete stack trace from a Lua state
func GetStackTrace(l *lua.LState) *StackTrace {
	trace := &StackTrace{
		ThreadID: fmt.Sprintf("%p", l),
	}

	for level := 0; ; level++ {
		frame, ok := getStackFrame(l, level)
		if !ok {
			break
		}
		trace.Frames = append(trace.Frames, frame)
	}

	return trace
}

// getStackFrame captures information about a single stack frame
func getStackFrame(l *lua.LState, level int) (StackFrame, bool) {
	ar, ok := l.GetStack(level)
	if !ok {
		return StackFrame{}, false
	}

	// Spawn debug info with function info
	funcTable := l.NewTable()
	if _, err := l.GetInfo("nSluf", ar, funcTable); err != nil {
		return StackFrame{}, false
	}

	frame := StackFrame{
		Level:       level,
		Source:      ar.Source,
		CurrentLine: ar.CurrentLine,
		Name:        ar.Name,
		FuncType:    ar.What,
	}

	// If no name is provided and this is a Go function, try to determine the name
	if frame.Name == "" && (frame.FuncType == "Go" || frame.FuncType == "C") {
		// Spawn the actual function from the debug info table
		fn := funcTable.RawGet(lua.LString("f"))
		if luaFn, ok := fn.(*lua.LFunction); ok {
			// Iterate over globals and see if any key maps to this function
			globals := l.Get(lua.GlobalsIndex).(*lua.LTable)
			globals.ForEach(func(key, value lua.LValue) {
				if globalFn, ok := value.(*lua.LFunction); ok && globalFn == luaFn {
					// Use the global key as the function name.
					frame.Name = key.String()
				}
			})
		}
		// Fallback if no match was found.
		if frame.Name == "" {
			frame.Name = fmt.Sprintf("<go function at %s:%d>", frame.Source, frame.CurrentLine)
		}
	}

	// Spawn locals (parameters and local variables)
	for i := 1; ; i++ {
		name, value := l.GetLocal(ar, i)
		if name == "" {
			break
		}
		frame.Locals = append(frame.Locals, Local{name, value})
	}

	// Only get upvalues for Lua functions
	if fn := funcTable.RawGet(lua.LString("f")); fn != lua.LNil {
		if luaFn, ok := fn.(*lua.LFunction); ok {
			for i := 1; ; i++ {
				name, value := l.GetUpvalue(luaFn, i)
				if name == "" {
					break
				}
				frame.Upvalues = append(frame.Upvalues, Upvalue{name, value})
			}
		}
	}

	return frame, true
}

// GetAllCoroutineStacks gets stack traces for all coroutines in the registry
func GetAllCoroutineStacks(l *lua.LState) []*StackTrace {
	var traces []*StackTrace

	// AddCleanup main thread
	traces = append(traces, GetStackTrace(l))

	// We need to get any global coroutines first
	globals := l.Get(lua.GlobalsIndex).(*lua.LTable)
	globals.ForEach(func(_, value lua.LValue) {
		if co, ok := value.(*lua.LState); ok {
			trace := GetStackTrace(co)
			// Only add if it has frames (active/yielded coroutine)
			if len(trace.Frames) > 0 {
				traces = append(traces, trace)
			}
		}
	})

	return traces
}
