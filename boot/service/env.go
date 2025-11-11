package service

import (
	"context"

	"github.com/ponyruntime/pony/api/boot"
	"github.com/ponyruntime/pony/api/event"
	logapi "github.com/ponyruntime/pony/api/logs"
	"github.com/ponyruntime/pony/api/payload"
	bootpkg "github.com/ponyruntime/pony/boot"
	envservice "github.com/ponyruntime/pony/service/env"
	"github.com/ponyruntime/pony/system/eventbus"
	reghandler "github.com/ponyruntime/pony/system/registry/events"
)

type envPlugin struct {
	handler eventbus.EventHandler
}

func (p *envPlugin) Name() string        { return "env_service" }
func (p *envPlugin) Phase() boot.Phase   { return boot.PostInit }
func (p *envPlugin) DependsOn() []string { return nil }

func (p *envPlugin) Load(ctx context.Context) (context.Context, error) {
	logger := logapi.GetLogger(ctx)
	dtt := payload.GetTranscoder(ctx)
	bus := event.GetBus(ctx)

	manager := envservice.NewManager(
		bus,
		dtt,
		logger.Named("env"),
		envservice.NewDefaultEnvStorageFactory(),
	)

	p.handler = reghandler.NewRegistryHandler("env.**", manager)
	return ctx, nil
}

func (p *envPlugin) Handler() eventbus.EventHandler {
	return p.handler
}

func init() {
	bootpkg.MustRegister(&envPlugin{})
}
