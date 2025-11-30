package lua

import (
	"context"

	"github.com/wippyai/runtime/api/boot"
	luatemplate "github.com/wippyai/runtime/runtime/lua/modules/template"
)

func Template() boot.Component {
	return boot.New(boot.P{
		Name:      LuaTemplateName,
		DependsOn: []boot.ComponentName{LuaEngineName},
		Load: func(ctx context.Context) (context.Context, error) {
			cm := GetCodeManager(ctx)
			if cm == nil {
				return ctx, nil
			}

			if err := AddModules(ctx, cm, luatemplate.Module); err != nil {
				return ctx, err
			}

			return ctx, nil
		},
	})
}
