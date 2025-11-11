package service

import (
	"context"

	"github.com/ponyruntime/pony/api/boot"
	"github.com/ponyruntime/pony/api/event"
	logapi "github.com/ponyruntime/pony/api/logs"
	"github.com/ponyruntime/pony/api/payload"
	bootpkg "github.com/ponyruntime/pony/boot"
	fsdir "github.com/ponyruntime/pony/service/directory"
	"github.com/ponyruntime/pony/system/eventbus"
	reghandler "github.com/ponyruntime/pony/system/registry/events"
)

type directoryPlugin struct {
	handler eventbus.EventHandler
}

func (p *directoryPlugin) Name() string        { return "directory" }
func (p *directoryPlugin) Phase() boot.Phase   { return boot.PostInit }
func (p *directoryPlugin) DependsOn() []string { return nil }

func (p *directoryPlugin) Load(ctx context.Context) (context.Context, error) {
	logger := logapi.GetLogger(ctx)
	dtt := payload.GetTranscoder(ctx)
	bus := event.GetBus(ctx)

	manager := fsdir.NewDirectoryManager(
		bus,
		dtt,
		nil,
		logger.Named("fs.dir"),
	)

	p.handler = reghandler.NewRegistryHandler("fs.directory", manager)
	return ctx, nil
}

func (p *directoryPlugin) Handler() eventbus.EventHandler {
	return p.handler
}

func init() {
	bootpkg.MustRegister(&directoryPlugin{})
}
