package errors

import (
	"errors"
	"fmt"
	"io"
	"runtime"
	"strings"

	"github.com/ponyruntime/pony/runtime/lua/engine/inspect"
	"github.com/ponyruntime/pony/runtime/lua/engine/value"
	lua "github.com/yuin/gopher-lua"
)

// WrappedError represents an error that occurred in either Go or Lua Context,
// preserving stack traces from both environments.
type WrappedError struct {
	Err      error               // Points to parent error
	LuaStack *inspect.StackTrace // Lua stack at this wrap point
	goStack  []uintptr           // Go stack at this wrap point
	Context  string              // Context for this wrap
}

// Error implements the error interface.
func (e *WrappedError) Error() string {
	if e.Context != "" {
		return fmt.Sprintf("%s: %v", e.Context, e.Err)
	}
	return e.Err.Error()
}

// LuaType returns the Lua type of this value.
func (e *WrappedError) Type() lua.LValueType {
	return lua.LTUserData
}

// String implements the fmt.Stringer interface.
func (e *WrappedError) String() string {
	return e.Error()
}

// Unwrap returns the underlying error for compatibility with Go 1.13+ error unwrapping.
func (e *WrappedError) Unwrap() error {
	if e == nil {
		return nil
	}
	return e.Err
}

// Stack returns a formatted string containing both Lua and Go stack traces
// by walking the error chain.
func (e *WrappedError) Stack() string {
	var result strings.Builder
	var seenFrames = make(map[string]bool)

	// Walk error chain from top to bottom
	current := e
	for current != nil {
		if current.Context != "" {
			result.WriteString(current.Context)
			result.WriteString(":\n")
		}

		// Add this level's Lua stack if present
		if current.LuaStack != nil {
			luaFrames := current.LuaStack.String()
			if !seenFrames[luaFrames] {
				result.WriteString(luaFrames)
				result.WriteString("\n")
				seenFrames[luaFrames] = true
			}
		}

		// Add this level's Go stack if present
		if len(current.goStack) > 0 {
			frames := runtime.CallersFrames(current.goStack)
			for {
				frame, more := frames.Next()

				// Skip runtime frames
				if shouldSkipFrame(frame.Function) {
					if more {
						continue
					}
					break
				}

				frameID := fmt.Sprintf("%s:%d", frame.File, frame.Line)
				if !seenFrames[frameID] {
					result.WriteString(fmt.Sprintf("  %s:%d (%s)\n",
						frame.File, frame.Line, frame.Function))
					seenFrames[frameID] = true
				}

				if !more {
					break
				}
			}
		}

		// Move to parent error if it's a WrappedError
		var next *WrappedError
		if errors.As(current.Err, &next) {
			current = next
		} else {
			break
		}
	}

	return result.String()
}

// shouldSkipFrame returns true for runtime internal frames that should be filtered.
func shouldSkipFrame(name string) bool {
	return strings.HasPrefix(name, "runtime.") &&
		(strings.Contains(name, "panic") ||
			strings.Contains(name, "throw") ||
			strings.Contains(name, "sigpanic"))
}

func New(err error) *WrappedError {
	return &WrappedError{Err: err}
}

// WrapError creates a new wrapped error with the given Context.
func WrapError(l *lua.LState, err error, context string) *WrappedError {
	if err == nil {
		return nil
	}

	// Spawn new stack traces
	goStack := make([]uintptr, 64)
	n := runtime.Callers(2, goStack)
	luaStack := inspect.GetStackTrace(l)

	// If already wrapped, preserve Context
	var we *WrappedError
	if errors.As(err, &we) {
		if context == "" {
			// Even with no new Context, create new wrapper to capture current stacks
			return &WrappedError{
				Err:      we.Err,      // Link to original inner error
				LuaStack: luaStack,    // New Lua stack
				goStack:  goStack[:n], // New Go stack
				Context:  we.Context,  // Preserve Context
			}
		}

		return &WrappedError{
			Err:      we,
			LuaStack: luaStack,
			goStack:  goStack[:n],
			Context:  context,
		}
	}

	// Spawn new wrapped error
	return &WrappedError{
		Err:      err,
		LuaStack: luaStack,
		goStack:  goStack[:n],
		Context:  context,
	}
}

// RaiseError wraps a Go error and raises it in the Lua state.
func RaiseError(l *lua.LState, err error) {
	wrapped := WrapError(l, err, "")

	ud := l.NewUserData()
	ud.Value = wrapped
	ud.Metatable = value.GetTypeMetatable(nil, "error")
	l.Error(ud, 1)
}

// ToValue wraps a Go error in a Lua userdata and returns it as a Lua value.
func ToValue(l *lua.LState, err error) lua.LValue {
	var w *WrappedError
	if errors.As(err, &w) {
		ud := l.NewUserData()
		ud.Value = w
		ud.Metatable = value.GetTypeMetatable(nil, "error")
		return ud
	}

	wrapped := WrapError(l, err, "")

	ud := l.NewUserData()
	ud.Value = wrapped
	ud.Metatable = value.GetTypeMetatable(nil, "error")
	return ud
}

// GetWrappedError attempts to extract a WrappedError from a Lua error value.
func GetWrappedError(err error) *WrappedError {
	if err == nil {
		return nil
	}

	var we *WrappedError
	if errors.As(err, &we) {
		return we
	}

	var apiErr *lua.ApiError
	if !errors.As(err, &apiErr) {
		return nil
	}

	// Try to extract from Object field
	if ud, ok := apiErr.Object.(*lua.LUserData); ok {
		if wrapped, ok := ud.Value.(*WrappedError); ok {
			return wrapped
		}
	}

	// Try to extract from Object field
	if ud, ok := apiErr.Object.(*WrappedError); ok {
		return ud
	}

	// Check the Cause field if Object didn't contain our error
	if apiErr.Cause != nil {
		return GetWrappedError(apiErr.Cause)
	}

	return nil
}

// Format implements fmt.Formatter interface.
func (e *WrappedError) Format(s fmt.State, verb rune) {
	switch verb {
	case 'v':
		if s.Flag('+') {
			_, _ = io.WriteString(s, e.Error())
			_, _ = io.WriteString(s, "\n")
			_, _ = io.WriteString(s, e.Stack())
			return
		}
		fallthrough
	case 's':
		_, _ = io.WriteString(s, e.Error())
	case 'q':
		_, _ = fmt.Fprintf(s, "%q", e.Error())
	}
}

// RegisterErrorsModule registers the errors module in Lua.
// RegisterErrorsModule registers the errors module in Lua.
func RegisterErrorsModule(l *lua.LState) {
	// Register error type with just metamethods
	value.RegisterMetamethods(l, "error", map[string]lua.LGFunction{
		"__tostring": func(L *lua.LState) int {
			if ud := L.CheckUserData(1); ud != nil {
				if err, ok := ud.Value.(error); ok {
					L.Push(lua.LString(err.Error()))
					return 1
				}
			}
			return 0
		},
	})

	// Create errors module table with exact size
	mod := l.CreateTable(0, 3) // pre-allocate for 3 functions

	// Add functions to module with direct access
	mod.RawSetString("new", l.NewFunction(newError))
	mod.RawSetString("wrap", l.NewFunction(wrapError))
	mod.RawSetString("call_stack", l.NewFunction(callStack))

	// Set global with direct access
	l.SetGlobal("errors", mod)
}

// callStack implements the errors.call_stack() function in Lua.
// Returns the Lua stack trace as a Lua table for easier analysis.
func callStack(l *lua.LState) int {
	v := l.Get(1)
	ud, ok := v.(*lua.LUserData)
	if !ok {
		l.Push(lua.LNil)
		return 1
	}

	w, ok := ud.Value.(*WrappedError)
	if !ok {
		l.Push(lua.LNil)
		return 1
	}

	// Create a table representation of the stack trace
	err := l.NewTable()

	// Set thread Source
	err.RawSetString("thread", lua.LString(w.LuaStack.ThreadID))

	// Create frames table array
	framesTable := l.NewTable()
	for i, frame := range w.LuaStack.Frames {
		frameTable := l.NewTable()

		// Add frame details
		frameTable.RawSetString("level", lua.LNumber(frame.Level))
		frameTable.RawSetString("source", lua.LString(frame.Source))
		frameTable.RawSetString("line", lua.LNumber(frame.CurrentLine))
		frameTable.RawSetString("name", lua.LString(frame.Name))
		frameTable.RawSetString("type", lua.LString(frame.FuncType))

		framesTable.RawSetInt(i+1, frameTable)
	}

	err.RawSetString("frames", framesTable)
	l.Push(err)

	return 1
}

// newError implements errors.new(message) in Lua.
func newError(l *lua.LState) int {
	msg := l.CheckString(1)
	// Spawn new wrapped error
	wrappedErr := WrapError(l, fmt.Errorf("%s", msg), "")

	// Return as userdata with error metatable
	ud := l.NewUserData()
	ud.Value = wrappedErr
	ud.Metatable = value.GetTypeMetatable(nil, "error")
	l.Push(ud)

	return 1
}

// wrapError implements errors.wrap(parent, new_error) in Lua.
func wrapError(l *lua.LState) int {
	parent := l.CheckAny(1) // Parent error
	newErr := l.CheckAny(2) // New error message/error

	// Handle parent error
	var parentErr error

	switch v := parent.(type) {
	case *lua.LUserData:
		if err, ok := v.Value.(error); ok {
			parentErr = err
		} else {
			l.ArgError(1, "parent must be string or error object")
			return 0
		}
	case lua.LString:
		parentErr = fmt.Errorf("%s", string(v))
	default:
		l.ArgError(1, "parent must be string or error object")
		return 0
	}

	// Spawn Context from new error
	var context string
	switch v := newErr.(type) {
	case *lua.LUserData:
		if err, ok := v.Value.(error); ok {
			context = err.Error()
		} else {
			l.ArgError(2, "new error must be string or error object")
			return 0
		}
	case lua.LString:
		context = string(v)
	default:
		l.ArgError(2, "new error must be string or error object")
		return 0
	}

	// Spawn new wrapped error
	wrappedErr := WrapError(l, parentErr, context)

	// Return as userdata with error metatable
	ud := l.NewUserData()
	ud.Value = wrappedErr
	ud.Metatable = value.GetTypeMetatable(nil, "error")
	l.Push(ud)

	return 1
}

// Unwrap extracts a WrappedError from various Lua and Go error types.
func Unwrap(l any) *WrappedError {
	switch v := l.(type) {
	case *lua.LUserData:
		// Try to extract WrappedError directly from userdata value
		if err, ok := v.Value.(*WrappedError); ok {
			return err
		}

		// If userdata contains ApiError, try to extract from there
		if apiErr, ok := v.Value.(*lua.ApiError); ok {
			return GetWrappedError(apiErr)
		}

	case *lua.ApiError:
		return GetWrappedError(v)

	case *WrappedError:
		return v

	case error:
		// Try to extract WrappedError using errors.As
		var we *WrappedError
		if errors.As(v, &we) {
			return we
		}
		return GetWrappedError(v)
	}

	return nil
}
