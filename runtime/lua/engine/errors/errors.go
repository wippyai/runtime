package errors

import (
	"errors"
	"fmt"
	"io"
	"runtime"
	"strings"

	"github.com/wippyai/runtime/api/attrs"
	apierr "github.com/wippyai/runtime/api/error"
	"github.com/wippyai/runtime/runtime/lua/engine/inspect"
	"github.com/wippyai/runtime/runtime/lua/engine/value"
	lua "github.com/yuin/gopher-lua"
)

// WrappedError represents an error that occurred in either Go or Lua Context,
// preserving stack traces from both environments.
type WrappedError struct {
	Err       error               // Points to parent error
	LuaStack  *inspect.StackTrace // Lua stack at this wrap point
	goStack   []uintptr           // Go stack at this wrap point
	Context   string              // Context for this wrap
	kind      apierr.Kind         // Error category
	retryable *bool               // Retryable metadata (nil=unknown, true, false)
	details   attrs.Bag           // Structured metadata
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

// Kind returns the error category.
func (e *WrappedError) Kind() apierr.Kind {
	if e == nil || e.kind == "" {
		return apierr.KindUnknown
	}
	return e.kind
}

// Retryable indicates if the operation should be retried.
func (e *WrappedError) Retryable() apierr.Ternary {
	if e == nil || e.retryable == nil {
		return apierr.Unknown
	}
	if *e.retryable {
		return apierr.True
	}
	return apierr.False
}

// Details returns structured metadata about the error.
func (e *WrappedError) Details() attrs.Attributes {
	if e == nil || e.details == nil {
		return attrs.NewBag()
	}
	return e.details
}

// SetKind sets the error category.
func (e *WrappedError) SetKind(kind apierr.Kind) {
	if e != nil {
		e.kind = kind
	}
}

// SetRetryable sets whether the error is retryable.
func (e *WrappedError) SetRetryable(retryable *bool) {
	if e != nil {
		e.retryable = retryable
	}
}

// SetDetails sets the error details.
func (e *WrappedError) SetDetails(details attrs.Bag) {
	if e != nil {
		e.details = details
	}
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
func RegisterErrorsModule(l *lua.LState) {
	// Register error type with metamethods and type methods
	value.RegisterTypeMethods(l, "error", map[string]lua.LGFunction{
		"__tostring": func(L *lua.LState) int {
			if ud := L.CheckUserData(1); ud != nil {
				if err, ok := ud.Value.(error); ok {
					L.Push(lua.LString(err.Error()))
					return 1
				}
			}
			return 0
		},
	}, map[string]lua.LGFunction{
		"kind":      errorKindMethod,
		"retryable": errorRetryableMethod,
		"details":   errorDetailsMethod,
		"message":   errorMessageMethod,
	})

	// Create errors module table (3 functions + 11 kind constants)
	mod := l.CreateTable(0, 14)

	// Add functions to module
	mod.RawSetString("new", l.NewFunction(newError))
	mod.RawSetString("wrap", l.NewFunction(wrapError))
	mod.RawSetString("call_stack", l.NewFunction(callStack))

	// Add kind constants (UPPERCASE convention)
	mod.RawSetString("NOT_FOUND", lua.LString(string(apierr.KindNotFound)))
	mod.RawSetString("ALREADY_EXISTS", lua.LString(string(apierr.KindAlreadyExists)))
	mod.RawSetString("INVALID", lua.LString(string(apierr.KindInvalid)))
	mod.RawSetString("PERMISSION_DENIED", lua.LString(string(apierr.KindPermissionDenied)))
	mod.RawSetString("UNAVAILABLE", lua.LString(string(apierr.KindUnavailable)))
	mod.RawSetString("INTERNAL", lua.LString(string(apierr.KindInternal)))
	mod.RawSetString("CANCELED", lua.LString(string(apierr.KindCanceled)))
	mod.RawSetString("CONFLICT", lua.LString(string(apierr.KindConflict)))
	mod.RawSetString("TIMEOUT", lua.LString(string(apierr.KindTimeout)))
	mod.RawSetString("RATE_LIMITED", lua.LString(string(apierr.KindRateLimited)))
	mod.RawSetString("UNKNOWN", lua.LString(string(apierr.KindUnknown)))

	// Set global
	l.SetGlobal("errors", mod)
}

// errorKindMethod implements err:kind() method in Lua.
// Returns the error kind as a string.
func errorKindMethod(l *lua.LState) int {
	ud := l.CheckUserData(1)
	if wrappedErr, ok := ud.Value.(*WrappedError); ok {
		l.Push(lua.LString(string(wrappedErr.Kind())))
		return 1
	}
	l.Push(lua.LString(string(apierr.KindUnknown)))
	return 1
}

// errorRetryableMethod implements err:retryable() method in Lua.
// Returns boolean or nil (for unknown).
func errorRetryableMethod(l *lua.LState) int {
	ud := l.CheckUserData(1)
	if wrappedErr, ok := ud.Value.(*WrappedError); ok {
		ternary := wrappedErr.Retryable()
		switch ternary {
		case apierr.Unknown:
			l.Push(lua.LNil)
		case apierr.True:
			l.Push(lua.LBool(true))
		case apierr.False:
			l.Push(lua.LBool(false))
		default:
			l.Push(lua.LNil)
		}
		return 1
	}
	l.Push(lua.LNil)
	return 1
}

// errorDetailsMethod implements err:details() method in Lua.
// Returns a table with all details or nil if no details.
func errorDetailsMethod(l *lua.LState) int {
	ud := l.CheckUserData(1)
	if wrappedErr, ok := ud.Value.(*WrappedError); ok {
		details := wrappedErr.Details()
		if bag, ok := details.(attrs.Bag); ok {
			if len(bag) == 0 {
				l.Push(lua.LNil)
				return 1
			}
			detailsTable := l.CreateTable(0, len(bag))
			bag.Iterate(func(key string, value any) {
				detailsTable.RawSetString(key, goValueToLua(l, value))
			})
			l.Push(detailsTable)
			return 1
		}
	}
	l.Push(lua.LNil)
	return 1
}

// errorMessageMethod implements err:message() method in Lua.
// Returns just the error message string without stack trace.
func errorMessageMethod(l *lua.LState) int {
	ud := l.CheckUserData(1)
	if err, ok := ud.Value.(error); ok {
		l.Push(lua.LString(err.Error()))
		return 1
	}
	l.Push(lua.LString(""))
	return 1
}

// goValueToLua converts a Go value to a Lua value.
func goValueToLua(l *lua.LState, val any) lua.LValue {
	switch v := val.(type) {
	case string:
		return lua.LString(v)
	case int:
		return lua.LNumber(v)
	case int64:
		return lua.LNumber(v)
	case float64:
		return lua.LNumber(v)
	case bool:
		return lua.LBool(v)
	case map[string]any:
		table := l.CreateTable(0, len(v))
		for k, val := range v {
			table.RawSetString(k, goValueToLua(l, val))
		}
		return table
	case nil:
		return lua.LNil
	default:
		return lua.LString(fmt.Sprintf("%v", v))
	}
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

// newError implements errors.new(message) or errors.new({...}) in Lua.
// Accepts either a string for simple errors or a table with:
//   - message: error message (required)
//   - kind: error kind constant (optional)
//   - retryable: boolean (optional)
//   - details: table of key-value pairs (optional)
func newError(l *lua.LState) int {
	arg := l.CheckAny(1)

	var msg string
	var kind apierr.Kind
	var retryable *bool
	var details attrs.Bag

	switch v := arg.(type) {
	case lua.LString:
		// Simple string error (backward compatible)
		msg = string(v)
		kind = apierr.KindUnknown

	case *lua.LTable:
		// Table-based error with metadata
		msgVal := v.RawGetString("message")
		if msgVal == lua.LNil {
			l.ArgError(1, "message field is required")
			return 0
		}
		if msgStr, ok := msgVal.(lua.LString); ok {
			msg = string(msgStr)
		} else {
			l.ArgError(1, "message must be a string")
			return 0
		}

		// Extract kind
		kindVal := v.RawGetString("kind")
		if kindVal != lua.LNil {
			if kindStr, ok := kindVal.(lua.LString); ok {
				kind = apierr.Kind(string(kindStr))
			}
		}
		if kind == "" {
			kind = apierr.KindUnknown
		}

		// Extract retryable
		retryableVal := v.RawGetString("retryable")
		if retryableVal != lua.LNil {
			if retryableBool, ok := retryableVal.(lua.LBool); ok {
				b := bool(retryableBool)
				retryable = &b
			}
		}

		// Extract details
		detailsVal := v.RawGetString("details")
		if detailsVal != lua.LNil {
			if detailsTable, ok := detailsVal.(*lua.LTable); ok {
				details = attrs.NewBag()
				detailsTable.ForEach(func(k, val lua.LValue) {
					if keyStr, ok := k.(lua.LString); ok {
						details.Set(string(keyStr), luaValueToGo(val))
					}
				})
			}
		}

	default:
		l.ArgError(1, "expected string or table")
		return 0
	}

	// Create wrapped error
	wrappedErr := WrapError(l, fmt.Errorf("%s", msg), "")
	wrappedErr.kind = kind
	wrappedErr.retryable = retryable
	wrappedErr.details = details

	// Return as userdata with error metatable
	ud := l.NewUserData()
	ud.Value = wrappedErr
	ud.Metatable = value.GetTypeMetatable(nil, "error")
	l.Push(ud)

	return 1
}

// luaValueToGo converts a Lua value to a Go value for attrs.Bag storage.
func luaValueToGo(val lua.LValue) any {
	if val.Type() == lua.LTNil {
		return nil
	}

	switch v := val.(type) {
	case lua.LString:
		return string(v)
	case lua.LNumber:
		return float64(v)
	case lua.LBool:
		return bool(v)
	case *lua.LTable:
		// Convert table to map[string]any
		m := make(map[string]any)
		v.ForEach(func(k, val lua.LValue) {
			if keyStr, ok := k.(lua.LString); ok {
				m[string(keyStr)] = luaValueToGo(val)
			}
		})
		return m
	default:
		return val.String()
	}
}

// wrapError implements errors.wrap(parent, new_error) in Lua.
// Preserves metadata (kind, retryable, details) from parent error.
func wrapError(l *lua.LState) int {
	parent := l.CheckAny(1) // Parent error
	newErr := l.CheckAny(2) // New error message/error

	// Handle parent error
	var parentErr error
	var parentWrapped *WrappedError

	switch v := parent.(type) {
	case *lua.LUserData:
		if err, ok := v.Value.(error); ok {
			parentErr = err
			// Try to extract WrappedError to preserve metadata
			if we, ok := v.Value.(*WrappedError); ok {
				parentWrapped = we
			}
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

	// Preserve metadata from parent if available
	if parentWrapped != nil {
		if parentWrapped.kind != "" {
			wrappedErr.kind = parentWrapped.kind
		}
		if parentWrapped.retryable != nil {
			wrappedErr.retryable = parentWrapped.retryable
		}
		if parentWrapped.details != nil && parentWrapped.details.Len() > 0 {
			wrappedErr.details = parentWrapped.details
		}
	}

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
