package system

import (
	"context"

	"github.com/ponyruntime/pony/api/boot"
	logapi "github.com/ponyruntime/pony/api/logs"
	pubsubapi "github.com/ponyruntime/pony/api/pubsub"
	topapi "github.com/ponyruntime/pony/api/topology"
	bootpkg "github.com/ponyruntime/pony/boot"
	"github.com/ponyruntime/pony/system/topology"
)

type topologyPlugin struct{}

func (p *topologyPlugin) Name() string        { return bootpkg.Topology }
func (p *topologyPlugin) Phase() boot.Phase   { return boot.Init }
func (p *topologyPlugin) DependsOn() []string { return []string{bootpkg.Logger, bootpkg.PubSub} }

func (p *topologyPlugin) Load(ctx context.Context) (context.Context, error) {
	logger := logapi.GetLogger(ctx)
	node := pubsubapi.GetNode(ctx)

	topo := topology.NewTopology(node)
	pidReg := topology.NewPIDRegistry(topology.PIDRegistryConfig{
		Parent: nil,
		Logger: logger.Named("pid"),
	})

	ctx = topapi.WithTopology(ctx, topo)
	ctx = topapi.WithRegistry(ctx, pidReg)

	return ctx, nil
}

func init() {
	bootpkg.MustRegister(&topologyPlugin{})
}
