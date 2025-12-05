package core

import (
	"context"

	"github.com/wippyai/runtime/api/boot"
	"github.com/wippyai/runtime/api/event"
	logapi "github.com/wippyai/runtime/api/logs"
	"github.com/wippyai/runtime/api/payload"
	policyapi "github.com/wippyai/runtime/api/service/security/policy"
	bootpkg "github.com/wippyai/runtime/boot"
	"github.com/wippyai/runtime/service/security/policy"
)

func SecurityPolicy() boot.Component {
	return boot.New(boot.P{
		Name:      SecurityPolicyName,
		DependsOn: []boot.Name{SecurityName},
		Load: func(ctx context.Context) (context.Context, error) {
			logger := logapi.GetLogger(ctx)
			dtt := payload.GetTranscoder(ctx)
			bus := event.GetBus(ctx)
			handlers := bootpkg.GetHandlerRegistry(ctx)

			factory := policy.NewDefaultFactory(dtt)
			manager := policy.NewManager(bus, factory, logger.Named("security.policy"))

			handlers.RegisterListener(string(policyapi.Kind), manager)
			handlers.RegisterListener(string(policyapi.ExprKind), manager)

			return ctx, nil
		},
	})
}
