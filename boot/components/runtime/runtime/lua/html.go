//go:build plugin_lua_html

package lua

import (
	"context"

	"github.com/wippyai/runtime/api/boot"
	bootpkg "github.com/wippyai/runtime/boot"
	"github.com/wippyai/runtime/runtime/lua/modules/html"
)

func LuaHTML() boot.Component {
	return boot.New(boot.P{
		Name:      bootpkg.LuaHTML,
		Phase:     boot.PostInit,
		DependsOn: []boot.ComponentName{LuaEngineName},
		Load: func(ctx context.Context) (context.Context, error) {
			codeManager := GetCodeManager(ctx)

			if err := AddModules(ctx, codeManager,
				html.NewHTMLModule(),
			); err != nil {
				return ctx, err
			}

			return ctx, nil
		},
	})
}
