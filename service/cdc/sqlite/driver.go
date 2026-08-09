// SPDX-License-Identifier: MPL-2.0

package sqlite

import (
	"context"

	"go.uber.org/zap"

	"github.com/wippyai/runtime/api/registry"
	"github.com/wippyai/runtime/api/resource"
	config "github.com/wippyai/runtime/api/service/cdc"
	"github.com/wippyai/runtime/api/supervisor"
	cdcservice "github.com/wippyai/runtime/service/cdc"
	entryutil "github.com/wippyai/runtime/system/entry"
)

// managedSource is the narrow internal contract shared by build-tagged
// implementations. Keeping it in the untagged driver file makes the package
// fail closed without the SQLite preupdate build tag rather than exposing a
// half-working source.
type managedSource interface {
	config.Source
	supervisor.Service
}

type sourceOptions struct {
	res            resource.Registry
	log            *zap.Logger
	id             registry.ID
	dbResource     registry.ID
	name           string
	statusInterval string
	tables         []string
	lifecycle      supervisor.LifecycleConfig
	snapshot       bool
}

// Driver wires the SQLite CDC implementation into the driver-neutral CDC
// manager. It owns no process-global SQL driver registration.
type Driver struct{}

func NewDriver() cdcservice.Driver { return Driver{} }

func (Driver) Kind() registry.Kind { return config.SQLite }

func (Driver) Create(ctx context.Context, entry registry.Entry, deps cdcservice.Dependencies) (cdcservice.ManagedSource, error) {
	if deps.Resources == nil {
		return nil, ErrResourceRegRequired
	}
	cfg, err := entryutil.DecodeEntryConfig[config.SQLiteConfig](ctx, deps.Transcoder, entry)
	if err != nil {
		return nil, NewInvalidConfigError(err)
	}
	if err := cfg.Validate(); err != nil {
		return nil, NewInvalidConfigError(err)
	}
	log := deps.Logger
	if log == nil {
		log = zap.NewNop()
	}
	return buildSource(sourceOptions{
		res:            deps.Resources,
		log:            log.With(zap.String("id", entry.ID.String())),
		id:             entry.ID,
		dbResource:     registry.ParseID(cfg.DBResource),
		name:           entry.ID.String(),
		statusInterval: cfg.StatusInterval,
		tables:         cfg.Tables,
		snapshot:       cfg.Snapshot,
		lifecycle:      cfg.Lifecycle,
	})
}

var _ cdcservice.Driver = Driver{}
