package engine

import (
	"fmt"
	lua "github.com/yuin/gopher-lua"
	"strings"
)

// ErrorWrapper holds Go error details for Lua
type ErrorWrapper struct {
	Err error
}

const (
	errorTypeName = "go.error"
)

// Core metamethods for error behavior
func errorToString(L *lua.LState) int {
	ud := L.CheckUserData(1)
	if wrapper, ok := ud.Value.(*ErrorWrapper); ok {
		L.Push(lua.LString(wrapper.Err.Error()))
		return 1
	}
	return 0
}

// Creates the error type metatable
func RegisterErrorType(L *lua.LState) {
	mt := L.NewTypeMetatable(errorTypeName)

	// String conversion (used by print)
	L.SetField(mt, "__tostring", L.NewFunction(errorToString))

	// Important: When used with error(), Lua calls tostring
	// So __tostring makes our error behave like a normal Lua error string
}

// WrapError creates a new error userdata
func WrapError(L *lua.LState, err error) *lua.LUserData {
	wrapper := &ErrorWrapper{Err: err}
	ud := L.NewUserData()
	ud.Value = wrapper
	L.SetMetatable(ud, L.GetTypeMetatable(errorTypeName))
	return ud
}

// GetGoError retrieves the original Go error if present
func GetGoError(L *lua.LState, idx int) error {
	if ud, ok := L.Get(idx).(*lua.LUserData); ok {
		if wrapper, ok := ud.Value.(*ErrorWrapper); ok {
			return wrapper.Err
		}
	}
	return nil
}

// RaiseError creates and raises an error
func RaiseError(L *lua.LState, err error) {
	errObj := WrapError(L, err)
	L.Error(errObj, 1) // This will call __tostring when propagating
}

func getStackTrace(L *lua.LState) []string {
	var traces []string

	// Skip level 0 as it's usually the error handler itself
	for level := 1; ; level++ {
		if ar, ok := L.GetStack(level); ok {
			funcTable := L.NewTable()
			if _, err := L.GetInfo("Slnf", ar, funcTable); err != nil {
				break
			}

			// Format stack trace entry
			trace := fmt.Sprintf("%s:%d (in %s)",
				ar.Source,
				ar.CurrentLine,
				ar.Name)

			// Add local variables if present
			var locals []string
			for i := 1; ; i++ {
				name, value := L.GetLocal(ar, i)
				if name == "" {
					break
				}
				locals = append(locals, fmt.Sprintf("%s = %v", name, value))
			}

			if len(locals) > 0 {
				trace += fmt.Sprintf(" [locals: %s]", strings.Join(locals, ", "))
			}

			traces = append(traces, trace)
		} else {
			break
		}
	}
	return traces
}

// Enhanced error type
type EnhancedError struct {
	Err        error
	StackTrace []string
}

func (e *EnhancedError) Error() string {
	return fmt.Sprintf("%v\nStack trace:\n%s",
		e.Err,
		strings.Join(e.StackTrace, "\n"))
}

// Enhanced error wrapper
//func WrapErrorWithStack(L *lua.LState, err error) *lua.LUserData {
//	enhanced := &EnhancedError{
//		Err:        err,
//		StackTrace: getStackTrace(L),
//	}
//
//	ud := L.NewUserData()
//	ud.Value = &ErrorWrapper{Err: enhanced}
//	L.SetMetatable(ud, L.GetTypeMetatable(errorTypeName))
//	return ud
//}
