package lua

import (
	"context"

	"github.com/wippyai/runtime/api/boot"
	"github.com/wippyai/runtime/runtime/lua/modules/html"
)

func HTML() boot.Component {
	return boot.New(boot.P{
		Name:      LuaHTMLName,
		DependsOn: []boot.Name{LuaEngineName},
		Load: func(ctx context.Context) (context.Context, error) {
			cm := GetCodeManager(ctx)
			if cm == nil {
				return ctx, nil
			}

			if err := AddModules(ctx, cm,
				html.Module,
			); err != nil {
				return ctx, err
			}

			return ctx, nil
		},
	})
}
