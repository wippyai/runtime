// Package errors provides error handling functionality for Lua-Go interoperability.
// It implements error wrapping, stack trace collection, and proper error propagation
// between Go and Lua environments.
package errors

import (
	"errors"
	"fmt"
	"github.com/ponyruntime/pony/runtime/lua/engine/inspect"
	lua "github.com/yuin/gopher-lua"
	"runtime"
	"strings"
)

// WrappedError represents an error that occurred in either Go or Lua context,
// preserving stack traces from both environments.
type WrappedError struct {
	err     error               // The underlying error
	stack   *inspect.StackTrace // Lua stack trace at the time of error
	goStack []uintptr           // Go stack trace at the time of error
}

// Error implements the error interface.
func (e *WrappedError) Error() string {
	return e.err.Error()
}

// Unwrap returns the underlying error for compatibility with Go 1.13+ error unwrapping.
func (e *WrappedError) Unwrap() error {
	if e == nil {
		return nil
	}
	return e.err
}

// Is reports whether the error matches a specific error value in the chain.
func (e *WrappedError) Is(target error) bool {
	return errors.Is(e.err, target)
}

// As attempts to convert the error to a specific type.
func (e *WrappedError) As(target any) bool {
	return errors.As(e.err, target)
}

// Stack returns a formatted string containing both Lua and Go stack traces
// with filtering of redundant and internal frames.
func (e *WrappedError) Stack() string {
	var result strings.Builder

	// Include source location from the original error
	result.WriteString(fmt.Sprintf("Source: %v\n", e.err))

	// Include Lua stack if available
	if e.stack != nil {
		result.WriteString(fmt.Sprintf("Lua Stack:\n%s", e.stack.String()))
	}

	// Add Go stack if available
	if len(e.goStack) > 0 {
		frames := runtime.CallersFrames(e.goStack)
		result.WriteString("\nGo Stack:\n")

		// Keep track of seen frames to avoid duplicates
		seenFrames := make(map[string]bool)

		for {
			frame, more := frames.Next()

			// Skip runtime internal functions that don't provide useful context
			if strings.HasPrefix(frame.Function, "runtime.") &&
				(strings.Contains(frame.Function, "panic") ||
					strings.Contains(frame.Function, "throw") ||
					strings.Contains(frame.Function, "sigpanic")) {
				if more {
					continue
				}
				break
			}

			// Create unique frame identifier
			frameID := fmt.Sprintf("%s:%d", frame.File, frame.Line)

			// Skip duplicate frames
			if seenFrames[frameID] {
				if more {
					continue
				}
				break
			}

			seenFrames[frameID] = true
			result.WriteString(fmt.Sprintf("  %s:%d (%s)\n",
				frame.File,
				frame.Line,
				frame.Function))

			if !more {
				break
			}
		}
	}

	return result.String()
}

// ConfigureErrorMetatable sets up the error metatable in the Lua state
// to properly handle Go errors when converting them to strings in Lua.
func ConfigureErrorMetatable(L *lua.LState) {
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
}

// WrapError captures both Go and Lua stack traces at the point of error creation.
func WrapError(L *lua.LState, err error) *WrappedError {
	if err == nil {
		return nil
	}

	// Capture Go stack
	stack := make([]uintptr, 64)
	n := runtime.Callers(1, stack)

	return &WrappedError{
		err:     err,
		stack:   inspect.GetStackTrace(L),
		goStack: stack[:n],
	}
}

// RaiseError wraps a Go error and raises it in the Lua state.
// This is the primary method for propagating Go errors into Lua code.
func RaiseError(L *lua.LState, err error) {
	wrapped := WrapError(L, err)

	key := fmt.Sprintf("__error_%p", wrapped)
	L.SetField(L.Get(lua.RegistryIndex).(*lua.LTable), key, lua.LString(wrapped.Error()))

	// Create user data and raise
	ud := L.NewUserData()
	ud.Value = wrapped
	L.SetMetatable(ud, L.GetTypeMetatable("error"))
	L.Error(ud, 1)
}

// GetWrappedError attempts to extract a WrappedError from a Lua error value.
// Returns nil if the error is not a wrapped error or cannot be extracted.
func GetWrappedError(err error) *WrappedError {
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
