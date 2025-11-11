package service

import (
	"context"

	"github.com/ponyruntime/pony/api/boot"
	"github.com/ponyruntime/pony/api/event"
	logapi "github.com/ponyruntime/pony/api/logs"
	"github.com/ponyruntime/pony/api/payload"
	bootpkg "github.com/ponyruntime/pony/boot"
	native "github.com/ponyruntime/pony/service/exec"
	"github.com/ponyruntime/pony/system/eventbus"
	reghandler "github.com/ponyruntime/pony/system/registry/events"
)

type nativeExecPlugin struct {
	handler eventbus.EventHandler
}

func (p *nativeExecPlugin) Name() string        { return bootpkg.NativeExec }
func (p *nativeExecPlugin) Phase() boot.Phase   { return boot.PostInit }
func (p *nativeExecPlugin) DependsOn() []string { return nil }

func (p *nativeExecPlugin) Load(ctx context.Context) (context.Context, error) {
	logger := logapi.GetLogger(ctx)
	dtt := payload.GetTranscoder(ctx)
	bus := event.GetBus(ctx)

	manager := native.NewManager(
		bus,
		dtt,
		logger.Named("exec"),
	)

	p.handler = reghandler.NewRegistryHandler("exec.native", manager)
	return ctx, nil
}

func (p *nativeExecPlugin) Handler() eventbus.EventHandler {
	return p.handler
}

func init() {
	bootpkg.MustRegister(&nativeExecPlugin{})
}
