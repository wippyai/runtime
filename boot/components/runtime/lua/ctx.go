package lua

import (
	"context"

	"github.com/wippyai/runtime/api/boot"
	ctxmod "github.com/wippyai/runtime/runtime/lua/modules/ctx"
)

func Ctx() boot.Component {
	return boot.New(boot.P{
		Name:      ContextName,
		DependsOn: []boot.Name{EngineName},
		Load: func(ctx context.Context) (context.Context, error) {
			cm := GetCodeManager(ctx)
			if cm == nil {
				return ctx, nil
			}

			if err := AddModules(ctx, cm, ctxmod.Module); err != nil {
				return ctx, err
			}

			return ctx, nil
		},
	})
}
