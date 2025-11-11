package service

import (
	"context"

	"github.com/ponyruntime/pony/api/boot"
	"github.com/ponyruntime/pony/api/event"
	logapi "github.com/ponyruntime/pony/api/logs"
	"github.com/ponyruntime/pony/api/payload"
	bootpkg "github.com/ponyruntime/pony/boot"
	prochost "github.com/ponyruntime/pony/service/host"
	"github.com/ponyruntime/pony/system/eventbus"
	reghandler "github.com/ponyruntime/pony/system/registry/events"
)

type ephemeralHostPlugin struct {
	handler eventbus.EventHandler
}

func (p *ephemeralHostPlugin) Name() string        { return bootpkg.EphemeralHost }
func (p *ephemeralHostPlugin) Phase() boot.Phase   { return boot.PostInit }
func (p *ephemeralHostPlugin) DependsOn() []string { return nil }

func (p *ephemeralHostPlugin) Load(ctx context.Context) (context.Context, error) {
	logger := logapi.GetLogger(ctx)
	dtt := payload.GetTranscoder(ctx)
	bus := event.GetBus(ctx)

	manager := prochost.NewHostManager(
		bus,
		dtt,
		logger.Named("hosts"),
	)

	p.handler = reghandler.NewRegistryHandler("process.host", manager)
	return ctx, nil
}

func (p *ephemeralHostPlugin) Handler() eventbus.EventHandler {
	return p.handler
}

func init() {
	bootpkg.MustRegister(&ephemeralHostPlugin{})
}
