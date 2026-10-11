//go:build !cgo

// SPDX-License-Identifier: MPL-2.0

package sql

import lua "github.com/wippyai/go-lua"

func wrapSQLiteError(wrapped *lua.Error, _ error) *lua.Error {
	return wrapped
}
