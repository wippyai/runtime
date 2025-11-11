package system

import (
	"context"

	"github.com/ponyruntime/pony/api/boot"
	"github.com/ponyruntime/pony/api/event"
	logapi "github.com/ponyruntime/pony/api/logs"
	resourceapi "github.com/ponyruntime/pony/api/resource"
	bootpkg "github.com/ponyruntime/pony/boot"
	"github.com/ponyruntime/pony/system/resource"
)

type resourcesPlugin struct{}

func (p *resourcesPlugin) Name() string        { return bootpkg.Resources }
func (p *resourcesPlugin) Phase() boot.Phase   { return boot.Init }
func (p *resourcesPlugin) DependsOn() []string { return []string{bootpkg.EventBus, bootpkg.Logger} }

func (p *resourcesPlugin) Load(ctx context.Context) (context.Context, error) {
	logger := logapi.GetLogger(ctx)
	bus := event.GetBus(ctx)

	resources := resource.NewResourceRegistry(bus, logger.Named("resources"))
	return resourceapi.WithRegistry(ctx, resources), nil
}

func (p *resourcesPlugin) Start(ctx context.Context) error {
	resources := resourceapi.GetRegistry(ctx)
	return resources.Start(ctx)
}

func (p *resourcesPlugin) Stop(ctx context.Context) error {
	resources := resourceapi.GetRegistry(ctx)
	return resources.Stop()
}

func init() {
	bootpkg.MustRegister(&resourcesPlugin{})
}
