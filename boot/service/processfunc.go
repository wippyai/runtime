package service

import (
	"context"

	"github.com/ponyruntime/pony/api/boot"
	"github.com/ponyruntime/pony/api/event"
	logapi "github.com/ponyruntime/pony/api/logs"
	procapi "github.com/ponyruntime/pony/api/process"
	bootpkg "github.com/ponyruntime/pony/boot"
	"github.com/ponyruntime/pony/service/processfunc"
	"github.com/ponyruntime/pony/system/eventbus"
)

type processFuncBridgePlugin struct {
	handler eventbus.EventHandler
}

func (p *processFuncBridgePlugin) Name() string        { return "process_function_bridge" }
func (p *processFuncBridgePlugin) Phase() boot.Phase   { return boot.PostInit }
func (p *processFuncBridgePlugin) DependsOn() []string { return []string{bootpkg.Process} }

func (p *processFuncBridgePlugin) Load(ctx context.Context) (context.Context, error) {
	logger := logapi.GetLogger(ctx)
	bus := event.GetBus(ctx)
	processes := procapi.GetManager(ctx)

	p.handler = processfunc.WithProcessFunctionBridge(
		logger.Named("pfunc"),
		bus,
		processes,
	)

	return ctx, nil
}

func (p *processFuncBridgePlugin) Handler() eventbus.EventHandler {
	return p.handler
}

func init() {
	bootpkg.MustRegister(&processFuncBridgePlugin{})
}
