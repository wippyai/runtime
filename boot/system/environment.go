package system

import (
	"context"

	"github.com/ponyruntime/pony/api/boot"
	envapi "github.com/ponyruntime/pony/api/env"
	"github.com/ponyruntime/pony/api/event"
	logapi "github.com/ponyruntime/pony/api/logs"
	bootpkg "github.com/ponyruntime/pony/boot"
	"github.com/ponyruntime/pony/system/env"
)

type environmentPlugin struct{}

func (p *environmentPlugin) Name() string        { return bootpkg.Environment }
func (p *environmentPlugin) Phase() boot.Phase   { return boot.Init }
func (p *environmentPlugin) DependsOn() []string { return []string{bootpkg.EventBus, bootpkg.Logger} }

func (p *environmentPlugin) Load(ctx context.Context) (context.Context, error) {
	logger := logapi.GetLogger(ctx)
	bus := event.GetBus(ctx)

	envRegistry := env.NewRegistry(bus, logger.Named("env"))
	return envapi.WithRegistry(ctx, envRegistry), nil
}

func (p *environmentPlugin) Start(ctx context.Context) error {
	envRegistry := envapi.GetRegistry(ctx)
	return envRegistry.Start(ctx)
}

func (p *environmentPlugin) Stop(ctx context.Context) error {
	envRegistry := envapi.GetRegistry(ctx)
	return envRegistry.Stop()
}

func init() {
	bootpkg.MustRegister(&environmentPlugin{})
}
