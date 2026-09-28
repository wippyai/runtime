// SPDX-License-Identifier: MPL-2.0

package sql

import (
	"context"

	"github.com/wippyai/runtime/api/registry"
	config "github.com/wippyai/runtime/api/service/sql"
)

// Factory creates and updates connection pools. It dispatches to the engine
// registered for an entry's kind, so it never needs per-engine branches.
type Factory interface {
	CreatePool(ctx context.Context, deps EngineDeps, entry registry.Entry) (*ConnPool, config.EngineConfig, error)
	UpdatePool(ctx context.Context, deps EngineDeps, pool *ConnPool, entry registry.Entry) (config.EngineConfig, error)
}

// DefaultPoolFactory dispatches entries to the drivers supplied at
// construction. It deliberately has no package-global registry.
type DefaultPoolFactory struct {
	drivers map[registry.Kind]Driver
}

// NewDefaultPoolFactory creates a pool factory with the supplied drivers.
func NewDefaultPoolFactory(drivers ...Driver) Factory {
	registered := make(map[registry.Kind]Driver, len(drivers))
	for _, driver := range drivers {
		if driver != nil {
			registered[driver.Kind()] = driver
		}
	}
	return &DefaultPoolFactory{drivers: registered}
}

// CreatePool implements Factory.CreatePool.
func (f *DefaultPoolFactory) CreatePool(ctx context.Context, deps EngineDeps, entry registry.Entry) (*ConnPool, config.EngineConfig, error) {
	driver, ok := f.drivers[entry.Kind]
	if !ok {
		return nil, nil, NewUnsupportedEntryKindError(entry.Kind)
	}

	return createPool(ctx, deps, driver, entry)
}

// UpdatePool implements Factory.UpdatePool.
func (f *DefaultPoolFactory) UpdatePool(ctx context.Context, deps EngineDeps, pool *ConnPool, entry registry.Entry) (config.EngineConfig, error) {
	driver, ok := f.drivers[entry.Kind]
	if !ok {
		return nil, NewUnsupportedEntryKindError(entry.Kind)
	}

	return updatePool(ctx, deps, driver, pool, entry)
}
