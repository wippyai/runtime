// SPDX-License-Identifier: MPL-2.0

package pg

import (
	lua "github.com/wippyai/go-lua"
)

// ScopeSeparator is the delimiter between scope name and group name.
//
// Deprecated: use pg.open() to acquire isolated PG scope instances instead.
const ScopeSeparator = "::"

// scope returns an error directing users to pg.open().
//
// Deprecated: pg.scope() used group name prefixing for namespace isolation.
// Use pg.open("app:pg_name") instead, which provides real scope isolation
// backed by independent PG service instances.
func scope(l *lua.LState) int {
	return pushPGError(l, lua.LNil, newPGError(l, lua.Invalid,
		"pg.scope() is deprecated; use pg.open(\"app:<scope_id>\") to acquire an isolated PG scope instance"))
}
