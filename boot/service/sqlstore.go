package service

import (
	"context"

	"github.com/ponyruntime/pony/api/boot"
	"github.com/ponyruntime/pony/api/event"
	logapi "github.com/ponyruntime/pony/api/logs"
	"github.com/ponyruntime/pony/api/payload"
	bootpkg "github.com/ponyruntime/pony/boot"
	"github.com/ponyruntime/pony/service/sqlstore"
	"github.com/ponyruntime/pony/system/eventbus"
	reghandler "github.com/ponyruntime/pony/system/registry/events"
)

type sqlStorePlugin struct {
	handler eventbus.EventHandler
}

func (p *sqlStorePlugin) Name() string        { return bootpkg.SQLStore }
func (p *sqlStorePlugin) Phase() boot.Phase   { return boot.PostInit }
func (p *sqlStorePlugin) DependsOn() []string { return nil }

func (p *sqlStorePlugin) Load(ctx context.Context) (context.Context, error) {
	logger := logapi.GetLogger(ctx)
	dtt := payload.GetTranscoder(ctx)
	bus := event.GetBus(ctx)

	manager := sqlstore.NewManager(
		bus,
		dtt,
		logger.Named("sqlstore"),
	)

	p.handler = reghandler.NewRegistryHandler("store.sql", manager)
	return ctx, nil
}

func (p *sqlStorePlugin) Handler() eventbus.EventHandler {
	return p.handler
}

func init() {
	bootpkg.MustRegister(&sqlStorePlugin{})
}
