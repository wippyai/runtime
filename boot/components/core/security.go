package core

import (
	"context"

	"github.com/wippyai/runtime/api/boot"
	"github.com/wippyai/runtime/api/event"
	logapi "github.com/wippyai/runtime/api/logs"
	secapi "github.com/wippyai/runtime/api/security"
	"github.com/wippyai/runtime/system/security"
)

func Security() boot.Component {
	var policyRegistry *security.PolicyRegistry

	return boot.New(boot.P{
		Name:      SecurityName,
		Phase:     boot.PreInit,
		DependsOn: []boot.ComponentName{},
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
		Stop: func(_ context.Context) error {
			if policyRegistry != nil {
				return policyRegistry.Stop()
			}
			return nil
		},
	})
}
