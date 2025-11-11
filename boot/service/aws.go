package service

import (
	"context"

	"github.com/ponyruntime/pony/api/boot"
	envapi "github.com/ponyruntime/pony/api/env"
	"github.com/ponyruntime/pony/api/event"
	logapi "github.com/ponyruntime/pony/api/logs"
	"github.com/ponyruntime/pony/api/payload"
	bootpkg "github.com/ponyruntime/pony/boot"
	"github.com/ponyruntime/pony/service/aws/config"
	"github.com/ponyruntime/pony/system/eventbus"
	reghandler "github.com/ponyruntime/pony/system/registry/events"
)

type awsConfigPlugin struct {
	handler eventbus.EventHandler
}

func (p *awsConfigPlugin) Name() string        { return bootpkg.AWSConfig }
func (p *awsConfigPlugin) Phase() boot.Phase   { return boot.PostInit }
func (p *awsConfigPlugin) DependsOn() []string { return []string{bootpkg.Environment} }

func (p *awsConfigPlugin) Load(ctx context.Context) (context.Context, error) {
	logger := logapi.GetLogger(ctx)
	dtt := payload.GetTranscoder(ctx)
	bus := event.GetBus(ctx)
	envRegistry := envapi.GetRegistry(ctx)

	manager := config.NewManager(
		bus,
		dtt,
		logger.Named("config.aws"),
		envRegistry,
	)

	p.handler = reghandler.NewRegistryHandler("config.aws", manager)
	return ctx, nil
}

func (p *awsConfigPlugin) Handler() eventbus.EventHandler {
	return p.handler
}

func init() {
	bootpkg.MustRegister(&awsConfigPlugin{})
}
