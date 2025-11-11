package service

import (
	"context"

	"github.com/ponyruntime/pony/api/boot"
	"github.com/ponyruntime/pony/api/event"
	logapi "github.com/ponyruntime/pony/api/logs"
	"github.com/ponyruntime/pony/api/payload"
	bootpkg "github.com/ponyruntime/pony/boot"
	"github.com/ponyruntime/pony/service/terminal"
	"github.com/ponyruntime/pony/system/eventbus"
	reghandler "github.com/ponyruntime/pony/system/registry/events"
)

type terminalPlugin struct {
	handler eventbus.EventHandler
}

func (p *terminalPlugin) Name() string        { return bootpkg.Terminal }
func (p *terminalPlugin) Phase() boot.Phase   { return boot.PostInit }
func (p *terminalPlugin) DependsOn() []string { return nil }

func (p *terminalPlugin) Load(ctx context.Context) (context.Context, error) {
	logger := logapi.GetLogger(ctx)
	dtt := payload.GetTranscoder(ctx)
	bus := event.GetBus(ctx)

	manager := terminal.NewTerminalManager(
		bus,
		dtt,
		logger.Named("terminal"),
	)

	p.handler = reghandler.NewRegistryHandler("terminal.host", manager)
	return ctx, nil
}

func (p *terminalPlugin) Handler() eventbus.EventHandler {
	return p.handler
}

func init() {
	bootpkg.MustRegister(&terminalPlugin{})
}
