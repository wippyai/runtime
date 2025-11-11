package service

import (
	"context"

	"github.com/ponyruntime/pony/api/boot"
	"github.com/ponyruntime/pony/api/event"
	logapi "github.com/ponyruntime/pony/api/logs"
	"github.com/ponyruntime/pony/api/payload"
	bootpkg "github.com/ponyruntime/pony/boot"
	"github.com/ponyruntime/pony/service/template"
	"github.com/ponyruntime/pony/system/eventbus"
	reghandler "github.com/ponyruntime/pony/system/registry/events"
)

type templatePlugin struct {
	handler eventbus.EventHandler
}

func (p *templatePlugin) Name() string        { return bootpkg.Template }
func (p *templatePlugin) Phase() boot.Phase   { return boot.PostInit }
func (p *templatePlugin) DependsOn() []string { return nil }

func (p *templatePlugin) Load(ctx context.Context) (context.Context, error) {
	logger := logapi.GetLogger(ctx)
	dtt := payload.GetTranscoder(ctx)
	bus := event.GetBus(ctx)

	manager := template.NewManager(
		bus,
		dtt,
		logger.Named("tmpl"),
	)

	p.handler = reghandler.NewRegistryHandler("template.(jet|set)", manager)
	return ctx, nil
}

func (p *templatePlugin) Handler() eventbus.EventHandler {
	return p.handler
}

func init() {
	bootpkg.MustRegister(&templatePlugin{})
}
