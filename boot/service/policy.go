package service

import (
	"context"

	"github.com/ponyruntime/pony/api/boot"
	"github.com/ponyruntime/pony/api/event"
	logapi "github.com/ponyruntime/pony/api/logs"
	"github.com/ponyruntime/pony/api/payload"
	bootpkg "github.com/ponyruntime/pony/boot"
	"github.com/ponyruntime/pony/service/policy"
	"github.com/ponyruntime/pony/system/eventbus"
	reghandler "github.com/ponyruntime/pony/system/registry/events"
)

type yamlPolicyPlugin struct {
	handler eventbus.EventHandler
}

func (p *yamlPolicyPlugin) Name() string        { return bootpkg.YAMLPolicy }
func (p *yamlPolicyPlugin) Phase() boot.Phase   { return boot.PostInit }
func (p *yamlPolicyPlugin) DependsOn() []string { return nil }

func (p *yamlPolicyPlugin) Load(ctx context.Context) (context.Context, error) {
	logger := logapi.GetLogger(ctx)
	dtt := payload.GetTranscoder(ctx)
	bus := event.GetBus(ctx)

	manager := policy.NewManager(
		bus,
		policy.NewDefaultFactory(dtt),
		logger.Named("policy"),
	)

	p.handler = reghandler.NewRegistryHandler("security.policy", manager)
	return ctx, nil
}

func (p *yamlPolicyPlugin) Handler() eventbus.EventHandler {
	return p.handler
}

func init() {
	bootpkg.MustRegister(&yamlPolicyPlugin{})
}
