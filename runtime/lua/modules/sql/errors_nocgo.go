//go:build !cgo

// SPDX-License-Identifier: MPL-2.0

package sql

import lua "github.com/wippyai/go-lua"

// sqliteNative reports no SQLite error: the SQLite driver requires cgo.
func sqliteNative(wrapped *lua.Error, _ error) (*lua.Error, lua.Kind, bool, bool) {
	return wrapped, lua.Unknown, false, false
}
