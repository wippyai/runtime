package service

import (
	"context"

	"github.com/ponyruntime/pony/api/boot"
	"github.com/ponyruntime/pony/api/event"
	logapi "github.com/ponyruntime/pony/api/logs"
	"github.com/ponyruntime/pony/api/payload"
	resourceapi "github.com/ponyruntime/pony/api/resource"
	secapi "github.com/ponyruntime/pony/api/security"
	bootpkg "github.com/ponyruntime/pony/boot"
	"github.com/ponyruntime/pony/service/tokenstore"
	"github.com/ponyruntime/pony/system/eventbus"
	reghandler "github.com/ponyruntime/pony/system/registry/events"
)

type tokenStorePlugin struct {
	handler eventbus.EventHandler
}

func (p *tokenStorePlugin) Name() string        { return bootpkg.TokenStore }
func (p *tokenStorePlugin) Phase() boot.Phase   { return boot.PostInit }
func (p *tokenStorePlugin) DependsOn() []string { return []string{bootpkg.Resources, bootpkg.Security} }

func (p *tokenStorePlugin) Load(ctx context.Context) (context.Context, error) {
	logger := logapi.GetLogger(ctx)
	dtt := payload.GetTranscoder(ctx)
	bus := event.GetBus(ctx)
	resources := resourceapi.GetRegistry(ctx)
	security := secapi.GetRegistry(ctx)

	manager := tokenstore.NewManager(
		bus,
		dtt,
		resources,
		security,
		logger.Named("tstore"),
	)

	p.handler = reghandler.NewRegistryHandler("security.token_store", manager)
	return ctx, nil
}

func (p *tokenStorePlugin) Handler() eventbus.EventHandler {
	return p.handler
}

func init() {
	bootpkg.MustRegister(&tokenStorePlugin{})
}
