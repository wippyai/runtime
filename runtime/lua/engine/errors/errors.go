// Package errors provides error handling functionality for Lua-Go interoperability.
// It implements error wrapping, stack trace collection, and proper error propagation
// between Go and Lua environments.
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

// WrappedError represents an error that occurred in either Go or Lua context,
// preserving stack traces from both environments.
type WrappedError struct {
	err      error               // The underlying error
	luaStack *inspect.StackTrace // Lua stack at time of wrapping
	goStack  []uintptr           // Go stack at time of wrapping
	context  string              // Additional context message
}

// Error implements the error interface.
func (e *WrappedError) Error() string {
	if e.context != "" {
		return fmt.Sprintf("%s: %v", e.context, e.err)
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
// with filtering of redundant and internal frames.
func (e *WrappedError) Stack() string {
	var result strings.Builder
	var seenFrames = make(map[string]bool)

	// Walk the error chain and collect all stack traces
	var stacks []*WrappedError
	current := e
	for current != nil {
		stacks = append(stacks, current)
		var next *WrappedError
		if errors.As(current.err, &next) {
			current = next
		} else {
			break
		}
	}

	// Print traces in reverse order (deepest first)
	for i := len(stacks) - 1; i >= 0; i-- {
		we := stacks[i]
		if we.context != "" {
			result.WriteString(fmt.Sprintf("Error layer: %s\n", we.context))
		}

		// Add Lua stack if present and unique
		if we.luaStack != nil {
			luaFrames := we.luaStack.String()
			if !seenFrames[luaFrames] {
				result.WriteString("Lua Stack:\n")
				result.WriteString(luaFrames)
				result.WriteString("\n")
				seenFrames[luaFrames] = true
			}
		}

		// Add Go stack if present
		if len(we.goStack) > 0 {
			frames := runtime.CallersFrames(we.goStack)
			result.WriteString("Go Stack:\n")
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
	}

	return result.String()
}

// Format implements the fmt.Formatter interface for WrappedError.
// It supports various formatting verbs:
//
//	%v (default) - prints only the error message
//	%+v        - prints the error message along with the stack trace
//	%s         - prints just the error message
//	%q         - prints the error message as a quoted string
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

// shouldSkipFrame returns true for runtime internal frames that should be filtered.
func shouldSkipFrame(name string) bool {
	return strings.HasPrefix(name, "runtime.") &&
		(strings.Contains(name, "panic") ||
			strings.Contains(name, "throw") ||
			strings.Contains(name, "sigpanic"))
}

// WrapError creates a new wrapped error with the given context.
func WrapError(L *lua.LState, err error, context string) *WrappedError {
	if err == nil {
		return nil
	}

	// First check if we're wrapping an existing wrapped error
	if existing := GetWrappedError(err); existing != nil {
		// Create new wrapper but preserve the original stack trace
		return &WrappedError{
			err:      err,
			luaStack: existing.luaStack, // Preserve original Lua stack
			goStack:  existing.goStack,  // Preserve original Go stack
			context:  context,
		}
	}

	// If it's a new error, capture current stacks
	stack := make([]uintptr, 64)
	n := runtime.Callers(2, stack) // Skip WrapError frame

	return &WrappedError{
		err:      err,
		luaStack: inspect.GetStackTrace(L),
		goStack:  stack[:n],
		context:  context,
	}
}

// RaiseError wraps a Go error and raises it in the Lua state.
// This is the primary method for propagating Go errors into Lua code.
func RaiseError(L *lua.LState, err error) {
	wrapped := WrapError(L, err, "")
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

// RegisterErrorsModule registers the errors module in Lua with wrapping functionality.
func RegisterErrorsModule(L *lua.LState) {
	fmt.Printf("Registering errors module\n")

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
// parent can be either a string or wrapped error userdata
// new_error can be either a string or wrapped error userdata
func wrapError(L *lua.LState) int {
	parent := L.CheckAny(1) // Parent error
	newErr := L.CheckAny(2) // New error message/error

	// Handle parent error and preserve its stack if it exists
	var parentErr error
	var existingStack *inspect.StackTrace

	switch v := parent.(type) {
	case *lua.LUserData:
		if err, ok := v.Value.(*WrappedError); ok {
			parentErr = err
			existingStack = err.luaStack // Preserve the original stack
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

	// Get context from new error
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

	// Create wrapped error using existing stack if available
	var wrappedErr *WrappedError
	if existingStack != nil {
		// Create new wrapper but preserve the original stack trace
		wrappedErr = &WrappedError{
			err:      parentErr,
			luaStack: existingStack,
			goStack:  make([]uintptr, 64),
			context:  context,
		}
		// Capture current Go stack
		runtime.Callers(2, wrappedErr.goStack)
	} else {
		// New error, capture current stacks
		wrappedErr = WrapError(L, parentErr, context)
	}

	// Return as userdata with error metatable
	ud := L.NewUserData()
	ud.Value = wrappedErr
	L.SetMetatable(ud, L.GetTypeMetatable("error"))
	L.Push(ud)

	return 1
}
