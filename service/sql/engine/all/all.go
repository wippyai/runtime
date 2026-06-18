// SPDX-License-Identifier: MPL-2.0

// Package all registers every built-in SQL engine via blank import. A composition
// root (for example the storage boot component) imports this package so the standard
// dialects are available; out-of-tree engines register themselves the same way.
package all

import (
	// Blank imports register the built-in engines with service/sql via init.
	_ "github.com/wippyai/runtime/service/sql/engine/sqlite"
	_ "github.com/wippyai/runtime/service/sql/engine/standard"
)
