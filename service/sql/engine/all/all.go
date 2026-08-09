// SPDX-License-Identifier: MPL-2.0

// Package all provides the built-in SQL drivers for composition roots that want
// the standard Wippy database set. It does not register anything globally.
package all

import (
	sqlservice "github.com/wippyai/runtime/service/sql"
	"github.com/wippyai/runtime/service/sql/engine/sqlite"
	"github.com/wippyai/runtime/service/sql/engine/standard"
)

// Drivers returns the built-in SQL drivers in a deterministic order.
func Drivers() []sqlservice.Driver {
	return []sqlservice.Driver{
		standard.NewPostgresDriver(),
		standard.NewMySQLDriver(),
		sqlite.NewDriver(),
	}
}
