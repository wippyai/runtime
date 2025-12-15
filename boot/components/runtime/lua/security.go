package lua

import (
	"context"

	"github.com/wippyai/runtime/api/boot"
	securitymod "github.com/wippyai/runtime/runtime/lua/modules/security"
)

func Security() boot.Component {
	return boot.New(boot.P{
		Name:      SecurityName,
		DependsOn: []boot.Name{EngineName},
		Load: func(ctx context.Context) (context.Context, error) {
			cm := GetCodeManager(ctx)
			if cm == nil {
				return ctx, nil
			}

			if err := AddModules(ctx, cm, securitymod.Module); err != nil {
				return ctx, err
			}

			return ctx, nil
		},
	})
}
