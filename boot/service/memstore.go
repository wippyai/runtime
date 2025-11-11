package service

import (
	"context"

	"github.com/ponyruntime/pony/api/boot"
	"github.com/ponyruntime/pony/api/event"
	logapi "github.com/ponyruntime/pony/api/logs"
	"github.com/ponyruntime/pony/api/payload"
	bootpkg "github.com/ponyruntime/pony/boot"
	"github.com/ponyruntime/pony/service/memstore"
	"github.com/ponyruntime/pony/system/eventbus"
	reghandler "github.com/ponyruntime/pony/system/registry/events"
)

type memStorePlugin struct {
	handler eventbus.EventHandler
}

func (p *memStorePlugin) Name() string        { return bootpkg.MemStore }
func (p *memStorePlugin) Phase() boot.Phase   { return boot.PostInit }
func (p *memStorePlugin) DependsOn() []string { return nil }

func (p *memStorePlugin) Load(ctx context.Context) (context.Context, error) {
	logger := logapi.GetLogger(ctx)
	dtt := payload.GetTranscoder(ctx)
	bus := event.GetBus(ctx)

	manager := memstore.NewManager(
		bus,
		dtt,
		logger.Named("memory"),
	)

	p.handler = reghandler.NewRegistryHandler("store.memory", manager)
	return ctx, nil
}

func (p *memStorePlugin) Handler() eventbus.EventHandler {
	return p.handler
}

func init() {
	bootpkg.MustRegister(&memStorePlugin{})
}
