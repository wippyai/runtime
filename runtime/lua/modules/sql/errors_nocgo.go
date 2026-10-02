//go:build !cgo

// SPDX-License-Identifier: MPL-2.0

package sql

import lua "github.com/wippyai/go-lua"

// The SQLite driver exposes native result codes only in CGO builds.
func wrapSQLError(l *lua.LState, cause error, operation string) *lua.Error {
	return lua.WrapErrorWithLua(l, cause, operation)
}
