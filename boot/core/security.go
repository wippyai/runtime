package core

import (
	"context"

	"github.com/ponyruntime/pony/api/boot"
	"github.com/ponyruntime/pony/api/event"
	logapi "github.com/ponyruntime/pony/api/logs"
	secapi "github.com/ponyruntime/pony/api/security"
	"github.com/ponyruntime/pony/system/security"
)

func Security() boot.Plugin {
	var policyRegistry *security.PolicyRegistry

	return boot.New(boot.P{
		Name:      SecurityName,
		Phase:     boot.PreInit,
		DependsOn: []string{LoggerName, EventBusName},
		Load: func(ctx context.Context) (context.Context, error) {
			logger := logapi.GetLogger(ctx)
			bus := event.GetBus(ctx)

			policyRegistry = security.NewPolicyRegistry(bus, logger.Named("security"))
			return secapi.WithRegistry(ctx, policyRegistry), nil
		},
		Start: func(ctx context.Context) error {
			if policyRegistry != nil {
				return policyRegistry.Start(ctx)
			}
			return nil
		},
		Stop: func(ctx context.Context) error {
			if policyRegistry != nil {
				return policyRegistry.Stop()
			}
			return nil
		},
	})
}
