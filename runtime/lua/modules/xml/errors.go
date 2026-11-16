// SPDX-License-Identifier: MPL-2.0

package xml

import (
	"context"
	"errors"

	lua "github.com/wippyai/go-lua"
	apierror "github.com/wippyai/runtime/api/error"
)

var detailKeys = [...]string{"line", "column", "offset", "limit"}

// luaError converts an operation failure into the module's structured error. Core
// errors carry their kind and details; any other failure comes from a stream or the
// runtime and is an I/O error unless the operation was canceled.
func luaError(l *lua.LState, err error) *lua.Error {
	kind := lua.Internal
	details := map[string]any{"code": "io"}
	var e apierror.Error
	switch {
	case errors.As(err, &e) && e.Details() != nil && e.Details().GetString("code", "") != "":
		kind = lua.Kind(e.Kind())
		d := e.Details()
		details["code"] = d.GetString("code", "")
		for _, k := range detailKeys {
			if v, ok := d.Get(k); ok {
				details[k] = v
			}
		}
		if path, ok := d.Get("path"); ok {
			names, _ := path.([]string)
			list := make([]any, len(names))
			for i, n := range names {
				list[i] = n
			}
			details["path"] = list
		}
	case errors.Is(err, context.Canceled) || errors.Is(err, context.DeadlineExceeded):
		kind = lua.Canceled
		details["code"] = "canceled"
	}
	return lua.WrapErrorWithLua(l, err, "").WithKind(kind).WithRetryable(false).WithDetails(details)
}

// invalid builds an Invalid error with the given code.
func invalid(l *lua.LState, code, msg string) *lua.Error {
	return lua.NewLuaError(l, msg).WithKind(lua.Invalid).WithRetryable(false).WithDetails(map[string]any{"code": code})
}

func argument(l *lua.LState, msg string) *lua.Error { return invalid(l, "argument", msg) }

func state(l *lua.LState, msg string) *lua.Error { return invalid(l, "state", msg) }

// fail pushes first followed by an error and returns the result count.
func fail(l *lua.LState, first lua.LValue, err *lua.Error) int {
	l.Push(first)
	l.Push(err)
	return 2
}

// raise throws a structured error from a function without an error result.
func raise(l *lua.LState, err *lua.Error) int {
	l.Error(err, 1)
	return 0
}
