// SPDX-License-Identifier: MPL-2.0

package sql

import (
	"sync"

	"github.com/wippyai/runtime/api/registry"
)

// driverOverrides lets an out-of-tree extension swap the database/sql driver an
// engine opens, keyed by registry kind. It is the only seam through which engine
// connection behavior (for example, a SQLite build that installs preupdate hooks)
// is altered without modifying the core package.
var (
	driverMu        sync.RWMutex
	driverOverrides = make(map[registry.Kind]string)
)

// RegisterDriver overrides the database/sql driver name used for the given kind.
// Extensions call this from an init function, before any pool is created.
func RegisterDriver(kind registry.Kind, name string) {
	driverMu.Lock()
	driverOverrides[kind] = name
	driverMu.Unlock()
}

// driverName returns the registered override for kind, falling back to def.
func driverName(kind registry.Kind, def string) string {
	driverMu.RLock()
	name, ok := driverOverrides[kind]
	driverMu.RUnlock()
	if ok {
		return name
	}

	return def
}
