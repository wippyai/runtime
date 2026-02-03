package lua

import (
	"context"

	"github.com/wippyai/runtime/api/boot"
	timemod "github.com/wippyai/runtime/runtime/lua/modules/time"
)

func Time() boot.Component {
	return boot.New(boot.P{
		Name:      TimeName,
		DependsOn: []boot.Name{EngineName},
		Load: func(ctx context.Context) (context.Context, error) {
			cm := GetCodeManager(ctx)
			if cm == nil {
				return ctx, nil
			}

			if err := AddModules(ctx, cm, timemod.Module); err != nil {
				return ctx, err
			}

			return ctx, nil
		},
	})
}
