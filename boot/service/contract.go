package service

import (
	"context"

	"github.com/ponyruntime/pony/api/boot"
	"github.com/ponyruntime/pony/api/event"
	logapi "github.com/ponyruntime/pony/api/logs"
	"github.com/ponyruntime/pony/api/payload"
	bootpkg "github.com/ponyruntime/pony/boot"
	"github.com/ponyruntime/pony/service/di"
	"github.com/ponyruntime/pony/system/eventbus"
	reghandler "github.com/ponyruntime/pony/system/registry/events"
)

type contractSystemPlugin struct {
	handler eventbus.EventHandler
}

func (p *contractSystemPlugin) Name() string        { return "contract_system" }
func (p *contractSystemPlugin) Phase() boot.Phase   { return boot.PostInit }
func (p *contractSystemPlugin) DependsOn() []string { return nil }

func (p *contractSystemPlugin) Load(ctx context.Context) (context.Context, error) {
	logger := logapi.GetLogger(ctx)
	dtt := payload.GetTranscoder(ctx)
	bus := event.GetBus(ctx)

	manager := di.NewManager(
		bus,
		dtt,
		logger.Named("contract"),
	)

	p.handler = reghandler.NewRegistryHandler("contract.(definition|binding)", manager)
	return ctx, nil
}

func (p *contractSystemPlugin) Handler() eventbus.EventHandler {
	return p.handler
}

func init() {
	bootpkg.MustRegister(&contractSystemPlugin{})
}
