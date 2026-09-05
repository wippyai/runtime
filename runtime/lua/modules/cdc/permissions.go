// SPDX-License-Identifier: MPL-2.0

package cdc

import (
	"fmt"

	lua "github.com/wippyai/go-lua"
	"github.com/wippyai/runtime/runtime/security"
)

// Source authority is separate from database access: CDC can expose every
// captured row, including before images. Filters only narrow delivery; they
// never grant access to a source.
func allowSource(l *lua.LState, action, source string) bool {
	if security.IsAllowed(l.Context(), action, source, nil) {
		return true
	}
	l.Push(lua.LNil)
	l.Push(lua.NewLuaError(l, fmt.Sprintf("not allowed to %s: %s", action, source)).WithKind(lua.PermissionDenied).WithRetryable(false))
	return false
}
