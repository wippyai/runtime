package service

import (
	"context"
	"fmt"

	"github.com/ponyruntime/pony/api/boot"
	envapi "github.com/ponyruntime/pony/api/env"
	"github.com/ponyruntime/pony/api/event"
	logapi "github.com/ponyruntime/pony/api/logs"
	"github.com/ponyruntime/pony/api/payload"
	bootpkg "github.com/ponyruntime/pony/boot"
	"github.com/ponyruntime/pony/service/sql"
	"github.com/ponyruntime/pony/system/eventbus"
	reghandler "github.com/ponyruntime/pony/system/registry/events"
)

type sqlPlugin struct {
	handler eventbus.EventHandler
}

func (p *sqlPlugin) Name() string        { return bootpkg.SQL }
func (p *sqlPlugin) Phase() boot.Phase   { return boot.PostInit }
func (p *sqlPlugin) DependsOn() []string { return []string{bootpkg.Environment} }

func (p *sqlPlugin) Load(ctx context.Context) (context.Context, error) {
	logger := logapi.GetLogger(ctx)
	dtt := payload.GetTranscoder(ctx)
	bus := event.GetBus(ctx)
	envRegistry := envapi.GetRegistry(ctx)

	manager, err := sql.NewManager(
		dtt,
		bus,
		logger.Named("sql"),
		envRegistry,
	)
	if err != nil {
		return ctx, fmt.Errorf("failed to create sql manager: %w", err)
	}

	p.handler = reghandler.NewRegistryHandler("db.sql.*", manager)
	return ctx, nil
}

func (p *sqlPlugin) Handler() eventbus.EventHandler {
	return p.handler
}

func init() {
	bootpkg.MustRegister(&sqlPlugin{})
}
