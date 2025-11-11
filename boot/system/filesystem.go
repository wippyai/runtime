package system

import (
	"context"

	"github.com/ponyruntime/pony/api/boot"
	"github.com/ponyruntime/pony/api/event"
	fsapi "github.com/ponyruntime/pony/api/fs"
	logapi "github.com/ponyruntime/pony/api/logs"
	bootpkg "github.com/ponyruntime/pony/boot"
	"github.com/ponyruntime/pony/system/fs"
)

type filesystemPlugin struct{}

func (p *filesystemPlugin) Name() string        { return bootpkg.Filesystem }
func (p *filesystemPlugin) Phase() boot.Phase   { return boot.Init }
func (p *filesystemPlugin) DependsOn() []string { return []string{bootpkg.EventBus, bootpkg.Logger} }

func (p *filesystemPlugin) Load(ctx context.Context) (context.Context, error) {
	logger := logapi.GetLogger(ctx)
	bus := event.GetBus(ctx)

	fsRegistry := fs.NewFSRegistry(bus, logger.Named("fs"))
	return fsapi.WithRegistry(ctx, fsRegistry), nil
}

func (p *filesystemPlugin) Start(ctx context.Context) error {
	fsRegistry := fsapi.GetRegistry(ctx)
	return fsRegistry.Start(ctx)
}

func (p *filesystemPlugin) Stop(ctx context.Context) error {
	fsRegistry := fsapi.GetRegistry(ctx)
	return fsRegistry.Stop()
}

func init() {
	bootpkg.MustRegister(&filesystemPlugin{})
}
