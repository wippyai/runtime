// Package errors provides error handling functionality for Lua-Go interoperability.
package errors

import (
	"errors"
	"fmt"
	"github.com/ponyruntime/pony/runtime/lua/engine/inspect"
	lua "github.com/yuin/gopher-lua"
	"io"
	"runtime"
	"strings"
)

// WrappedError represents an error that occurred in either Go or Lua Context,
// preserving stack traces from both environments.
type WrappedError struct {
	err      error               // Points to parent error
	LuaStack *inspect.StackTrace // Lua stack at this wrap point
	goStack  []uintptr           // Go stack at this wrap point
	Context  string              // Context for this wrap
}

// Error implements the error interface.
func (e *WrappedError) Error() string {
	if e.Context != "" {
		return fmt.Sprintf("%s: %v", e.Context, e.err)
	}
	return e.err.Error()
}

// Unwrap returns the underlying error for compatibility with Go 1.13+ error unwrapping.
func (e *WrappedError) Unwrap() error {
	if e == nil {
		return nil
	}
	return e.err
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
		if next, ok := current.err.(*WrappedError); ok {
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

// WrapError creates a new wrapped error with the given Context.
func WrapError(L *lua.LState, err error, context string) *WrappedError {
	if err == nil {
		return nil
	}

	// Create new stack traces
	goStack := make([]uintptr, 64)
	n := runtime.Callers(2, goStack)
	luaStack := inspect.GetStackTrace(L)

	// If already wrapped, preserve Context
	if we, ok := err.(*WrappedError); ok {
		if context == "" {
			// Even with no new Context, create new wrapper to capture current stacks
			return &WrappedError{
				err:      we.err,      // Link to original inner error
				LuaStack: luaStack,    // New Lua stack
				goStack:  goStack[:n], // New Go stack
				Context:  we.Context,  // Preserve Context
			}
		}

		return &WrappedError{
			err:      we,
			LuaStack: inspect.GetStackTrace(L),
			goStack:  goStack[:n],
			Context:  context,
		}
	}

	// Create new wrapped error
	return &WrappedError{
		err:      err,
		LuaStack: inspect.GetStackTrace(L),
		goStack:  goStack[:n],
		Context:  context,
	}
}

// RaiseError wraps a Go error and raises it in the Lua state.
func RaiseError(L *lua.LState, err error) {
	wrapped := WrapError(L, err, "")

	ud := L.NewUserData()
	ud.Value = wrapped
	L.SetMetatable(ud, L.GetTypeMetatable("error"))
	L.Error(ud, 1)
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
func RegisterErrorsModule(L *lua.LState) {
	// Create errors module
	mod := L.NewTable()
	L.SetGlobal("errors", mod)

	mt := L.NewTypeMetatable("error")
	L.SetField(mt, "__tostring", L.NewFunction(func(L *lua.LState) int {
		if ud := L.CheckUserData(1); ud != nil {
			if err, ok := ud.Value.(error); ok {
				L.Push(lua.LString(err.Error()))
				return 1
			}
		}
		return 0
	}))

	// Add wrap function
	L.SetField(mod, "wrap", L.NewFunction(wrapError))
}

// wrapError implements errors.wrap(parent, new_error) in Lua.
func wrapError(L *lua.LState) int {
	parent := L.CheckAny(1) // Parent error
	newErr := L.CheckAny(2) // New error message/error

	// Handle parent error
	var parentErr error

	switch v := parent.(type) {
	case *lua.LUserData:
		if err, ok := v.Value.(*WrappedError); ok {
			parentErr = err
		} else if err, ok := v.Value.(error); ok {
			parentErr = err
		} else {
			L.ArgError(1, "parent must be string or error object")
			return 0
		}
	case lua.LString:
		parentErr = fmt.Errorf("%s", string(v))
	default:
		L.ArgError(1, "parent must be string or error object")
		return 0
	}

	// Get Context from new error
	var context string
	switch v := newErr.(type) {
	case *lua.LUserData:
		if err, ok := v.Value.(error); ok {
			context = err.Error()
		} else {
			L.ArgError(2, "new error must be string or error object")
			return 0
		}
	case lua.LString:
		context = string(v)
	default:
		L.ArgError(2, "new error must be string or error object")
		return 0
	}

	// Create new wrapped error
	wrappedErr := WrapError(L, parentErr, context)

	// Return as userdata with error metatable
	ud := L.NewUserData()
	ud.Value = wrappedErr
	L.SetMetatable(ud, L.GetTypeMetatable("error"))
	L.Push(ud)

	return 1
}
