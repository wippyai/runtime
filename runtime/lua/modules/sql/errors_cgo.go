//go:build cgo

// SPDX-License-Identifier: MPL-2.0

package sql

import (
	"errors"

	"github.com/rqlite/go-sqlite3"
	lua "github.com/wippyai/go-lua"
)

// wrapSQLError retains driver result codes at the public Lua boundary.
func wrapSQLError(l *lua.LState, cause error, operation string) *lua.Error {
	wrapped := lua.WrapErrorWithLua(l, cause, operation)
	var native sqlite3.Error
	if errors.As(cause, &native) {
		details := make(map[string]any, len(wrapped.Details())+2)
		for key, value := range wrapped.Details() {
			details[key] = value
		}
		details["sqlite_code"] = int(native.Code)
		details["sqlite_extended_code"] = int(native.ExtendedCode)
		return wrapped.WithDetails(details)
	}
	return wrapped
}
