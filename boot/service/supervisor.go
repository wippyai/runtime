package service

import (
	"context"

	"github.com/ponyruntime/pony/api/boot"
	"github.com/ponyruntime/pony/api/event"
	logapi "github.com/ponyruntime/pony/api/logs"
	procapi "github.com/ponyruntime/pony/api/process"
	bootpkg "github.com/ponyruntime/pony/boot"
	service "github.com/ponyruntime/pony/service/supervisor"
	"github.com/ponyruntime/pony/system/eventbus"
	reghandler "github.com/ponyruntime/pony/system/registry/events"
)

type processSupervisorPlugin struct {
	handler eventbus.EventHandler
}

func (p *processSupervisorPlugin) Name() string        { return bootpkg.ProcessSupervisor }
func (p *processSupervisorPlugin) Phase() boot.Phase   { return boot.PostInit }
func (p *processSupervisorPlugin) DependsOn() []string { return []string{bootpkg.Process} }

func (p *processSupervisorPlugin) Load(ctx context.Context) (context.Context, error) {
	logger := logapi.GetLogger(ctx)
	bus := event.GetBus(ctx)
	processes := procapi.GetManager(ctx)

	manager := service.NewSupervisorServiceManager(
		bus,
		processes,
		logger.Named("super"),
	)

	p.handler = reghandler.NewRegistryHandler("process.service", manager)
	return ctx, nil
}

func (p *processSupervisorPlugin) Handler() eventbus.EventHandler {
	return p.handler
}

func init() {
	bootpkg.MustRegister(&processSupervisorPlugin{})
}
