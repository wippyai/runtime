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
func GetStackTrace(L *lua.LState) *StackTrace {
	trace := &StackTrace{
		ThreadID: fmt.Sprintf("%p", L),
	}

	for level := 0; ; level++ {
		frame, ok := getStackFrame(L, level)
		if !ok {
			break
		}
		trace.Frames = append(trace.Frames, frame)
	}

	return trace
}

// getStackFrame captures information about a single stack frame
func getStackFrame(L *lua.LState, level int) (StackFrame, bool) {
	// Get basic debug info
	ar, ok := L.GetStack(level)
	if !ok {
		return StackFrame{}, false
	}

	frame := StackFrame{Level: level}

	// Create table for function info
	funcTable := L.NewTable()
	if _, err := L.GetInfo("Slnuf", ar, funcTable); err != nil {
		return frame, false
	}

	// Fill in basic frame info
	frame.Source = ar.Source
	frame.CurrentLine = ar.CurrentLine
	frame.Name = ar.Name
	frame.FuncType = ar.What

	// Get local variables
	for i := 1; ; i++ {
		name, value := L.GetLocal(ar, i)
		if name == "" {
			break
		}
		frame.Locals = append(frame.Locals, Local{name, value})
	}

	// Get upvalues if we have function
	if fn := funcTable.RawGet(lua.LString("f")); fn != lua.LNil {
		if luaFn, ok := fn.(*lua.LFunction); ok {
			for i := 1; ; i++ {
				name, value := L.GetUpvalue(luaFn, i)
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
func GetAllCoroutineStacks(L *lua.LState) []*StackTrace {
	var traces []*StackTrace

	// Add main thread
	traces = append(traces, GetStackTrace(L))

	// We need to get any global coroutines first
	globals := L.Get(lua.GlobalsIndex).(*lua.LTable)
	globals.ForEach(func(key, value lua.LValue) {
		if co, ok := value.(*lua.LState); ok {
			trace := GetStackTrace(co)
			// Only add if it has frames (active/yielded coroutine)
			if len(trace.Frames) > 0 {
				traces = append(traces, trace)
			}
		}
	})

	// Also check the registry for any additional coroutines
	registry := L.Get(lua.RegistryIndex).(*lua.LTable)
	registry.ForEach(func(_, value lua.LValue) {
		if co, ok := value.(*lua.LState); ok {
			trace := GetStackTrace(co)
			// Only add if it has frames (active/yielded coroutine)
			if len(trace.Frames) > 0 {
				// Avoid duplicates - check if we already have this thread
				for _, existing := range traces {
					if existing.ThreadID == fmt.Sprintf("%p", co) {
						return
					}
				}
				traces = append(traces, trace)
			}
		}
	})

	return traces
}
