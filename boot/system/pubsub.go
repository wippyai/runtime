package system

import (
	"context"
	"fmt"

	"github.com/ponyruntime/pony/api/boot"
	"github.com/ponyruntime/pony/api/event"
	funcapi "github.com/ponyruntime/pony/api/function"
	logapi "github.com/ponyruntime/pony/api/logs"
	pubsubapi "github.com/ponyruntime/pony/api/pubsub"
	topapi "github.com/ponyruntime/pony/api/topology"
	bootpkg "github.com/ponyruntime/pony/boot"
	"github.com/ponyruntime/pony/system/pubsub"
)

type pubsubPlugin struct{}

func (p *pubsubPlugin) Name() string        { return bootpkg.PubSub }
func (p *pubsubPlugin) Phase() boot.Phase   { return boot.Init }
func (p *pubsubPlugin) DependsOn() []string { return []string{bootpkg.EventBus, bootpkg.Logger} }

func (p *pubsubPlugin) Load(ctx context.Context) (context.Context, error) {
	logger := logapi.GetLogger(ctx)
	bus := event.GetBus(ctx)

	localNode := pubsub.NewNode("local")

	nodeManager := pubsub.NewNodeManager(
		localNode,
		bus,
		logger.Named("pubsub"),
	)
	router := pubsub.NewRouter(localNode, nil)

	err := localNode.RegisterHost(topapi.ControlHost, pubsub.NewHost(ctx, pubsub.HostConfig{
		BufferSize:  1024,
		WorkerCount: 16,
		Logger:      logger.Named("control"),
	}))
	if err != nil {
		return ctx, fmt.Errorf("failed to register control host: %w", err)
	}

	funcHost := pubsub.NewHost(ctx, pubsub.HostConfig{
		BufferSize:  1024,
		WorkerCount: 16,
		Logger:      logger.Named("functions"),
	})
	err = localNode.RegisterHost(funcapi.HostID, funcHost)
	if err != nil {
		return ctx, fmt.Errorf("failed to register function host: %w", err)
	}

	ctx = pubsubapi.WithNode(ctx, localNode)
	ctx = pubsubapi.WithRouter(ctx, router)
	ctx = pubsubapi.WithNodeManager(ctx, nodeManager)

	return ctx, nil
}

func (p *pubsubPlugin) Start(ctx context.Context) error {
	nodeManager := pubsubapi.GetNodeManager(ctx)
	return nodeManager.Start(ctx)
}

func (p *pubsubPlugin) Stop(ctx context.Context) error {
	nodeManager := pubsubapi.GetNodeManager(ctx)
	return nodeManager.Stop()
}

func init() {
	bootpkg.MustRegister(&pubsubPlugin{})
}
