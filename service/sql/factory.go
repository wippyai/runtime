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

// DefaultPoolFactory is the registry-backed Factory used in production.
type DefaultPoolFactory struct{}

// NewDefaultPoolFactory creates a new default pool factory.
func NewDefaultPoolFactory() Factory {
	return &DefaultPoolFactory{}
}

// CreatePool implements Factory.CreatePool.
func (f *DefaultPoolFactory) CreatePool(ctx context.Context, deps EngineDeps, entry registry.Entry) (*ConnPool, config.EngineConfig, error) {
	eng, ok := engineFor(entry.Kind)
	if !ok {
		return nil, nil, NewUnsupportedEntryKindError(entry.Kind)
	}

	return createPool(ctx, deps, eng, entry)
}

// UpdatePool implements Factory.UpdatePool.
func (f *DefaultPoolFactory) UpdatePool(ctx context.Context, deps EngineDeps, pool *ConnPool, entry registry.Entry) (config.EngineConfig, error) {
	eng, ok := engineFor(entry.Kind)
	if !ok {
		return nil, NewUnsupportedEntryKindError(entry.Kind)
	}

	return updatePool(ctx, deps, eng, pool, entry)
}
