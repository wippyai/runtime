package system

import (
	"context"

	"github.com/ponyruntime/pony/api/boot"
	"github.com/ponyruntime/pony/api/event"
	logapi "github.com/ponyruntime/pony/api/logs"
	procapi "github.com/ponyruntime/pony/api/process"
	pubsubapi "github.com/ponyruntime/pony/api/pubsub"
	topapi "github.com/ponyruntime/pony/api/topology"
	bootpkg "github.com/ponyruntime/pony/boot"
	"github.com/ponyruntime/pony/system/process"
)

type processPlugin struct{}

func (p *processPlugin) Name() string      { return bootpkg.Process }
func (p *processPlugin) Phase() boot.Phase { return boot.Init }
func (p *processPlugin) DependsOn() []string {
	return []string{bootpkg.EventBus, bootpkg.Logger, bootpkg.Topology, bootpkg.PubSub}
}

func (p *processPlugin) Load(ctx context.Context) (context.Context, error) {
	logger := logapi.GetLogger(ctx)
	bus := event.GetBus(ctx)
	node := pubsubapi.GetNode(ctx)
	topo := topapi.GetTopology(ctx)

	prototypes := process.NewPrototypeFactory(bus, logger.Named("prototypes"))
	hosts := process.NewHostRegistry(bus, logger.Named("hosts"))

	processes := process.NewProcessManager(
		hosts,
		prototypes,
		node.ID(),
		logger.Named("processes"),
	)

	ctx = procapi.WithManager(ctx, processes)
	ctx = procapi.WithPrototypes(ctx, prototypes)
	ctx = procapi.WithHosts(ctx, hosts)

	topo.SetPrototypeRegistry(prototypes)

	return ctx, nil
}

func (p *processPlugin) Start(ctx context.Context) error {
	prototypes := procapi.GetPrototypes(ctx)
	if err := prototypes.Start(ctx); err != nil {
		return err
	}

	hosts := procapi.GetHosts(ctx)
	return hosts.Start(ctx)
}

func (p *processPlugin) Stop(ctx context.Context) error {
	hosts := procapi.GetHosts(ctx)
	if err := hosts.Stop(); err != nil {
		return err
	}

	prototypes := procapi.GetPrototypes(ctx)
	return prototypes.Stop()
}

func init() {
	bootpkg.MustRegister(&processPlugin{})
}
